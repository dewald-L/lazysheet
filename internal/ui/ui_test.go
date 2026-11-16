package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/dewald-L/dock/plugin"

	"github.com/dewald-L/lazysheet/internal/sheet"
)

func testConfig() *sheet.Config {
	return &sheet.Config{
		Output:       "/nonexistent/timesheets/TimesheetUpload.xlsx",
		GroupBy:      "week",
		Defaults:     sheet.Defaults{Project: "Acme", Category: "Dev", Location: "Home", Sentiment: "Neutral"},
		AskEveryTime: map[string]any{"sentiment": false},
		Projects: []sheet.Project{
			{Name: "Acme", Billable: "Yes", Categories: []string{"Dev", "Meetings"}},
			{Name: "Training", Billable: "No", Categories: []string{"Self-study", "DOJO Attendance", "External Conference Presentation"}},
		},
		Locations:  []string{"Home", "Office"},
		Sentiments: []string{"Neutral", "Happy", "Sad"},
	}
}

func entry(d time.Time, row, mins int, project, cat, desc string) sheet.Entry {
	return sheet.Entry{Date: d, Hours: mins / 60, Minutes: mins % 60, Row: row, Path: "/nonexistent/x.xlsx",
		Fields: map[string]string{"Project": project, "Category": cat, "Description": desc, "Billable": "Yes", "WorkedFrom": "Home", "Sentiment": "Neutral"}}
}

// newTestApp is an App loaded with a config and entries, without touching
// any files.
func newTestApp(t *testing.T, embedded bool) *App {
	t.Helper()
	a := New(plugin.DefaultTheme(), embedded)
	today := sheet.Today()
	mon := sheet.Monday(today)
	entries := []sheet.Entry{
		entry(mon, 1, 300, "Acme", "Dev", "Standup, refinement and a long planning session that ran over time and covered far too many topics to fit on one line"),
		entry(mon, 2, 210, "Training", "Self-study", "Go generics"),
		entry(today, 3, 75, "Acme", "Meetings", "lazysheet"),
	}
	a.loadGen = 1
	a.Update(loadedMsg{gen: 1, cfg: testConfig(), today: today, entries: entries, folder: "/nonexistent/timesheets/x"})
	a.Update(plugin.FocusMsg{})
	return a
}

func checkFits(t *testing.T, a *App, w, h int, what string) {
	t.Helper()
	a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	lines := strings.Split(a.View(), "\n")
	if len(lines) != h {
		t.Errorf("%s at %dx%d: %d lines", what, w, h, len(lines))
	}
	for i, l := range lines {
		if n := ansi.StringWidth(l); n != w {
			t.Errorf("%s at %dx%d: line %d is %d wide: %q", what, w, h, i, n, ansi.Strip(l))
			return
		}
	}
}

func key(a *App, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "shift+tab":
			msg = tea.KeyMsg{Type: tea.KeyShiftTab}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		a.Update(msg)
	}
}

func TestViewFitsEverySize(t *testing.T) {
	for _, embedded := range []bool{true, false} {
		a := newTestApp(t, embedded)
		sizes := [][2]int{{90, 24}, {100, 30}, {120, 40}, {200, 60}}
		for _, s := range sizes {
			checkFits(t, a, s[0], s[1], "browse")
		}
		for p := panelDays; p < panelCount; p++ {
			a.focus = p
			checkFits(t, a, 90, 24, "panel "+panelNames[p])
		}
		key(a, "2", "d")
		checkFits(t, a, 90, 24, "confirm delete")
		key(a, "n") // keeps it
		key(a, "?")
		checkFits(t, a, 120, 40, "help")
		key(a, "x")
		for _, p := range []string{"t", "w", "m"} {
			key(a, p)
			checkFits(t, a, 90, 24, "hours "+p)
		}
		key(a, "n")
		checkFits(t, a, 90, 24, "form: day")
		key(a, "enter", "2")
		checkFits(t, a, 90, 24, "form: category")
		key(a, "enter")
		checkFits(t, a, 90, 24, "form: time")
		key(a, "esc")
	}
}

func TestFormFlow(t *testing.T) {
	a := newTestApp(t, true)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	key(a, "n")
	f := a.form
	if f == nil || f.step != fDay || f.options[f.cursor] != a.today.Format("2006-01-02") {
		t.Fatalf("new form should ask the day with today selected: %+v", f)
	}
	key(a, "1") // Monday
	if !f.date.Equal(sheet.Monday(a.today)) || f.step != fProject {
		t.Fatalf("day: %v step %d", f.date, f.step)
	}
	if f.options[f.cursor] != "Acme" {
		t.Errorf("project should default to Acme, got %s", f.options[f.cursor])
	}
	key(a, "2") // Training
	key(a, "j", "enter")
	if f.category != "DOJO Attendance" || f.step != fTime {
		t.Fatalf("category %q step %d", f.category, f.step)
	}
	key(a, "q", "?", "enter") // typed into the input: not a duration
	if f.err == "" || a.form == nil {
		t.Fatal("q? should be typed into the time input and rejected")
	}
	a.form.input.SetValue("1h50")
	key(a, "enter")
	if f.hours != 2 || f.mins != 0 || !strings.Contains(f.values[fTime].note, "rounded up from 1h50") {
		t.Errorf("time %d:%d %q", f.hours, f.mins, f.values[fTime].note)
	}
	key(a, "2") // Office
	if f.step != fDescription || f.sentiment != "Neutral" || f.values[fSentiment].note != skippedNote {
		t.Fatalf("sentiment should be skipped with its default: step %d %+v", f.step, f.values[fSentiment])
	}
	key(a, "shift+tab")
	if f.step != fLocation {
		t.Errorf("back from description should skip the sentiment and reach location, got %d", f.step)
	}
	key(a, "enter")
	if f.step != fDescription {
		t.Fatalf("step %d", f.step)
	}
	key(a, "enter")
	if f.err != "A description is required." {
		t.Errorf("empty description: %q", f.err)
	}
	ans := f.answers()
	if ans.Project != "Training" || ans.Location != "Office" || ans.Hours != 2 {
		t.Errorf("answers %+v", ans)
	}
	key(a, "esc")
	if a.form != nil {
		t.Error("esc should cancel the form")
	}
}

func TestSkippedQuestionsAndPrev(t *testing.T) {
	a := newTestApp(t, true)
	a.cfg.AskEveryTime = map[string]any{"day": false, "project": false, "category": false, "time": false, "location": true}
	a.cfg.Defaults.Time = "45m"
	key(a, "n")
	f := a.form
	if f.step != fLocation {
		t.Fatalf("first asked question should be location, got %d", f.step)
	}
	if !f.date.Equal(a.today) || f.project != "Acme" || f.category != "Dev" || f.hours != 0 || f.mins != 45 {
		t.Errorf("defaults: %+v", f.answers())
	}
	key(a, "esc")

	// a default category that isn't in the project's list is asked anyway
	a.cfg.Defaults.Category = "Nope"
	key(a, "n")
	if a.form.step != fCategory {
		t.Errorf("invalid default category should be asked, step %d", a.form.step)
	}
	key(a, "esc")

	// the next entry starts from the previous one
	a.cfg.AskEveryTime = map[string]any{}
	prev := sheet.Answers{Date: sheet.Monday(a.today), Project: "Training", Category: "Self-study", Location: "Office", Sentiment: "Sad"}
	a.prev = &prev
	key(a, "n")
	f = a.form
	if f.options[f.cursor] != prev.Date.Format("2006-01-02") {
		t.Errorf("day should start on the previous entry's day")
	}
	key(a, "enter")
	if f.options[f.cursor] != "Training" {
		t.Errorf("project should start on the previous entry's, got %s", f.options[f.cursor])
	}
	key(a, "esc")

	// a day picked in Days wins over the previous entry's day
	key(a, "1", "g", "j", "j", "n")
	if got, want := a.form.options[a.form.cursor], sheet.Monday(a.today).AddDate(0, 0, 2).Format("2006-01-02"); got != want {
		t.Errorf("n on a picked day: %s, want %s", got, want)
	}
}

func TestEditPrefillsAndAsksEverything(t *testing.T) {
	a := newTestApp(t, true)
	key(a, "2", "G") // oldest entry this week: Monday's first row
	e := a.selectedEntry()
	if e == nil || e.Row != 1 {
		t.Fatalf("selected %+v", e)
	}
	key(a, "e")
	f := a.form
	if f.edit == nil || len(f.days) != 7 || !f.days[0].Equal(sheet.Monday(e.Date)) {
		t.Fatal("edit form should offer the entry's week")
	}
	for _, k := range sheet.AskFields {
		if !f.asking[k] {
			t.Errorf("edit should ask %s", k)
		}
	}
	key(a, "enter", "enter", "enter")
	if f.step != fTime || f.input.Value() != "5h00" {
		t.Errorf("time prefilled %q (step %d)", f.input.Value(), f.step)
	}
	key(a, "enter", "enter", "enter")
	if f.step != fDescription || !strings.HasPrefix(f.input.Value(), "Standup") {
		t.Errorf("description prefilled %q", f.input.Value())
	}
}

func TestWeekLabels(t *testing.T) {
	if got := weekTitle(sheet.Date(2026, 11, 13)); got != "09-15 Nov" {
		t.Errorf("weekTitle %q", got)
	}
	if got := weekTitle(sheet.Date(2026, 11, 1)); got != "26 Oct-01 Nov" {
		t.Errorf("weekTitle across months %q", got)
	}
	if got := weekLabel(sheet.Date(2026, 10, 26)); got != "26 Oct–01 Nov" {
		t.Errorf("weekLabel %q", got)
	}
}

func TestWeekPagingAndRightPane(t *testing.T) {
	a := newTestApp(t, true)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	cur := a.weekIdx
	key(a, "[")
	if a.weekIdx != cur-1 && cur > 0 {
		t.Errorf("[ should page to the previous week: %d -> %d", cur, a.weekIdx)
	}
	key(a, "]", "]", "]")
	if a.weekIdx != len(a.weeks())-1 {
		t.Errorf("] should stop at the last week: %d of %d", a.weekIdx, len(a.weeks()))
	}
	if strings.Contains(ansi.Strip(a.View()), "Weeks") {
		t.Error("there is no Weeks panel any more")
	}
	key(a, "t")
	if v := ansi.Strip(a.View()); !a.showHours || !strings.Contains(v, "[3] Hours") {
		t.Error("t should show Hours in the right pane")
	}
	key(a, "2", "j")
	if a.showHours {
		t.Error("moving in Entries should show the entry again")
	}
	key(a, "w", "3", "esc")
	if a.showHours || a.focus != panelEntries {
		t.Error("esc in Hours should go back to the entry")
	}
}
