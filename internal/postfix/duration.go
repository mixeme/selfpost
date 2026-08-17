package postfix

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ParseDuration converts a Postfix time value to a duration. Units are those
// postconf prints and accepts: s, m, h, d, w. A bare number is seconds. Values
// may concatenate units (`1h7m`), matching Postfix's own conv_time.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("postfix: empty time value")
	}
	var total time.Duration
	i := 0
	for i < len(s) {
		for i < len(s) && unicode.IsSpace(rune(s[i])) {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] == '-' {
			return 0, fmt.Errorf("postfix: negative time value %q", s)
		}
		if s[i] == '+' {
			i++
		}
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return 0, fmt.Errorf("postfix: invalid time value %q", s)
		}
		n, err := strconv.ParseInt(s[start:i], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("postfix: invalid time value %q", s)
		}
		unit := byte('s')
		if i < len(s) && isTimeUnit(s[i]) {
			unit = s[i] | 0x20 // ASCII fold to lowercase
			i++
		}
		part, err := durationForUnit(n, unit)
		if err != nil {
			return 0, err
		}
		total += part
	}
	return total, nil
}

func isTimeUnit(c byte) bool {
	switch c | 0x20 {
	case 's', 'm', 'h', 'd', 'w':
		return true
	}
	return false
}

func durationForUnit(n int64, unit byte) (time.Duration, error) {
	var unitDur time.Duration
	switch unit {
	case 's':
		unitDur = time.Second
	case 'm':
		unitDur = time.Minute
	case 'h':
		unitDur = time.Hour
	case 'd':
		unitDur = 24 * time.Hour
	case 'w':
		unitDur = 7 * 24 * time.Hour
	default:
		return 0, fmt.Errorf("postfix: unknown time unit %q", string(unit))
	}
	return time.Duration(n) * unitDur, nil
}

// FormatDuration renders a Postfix interval the way the Mail queue card and
// delivery history share it, so the two cannot drift. Exact day/hour/minute/
// second values stay exact (`5 minutes`, `5 days`, `1 hour`); a remainder that
// is rounded to the nearest minute is marked `about` (`about 1 hour 7 minutes`
// for the stock 4000s backoff cap).
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	sec := int64(d / time.Second)
	if sec == 0 {
		return "0 seconds"
	}

	days := sec / 86400
	rem := sec % 86400
	hours := rem / 3600
	rem %= 3600
	minutes := rem / 60
	seconds := rem % 60

	about := false
	if days > 0 || hours > 0 {
		if seconds >= 30 {
			minutes++
			about = true
		} else if seconds > 0 {
			about = true
		}
		seconds = 0
		if minutes >= 60 {
			hours++
			minutes = 0
		}
		if hours >= 24 {
			days++
			hours = 0
		}
	}

	parts := make([]string, 0, 4)
	if days > 0 {
		parts = append(parts, counted(days, "day"))
	}
	if hours > 0 {
		parts = append(parts, counted(hours, "hour"))
	}
	if minutes > 0 {
		parts = append(parts, counted(minutes, "minute"))
	}
	if seconds > 0 {
		parts = append(parts, counted(seconds, "second"))
	}
	s := strings.Join(parts, " ")
	if about {
		return "about " + s
	}
	return s
}

func counted(n int64, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.FormatInt(n, 10) + " " + unit + "s"
}
