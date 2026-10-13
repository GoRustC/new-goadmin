package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/GoRustC/new-goadmin/pkg/encoding/json"
	"github.com/GoRustC/new-goadmin/pkg/encoding/toml"
	"github.com/GoRustC/new-goadmin/pkg/errors"
	"github.com/creasty/defaults"
)

var (
	once sync.Once
	C    = new(Config)
)

func MustLoad(dir string, names ...string) {
	once.Do(func() {
		if err := Load(dir, names...); err != nil {
			panic(err)
		}
	})
}

// Loads configuration files in various formats from a directory and parses them into
// a struct.
func Load(dir string, names ...string) error {
	// Set default values
	if err := defaults.Set(C); err != nil {
		return err
	}

	supportExts := []string{".json", ".toml"}
	parseFile := func(name string) error {
		ext := filepath.Ext(name)
		if ext == "" || !strings.Contains(strings.Join(supportExts, ","), ext) {
			return nil
		}

		buf, err := os.ReadFile(name)
		if err != nil {
			return errors.Wrapf(err, "failed to read config file %s", name)
		}

		switch ext {
		case ".json":
			err = json.Unmarshal(buf, C)
		case ".toml":
			err = toml.Unmarshal(buf, C)
		}
		return errors.Wrapf(err, "failed to unmarshal config %s", name)
	}

	for _, name := range names {
		fullname := filepath.Join(dir, name)
		info, err := os.Stat(fullname)
		if err != nil {
			return errors.Wrapf(err, "failed to get config file %s", name)
		}

		if info.IsDir() {
			err := filepath.WalkDir(fullname, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				} else if d.IsDir() {
					return nil
				}
				return parseFile(path)
			})
			if err != nil {
				return errors.Wrapf(err, "failed to walk config dir %s", name)
			}
			continue
		}
		if err := parseFile(fullname); err != nil {
			return err
		}
	}

	// Expand environment references in the parsed values, not in the file
	// text: comments are ignored and values containing quotes or backslashes
	// cannot break the TOML or JSON syntax.
	if err := ExpandEnv(C); err != nil {
		return errors.Wrap(err, "failed to expand environment variables in config")
	}
	return nil
}
