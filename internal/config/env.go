package config

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
)

// envRefPattern matches ${NAME} and ${NAME:-default}. The bare $NAME form is
// deliberately not supported so values such as bcrypt hashes stay untouched.
var envRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// ExpandEnv replaces ${NAME} and ${NAME:-default} references in every string
// reachable from v (struct fields, slices, maps and pointers) with values from
// the environment. v must be a pointer. A reference to an unset variable
// without a default is an error naming the variable and the config field, so
// a missing secret fails loudly instead of becoming an empty string.
func ExpandEnv(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("ExpandEnv needs a non-nil pointer, got %T", v)
	}
	var missing []string
	expandValue(rv.Elem(), "", &missing)
	if len(missing) > 0 {
		return fmt.Errorf("environment variables not set: %s", strings.Join(missing, ", "))
	}
	return nil
}

func expandValue(v reflect.Value, path string, missing *[]string) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			expandValue(v.Elem(), path, missing)
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).IsExported() {
				expandValue(v.Field(i), joinPath(path, t.Field(i).Name), missing)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			expandValue(v.Index(i), fmt.Sprintf("%s[%d]", path, i), missing)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			// Map elements are not addressable: expand a copy and store it back.
			elem := reflect.New(v.Type().Elem()).Elem()
			elem.Set(v.MapIndex(k))
			expandValue(elem, fmt.Sprintf("%s[%v]", path, k), missing)
			v.SetMapIndex(k, elem)
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString(expandString(v.String(), path, missing))
		}
	}
}

func expandString(s, path string, missing *[]string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return envRefPattern.ReplaceAllStringFunc(s, func(ref string) string {
		// Group 1 is the name, group 2 the default; index -1 means no default.
		idx := envRefPattern.FindStringSubmatchIndex(ref)
		name := ref[idx[2]:idx[3]]
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		if idx[4] >= 0 {
			return ref[idx[4]:idx[5]]
		}
		*missing = append(*missing, fmt.Sprintf("%s (%s)", name, path))
		return ref
	})
}

func joinPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}
