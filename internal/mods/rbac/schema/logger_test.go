package schema

import (
	"testing"
	"time"
)

func TestParseTimeRange(t *testing.T) {
	local := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ok := []struct {
		start, end         string
		wantStart, wantEnd *time.Time
	}{
		{"", "", nil, nil},
		{"2026-10-01 08:30:00", "", new(local("2026-10-01 08:30:00")), nil},
		{"", "2026-10-02", nil, new(local("2026-10-02 00:00:00"))},
		{"2026-10-01T00:00:00Z", "2026-10-02T00:00:00+08:00", new(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)), nil},
	}
	for _, tt := range ok {
		p := LoggerQueryParam{StartTime: tt.start, EndTime: tt.end}
		if err := p.ParseTimeRange(); err != nil {
			t.Errorf("%q..%q: unexpected error %v", tt.start, tt.end, err)
			continue
		}
		if !sameTime(p.StartAt, tt.wantStart) || (tt.wantEnd != nil && !sameTime(p.EndAt, tt.wantEnd)) || (tt.end == "" && p.EndAt != nil) {
			t.Errorf("%q..%q: got %v..%v", tt.start, tt.end, p.StartAt, p.EndAt)
		}
	}

	for _, bad := range [][2]string{
		{"yesterday", ""},
		{"", "2026-13-01"},
		{"2026-10-01'; DROP TABLE logger; --", ""},
		{"2026-10-02", "2026-10-01"}, // start after end
	} {
		p := LoggerQueryParam{StartTime: bad[0], EndTime: bad[1]}
		if err := p.ParseTimeRange(); err == nil {
			t.Errorf("%q..%q should be rejected", bad[0], bad[1])
		}
	}
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
