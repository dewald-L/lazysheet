// Package ui is lazysheet's Bubble Tea app: a lazygit-style view of osheet's
// timesheets with an inline entry form. All file access goes through the
// sheet package, from commands (never from Update or View).
package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dewald-L/dock/plugin"

	"github.com/dewald-L/lazysheet/internal/sheet"
)

type panelID int

const (
	panelDays panelID = iota
	panelEntries
	panelPane // the right pane: the selected entry, or the Hours table
	panelCount
)

var panelNames = [panelCount]string{"Days", "Entries", "Entry"}

// Hours summary periods.
const (
	periodDay   = "day"
	periodWeek  = "week"
	periodMonth = "month"
)

var periods = []string{periodDay, periodWeek, periodMonth}

// App is the top-level model.
type App struct {
	st       styles
	embedded bool
	focused  bool

	width, height int

	// loaded state (from loadedMsg)
	loaded       bool
	opened       bool // the first load has selected the current week
	cfg          *sheet.Config
	cfgErr       error
	today        time.Time
	entries      []sheet.Entry // reading order, the month plus the current week
	newest       []sheet.Entry // the same, newest first
	folder       string        // the current period's timesheet folder
	folderExists bool
	loadGen      int

	focus     panelID
	dayIdx    int  // 0 = Monday of the current week
	dayPicked bool // the user moved in Days since the last new entry
	weekIdx   int  // index into weeks(): the week shown in Days and Entries
	entryIdx  int  // index into the selected week's entries
	sumScroll int
	period    string
	showHours bool // the right pane shows the Hours table instead of the entry

	form          *entryForm
	confirmDelete bool
	showHelp      bool
	busy          bool // a save or delete is running

	prev      *sheet.Answers // the last new entry, to start the next one from
	selectRow *rowKey        // entry to select after the next load

	status, errMsg string
	title          string
}

type rowKey struct {
	path string
	row  int
}

// New builds the app. embedded hides lazysheet's own status and help bars
// (dock shows them).
func New(th plugin.Theme, embedded bool) *App {
	a := &App{st: newStyles(th), embedded: embedded, period: periodWeek, today: sheet.Today()}
	a.dayIdx = sheet.Weekday(a.today)
	return a
}

func (a *App) Init() tea.Cmd { return tea.Batch(a.load(), tick()) }

// --- loading -------------------------------------------------------------------

type loadedMsg struct {
	gen          int
	cfg          *sheet.Config
	cfgErr       error
	today        time.Time
	entries      []sheet.Entry
	err          error
	folder       string
	folderExists bool
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Minute, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// loadRange is the days lazysheet reads: every week that overlaps the month,
// through the end of the month and of the current week.
func loadRange(today time.Time) (time.Time, time.Time) {
	first, last := sheet.MonthRange(today)
	end := sheet.Monday(today).AddDate(0, 0, 6)
	if last.After(end) {
		end = last
	}
	return sheet.Monday(first), end
}

func (a *App) load() tea.Cmd {
	a.loadGen++
	gen := a.loadGen
	return func() tea.Msg {
		msg := loadedMsg{gen: gen, today: sheet.Today()}
		cfg, err := sheet.LoadConfig()
		if err != nil {
			msg.cfgErr = err
			return msg
		}
		msg.cfg = cfg
		from, to := loadRange(msg.today)
		msg.entries, msg.err = cfg.EntriesBetween(from, to)
		msg.folder = filepath.Dir(cfg.OutputPath(msg.today))
		if fi, err := os.Stat(msg.folder); err == nil && fi.IsDir() {
			msg.folderExists = true
		}
		return msg
	}
}

func (a *App) applyLoaded(msg loadedMsg) {
	if msg.gen != a.loadGen {
		return // a newer load is on its way
	}
	var keep *sheet.Entry
	if e := a.selectedEntry(); e != nil {
		keep = e
	}
	dayChanged := !msg.today.Equal(a.today)
	a.loaded = true
	a.cfg, a.cfgErr = msg.cfg, msg.cfgErr
	a.today = msg.today
	a.entries = msg.entries
	a.newest = sheet.NewestFirst(msg.entries)
	a.folder, a.folderExists = msg.folder, msg.folderExists
	if msg.err != nil {
		a.errMsg = msg.err.Error()
	}
	weeks := a.weeks()
	if dayChanged {
		a.dayIdx = sheet.Weekday(a.today)
		a.weekIdx = a.currentWeekIdx()
	}
	if !a.opened { // the first load opens the current week
		a.weekIdx = a.currentWeekIdx()
		a.opened = true
	}
	a.weekIdx = clampIdx(a.weekIdx, len(weeks))
	switch {
	case a.selectRow != nil:
		a.selectByKey(*a.selectRow)
		a.selectRow = nil
	case keep != nil:
		a.selectByKey(rowKey{keep.Path, keep.Row})
	}
	a.entryIdx = clampIdx(a.entryIdx, len(a.weekEntries()))
}

// --- derived data ----------------------------------------------------------------

// weeks lists the Mondays of the weeks you can page through with [ and ]: every week
// that overlaps the month, up to the current week (and later ones that have
// entries).
func (a *App) weeks() []time.Time {
	first, last := sheet.MonthRange(a.today)
	end := sheet.Monday(a.today)
	for _, e := range a.entries {
		if m := sheet.Monday(e.Date); m.After(end) && !e.Date.After(last) {
			end = m
		}
	}
	var out []time.Time
	for m := sheet.Monday(first); !m.After(end); m = m.AddDate(0, 0, 7) {
		out = append(out, m)
	}
	return out
}

func (a *App) currentWeekIdx() int {
	cur := sheet.Monday(a.today)
	for i, m := range a.weeks() {
		if m.Equal(cur) {
			return i
		}
	}
	return 0
}

func (a *App) selectedWeek() time.Time {
	weeks := a.weeks()
	if len(weeks) == 0 {
		return sheet.Monday(a.today)
	}
	return weeks[clampIdx(a.weekIdx, len(weeks))]
}

// weekEntries are the selected week's entries, newest first.
func (a *App) weekEntries() []sheet.Entry {
	return entriesInWeek(a.newest, a.selectedWeek())
}

func entriesInWeek(entries []sheet.Entry, monday time.Time) []sheet.Entry {
	var out []sheet.Entry
	for _, e := range entries {
		if sheet.Monday(e.Date).Equal(monday) {
			out = append(out, e)
		}
	}
	return out
}

func (a *App) selectedEntry() *sheet.Entry {
	es := a.weekEntries()
	if a.entryIdx < 0 || a.entryIdx >= len(es) {
		return nil
	}
	e := es[a.entryIdx]
	return &e
}

func (a *App) selectByKey(k rowKey) {
	for _, e := range a.newest {
		if e.Path == k.path && e.Row == k.row {
			weeks := a.weeks()
			for i, m := range weeks {
				if m.Equal(sheet.Monday(e.Date)) {
					a.weekIdx = i
				}
			}
			for i, we := range a.weekEntries() {
				if we.Path == k.path && we.Row == k.row {
					a.entryIdx = i
				}
			}
			return
		}
	}
}

// minutesByDay totals the loaded entries per date, leaving out skip (the
// entry being edited).
func (a *App) minutesByDay(skip *sheet.Entry) map[time.Time]int {
	out := map[time.Time]int{}
	for _, e := range a.entries {
		if skip != nil && e.Path == skip.Path && e.Row == skip.Row {
			continue
		}
		out[e.Date] += e.Total()
	}
	return out
}

// shownWeek is the week Days lists: the selected one.
func (a *App) shownWeek() []time.Time { return sheet.WeekOf(a.selectedWeek()) }

// setWeek pages to week i (clamped), keeping the day's weekday.
func (a *App) setWeek(i int) {
	i = clampIdx(i, len(a.weeks()))
	if i != a.weekIdx {
		a.weekIdx, a.entryIdx = i, 0
	}
}

// periodRange is the hours summary's date range and heading.
func (a *App) periodRange() (time.Time, time.Time, string, string) {
	t := a.today
	switch a.period {
	case periodDay:
		return t, t, "Today", t.Format("Mon 02 Jan")
	case periodMonth:
		first, last := sheet.MonthRange(t)
		return first, last, "This month", t.Format("January 2006")
	}
	mon := sheet.Monday(t)
	sun := mon.AddDate(0, 0, 6)
	return mon, sun, "This week", weekLabel(mon)
}

// weekTitle is the tab title's week, e.g. "09-15 Nov" or "26 Oct-01 Nov".
func weekTitle(today time.Time) string {
	mon := sheet.Monday(today)
	sun := mon.AddDate(0, 0, 6)
	if mon.Month() == sun.Month() {
		return fmt.Sprintf("%02d-%02d %s", mon.Day(), sun.Day(), sun.Format("Jan"))
	}
	return fmt.Sprintf("%s-%s", mon.Format("02 Jan"), sun.Format("02 Jan"))
}

// weekLabel is a week's date range, e.g. "09–15 Nov".
func weekLabel(mon time.Time) string {
	sun := mon.AddDate(0, 0, 6)
	if mon.Month() == sun.Month() {
		return fmt.Sprintf("%02d–%s", mon.Day(), sun.Format("02 Jan"))
	}
	return mon.Format("02 Jan") + "–" + sun.Format("02 Jan")
}

func clampIdx(i, n int) int {
	if i >= n {
		i = n - 1
	}
	if i < 0 {
		i = 0
	}
	return i
}

func entriesText(n int) string {
	if n == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", n)
}

func entrySummary(date time.Time, project, category string, minutes int, location string) string {
	return fmt.Sprintf("%s · %s / %s · %s · %s", date.Format("Mon 02 Jan"), project, category,
		sheet.FmtMinutes(minutes), location)
}

// sameEntry reports whether two reads of a row hold the same values.
func sameEntry(a, b sheet.Entry) bool { return a.Row == b.Row && reflect.DeepEqual(a.Fields, b.Fields) }
