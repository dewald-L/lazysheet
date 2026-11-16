package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dewald-L/lazysheet/internal/sheet"
)

// The entry form is a port of osheet's entry_steps: one question at a time,
// answers filling in a panel of fields above it. Questions that aren't asked
// every time answer themselves with their default (marked "(skipped)"), and
// Back (shift+tab) passes over them.

const (
	fDay = iota
	fProject
	fCategory
	fTime
	fLocation
	fSentiment
	fDescription
	fieldCount
)

var fieldLabels = [fieldCount]string{"Day", "Project", "Entry type", "Time", "Location", "Sentiment", "Description"}
var fieldKeys = [fieldCount]string{"day", "project", "category", "time", "location", "sentiment", "description"}

var defaultLocations = []string{"Home", "Office", "Client", "Other"}

const skippedNote = "(skipped)"

type fieldValue struct {
	text, note string
	dayMins    int  // the day field's logged minutes, drawn as a bar
	hasBar     bool // draw dayMins as a bar after the day
}

type entryForm struct {
	edit   *sheet.Entry // nil for a new entry
	asking map[string]bool
	days   []time.Time

	// answers
	date                                                         time.Time
	project, category, location, sentiment, description, timeTxt string
	hours, mins                                                  int

	values  [fieldCount]fieldValue
	step    int
	history []int

	// the current question
	question string
	isMenu   bool
	options  []string // menu values
	labels   []string // menu labels (the day menu adds hours)
	dayMenu  []int    // the day menu's minutes per option
	cursor   int
	typed    string
	input    textinput.Model
	err      string
}

func newInput() textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 0
	return in
}

// newForm starts a new entry. prev (the last entry logged this session)
// pre-selects its day, project, entry type, location and sentiment, like
// osheet --multi; day (when not nil) pre-selects a day of the week.
func (a *App) newForm(prev *sheet.Answers, day *time.Time) (*entryForm, tea.Cmd) {
	days := sheet.WeekOf(a.today)
	if day != nil {
		days = sheet.WeekOf(*day) // a day of the week shown in Days
	}
	f := &entryForm{asking: a.cfg.Asking(), days: days, input: newInput()}
	if prev != nil {
		f.date, f.project, f.category = prev.Date, prev.Project, prev.Category
		f.location, f.sentiment = prev.Location, prev.Sentiment
	}
	if day != nil {
		f.date = *day
	}
	return f, a.enterStep(f, 0)
}

// editForm edits e: every question is asked, starting from its values; the
// day can move within the entry's own week.
func (a *App) editForm(e sheet.Entry) (*entryForm, tea.Cmd) {
	asking := map[string]bool{}
	for _, k := range sheet.AskFields {
		asking[k] = true
	}
	f := &entryForm{
		edit: &e, asking: asking, days: sheet.WeekOf(e.Date), input: newInput(),
		date: e.Date, project: e.Get("Project"), category: e.Get("Category"),
		timeTxt:  fmt.Sprintf("%dh%02d", e.Hours, e.Minutes),
		location: e.Get("WorkedFrom"), sentiment: e.Get("Sentiment"), description: e.Get("Description"),
	}
	f.values[fDay] = fieldValue{text: e.Date.Format("Monday 02 Jan")}
	for i, v := range []string{f.project, f.category, f.timeTxt, f.location, f.sentiment, f.description} {
		f.values[fProject+i] = fieldValue{text: v}
	}
	return f, a.enterStep(f, 0)
}

// pick is osheet's pick: the index of preferred in options, else fallback.
func pick(options []string, preferred string, fallback int) int {
	for i, o := range options {
		if o == preferred {
			return i
		}
	}
	return fallback
}

func contains(options []string, s string) bool { return pick(options, s, -1) >= 0 }

func joinNotes(notes ...string) string {
	var out []string
	for _, n := range notes {
		if n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, " ")
}

func loggedNote(mins int) string {
	if mins == 0 {
		return ""
	}
	return fmt.Sprintf("(%s already logged)", sheet.FmtMinutes(mins))
}

func (a *App) categories(project string) []string {
	if p := a.cfg.Project(project); p != nil {
		return p.Categories
	}
	return nil
}

func (a *App) locations() []string {
	if len(a.cfg.Locations) > 0 {
		return a.cfg.Locations
	}
	return defaultLocations
}

// enterStep runs steps from i on: steps that answer themselves are filled in
// and passed over; the first one that needs an answer becomes the question.
// Past the last step the form is complete and is saved.
func (a *App) enterStep(f *entryForm, i int) tea.Cmd {
	for ; i < fieldCount; i++ {
		f.step, f.err, f.typed = i, "", ""
		if !a.autoAnswer(f, i) {
			return a.ask(f, i)
		}
	}
	f.step = fieldCount
	return a.saveForm()
}

// autoAnswer answers step i with its default when it isn't asked every time
// (and the default is valid), as osheet does.
func (a *App) autoAnswer(f *entryForm, i int) bool {
	d := a.cfg.Defaults
	switch i {
	case fDay:
		if f.asking["day"] {
			return false
		}
		f.date = a.today
		mins := a.minutesByDay(f.edit)[a.today]
		f.values[fDay] = fieldValue{text: a.today.Format("Monday 02 Jan"), note: joinNotes(loggedNote(mins), skippedNote), dayMins: mins, hasBar: true}
	case fProject:
		if f.asking["project"] || !contains(a.cfg.ProjectNames(), d.Project) {
			return false
		}
		f.project = d.Project
		f.values[fProject] = fieldValue{text: f.project, note: skippedNote}
	case fCategory:
		if f.asking["category"] || !contains(a.categories(f.project), d.Category) {
			return false
		}
		f.category = d.Category
		f.values[fCategory] = fieldValue{text: f.category, note: skippedNote}
	case fTime:
		if f.asking["time"] {
			return false
		}
		minutes, err := sheet.CheckDuration(d.Time)
		if err != nil { // no usable default time: ask anyway
			return false
		}
		note := f.setTime(minutes)
		f.values[fTime] = fieldValue{text: f.timeTxt, note: joinNotes(note, skippedNote)}
	case fLocation:
		if f.asking["location"] || !contains(a.cfg.Locations, d.Location) {
			return false
		}
		f.location = d.Location
		f.values[fLocation] = fieldValue{text: f.location, note: skippedNote}
	case fSentiment:
		if f.asking["sentiment"] || !contains(a.cfg.SentimentOptions(), d.Sentiment) {
			return false
		}
		f.sentiment = d.Sentiment
		f.values[fSentiment] = fieldValue{text: f.sentiment, note: skippedNote}
	default:
		return false
	}
	return true
}

// setTime stores minutes rounded up to the next quarter hour and returns the
// rounding note.
func (f *entryForm) setTime(minutes int) string {
	rounded := sheet.RoundUp15(minutes)
	f.hours, f.mins = rounded/60, rounded%60
	f.timeTxt = fmt.Sprintf("%dh%02d", f.hours, f.mins)
	if rounded != minutes {
		return fmt.Sprintf("(rounded up from %dh%02d)", minutes/60, minutes%60)
	}
	return ""
}

// ask sets up the question for step i.
func (a *App) ask(f *entryForm, i int) tea.Cmd {
	d := a.cfg.Defaults
	menu := func(q string, options []string, current, def string) {
		f.question, f.isMenu = q, true
		f.options, f.labels, f.dayMenu = options, options, nil
		f.cursor = pick(options, current, pick(options, def, 0))
	}
	switch i {
	case fDay:
		mins := a.minutesByDay(f.edit)
		f.question, f.isMenu = "Which day is the timesheet for?", true
		f.options, f.labels, f.dayMenu = nil, nil, nil
		for _, day := range f.days {
			label := day.Format("Monday 02 Jan")
			if day.Equal(a.today) {
				label += "  (today)"
			}
			f.options = append(f.options, day.Format("2006-01-02"))
			f.labels = append(f.labels, label)
			f.dayMenu = append(f.dayMenu, mins[day])
		}
		f.cursor = 0
		for j, day := range f.days {
			if day.Equal(a.today) {
				f.cursor = j
			}
		}
		for j, day := range f.days {
			if day.Equal(f.date) {
				f.cursor = j
			}
		}
	case fProject:
		menu("Which project?", a.cfg.ProjectNames(), f.project, d.Project)
	case fCategory:
		menu("What type of entry is it?", a.categories(f.project), f.category, d.Category)
	case fLocation:
		menu("Where did you work from?", a.locations(), f.location, d.Location)
	case fSentiment:
		menu("How did it go?", a.cfg.SentimentOptions(), f.sentiment, d.Sentiment)
	case fTime:
		f.question, f.isMenu = "Total time spent? (e.g. 1:20, 1h20, 80m, 1.5)", false
		f.input.SetValue(f.timeTxt)
	case fDescription:
		f.question, f.isMenu = "Description? (max 255 characters)", false
		f.input.SetValue(f.description)
	}
	if f.isMenu {
		f.input.Blur()
		return nil
	}
	f.input.CursorEnd()
	if a.focused {
		return f.input.Focus()
	}
	return nil
}

// answerMenu records the highlighted menu option for the current step.
func (a *App) answerMenu(f *entryForm) tea.Cmd {
	if len(f.options) == 0 {
		return nil
	}
	choice := f.options[f.cursor]
	switch f.step {
	case fDay:
		day := f.days[f.cursor]
		f.date = day
		mins := f.dayMenu[f.cursor]
		f.values[fDay] = fieldValue{text: day.Format("Monday 02 Jan"), note: loggedNote(mins), dayMins: mins, hasBar: true}
	case fProject:
		f.project = choice
	case fCategory:
		f.category = choice
	case fLocation:
		f.location = choice
	case fSentiment:
		f.sentiment = choice
	}
	if f.step != fDay {
		f.values[f.step] = fieldValue{text: choice}
	}
	return a.next(f)
}

// answerInput checks and records the typed answer for the current step.
func (a *App) answerInput(f *entryForm) tea.Cmd {
	raw := strings.TrimSpace(f.input.Value())
	switch f.step {
	case fTime:
		if raw == "" {
			raw = f.timeTxt
		}
		minutes, err := sheet.CheckDuration(raw)
		if err != nil {
			f.err = err.Error()
			return nil
		}
		note := f.setTime(minutes)
		f.values[fTime] = fieldValue{text: f.timeTxt, note: note}
	case fDescription:
		if raw == "" {
			raw = f.description
		}
		desc, err := sheet.CheckDescription(raw)
		if err != nil {
			f.err = err.Error()
			return nil
		}
		f.description = desc
		f.values[fDescription] = fieldValue{text: desc}
	}
	f.input.Blur()
	return a.next(f)
}

func (a *App) next(f *entryForm) tea.Cmd {
	f.history = append(f.history, f.step)
	return a.enterStep(f, f.step+1)
}

// back re-asks the previous question that was actually asked.
func (a *App) back(f *entryForm) tea.Cmd {
	if len(f.history) == 0 {
		return nil
	}
	prev := f.history[len(f.history)-1]
	f.history = f.history[:len(f.history)-1]
	f.step, f.err, f.typed = prev, "", ""
	return a.ask(f, prev)
}

// typeNumber picks a menu option by number as soon as the number can't grow
// into another option's number (osheet's menus).
func (a *App) typeNumber(f *entryForm, digit string) tea.Cmd {
	typed := f.typed + digit
	f.typed = ""
	var matches []int
	for i := range f.options {
		if strings.HasPrefix(strconv.Itoa(i+1), typed) {
			matches = append(matches, i)
		}
	}
	switch {
	case len(matches) == 1 && strconv.Itoa(matches[0]+1) == typed:
		f.cursor = matches[0]
		return a.answerMenu(f)
	case len(matches) > 0:
		f.cursor, f.typed = matches[0], typed
	}
	return nil
}

// answers are the completed form's values.
func (f *entryForm) answers() sheet.Answers {
	return sheet.Answers{
		Date: f.date, Project: f.project, Category: f.category, Location: f.location,
		Sentiment: f.sentiment, Description: f.description, Hours: f.hours, Minutes: f.mins,
	}
}

// descCount is the description's length as osheet counts it.
func descCount(s string) int { return utf8.RuneCountInString(strings.TrimSpace(s)) }
