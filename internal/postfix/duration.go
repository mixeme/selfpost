package postfix

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ParseTime reads a Postfix time value as Postfix 3.7.11 reads it:
// src/global/conv_time.c, conv_time(), called by get_mail_conf_time() in
// src/global/mail_conf_time.c. One number - strtol(), so leading blanks and a
// "+" are taken, a negative number or one past an int is invalid - followed
// by one unit, w d h m or s in lower case; with no unit, defUnit, which
// Postfix takes from the parameter's default value (get_def_time_unit(): "5d"
// gives d, "300s" s, "0h" h). Whatever follows the unit is ignored, so "1h7m"
// is one hour (assumptions B-PQ10, B-PQ11). A string with no digits is read as
// 0 of the unit it starts with, as strtol() leaves it there.
func ParseTime(s string, defUnit byte) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("postfix: empty time value")
	}
	i := 0
	for i < len(s) && isCSpace(s[i]) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	start := i
	var n int64
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int64(s[i]-'0')
		if n > math.MaxInt32 {
			return 0, fmt.Errorf("postfix: time value %q out of range", s)
		}
		i++
	}
	end := i
	if i == start {
		// strtol() converted nothing: the end is the start of the string.
		end, n, neg = 0, 0, false
	}
	if neg && n > 0 {
		return 0, fmt.Errorf("postfix: negative time value %q", s)
	}
	unit := defUnit
	if end < len(s) {
		unit = s[end]
	}
	var per int64
	switch unit {
	case 'w':
		per = 7 * 24 * 3600
	case 'd':
		per = 24 * 3600
	case 'h':
		per = 3600
	case 'm':
		per = 60
	case 's':
		per = 1
	default:
		return 0, fmt.Errorf("postfix: invalid time value %q", s)
	}
	// conv_time(): "if (intval < INT_MAX / WEEK)" and the like.
	if per > 1 && n >= math.MaxInt32/per {
		return 0, fmt.Errorf("postfix: time value %q out of range", s)
	}
	return time.Duration(n*per) * time.Second, nil
}

// isCSpace is C's isspace() in the "C" locale, which strtol() skips.
func isCSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
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
