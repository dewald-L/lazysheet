package sheet

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Entry is one timesheet row.
type Entry struct {
	Date           time.Time
	Hours, Minutes int
	Fields         map[string]string // every column by header, as read
	Row            int               // position in the sheet's rows, for updates
	Path           string            // the workbook it was read from
}

// Get is a column's value ("" when the cell is empty).
func (e Entry) Get(header string) string { return e.Fields[header] }

// Total is the entry's time in minutes.
func (e Entry) Total() int { return e.Hours*60 + e.Minutes }

func pyIntFloat(s string) (int, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("not a number: %q", s)
	}
	return int(f), nil // int() truncates toward zero, as in Python
}

// ReadEntries reads the entries of a timesheet workbook (osheet's
// read_entries): rows without a date are skipped.
func ReadEntries(p string) ([]Entry, error) {
	rows, err := readSheetRows(p)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	headers := rows[0]
	var out []Entry
	for i := 1; i < len(rows); i++ {
		fields := map[string]string{}
		for col, val := range rows[i] {
			if h, ok := headers[col]; ok {
				fields[h] = val
			}
		}
		if fields["Date"] == "" {
			continue
		}
		serial, err := pyIntFloat(fields["Date"])
		if err != nil {
			return nil, fmt.Errorf("%s row %d: Date: %w", p, i+1, err)
		}
		e := Entry{Date: FromSerial(serial), Fields: fields, Row: i, Path: p}
		for _, k := range []string{"Hours", "Minutes"} {
			n := 0
			if v := fields[k]; v != "" {
				if n, err = pyIntFloat(v); err != nil {
					return nil, fmt.Errorf("%s row %d: %s: %w", p, i+1, k, err)
				}
			}
			if k == "Hours" {
				e.Hours = n
			} else {
				e.Minutes = n
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// PathsFor lists, in date order and without repeats, the workbooks entries
// dated start..end would be in.
func (c *Config) PathsFor(start, end time.Time) []string {
	var paths []string
	seen := map[string]bool{}
	for _, d := range DaysBetween(start, end) {
		p := c.OutputPath(d)
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	return paths
}

// EntriesBetween reads the entries dated start..end from every file those
// days fall in, in file then row order. A file that can't be read is
// reported in the error, and the other files' entries are still returned.
func (c *Config) EntriesBetween(start, end time.Time) ([]Entry, error) {
	var out []Entry
	var errs []string
	for _, p := range c.PathsFor(start, end) {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		entries, err := ReadEntries(p)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		for _, e := range entries {
			if !e.Date.Before(start) && !e.Date.After(end) {
				out = append(out, e)
			}
		}
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return out, nil
}

// NewestFirst sorts entries by date then row, newest first (osheet's
// month_entries order).
func NewestFirst(entries []Entry) []Entry {
	out := append([]Entry(nil), entries...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Date.Equal(out[j].Date) {
			return out[i].Date.After(out[j].Date)
		}
		return out[i].Row > out[j].Row
	})
	return out
}

// TemplatePath is osheet's stored template copy, made from the configured
// template if it isn't there yet.
func (c *Config) TemplatePath() (string, error) {
	dst := TemplateCopyPath()
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}
	if c.Template == "" {
		return "", fmt.Errorf("no template: %s is missing; run osheet --setup", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := copyFile(absPath(c.Template), dst); err != nil {
		return "", fmt.Errorf("copying the template: %w", err)
	}
	return dst, nil
}

// Answers are a filled-in entry form.
type Answers struct {
	Date                                                time.Time
	Project, Category, Location, Sentiment, Description string
	Hours, Minutes                                      int
}

// Values are the row values for the answers (osheet's entry_values).
func (c *Config) Values(a Answers, ticket string) Values {
	billable := ""
	if p := c.Project(a.Project); p != nil {
		billable = p.Billable
	}
	return Values{
		"Date": a.Date, "Project": a.Project, "Category": a.Category,
		"Hours": a.Hours, "Minutes": a.Minutes, "Billable": billable,
		"Description": a.Description, "TicketNumber": ticket, "Sentiment": a.Sentiment,
		"WorkedFrom": a.Location,
	}
}

func lastRow(p string) (int, error) {
	entries, err := ReadEntries(p)
	if err != nil {
		return 0, err
	}
	row := 0
	for _, e := range entries {
		if e.Row > row {
			row = e.Row
		}
	}
	return row, nil
}

// Save appends a new entry to its week's or month's file, creating the file
// (and its folder) from the template when needed. It returns the file and
// the entry's row.
func (c *Config) Save(v Values) (string, int, error) {
	date, ok := v["Date"].(time.Time)
	if !ok {
		return "", 0, fmt.Errorf("entry has no date")
	}
	tpl, err := c.TemplatePath()
	if err != nil {
		return "", 0, err
	}
	p := c.OutputPath(date)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", 0, err
	}
	if err := appendRows(p, tpl, []Values{v}); err != nil {
		return "", 0, fmt.Errorf("%s: %w", p, err)
	}
	row, err := lastRow(p)
	return p, row, err
}

// Update replaces entry e with v: in place when the new date belongs in the
// same file, else it is deleted there and appended to the right file.
func (c *Config) Update(e Entry, v Values) (string, int, error) {
	date, ok := v["Date"].(time.Time)
	if !ok {
		return "", 0, fmt.Errorf("entry has no date")
	}
	tpl, err := c.TemplatePath()
	if err != nil {
		return "", 0, err
	}
	if c.OutputPath(date) == e.Path {
		if err := updateRow(e.Path, tpl, e.Row, v); err != nil {
			return "", 0, fmt.Errorf("%s: %w", e.Path, err)
		}
		return e.Path, e.Row, nil
	}
	if err := updateRow(e.Path, tpl, e.Row, nil); err != nil {
		return "", 0, fmt.Errorf("%s: %w", e.Path, err)
	}
	return c.Save(v)
}

// Delete removes entry e; the rows below it move up.
func (c *Config) Delete(e Entry) error {
	tpl, err := c.TemplatePath()
	if err != nil {
		return err
	}
	if err := updateRow(e.Path, tpl, e.Row, nil); err != nil {
		return fmt.Errorf("%s: %w", e.Path, err)
	}
	return nil
}

// --- hours summary -----------------------------------------------------------

// CategoryTotal is the time logged on one entry type.
type CategoryTotal struct {
	Name    string
	Minutes int
}

// ProjectTotal is the time logged on one project, busiest category first.
type ProjectTotal struct {
	Name       string
	Minutes    int
	Categories []CategoryTotal
}

// Summarise totals entries dated start..end per project and category,
// busiest first (osheet's show_hours). Ties keep reading order.
func Summarise(entries []Entry, start, end time.Time) ([]ProjectTotal, int) {
	var projects []ProjectTotal
	index := map[string]int{}
	total := 0
	for _, e := range entries {
		if e.Date.Before(start) || e.Date.After(end) {
			continue
		}
		name := e.Get("Project")
		if _, ok := e.Fields["Project"]; !ok {
			name = "?"
		}
		cat := e.Get("Category")
		if _, ok := e.Fields["Category"]; !ok {
			cat = "?"
		}
		i, ok := index[name]
		if !ok {
			i = len(projects)
			index[name] = i
			projects = append(projects, ProjectTotal{Name: name})
		}
		p := &projects[i]
		found := false
		for j := range p.Categories {
			if p.Categories[j].Name == cat {
				p.Categories[j].Minutes += e.Total()
				found = true
			}
		}
		if !found {
			p.Categories = append(p.Categories, CategoryTotal{cat, e.Total()})
		}
		p.Minutes += e.Total()
		total += e.Total()
	}
	for i := range projects {
		cats := projects[i].Categories
		sort.SliceStable(cats, func(a, b int) bool { return cats[a].Minutes > cats[b].Minutes })
	}
	sort.SliceStable(projects, func(a, b int) bool { return projects[a].Minutes > projects[b].Minutes })
	return projects, total
}
