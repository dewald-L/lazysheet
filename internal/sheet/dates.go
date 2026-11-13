package sheet

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Dates are calendar days, held as midnight UTC so arithmetic never trips over
// time zones or DST.

// Date builds a calendar day.
func Date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// Today is the local calendar day.
func Today() time.Time {
	now := time.Now()
	return Date(now.Year(), now.Month(), now.Day())
}

// Weekday is Python's date.weekday(): Monday 0 … Sunday 6.
func Weekday(d time.Time) int { return (int(d.Weekday()) + 6) % 7 }

// Monday is the Monday of d's week.
func Monday(d time.Time) time.Time { return d.AddDate(0, 0, -Weekday(d)) }

// WeekOf is the Monday-to-Sunday week d is in (osheet's week_of).
func WeekOf(d time.Time) []time.Time {
	mon := Monday(d)
	out := make([]time.Time, 7)
	for i := range out {
		out[i] = mon.AddDate(0, 0, i)
	}
	return out
}

// MonthRange is the first and last day of d's month.
func MonthRange(d time.Time) (time.Time, time.Time) {
	first := Date(d.Year(), d.Month(), 1)
	return first, first.AddDate(0, 1, -1)
}

// DaysBetween lists start..end inclusive.
func DaysBetween(start, end time.Time) []time.Time {
	var out []time.Time
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		out = append(out, d)
	}
	return out
}

// GroupLabel is the file name (without .xlsx) for d's period: "2026-11" by
// month, else "09-15_November" for the week (named after its Monday's month).
func GroupLabel(groupBy string, d time.Time) string {
	if groupBy == "month" {
		return d.Format("2006-01")
	}
	mon := Monday(d)
	sun := mon.AddDate(0, 0, 6)
	return fmt.Sprintf("%02d-%02d_%s", mon.Day(), sun.Day(), mon.Month())
}

// GroupedPath is <folder of base>/<yyyy_Month>/<period>.xlsx; a week is filed
// under the month of its Monday.
func GroupedPath(base, groupBy string, d time.Time) string {
	start := d
	if groupBy == "week" {
		start = Monday(d)
	}
	folder := fmt.Sprintf("%d_%s", start.Year(), start.Month())
	return filepath.Join(filepath.Dir(base), folder, GroupLabel(groupBy, d)+".xlsx")
}

// OutputPath is the workbook an entry dated d goes into. OSHEET_FILE
// overrides the grouping.
func (c *Config) OutputPath(d time.Time) string {
	if f := os.Getenv("OSHEET_FILE"); f != "" {
		return absPath(f)
	}
	base := absPath(c.Output)
	if c.GroupBy == "" {
		return base
	}
	return GroupedPath(base, c.GroupBy, d)
}

// --- durations ---------------------------------------------------------------

var (
	reClock    = regexp.MustCompile(`^(\d+):(\d{1,2})$`)
	reHoursMin = regexp.MustCompile(`^(?:(\d+(?:\.\d+)?)h)?(?:(\d+)m?)?$`)
	reDecimal  = regexp.MustCompile(`^(\d*\.\d+)$`)
)

// ParseDuration returns minutes. It accepts 1:30, 1h30, 1h 30m, 90m, 1.5h,
// 1.5 (hours) and 90 (minutes when over 24, else hours), like osheet.
func ParseDuration(text string) (int, bool) {
	t := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(text)), " ", "")
	if m := reClock.FindStringSubmatch(t); m != nil {
		h, err1 := strconv.Atoi(m[1])
		mm, err2 := strconv.Atoi(m[2])
		if err1 != nil || err2 != nil {
			return 0, false
		}
		return h*60 + mm, true
	}
	if m := reHoursMin.FindStringSubmatchIndex(t); m != nil {
		hasH, hasM := m[2] >= 0, m[4] >= 0
		if !hasH && !hasM {
			return 0, false
		}
		mins := 0
		if hasM {
			n, err := strconv.Atoi(t[m[4]:m[5]])
			if err != nil {
				return 0, false
			}
			mins = n
		}
		if hasH {
			f, err := strconv.ParseFloat(t[m[2]:m[3]], 64)
			if err != nil {
				return 0, false
			}
			return int(math.RoundToEven(f*60)) + mins, true
		}
		if strings.HasSuffix(t, "m") || mins > 24 {
			return mins, true
		}
		return mins * 60, true
	}
	if m := reDecimal.FindStringSubmatch(t); m != nil {
		f, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, false
		}
		return int(math.RoundToEven(f * 60)), true
	}
	return 0, false
}

// CheckDuration validates a typed duration (1 minute to 24 hours).
func CheckDuration(raw string) (int, error) {
	if raw != "" {
		if m, ok := ParseDuration(raw); ok && m > 0 && m <= 24*60 {
			return m, nil
		}
	}
	return 0, fmt.Errorf("Couldn't read that as a duration between 1 minute and 24 hours, try again.")
}

// RoundUp15 rounds minutes up to the next quarter hour.
func RoundUp15(m int) int { return (m + 14) / 15 * 15 }

// CheckDescription requires a description of at most 255 characters.
func CheckDescription(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("A description is required.")
	}
	if n := len([]rune(raw)); n > 255 {
		return "", fmt.Errorf("Too long (%d characters), keep it to 255.", n)
	}
	return raw, nil
}

// FmtMinutes formats minutes as 2h05.
func FmtMinutes(total int) string { return fmt.Sprintf("%dh%02d", total/60, total%60) }
