package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dewald-L/dock/plugin"

	"github.com/dewald-L/lazysheet/internal/sheet"
)

type savedMsg struct {
	path, verb, summary string
	row                 int
	answers             sheet.Answers
	isNew               bool
	err                 error
}

type deletedMsg struct {
	summary, path string
	err           error
}

type clipboardMsg struct {
	tool  string
	tools [][]string // the tools still to try if this one failed
	err   error
}

type editorMsg struct {
	path string
	err  error
}

// Update handles msg. When embedded in dock it also forwards status and
// error messages to the host's status bar, and in both modes it keeps the
// tab title on the current week.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := a.update(msg)
	cmds := []tea.Cmd{cmd}
	if a.embedded {
		if a.errMsg != "" {
			cmds = append(cmds, plugin.Error(errors.New(a.errMsg)))
			a.errMsg = ""
		} else if a.status != "" {
			cmds = append(cmds, plugin.Notify(a.status))
		}
		a.status = ""
	}
	if title := "lazysheet · " + weekTitle(a.today); title != a.title {
		a.title = title
		cmds = append(cmds, plugin.SetTitle(title))
	}
	return m, tea.Batch(cmds...)
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.sizeInput()
		return a, nil

	case plugin.FocusMsg:
		a.focused = true
		var cmd tea.Cmd
		if a.form != nil && !a.form.isMenu && a.form.step < fieldCount {
			cmd = a.form.input.Focus()
		}
		return a, tea.Batch(cmd, a.reload())

	case plugin.BlurMsg:
		a.focused = false
		if a.form != nil {
			a.form.input.Blur()
		}
		return a, nil

	case loadedMsg:
		a.applyLoaded(msg)
		return a, nil

	case tickMsg:
		if !sheet.Today().Equal(a.today) {
			return a, tea.Batch(a.reload(), tick())
		}
		return a, tick()

	case commandMsg:
		return a.runCommand(msg)

	case savedMsg:
		a.busy = false
		if msg.err != nil {
			a.errMsg = msg.err.Error()
			if a.form != nil { // back on the last question, so nothing typed is lost
				cmd := a.back(a.form)
				a.form.err = msg.err.Error()
				return a, tea.Batch(cmd, a.reload())
			}
			return a, a.reload()
		}
		a.form = nil
		if msg.isNew {
			ans := msg.answers
			a.prev = &ans
		}
		a.selectRow = &rowKey{msg.path, msg.row}
		a.focus = panelEntries
		a.status = fmt.Sprintf("%s %s → %s", msg.verb, msg.summary, sheet.TildePath(msg.path))
		return a, a.reload()

	case deletedMsg:
		a.busy = false
		if msg.err != nil {
			a.errMsg = msg.err.Error()
		} else {
			a.status = fmt.Sprintf("Deleted %s from %s", msg.summary, sheet.TildePath(msg.path))
		}
		return a, a.reload()

	case clipboardMsg:
		if msg.err == nil {
			a.status = "Copied folder path to clipboard (" + msg.tool + ")"
			return a, nil
		}
		if len(msg.tools) > 0 {
			return a, a.copyWith(msg.tools)
		}
		a.errMsg = fmt.Sprintf("%s failed: %v", msg.tool, msg.err)
		return a, nil

	case editorMsg:
		defer os.Remove(msg.path)
		if msg.err != nil {
			a.errMsg = "$EDITOR: " + msg.err.Error()
			return a, nil
		}
		data, err := os.ReadFile(msg.path)
		if err != nil {
			a.errMsg = err.Error()
			return a, nil
		}
		if f := a.form; f != nil && f.step == fDescription {
			f.input.SetValue(strings.Join(strings.Fields(string(data)), " "))
			f.input.CursorEnd()
			f.err = ""
			if a.focused {
				return a, f.input.Focus()
			}
		}
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(msg)
	}

	if a.form != nil && !a.form.isMenu { // cursor blink
		var cmd tea.Cmd
		a.form.input, cmd = a.form.input.Update(msg)
		return a, cmd
	}
	return a, nil
}

func (a *App) reload() tea.Cmd { return a.load() }

func (a *App) sizeInput() {
	if a.form != nil {
		a.form.input.Width = max(a.rightWidth()-10, 10) // inside the box, after "  ❯ ", with room for the cursor
	}
}

// --- keys ------------------------------------------------------------------------

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if a.showHelp {
		a.showHelp = false
		return a, nil
	}
	if !a.loaded || a.cfg == nil {
		switch k {
		case "q":
			return a, tea.Quit
		case "r":
			return a, a.reload()
		case "?":
			a.showHelp = true
		}
		return a, nil
	}
	if a.form != nil {
		return a.handleFormKey(msg)
	}
	if a.confirmDelete {
		a.confirmDelete = false
		if k == "y" || k == "Y" {
			return a.deleteSelected()
		}
		a.status = "Kept it"
		return a, nil
	}

	switch k {
	case "q":
		return a, tea.Quit
	case "?":
		a.showHelp = true
		return a, nil
	case "tab":
		a.focus = (a.focus + 1) % panelCount
		return a, nil
	case "shift+tab":
		a.focus = (a.focus + panelCount - 1) % panelCount
		return a, nil
	case "1", "2", "3", "4":
		a.focus = panelID(k[0] - '1')
		return a, nil
	case "n":
		var day *time.Time
		if a.focus == panelDays && a.dayPicked { // a day picked in Days beats the previous entry's
			d := a.currentWeek()[a.dayIdx]
			day = &d
		}
		a.dayPicked = false
		return a.startNew(day)
	case "e":
		return a.startEdit()
	case "d":
		if a.selectedEntry() == nil {
			a.status = "No entry selected"
			return a, nil
		}
		a.confirmDelete = true
		return a, nil
	case "y":
		return a, a.copyFolder()
	case "r":
		return a, a.reload()
	case "t":
		a.setPeriod(periodDay)
		return a, nil
	case "w":
		a.setPeriod(periodWeek)
		return a, nil
	case "m":
		a.setPeriod(periodMonth)
		return a, nil
	}

	switch a.focus {
	case panelDays:
		return a.handleDaysKey(k)
	case panelWeeks:
		return a.handleWeeksKey(k)
	case panelEntries:
		return a.handleEntriesKey(k)
	case panelHours:
		return a.handleHoursKey(k)
	}
	return a, nil
}

// move applies a j/k/g/G style key to idx within n items.
func move(k string, idx, n int) (int, bool) {
	switch k {
	case "j", "down":
		return clampIdx(idx+1, n), true
	case "k", "up":
		return clampIdx(idx-1, n), true
	case "g", "home":
		return 0, true
	case "G", "end":
		return clampIdx(n-1, n), true
	}
	return idx, false
}

func (a *App) handleDaysKey(k string) (tea.Model, tea.Cmd) {
	if i, ok := move(k, a.dayIdx, 7); ok {
		a.dayIdx, a.dayPicked = i, true
		a.showDay()
		return a, nil
	}
	if k == "enter" || k == "l" || k == "right" {
		a.showDay()
		a.focus = panelEntries
	}
	return a, nil
}

// showDay opens the current week in Entries, on the selected day's newest
// entry.
func (a *App) showDay() {
	a.weekIdx = a.currentWeekIdx()
	day := a.currentWeek()[a.dayIdx]
	for i, e := range a.weekEntries() {
		if e.Date.Equal(day) {
			a.entryIdx = i
			return
		}
	}
}

func (a *App) handleWeeksKey(k string) (tea.Model, tea.Cmd) {
	if i, ok := move(k, a.weekIdx, len(a.weeks())); ok {
		if i != a.weekIdx {
			a.weekIdx, a.entryIdx = i, 0
		}
		return a, nil
	}
	if k == "enter" || k == "l" || k == "right" {
		a.focus = panelEntries
	}
	return a, nil
}

func (a *App) handleEntriesKey(k string) (tea.Model, tea.Cmd) {
	if i, ok := move(k, a.entryIdx, len(a.weekEntries())); ok {
		a.entryIdx = i
		return a, nil
	}
	switch k {
	case "enter":
		return a.startEdit()
	case "h", "left", "esc":
		a.focus = panelWeeks
	}
	return a, nil
}

func (a *App) handleHoursKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "left", "h":
		a.cyclePeriod(-1)
	case "right", "l":
		a.cyclePeriod(1)
	default:
		if i, ok := move(k, a.sumScroll, a.summaryLineCount()); ok {
			a.sumScroll = i
		}
	}
	return a, nil
}

func (a *App) setPeriod(p string) {
	a.period, a.sumScroll = p, 0
}

func (a *App) cyclePeriod(d int) {
	i := pick(periods, a.period, 1)
	a.setPeriod(periods[(i+d+len(periods))%len(periods)])
}

func (a *App) startNew(day *time.Time) (tea.Model, tea.Cmd) {
	if a.busy {
		return a, nil
	}
	f, cmd := a.newForm(a.prev, day)
	a.form, a.confirmDelete = f, false
	a.sizeInput()
	return a, cmd
}

func (a *App) startEdit() (tea.Model, tea.Cmd) {
	e := a.selectedEntry()
	if e == nil {
		a.status = "No entry selected"
		return a, nil
	}
	if a.busy {
		return a, nil
	}
	f, cmd := a.editForm(*e)
	a.form, a.confirmDelete = f, false
	a.sizeInput()
	return a, cmd
}

func (a *App) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := a.form
	k := msg.String()
	if a.busy {
		return a, nil
	}
	switch k {
	case "esc":
		a.form = nil
		if f.edit != nil {
			a.status = "Edit cancelled, nothing changed"
		} else {
			a.status = "Cancelled, nothing saved"
		}
		return a, nil
	case "shift+tab":
		return a, a.back(f)
	}
	if f.isMenu {
		if i, ok := move(k, f.cursor, len(f.options)); ok {
			f.cursor, f.typed = i, ""
			return a, nil
		}
		switch {
		case k == "enter" || k == " ":
			f.typed = ""
			return a, a.answerMenu(f)
		case len(k) == 1 && k[0] >= '0' && k[0] <= '9':
			return a, a.typeNumber(f, k)
		}
		f.typed = ""
		return a, nil
	}
	switch k {
	case "enter":
		return a, a.answerInput(f)
	case "ctrl+e":
		if f.step == fDescription {
			return a, a.editDescription(f.input.Value())
		}
		return a, nil
	}
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	f.err = ""
	return a, cmd
}

// --- saving --------------------------------------------------------------------

// saveForm saves the completed form: a new entry is appended to its week's
// or month's file; an edit replaces its row, or moves it to another file
// when the new day belongs to another week or month. The ticket number is
// kept.
func (a *App) saveForm() tea.Cmd {
	f := a.form
	if f == nil {
		return nil
	}
	a.busy = true
	cfg := a.cfg
	ans := f.answers()
	summary := entrySummary(ans.Date, ans.Project, ans.Category, ans.Hours*60+ans.Minutes, ans.Location)
	if f.edit == nil {
		return func() tea.Msg {
			p, row, err := cfg.Save(cfg.Values(ans, ""))
			return savedMsg{path: p, row: row, verb: "Saved", summary: summary, answers: ans, isNew: true, err: err}
		}
	}
	e := *f.edit
	return func() tea.Msg {
		if err := checkUnchanged(e); err != nil {
			return savedMsg{err: err}
		}
		p, row, err := cfg.Update(e, cfg.Values(ans, e.Get("TicketNumber")))
		return savedMsg{path: p, row: row, verb: "Updated", summary: summary, answers: ans, err: err}
	}
}

// checkUnchanged makes sure e's row still holds what was loaded (osheet may
// have changed the file since), so an edit or delete never hits another row.
func checkUnchanged(e sheet.Entry) error {
	entries, err := sheet.ReadEntries(e.Path)
	if err != nil {
		return err
	}
	for _, cur := range entries {
		if sameEntry(cur, e) {
			return nil
		}
	}
	return fmt.Errorf("%s changed since it was loaded; reloaded, try again", sheet.TildePath(e.Path))
}

func (a *App) deleteSelected() (tea.Model, tea.Cmd) {
	e := a.selectedEntry()
	if e == nil || a.busy {
		return a, nil
	}
	a.busy = true
	cfg, entry := a.cfg, *e
	summary := entrySummary(entry.Date, entry.Get("Project"), entry.Get("Category"), entry.Total(), entry.Get("WorkedFrom"))
	return a, func() tea.Msg {
		if err := checkUnchanged(entry); err != nil {
			return deletedMsg{err: err}
		}
		return deletedMsg{summary: summary, path: entry.Path, err: cfg.Delete(entry)}
	}
}

// --- clipboard and editor --------------------------------------------------------

// clipboardTools lists the clipboard programs to try, as osheet does.
func clipboardTools() [][]string {
	var out [][]string
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		out = append(out, []string{"wl-copy"})
	}
	out = append(out, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
	var found [][]string
	for _, c := range out {
		if _, err := exec.LookPath(c[0]); err == nil {
			found = append(found, c)
		}
	}
	return found
}

func (a *App) copyFolder() tea.Cmd {
	if a.folder == "" {
		return nil
	}
	tools := clipboardTools()
	if len(tools) == 0 {
		a.errMsg = "No clipboard tool found (install wl-clipboard, xclip or xsel); the folder is " + a.folder
		return nil
	}
	return a.copyWith(tools)
}

// copyWith runs the first clipboard tool through the host (plugin.Exec), with
// the folder on its stdin; on failure the next one is tried.
func (a *App) copyWith(tools [][]string) tea.Cmd {
	c := exec.Command(tools[0][0], tools[0][1:]...)
	c.Stdin = strings.NewReader(a.folder)
	tool, rest := tools[0][0], tools[1:]
	return plugin.Exec(c, func(err error) tea.Msg { return clipboardMsg{tool: tool, tools: rest, err: err} })
}

// editDescription opens the description in $EDITOR (through the host).
func (a *App) editDescription(text string) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	return func() tea.Msg {
		f, err := os.CreateTemp("", "lazysheet-*.txt")
		if err != nil {
			return editorMsg{err: err}
		}
		f.WriteString(text)
		f.Close()
		c := exec.Command("sh", "-c", editor+` "$1"`, "sh", f.Name())
		p := f.Name()
		return plugin.Exec(c, func(err error) tea.Msg { return editorMsg{path: p, err: err} })()
	}
}
