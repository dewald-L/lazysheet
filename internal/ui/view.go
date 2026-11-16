package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dewald-L/lazysheet/internal/sheet"
)

const leftMax = 40 // the left column's width

func (a *App) bodyHeight() int {
	if a.embedded {
		return a.height
	}
	return a.height - 2 // status and help bars
}

func (a *App) leftWidth() int  { return min(leftMax, a.width/2) }
func (a *App) rightWidth() int { return a.width - a.leftWidth() }

func (a *App) View() string {
	if a.width <= 0 || a.height <= 0 {
		return ""
	}
	var view string
	switch {
	case a.showHelp:
		view = lipgloss.Place(a.width, a.bodyHeight(), lipgloss.Center, lipgloss.Center, a.renderHelp())
	case !a.loaded:
		view = lipgloss.Place(a.width, a.bodyHeight(), lipgloss.Center, lipgloss.Center, a.st.muted.Render("loading…"))
	case a.cfg == nil:
		view = lipgloss.Place(a.width, a.bodyHeight(), lipgloss.Center, lipgloss.Center, a.renderSetupNeeded())
	default:
		view = a.renderPanels()
	}
	if !a.embedded {
		view += "\n" + a.renderStatusBar() + "\n" + a.renderHelpBar()
	}
	return view
}

func (a *App) renderPanels() string {
	h := a.bodyHeight()
	lw, rw := a.leftWidth(), a.rightWidth()

	daysH := min(9, h-4)
	left := strings.Join([]string{
		a.renderDays(lw, daysH),
		a.renderEntries(lw, h-daysH),
	}, "\n")

	var right string
	switch {
	case a.form != nil:
		right = a.renderForm(rw, h)
	case a.showHours:
		right = a.renderHours(rw, h)
	default:
		right = a.renderDetail(rw, h)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (a *App) panelTitle(id panelID, extra string) string {
	t := fmt.Sprintf("[%d] %s", id+1, panelNames[id])
	if extra != "" {
		t += " · " + extra
	}
	return " " + t + " "
}

// --- left column -------------------------------------------------------------------

func (a *App) renderDays(w, h int) string {
	focused := a.focus == panelDays && a.form == nil
	mins := a.minutesByDay(nil)
	week := a.shownWeek()
	total := 0
	inner := w - 4
	cells := min(barCells, inner-19)
	var lines []string
	for i, d := range week {
		m := mins[d]
		total += m
		hours := ""
		if m > 0 {
			hours = sheet.FmtMinutes(m)
		}
		text := fmt.Sprintf("%s  %5s", d.Format("Mon 02 Jan"), hours)
		st := a.st.text
		if d.Equal(a.today) {
			st = a.st.today
		}
		if i == a.dayIdx && focused {
			st = st.Inherit(a.st.cursor)
		}
		line := st.Render(text)
		if cells >= 4 {
			line += "  " + a.st.dayBar(m, cells)
		}
		lines = append(lines, line)
	}
	return a.st.box(a.panelTitle(panelDays, weekLabel(week[0])+" · "+sheet.FmtMinutes(total)), lines, w, h, focused)
}

func (a *App) renderEntries(w, h int) string {
	focused := a.focus == panelEntries && a.form == nil
	es := a.weekEntries()
	var lines []string
	if len(es) == 0 {
		lines = []string{a.st.muted.Render("No entries this week."), a.st.muted.Render("Press n to log one.")}
	}
	inner := w - 4
	for i, e := range es {
		prefix := fmt.Sprintf("%s %5s  ", e.Date.Format("Mon 02"), sheet.FmtMinutes(e.Total()))
		desc := ansi.Truncate(e.Get("Description"), max(inner-len(prefix), 1), "…")
		var line string
		switch {
		case i == a.entryIdx && focused:
			line = a.st.cursor.Render(fit(prefix+desc, inner))
		case i == a.entryIdx:
			line = a.st.cursorDim.Render(prefix + desc)
		default:
			line = a.st.muted.Render(prefix) + desc
		}
		lines = append(lines, line)
	}
	if len(es) > 0 {
		lines = scrollWindow(lines, a.entryIdx, h-2)
	}
	return a.st.box(a.panelTitle(panelEntries, weekLabel(a.selectedWeek())), lines, w, h, focused)
}

// scrollWindow shows the part of lines around cursor that fits in n lines.
func scrollWindow(lines []string, cursor, n int) []string {
	if n <= 0 || len(lines) <= n {
		return lines
	}
	top := max(0, min(cursor-n/2, len(lines)-n))
	return lines[top : top+n]
}

// --- right column ------------------------------------------------------------------

const labelW = 11 // the widest field label, "Description"

// fieldLine renders "  Label        value  (note)", wrapping a long value
// under itself.
func (a *App) fieldLines(marker string, label string, text string, extra string, note string, width int, active bool) []string {
	lab := fmt.Sprintf("%-*s", labelW, label)
	if active {
		lab = a.st.accentBold.Render(lab)
		marker = a.st.accentBold.Render(marker)
	} else {
		lab = a.st.muted.Render(lab)
	}
	indent := ansi.StringWidth(marker) + labelW + 2
	var out []string
	if text == "" {
		return []string{marker + lab + "  " + a.st.muted.Render("—")}
	}
	parts := wrapText(text, max(width-indent, 10))
	pad := strings.Repeat(" ", indent)
	for i, p := range parts {
		v := a.st.bold.Render(p)
		if i == len(parts)-1 {
			v += extra
		}
		if i == 0 {
			out = append(out, marker+lab+"  "+v)
		} else {
			out = append(out, pad+v)
		}
	}
	if note != "" { // after the value, or on its own line when it doesn't fit
		last := len(out) - 1
		if ansi.StringWidth(out[last])+1+ansi.StringWidth(note) <= width {
			out[last] += " " + a.st.muted.Render(note)
		} else {
			out = append(out, pad+a.st.muted.Render(note))
		}
	}
	return out
}

func (a *App) renderDetail(w, h int) string {
	inner := w - 4
	var lines []string
	e := a.selectedEntry()
	if e == nil {
		lines = append(lines, a.st.muted.Render("No entry selected."), "",
			a.st.muted.Render("n  log a new entry"), a.st.muted.Render("?  all keys"))
	} else {
		for _, f := range []struct{ label, value string }{
			{"Day", e.Date.Format("Monday 02 Jan 2006")},
			{"Project", e.Get("Project")},
			{"Entry type", e.Get("Category")},
			{"Time", sheet.FmtMinutes(e.Total())},
			{"Location", e.Get("WorkedFrom")},
			{"Sentiment", e.Get("Sentiment")},
			{"Description", e.Get("Description")},
			{"Billable", e.Get("Billable")},
			{"Ticket", e.Get("TicketNumber")},
			{"File", sheet.TildePath(e.Path)},
		} {
			lines = append(lines, a.fieldLines("", f.label, f.value, "", "", inner, false)...)
		}
		if a.confirmDelete {
			lines = append(lines, "", a.st.err.Render("Delete this entry? (y/N)"),
				a.st.muted.Render("y deletes it, the rows below move up · any other key keeps it"))
		}
	}
	lines = append(lines, "", a.folderLine())
	return a.st.box(a.panelTitle(panelPane, ""), lines, w, h, a.focus == panelPane || a.confirmDelete)
}

func (a *App) folderLine() string {
	line := a.st.muted.Render(fmt.Sprintf("%-*s  ", labelW, "Folder")) + sheet.TildePath(a.folder)
	if !a.folderExists {
		line += a.st.muted.Render(" (not created yet)")
	}
	return line
}

func (a *App) renderForm(w, h int) string {
	f := a.form
	inner := w - 4
	var lines []string
	for i := 0; i < fieldCount; i++ {
		v := f.values[i]
		active := i == f.step
		marker := "  "
		if active {
			marker = "❯ "
		}
		extra := ""
		if cells := min(barCells, inner-2-labelW-2-ansi.StringWidth(v.text)-2); v.hasBar && cells >= 4 {
			extra = "  " + a.st.dayBar(v.dayMins, cells)
		}
		lines = append(lines, a.fieldLines(marker, fieldLabels[i], v.text, extra, v.note, inner, active)...)
	}
	lines = append(lines, "")
	if a.busy {
		lines = append(lines, a.st.muted.Render("Saving…"))
	} else if f.step < fieldCount {
		qs := wrapText("? "+f.question, inner)
		for i, l := range qs {
			q := a.st.question.Render(l)
			if i == len(qs)-1 && f.step == fDescription {
				n := descCount(f.input.Value())
				count := fmt.Sprintf("  %d/255", n)
				if n > 255 {
					q += a.st.err.Render(count)
				} else {
					q += a.st.muted.Render(count)
				}
			}
			lines = append(lines, q)
		}
		if f.isMenu {
			room := h - 2 - len(lines)
			lines = append(lines, a.menuLines(f, inner, max(room, 3))...)
		} else {
			lines = append(lines, a.st.accent.Render("  ❯ ")+f.input.View())
			if f.err != "" {
				for _, l := range wrapText(f.err, inner-2) {
					lines = append(lines, "  "+a.st.err.Render(l))
				}
			}
		}
	}
	if n := h - 2; len(lines) > n { // too short: keep the question in view
		lines = lines[len(lines)-n:]
	}
	title := " New entry "
	if f.edit != nil {
		title = " Edit entry "
	}
	return a.st.box(title, lines, w, h, true)
}

func (a *App) menuLines(f *entryForm, width, window int) []string {
	numW := len(strconv.Itoa(len(f.options)))
	labelWidth := 0
	for _, l := range f.labels {
		labelWidth = max(labelWidth, ansi.StringWidth(l))
	}
	var rows []string
	for i, label := range f.labels {
		text := fmt.Sprintf("%*d. %s", numW, i+1, label)
		if f.dayMenu != nil {
			text = fmt.Sprintf("%*d. %-*s", numW, i+1, labelWidth, label)
			hours := ""
			if f.dayMenu[i] > 0 {
				hours = sheet.FmtMinutes(f.dayMenu[i])
			}
			text += fmt.Sprintf("  %5s", hours)
		}
		var row string
		if i == f.cursor {
			row = a.st.accentBold.Render("❯ " + text)
		} else {
			row = "  " + text
		}
		if f.dayMenu != nil {
			if cells := min(barCells, width-ansi.StringWidth(row)-2); cells >= 4 {
				row += "  " + a.st.dayBar(f.dayMenu[i], cells)
			}
		}
		rows = append(rows, row)
	}
	rows = scrollWindow(rows, f.cursor, window)
	if f.typed != "" {
		rows = append(rows, a.st.muted.Render("  number: ")+f.typed)
	}
	return rows
}

// --- hours summary -----------------------------------------------------------------

func (a *App) summary() ([]sheet.ProjectTotal, int) {
	start, end, _, _ := a.periodRange()
	return sheet.Summarise(a.entries, start, end)
}

// summaryRows are the table rows below the header: project, category, time,
// and the row kind ("", "subtotal" or "total").
type sumRow struct{ project, category, time, kind string }

func (a *App) summaryRows() []sumRow {
	projects, total := a.summary()
	if len(projects) == 0 {
		return nil
	}
	var rows []sumRow
	for _, p := range projects {
		for i, c := range p.Categories {
			name := ""
			if i == 0 {
				name = p.Name
			}
			rows = append(rows, sumRow{name, c.Name, sheet.FmtMinutes(c.Minutes), ""})
		}
		if len(p.Categories) > 1 {
			rows = append(rows, sumRow{"", "Subtotal", sheet.FmtMinutes(p.Minutes), "subtotal"})
		}
	}
	return append(rows, sumRow{"Total", "", sheet.FmtMinutes(total), "total"})
}

func (a *App) summaryLineCount() int { return len(a.summaryRows()) }

func (a *App) renderHours(w, h int) string {
	focused := a.focus == panelPane && a.form == nil
	_, _, name, span := a.periodRange()
	inner := w - 4
	var tabs []string
	for _, p := range []struct{ key, period, label string }{{"t", periodDay, "today"}, {"w", periodWeek, "week"}, {"m", periodMonth, "month"}} {
		label := p.key + " " + p.label
		if p.period == a.period {
			tabs = append(tabs, a.st.accentBold.Render(label))
		} else {
			tabs = append(tabs, a.st.muted.Render(label))
		}
	}
	lines := []string{strings.Join(tabs, a.st.muted.Render(" · "))}
	rows := a.summaryRows()
	if len(rows)+2 > h-2 { // tight: the period is in the title anyway
		lines = nil
	}
	title := fmt.Sprintf(" [3] Hours · %s · %s ", name, span)
	if len(rows) == 0 {
		what := "this " + a.period
		if a.period == periodDay {
			what = "today"
		}
		lines = append(lines, a.st.muted.Render("No entries logged "+what+" yet."))
		return a.st.box(title, lines, w, h, focused)
	}
	timeW := 4
	for _, r := range rows {
		timeW = max(timeW, len(r.time))
	}
	rest := inner - timeW - 4
	projW := max(rest*2/5, 6)
	catW := max(rest-projW, 6)
	cell := func(s string, w int) string { return fmt.Sprintf("%-*s", w, ansi.Truncate(s, w, "…")) }
	lines = append(lines, a.st.muted.Render(cell("Project", projW)+"  "+cell("Entry type", catW)+"  "+fmt.Sprintf("%*s", timeW, "Time")))
	var body []string
	for _, r := range rows {
		p, c, t := cell(r.project, projW), cell(r.category, catW), fmt.Sprintf("%*s", timeW, r.time)
		switch r.kind {
		case "subtotal":
			body = append(body, p+"  "+a.st.bold.Render(c+"  "+t))
		case "total":
			body = append(body, a.st.ok.Bold(true).Render(p+"  "+c+"  "+t))
		default:
			body = append(body, a.st.bold.Render(p)+"  "+c+"  "+t)
		}
	}
	// the total stays in view; the rows above it scroll
	total, body := body[len(body)-1], body[:len(body)-1]
	room := h - 2 - len(lines) - 1
	if room > 0 && len(body) > room {
		top := min(a.sumScroll, len(body)-room)
		a.sumScroll = top
		body = body[top : top+room]
		if top+room < len(rows)-1 {
			body[room-1] = a.st.muted.Render(fmt.Sprintf("… %d more (4, then j/k)", len(rows)-1-top-room+1))
		}
	}
	lines = append(append(lines, body...), total)
	return a.st.box(title, lines, w, h, focused)
}

// --- screens and bars ----------------------------------------------------------------

func (a *App) renderSetupNeeded() string {
	var b strings.Builder
	b.WriteString(a.st.accentBold.Render("lazysheet needs osheet's config") + "\n\n")
	if errors.Is(a.cfgErr, sheet.ErrNoConfig) {
		b.WriteString("No osheet config was found at\n" + a.st.bold.Render(sheet.TildePath(sheet.ConfigPath())) + "\n\n")
		b.WriteString("lazysheet logs into the same timesheets as osheet and uses its\ntemplate, projects and defaults. Set osheet up first:\n\n")
	} else {
		b.WriteString("osheet's config can't be used:\n" + a.st.err.Render(fmt.Sprint(a.cfgErr)) + "\n\n")
	}
	b.WriteString("    " + a.st.bold.Render("osheet --setup") + "\n\n")
	b.WriteString(a.st.muted.Render("then press r here to try again · q to " + a.quitWord()))
	return a.st.modal.Render(b.String())
}

func (a *App) renderStatusBar() string {
	if a.errMsg != "" {
		return fit(a.st.err.Render("error: "+a.errMsg), a.width)
	}
	left := a.st.muted.Render("lazysheet — " + weekTitle(a.today))
	if a.status != "" {
		left = a.st.ok.Render(a.status)
	}
	return fit(left, a.width)
}

type kv struct{ k, v string }

func (a *App) renderHelpBar() string {
	var parts []string
	for _, p := range a.helpPairs() {
		parts = append(parts, a.st.helpKey.Render(p.k)+" "+a.st.muted.Render(p.v))
	}
	return fit(strings.Join(parts, "  "), a.width)
}

// helpPairs lists the keys for the current context (3–6 of them). It feeds
// both the standalone help bar and Help() in dock.
func (a *App) helpPairs() []kv {
	switch {
	case a.showHelp:
		return []kv{{"any key", "close help"}}
	case !a.loaded || a.cfg == nil:
		return []kv{{"r", "retry"}, {"?", "help"}, {"q", a.quitWord()}}
	case a.form != nil && a.form.isMenu:
		return []kv{{"j/k", "move"}, {"enter", "select"}, {"1-9", "pick"}, {"shift+tab", "back"}, {"esc", "cancel"}}
	case a.form != nil && a.form.step == fDescription:
		return []kv{{"enter", "save"}, {"ctrl+e", "$EDITOR"}, {"shift+tab", "back"}, {"esc", "cancel"}}
	case a.form != nil:
		return []kv{{"enter", "next"}, {"shift+tab", "back"}, {"esc", "cancel"}}
	case a.confirmDelete:
		return []kv{{"y", "delete"}, {"n", "keep"}}
	}
	var pairs []kv
	switch a.focus {
	case panelDays:
		pairs = []kv{{"j/k", "day"}, {"[/]", "week"}, {"n", "new on day"}}
	case panelEntries:
		pairs = []kv{{"e", "edit"}, {"d", "delete"}, {"n", "new"}}
	case panelPane:
		if a.showHours {
			pairs = []kv{{"←/→", "period"}, {"j/k", "scroll"}, {"esc", "entry"}}
		} else {
			pairs = []kv{{"e", "edit"}, {"t/w/m", "hours"}, {"n", "new"}}
		}
	}
	return append(pairs, kv{"tab", "panel"}, kv{"?", "help"}, kv{"q", a.quitWord()})
}

func (a *App) quitWord() string {
	if a.embedded {
		return "close"
	}
	return "quit"
}

func (a *App) renderHelp() string {
	sections := []struct{ title, body string }{
		{"Panels", "1-3 / tab / shift+tab   jump to / cycle Days, Entries, Entry (or Hours)\nj/k ↑/↓  g/G           move, top, bottom\n[ / ]                  previous / next week (Days and Entries)\nenter / l              Days: show in Entries · Entries: edit\nh / esc                Entries: back to Days · Hours: back to the entry"},
		{"Entries", "n   new entry (on the selected day in Days); starts from the\n    last entry's day, project, type, location and sentiment\ne   edit the selected entry (every question is asked)\nd   delete the selected entry (asks y/N)"},
		{"Entry form", "j/k enter   choose · 1-9 pick by number\nenter       next question; the last one saves\nshift+tab   back to the previous question\nctrl+e      description in $EDITOR\nesc         cancel, nothing is saved"},
		{"Hours and folder", "t / w / m   hours today, this week, this month (in the right pane)\n←/→         Hours: previous / next period\ny           copy the current timesheet folder's path\nr           reload from disk"},
		{"", "?   this help    q   " + a.quitWord() + " lazysheet"},
	}
	var b strings.Builder
	b.WriteString(a.st.accentBold.Render("lazysheet — keys") + "\n\n")
	for _, s := range sections {
		if s.title != "" {
			b.WriteString(a.st.accentBold.Render(s.title) + "\n")
		}
		b.WriteString(s.body + "\n\n")
	}
	b.WriteString(a.st.muted.Render("press any key to close"))
	return a.st.modal.Render(b.String())
}
