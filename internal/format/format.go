// Package format holds the human-readable formatting helpers shared by the
// TUI and the text report.
package format

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Size formats bytes with decimal units (1 GB = 10^9 bytes), matching Finder.
func Size(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	units := []string{"kB", "MB", "GB", "TB", "PB"}
	v := float64(b) / unit
	i := 0
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	switch {
	case v >= 100:
		return fmt.Sprintf("%.0f %s", v, units[i])
	case v >= 10:
		return fmt.Sprintf("%.1f %s", v, units[i])
	default:
		return fmt.Sprintf("%.2f %s", v, units[i])
	}
}

// ParseSize parses sizes like "500MB", "2G", "1.5gb", "750k" or "1024".
// Units are decimal, as in Size.
func ParseSize(s string) (int64, error) {
	t := strings.TrimSpace(strings.ToUpper(s))
	t = strings.TrimSuffix(t, "B")
	mult := 1.0
	if n := len(t); n > 0 {
		switch t[n-1] {
		case 'K':
			mult = 1e3
		case 'M':
			mult = 1e6
		case 'G':
			mult = 1e9
		case 'T':
			mult = 1e12
		}
		if mult != 1 {
			t = t[:n-1]
		}
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return int64(v * mult), nil
}

// Age formats how long ago t was, e.g. "today", "5d", "3w", "4mo", "2y 3mo".
func Age(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	days := int(d.Hours() / 24)
	switch {
	case days < 1:
		return "today"
	case days < 14:
		return fmt.Sprintf("%dd", days)
	case days < 60:
		return fmt.Sprintf("%dw", days/7)
	case days < 365:
		return fmt.Sprintf("%dmo", days/30)
	}
	years, months := days/365, min((days%365)/30, 11)
	if months == 0 {
		return fmt.Sprintf("%dy", years)
	}
	return fmt.Sprintf("%dy %dmo", years, months)
}

// Count formats an integer with thousands separators.
func Count(n int64) string {
	s := strconv.FormatInt(n, 10)
	if n < 0 {
		return "-" + Count(-n)
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// ShortPath replaces the home directory prefix with "~".
func ShortPath(p, home string) string {
	if home == "" || home == "/" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

// TruncLeft shortens s to at most w cells by cutting from the left, so the
// end of a path (the interesting part) stays visible.
func TruncLeft(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw <= w {
		return s
	}
	if w <= 1 {
		return ansi.Truncate(s, w, "")
	}
	return ansi.TruncateLeft(s, sw-w+1, "…")
}

// Trunc shortens s to at most w cells by cutting from the right.
func Trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// PadRight pads s with spaces to exactly w cells (truncating if needed).
func PadRight(s string, w int) string {
	s = Trunc(s, w)
	if n := w - ansi.StringWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

// PadLeft right-aligns s in w cells (truncating if needed).
func PadLeft(s string, w int) string {
	s = Trunc(s, w)
	if n := w - ansi.StringWidth(s); n > 0 {
		s = strings.Repeat(" ", n) + s
	}
	return s
}
