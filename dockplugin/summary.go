package dockplugin

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dewald-L/dock/plugin"

	"github.com/dewald-L/lazysheet/internal/sheet"
)

const (
	dayTarget  = 8 * 60  // minutes; today past this is a warning
	weekTarget = 40 * 60 // minutes
)

// Summary implements plugin.Summarizer for dock's home screen: today's and
// this week's hours plus a bar per day. It only reads osheet's config and
// this week's files, so it is safe to call while lazysheet is open.
func (Plugin) Summary(plugin.Context) (plugin.Summary, error) {
	return summary(sheet.Today())
}

func summary(today time.Time) (plugin.Summary, error) {
	cfg, err := sheet.LoadConfig()
	if errors.Is(err, sheet.ErrNoConfig) {
		return plugin.Summary{Lines: []string{"No osheet config: run osheet --setup"}}, nil
	}
	if err != nil {
		return plugin.Summary{}, err
	}
	week := sheet.WeekOf(today)
	entries, err := cfg.EntriesBetween(week[0], week[6])
	if err != nil {
		return plugin.Summary{}, err
	}
	var days [7]int
	for _, e := range entries {
		days[sheet.Weekday(e.Date)] += e.Total()
	}
	total := 0
	for _, m := range days {
		total += m
	}
	todayMin := days[sheet.Weekday(today)]
	s := plugin.Summary{Lines: []string{
		fmt.Sprintf("Today %s · this week %s / %dh", sheet.FmtMinutes(todayMin), sheet.FmtMinutes(total), weekTarget/60),
		dayBar(days),
	}}
	if todayMin > dayTarget {
		s.Level = plugin.LevelWarn
	}
	return s, nil
}

var dayNames = [7]string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}

// dayBar draws one block per day, its height in hours up to 8h:
// "Mo█ Tu▆ We▃ Th· Fr· Sa· Su·". A day with nothing logged is "·".
func dayBar(days [7]int) string {
	const levels = "▁▂▃▄▅▆▇█"
	bars := []rune(levels)
	parts := make([]string, 7)
	for i, m := range days {
		g := "·"
		if m > 0 {
			n := min((m*len(bars)+dayTarget-1)/dayTarget, len(bars)) // ceil, 1h per step
			g = string(bars[n-1])
		}
		parts[i] = dayNames[i] + g
	}
	return strings.Join(parts, " ")
}
