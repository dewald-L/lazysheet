package sheet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestMain points HOME and XDG_CONFIG_HOME at a throwaway directory, so no
// test can reach the real ~/.config/osheet or ~/timesheets.
func TestMain(m *testing.M) {
	if os.Getenv("OSHEET_SRC") == "" {
		os.Setenv("OSHEET_SRC", ExpandUser("~/Projects/osheet/osheet"))
	}
	home, err := os.MkdirTemp("", "lazysheet-test-home")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	os.Unsetenv("OSHEET_FILE")
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func TestParseDuration(t *testing.T) {
	cases := map[string]int{
		"1:20": 80, "1h20": 80, "80m": 80, "1.5": 90, "90": 90, "2": 120, "25": 25,
		".5": 30, "1h 30m": 90, "1H30M": 90, "2.5h": 150, "0:00": 0, "1.0125h": 61,
		"24": 1440, "0.0001": 0, " 45m ": 45, "1h": 60,
	}
	for in, want := range cases {
		got, ok := ParseDuration(in)
		if !ok || got != want {
			t.Errorf("ParseDuration(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "abc", "h", "1:2:3", "-1", "1.5.5", "m"} {
		if got, ok := ParseDuration(in); ok {
			t.Errorf("ParseDuration(%q) = %d, want invalid", in, got)
		}
	}
}

func TestParseDurationMatchesOsheet(t *testing.T) {
	inputs := []string{"1:20", "1h20", "80m", "1.5", "90", "2", "25", ".5", "1h 30m", "2.5h",
		"0:00", "", "abc", "1.0125h", "0.025h", "24", "24.5", "1:5", "1h5m", "15m", "7", "0.75", "3h", "99999"}
	var want []*int
	if err := json.Unmarshal([]byte(osheet(t, nil, append([]string{"duration"}, inputs...)...)), &want); err != nil {
		t.Fatal(err)
	}
	for i, in := range inputs {
		got, ok := ParseDuration(in)
		switch {
		case want[i] == nil && ok:
			t.Errorf("%q: got %d, osheet says invalid", in, got)
		case want[i] != nil && (!ok || got != *want[i]):
			t.Errorf("%q: got %d/%v, osheet says %d", in, got, ok, *want[i])
		}
	}
}

func TestCheckDurationAndRounding(t *testing.T) {
	for _, bad := range []string{"", "0", "0:00", "25h", "1441m"} {
		if _, err := CheckDuration(bad); err == nil {
			t.Errorf("CheckDuration(%q) accepted", bad)
		}
	}
	if m, err := CheckDuration("24h"); err != nil || m != 1440 {
		t.Errorf("24h: %d %v", m, err)
	}
	for in, want := range map[int]int{1: 15, 15: 15, 16: 30, 80: 90, 1440: 1440, 61: 75} {
		if got := RoundUp15(in); got != want {
			t.Errorf("RoundUp15(%d) = %d, want %d", in, got, want)
		}
	}
	if FmtMinutes(125) != "2h05" || FmtMinutes(0) != "0h00" {
		t.Error("FmtMinutes")
	}
}

func TestCheckDescription(t *testing.T) {
	if _, err := CheckDescription(""); err == nil || err.Error() != "A description is required." {
		t.Errorf("empty: %v", err)
	}
	if _, err := CheckDescription(strings.Repeat("é", 255)); err != nil {
		t.Errorf("255 runes rejected: %v", err)
	}
	if _, err := CheckDescription(strings.Repeat("x", 256)); err == nil || err.Error() != "Too long (256 characters), keep it to 255." {
		t.Errorf("256: %v", err)
	}
}

func TestGrouping(t *testing.T) {
	base := "/t/timesheets/TimesheetUpload.xlsx"
	cases := []struct {
		group string
		date  time.Time
		want  string
	}{
		{"week", Date(2026, 11, 13), "/t/timesheets/2026_November/09-15_November.xlsx"},
		{"week", Date(2026, 11, 1), "/t/timesheets/2026_October/26-01_October.xlsx"},  // Sunday: Monday is in October
		{"week", Date(2026, 10, 26), "/t/timesheets/2026_October/26-01_October.xlsx"}, // same week, same file
		{"week", Date(2027, 1, 2), "/t/timesheets/2026_December/28-03_December.xlsx"}, // across the year
		{"month", Date(2026, 11, 1), "/t/timesheets/2026_November/2026-11.xlsx"},
		{"month", Date(2026, 10, 31), "/t/timesheets/2026_October/2026-10.xlsx"},
	}
	for _, c := range cases {
		if got := GroupedPath(base, c.group, c.date); got != c.want {
			t.Errorf("GroupedPath(%s, %s) = %s, want %s", c.group, c.date.Format("2006-01-02"), got, c.want)
		}
	}
	if Weekday(Date(2026, 11, 13)) != 4 || !Monday(Date(2026, 11, 15)).Equal(Date(2026, 11, 9)) {
		t.Error("Weekday/Monday")
	}
	if Serial(Date(2026, 11, 13)) != 46339 || !FromSerial(46339).Equal(Date(2026, 11, 13)) {
		t.Errorf("Serial = %d", Serial(Date(2026, 11, 13)))
	}
}

func TestOutputPathMatchesOsheet(t *testing.T) {
	dir := t.TempDir()
	dates := []string{"2026-11-13", "2026-11-01", "2026-10-26", "2027-01-02", "2026-02-28", "2028-02-29"}
	for _, group := range []string{"week", "month", ""} {
		cfgPath := filepath.Join(dir, "config-"+group+".json")
		raw := map[string]any{"output": "~/timesheets/TimesheetUpload.xlsx", "projects": map[string]any{"A": map[string]any{}}}
		if group != "" {
			raw["group_by"] = group
		}
		b, _ := json.Marshal(raw)
		os.WriteFile(cfgPath, b, 0o644)
		cfg, err := loadConfig(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		// osheet runs with its own throwaway HOME; compare relative to it
		out := osheet(t, []string{"HOME=/h"}, append([]string{"paths", cfgPath}, dates...)...)
		var want []string
		json.Unmarshal([]byte(out), &want)
		t.Setenv("HOME", "/h")
		for i, d := range dates {
			day, _ := time.Parse("2006-01-02", d)
			if got := cfg.OutputPath(day); got != want[i] {
				t.Errorf("group %q, %s: got %s, osheet %s", group, d, got, want[i])
			}
		}
		t.Setenv("OSHEET_FILE", "~/one.xlsx")
		if got := cfg.OutputPath(Date(2026, 1, 1)); got != "/h/one.xlsx" {
			t.Errorf("OSHEET_FILE: %s", got)
		}
		os.Unsetenv("OSHEET_FILE")
	}
}

func TestConfigAndAsking(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"output": "~/ts/x.xlsx", "group_by": "week",
	  "defaults": {"project": "B", "category": "c1", "location": "Home", "sentiment": "Neutral", "time": 1.5},
	  "ask_every_time": {"day": false, "project": true, "time": null, "location": 0},
	  "projects": {"Z": {"billable": "Yes", "categories": ["z1"]}, "B": {"billable": "No", "categories": ["c1", "c2"]}, "A": {"billable": "No", "categories": ["a1"]}},
	  "locations": ["Home", "Office"]}`), 0o644)
	cfg, err := loadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ProjectNames(); !reflect.DeepEqual(got, []string{"Z", "B", "A"}) {
		t.Errorf("project order %v", got)
	}
	if cfg.Defaults.Time != "1.5" || cfg.Project("Z").Billable != "Yes" {
		t.Errorf("defaults %+v", cfg.Defaults)
	}
	want := map[string]bool{"day": false, "project": true, "category": true, "time": true, "location": true, "sentiment": false}
	if got := cfg.Asking(); !reflect.DeepEqual(got, want) {
		t.Errorf("Asking = %v", got)
	}
	if !reflect.DeepEqual(cfg.SentimentOptions(), DefaultSentiments) {
		t.Error("default sentiments")
	}
	if _, err := loadConfig(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("missing config loaded")
	}
}

// --- workbook writes ---------------------------------------------------------

func values(date time.Time, project string, h, m int, desc string) Values {
	return Values{"Date": date, "Project": project, "Category": "Software Development", "Hours": h, "Minutes": m,
		"Billable": "No", "Description": desc, "TicketNumber": "", "Sentiment": "Neutral", "WorkedFrom": "Home"}
}

func descs(t *testing.T, p string) []string {
	t.Helper()
	entries, err := ReadEntries(p)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, fmt.Sprintf("%s %s %s", e.Date.Format("01-02"), FmtMinutes(e.Total()), e.Get("Description")))
	}
	return out
}

func TestAppendUpdateDelete(t *testing.T) {
	for name, tpl := range templates(t) {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "wk.xlsx")
			d := Date(2026, 11, 9)
			if err := appendRows(p, tpl, []Values{values(d, "Acme", 1, 30, "first & <one>")}); err != nil {
				t.Fatal(err)
			}
			checkConsistent(t, p, 2) // placeholder row replaced
			if err := appendRows(p, tpl, []Values{values(d.AddDate(0, 0, 1), "Acme", 2, 0, "second"), values(d.AddDate(0, 0, 2), "Acme", 0, 15, "  third  ")}); err != nil {
				t.Fatal(err)
			}
			checkConsistent(t, p, 4)
			if got, want := descs(t, p), []string{"11-09 1h30 first & <one>", "11-10 2h00 second", "11-11 0h15   third  "}; !reflect.DeepEqual(got, want) {
				t.Errorf("after append: %q, want %q", got, want)
			}
			// date style added once and reused
			parts := zipParts(t, p)
			if n := strings.Count(parts["xl/styles.xml"], `numFmtId="14"`); n != 1 {
				t.Errorf("%d date styles", n)
			}
			if !strings.Contains(parts["xl/worksheets/sheet1.xml"], `<c r="A2" s="2"><v>46335</v></c>`) {
				t.Errorf("date cell not a serial in the date style")
			}

			// update in place keeps the row
			if err := updateRow(p, tpl, 2, values(d.AddDate(0, 0, 3), "Acme", 3, 45, "second, edited")); err != nil {
				t.Fatal(err)
			}
			checkConsistent(t, p, 4)
			if got := descs(t, p)[1]; got != "11-12 3h45 second, edited" {
				t.Errorf("after update: %q", got)
			}

			// delete the first: the rest move up
			if err := updateRow(p, tpl, 1, nil); err != nil {
				t.Fatal(err)
			}
			checkConsistent(t, p, 3)
			if got, want := descs(t, p), []string{"11-12 3h45 second, edited", "11-11 0h15   third  "}; !reflect.DeepEqual(got, want) {
				t.Errorf("after delete: %q, want %q", got, want)
			}
			updateRow(p, tpl, 2, nil)
			checkConsistent(t, p, 2)
			// deleting the only entry restores the template
			if err := updateRow(p, tpl, 1, nil); err != nil {
				t.Fatal(err)
			}
			a, _ := os.ReadFile(p)
			b, _ := os.ReadFile(tpl)
			if string(a) != string(b) {
				t.Error("deleting the last entry didn't restore the template")
			}
			if len(descs(t, p)) != 0 {
				t.Error("entries left")
			}
		})
	}
}

// writeConfig makes an osheet config in the test's HOME and returns it loaded.
func writeConfig(t *testing.T, tpl, group string) *Config {
	t.Helper()
	dir := ConfigDir()
	os.MkdirAll(dir, 0o755)
	data, _ := os.ReadFile(tpl)
	os.WriteFile(TemplateCopyPath(), data, 0o644)
	raw := map[string]any{
		"template": tpl, "output": filepath.Join(t.TempDir(), "timesheets", "TimesheetUpload.xlsx"), "group_by": group,
		"defaults": map[string]any{"project": "Acme", "category": "Software Development", "location": "Home", "sentiment": "Neutral"},
		"projects": map[string]any{"Acme": map[string]any{"billable": "Yes", "categories": []string{"Software Development"}}},
	}
	b, _ := json.Marshal(raw)
	os.WriteFile(ConfigPath(), b, 0o644)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestSaveUpdateMove(t *testing.T) {
	for name, tpl := range templates(t) {
		t.Run(name, func(t *testing.T) {
			cfg := writeConfig(t, tpl, "month")
			// Week of Mon 26 Oct – Sun 1 Nov 2026 spans two month files.
			a := Answers{Date: Date(2026, 10, 30), Project: "Acme", Category: "Software Development", Location: "Home",
				Sentiment: "Neutral", Description: "friday", Hours: 1}
			p1, row, err := cfg.Save(cfg.Values(a, ""))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(p1, "2026_October/2026-10.xlsx") || row != 1 {
				t.Fatalf("saved to %s row %d", p1, row)
			}
			b := a
			b.Description = "thursday"
			b.Date = Date(2026, 10, 29)
			cfg.Save(cfg.Values(b, ""))
			entries, _ := cfg.EntriesBetween(Date(2026, 10, 26), Date(2026, 11, 1))
			if len(entries) != 2 || entries[0].Get("Billable") != "Yes" {
				t.Fatalf("entries %+v", entries)
			}
			// keep the ticket number when editing: give the first one a ticket via osheet
			first := entries[0]
			// move the Friday entry to Sunday 1 Nov: another month's file
			moved := a
			moved.Date = Date(2026, 11, 1)
			moved.Description = "sunday"
			p2, row2, err := cfg.Update(first, cfg.Values(moved, "T-42"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(p2, "2026_November/2026-11.xlsx") || row2 != 1 {
				t.Fatalf("moved to %s row %d", p2, row2)
			}
			checkConsistent(t, p1, 2)
			checkConsistent(t, p2, 2)
			if got := descs(t, p1); !reflect.DeepEqual(got, []string{"10-29 1h00 thursday"}) {
				t.Errorf("old file: %q", got)
			}
			nov, _ := ReadEntries(p2)
			if len(nov) != 1 || nov[0].Get("TicketNumber") != "T-42" || nov[0].Get("Description") != "sunday" {
				t.Errorf("new file: %+v", nov)
			}
			// update within the same file stays in place
			entries, _ = cfg.EntriesBetween(Date(2026, 10, 26), Date(2026, 11, 1))
			if len(entries) != 2 {
				t.Fatalf("%d entries across files", len(entries))
			}
			sum, total := Summarise(entries, Date(2026, 10, 26), Date(2026, 11, 1))
			if total != 120 || len(sum) != 1 || sum[0].Categories[0].Minutes != 120 {
				t.Errorf("summary %+v %d", sum, total)
			}
			if err := cfg.Delete(entries[0]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSummariseOrder(t *testing.T) {
	e := func(p, c string, m int) Entry {
		return Entry{Date: Date(2026, 11, 10), Minutes: m, Fields: map[string]string{"Project": p, "Category": c}}
	}
	entries := []Entry{e("A", "x", 30), e("B", "y", 60), e("A", "z", 45), e("C", "q", 75), e("A", "x", 15)}
	sum, total := Summarise(entries, Date(2026, 11, 9), Date(2026, 11, 15))
	if total != 225 {
		t.Errorf("total %d", total)
	}
	var got []string
	for _, p := range sum {
		for _, c := range p.Categories {
			got = append(got, fmt.Sprintf("%s/%s=%d", p.Name, c.Name, c.Minutes))
		}
	}
	want := []string{"A/x=45", "A/z=45", "C/q=75", "B/y=60"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

// --- interop with osheet ----------------------------------------------------

type pyEntry map[string]any

func osheetRead(t *testing.T, p string) []pyEntry {
	t.Helper()
	var out []pyEntry
	if err := json.Unmarshal([]byte(osheet(t, nil, "read", p)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOsheetReadsLazysheetFiles(t *testing.T) {
	for name, tpl := range templates(t) {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "wk.xlsx")
			appendRows(p, tpl, []Values{
				values(Date(2026, 11, 9), "Acme", 1, 30, `quotes " and 'apostrophes' & ünïcödé`),
				values(Date(2026, 11, 10), "Acme", 8, 45, "second"),
			})
			v := values(Date(2026, 11, 11), "Acme", 0, 15, "third")
			v["TicketNumber"] = "T-1"
			appendRows(p, tpl, []Values{v})
			updateRow(p, tpl, 2, values(Date(2026, 11, 12), "Acme", 2, 0, "edited"))
			updateRow(p, tpl, 1, nil)
			got := osheetRead(t, p)
			if len(got) != 2 {
				t.Fatalf("osheet read %d entries: %v", len(got), got)
			}
			check := func(e pyEntry, date string, h, m float64, desc, ticket string, row float64) {
				if e["Date"] != date || e["Hours"] != h || e["Minutes"] != m || e["Description"] != desc || e["_row"] != row {
					t.Errorf("osheet read %v; want %s %v:%v %q row %v", e, date, h, m, desc, row)
				}
				if tk, _ := e["TicketNumber"].(string); tk != ticket {
					t.Errorf("ticket %q want %q", tk, ticket)
				}
				if e["Billable"] != "No" || e["WorkedFrom"] != "Home" || e["Sentiment"] != "Neutral" || e["Project"] != "Acme" {
					t.Errorf("columns %v", e)
				}
			}
			check(got[0], "2026-11-12", 2, 0, "edited", "", 1)
			check(got[1], "2026-11-11", 0, 15, "third", "T-1", 2)
		})
	}
}

func TestLazysheetReadsOsheetFiles(t *testing.T) {
	for name, tpl := range templates(t) {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "wk.xlsx")
			entries := `[{"Date": "2026-11-09", "Project": "Acme", "Category": "Software Development", "Hours": 1, "Minutes": 15, "Billable": "Yes", "Description": "from osheet <&>", "TicketNumber": "", "Sentiment": "Happy", "WorkedFrom": "Home"},
			             {"Date": "2026-11-13", "Project": "Acme", "Category": "Software Development", "Hours": 0, "Minutes": 45, "Billable": "Yes", "Description": "second", "TicketNumber": "X-9", "Sentiment": "Neutral", "WorkedFrom": "Client"}]`
			osheet(t, nil, "append", p, tpl, entries)
			checkConsistent(t, p, 3)
			got, err := ReadEntries(p)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[0].Get("Description") != "from osheet <&>" || got[0].Total() != 75 || !got[0].Date.Equal(Date(2026, 11, 9)) ||
				got[1].Get("TicketNumber") != "X-9" || got[1].Get("WorkedFrom") != "Client" || got[1].Row != 2 {
				t.Fatalf("read %+v", got)
			}
			// mixed history: lazysheet edits a file osheet wrote, then osheet edits it again
			if err := updateRow(p, tpl, 1, values(Date(2026, 11, 10), "Acme", 3, 0, "lazysheet edit")); err != nil {
				t.Fatal(err)
			}
			osheet(t, nil, "update", p, tpl, "2", `{"Date": "2026-11-14", "Project": "Acme", "Category": "Software Development", "Hours": 1, "Minutes": 0, "Billable": "Yes", "Description": "osheet edit", "TicketNumber": "X-9", "Sentiment": "Neutral", "WorkedFrom": "Home"}`)
			appendRows(p, tpl, []Values{values(Date(2026, 11, 15), "Acme", 0, 30, "lazysheet again")})
			checkConsistent(t, p, 4)
			if got, want := descs(t, p), []string{"11-10 3h00 lazysheet edit", "11-14 1h00 osheet edit", "11-15 0h30 lazysheet again"}; !reflect.DeepEqual(got, want) {
				t.Errorf("mixed: %q want %q", got, want)
			}
			if n := len(osheetRead(t, p)); n != 3 {
				t.Errorf("osheet reads %d", n)
			}
		})
	}
}

// TestByteIdenticalToOsheet checks that lazysheet and osheet produce the same
// XML for the same sequence of writes.
func TestByteIdenticalToOsheet(t *testing.T) {
	for name, tpl := range templates(t) {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			goP, pyP := filepath.Join(dir, "go.xlsx"), filepath.Join(dir, "py.xlsx")
			j := func(date, desc string, h, m int, ticket string) string {
				return fmt.Sprintf(`{"Date": %q, "Project": "Acme", "Category": "Software Development", "Hours": %d, "Minutes": %d, "Billable": "No", "Description": %q, "TicketNumber": %q, "Sentiment": "Neutral", "WorkedFrom": "Home"}`, date, h, m, desc, ticket)
			}
			gv := func(date, desc string, h, m int, ticket string) Values {
				d, _ := time.Parse("2006-01-02", date)
				v := values(d, "Acme", h, m, desc)
				v["TicketNumber"] = ticket
				return v
			}
			appendRows(goP, tpl, []Values{gv("2026-11-09", "a & b", 1, 0, ""), gv("2026-11-10", "new string", 0, 0, "T1")})
			osheet(t, nil, "append", pyP, tpl, "["+j("2026-11-09", "a & b", 1, 0, "")+","+j("2026-11-10", "new string", 0, 0, "T1")+"]")
			updateRow(goP, tpl, 1, gv("2026-11-11", "changed", 2, 15, ""))
			osheet(t, nil, "update", pyP, tpl, "1", j("2026-11-11", "changed", 2, 15, ""))
			appendRows(goP, tpl, []Values{gv("2026-11-12", "third", 0, 45, "")})
			osheet(t, nil, "append", pyP, tpl, "["+j("2026-11-12", "third", 0, 45, "")+"]")
			updateRow(goP, tpl, 2, nil)
			osheet(t, nil, "update", pyP, tpl, "2")
			g, p := zipParts(t, goP), zipParts(t, pyP)
			if len(g) != len(p) {
				t.Fatalf("%d parts vs %d", len(g), len(p))
			}
			for name, body := range p {
				if g[name] != body {
					t.Errorf("%s differs:\n go: %.400s\n py: %.400s", name, g[name], body)
				}
			}
		})
	}
}
