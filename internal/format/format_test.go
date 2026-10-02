package format

import (
	"testing"
	"time"
)

func TestSize(t *testing.T) {
	tests := map[int64]string{0: "0 B", 999: "999 B", 1_000: "1.00 kB", 52_100_000_000: "52.1 GB", 412_300_000_000: "412 GB"}
	for in, want := range tests {
		if got := Size(in); got != want {
			t.Errorf("Size(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSize(t *testing.T) {
	tests := map[string]int64{"500MB": 500_000_000, "2G": 2_000_000_000, "1.5gb": 1_500_000_000, "750k": 750_000, "1024": 1024}
	for in, want := range tests {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := ParseSize("lots"); err == nil {
		t.Error("want error for garbage")
	}
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	days := func(d int) time.Time { return now.Add(-time.Duration(d) * 24 * time.Hour) }
	tests := []struct {
		t    time.Time
		want string
	}{
		{time.Time{}, "-"},
		{now, "today"},
		{days(5), "5d"},
		{days(21), "3w"},
		{days(120), "4mo"},
		{days(365 + 362), "1y 11mo"},
		{days(3 * 365), "3y"},
	}
	for _, tt := range tests {
		if got := Age(tt.t, now); got != tt.want {
			t.Errorf("Age(%v) = %q, want %q", tt.t, got, tt.want)
		}
	}
}

func TestPaths(t *testing.T) {
	if got := ShortPath("/Users/me/x", "/Users/me"); got != "~/x" {
		t.Errorf("ShortPath = %q", got)
	}
	if got := ShortPath("/Users/meme", "/Users/me"); got != "/Users/meme" {
		t.Errorf("ShortPath must match whole components, got %q", got)
	}
	if got := TruncLeft("/a/b/c/file.log", 10); got != "…/file.log" {
		t.Errorf("TruncLeft = %q", got)
	}
}
