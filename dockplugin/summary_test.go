package dockplugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dewald-L/dock/plugin"

	"github.com/dewald-L/lazysheet/internal/sheet"
	"github.com/dewald-L/lazysheet/internal/sheet/sheettest"
)

// TestMain points HOME and XDG_CONFIG_HOME at a throwaway directory, so no
// test can reach the real ~/.config/osheet or ~/timesheets.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "lazysheet-dockplugin-test")
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

// setup writes an osheet config (and template) into the test HOME, with
// timesheets grouped by groupBy, and returns it. It is removed after the test.
func setup(t *testing.T, groupBy string) *sheet.Config {
	t.Helper()
	if !strings.HasPrefix(sheet.ConfigDir(), os.Getenv("HOME")) {
		t.Fatalf("refusing to touch %s", sheet.ConfigDir())
	}
	if err := os.MkdirAll(sheet.ConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sheet.ConfigDir()) })
	if err := sheettest.WriteTemplate(sheet.TemplateCopyPath()); err != nil {
		t.Fatal(err)
	}
	raw := map[string]any{
		"template": sheet.TemplateCopyPath(),
		"output":   filepath.Join(t.TempDir(), "timesheets", "TimesheetUpload.xlsx"),
		"group_by": groupBy,
		"defaults": map[string]any{"project": "Acme", "category": "Software Development", "location": "Home"},
		"projects": map[string]any{"Acme": map[string]any{"billable": "Yes", "categories": []string{"Software Development"}}},
	}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(sheet.ConfigPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := sheet.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func logTime(t *testing.T, cfg *sheet.Config, day time.Time, minutes int) {
	t.Helper()
	a := sheet.Answers{Date: day, Project: "Acme", Category: "Software Development", Location: "Home",
		Description: "work", Hours: minutes / 60, Minutes: minutes % 60}
	if _, _, err := cfg.Save(cfg.Values(a, "")); err != nil {
		t.Fatal(err)
	}
}

func TestSummaryWithoutConfig(t *testing.T) {
	s, err := Plugin{}.Summary(plugin.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Lines) != 1 || !strings.Contains(s.Lines[0], "osheet --setup") || s.Level != plugin.LevelNone {
		t.Fatalf("no config should say to run osheet --setup: %+v", s)
	}
}

func TestSummaryBrokenConfig(t *testing.T) {
	setup(t, "week")
	os.WriteFile(sheet.ConfigPath(), []byte(`{"output": "x.xlsx", "projects": {}}`), 0o644)
	if _, err := summary(sheet.Date(2026, 11, 11)); err == nil || !strings.Contains(err.Error(), "osheet --setup") {
		t.Fatalf("a config without projects is an error: %v", err)
	}
}

func TestSummaryWeek(t *testing.T) {
	cfg := setup(t, "week")
	wed := sheet.Date(2026, 11, 11)
	logTime(t, cfg, sheet.Date(2026, 11, 8), 180)  // previous Sunday: not this week
	logTime(t, cfg, sheet.Date(2026, 11, 9), 480)  // Mon
	logTime(t, cfg, sheet.Date(2026, 11, 10), 240) // Tue
	logTime(t, cfg, sheet.Date(2026, 11, 10), 30)
	logTime(t, cfg, wed, 75)
	logTime(t, cfg, sheet.Date(2026, 11, 16), 60) // next Monday

	s, err := summary(wed)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Today 1h15 · this week 13h45 / 40h", "Mo█ Tu▅ We▂ Th· Fr· Sa· Su·"}
	if strings.Join(s.Lines, "\n") != strings.Join(want, "\n") || s.Level != plugin.LevelNone {
		t.Fatalf("got %q level %v, want %q", s.Lines, s.Level, want)
	}

	// exactly 8h is fine; past 8h is a warning
	logTime(t, cfg, wed, 405)
	if s, _ = summary(wed); s.Level != plugin.LevelNone || !strings.HasPrefix(s.Lines[0], "Today 8h00") {
		t.Fatalf("8h00 today: %q %v", s.Lines, s.Level)
	}
	logTime(t, cfg, wed, 15)
	if s, _ = summary(wed); s.Level != plugin.LevelWarn || !strings.HasPrefix(s.Lines[0], "Today 8h15 · this week 20h45") {
		t.Fatalf("8h15 today should warn: %q %v", s.Lines, s.Level)
	}
	if got := s.Clean(); len(got.Lines) != len(s.Lines) {
		t.Fatalf("the summary must already be clean plain text: %q", s.Lines)
	}
}

func TestSummaryWeekAcrossMonthFiles(t *testing.T) {
	cfg := setup(t, "month")
	sun := sheet.Date(2026, 11, 1) // week of Mon 26 Oct
	logTime(t, cfg, sheet.Date(2026, 10, 30), 120)
	logTime(t, cfg, sun, 45)
	s, err := summary(sun)
	if err != nil {
		t.Fatal(err)
	}
	if s.Lines[0] != "Today 0h45 · this week 2h45 / 40h" || s.Lines[1] != "Mo· Tu· We· Th· Fr▂ Sa· Su▁" {
		t.Fatalf("both month files count: %q", s.Lines)
	}
}

func TestSummaryEmptyWeekAndConcurrency(t *testing.T) {
	cfg := setup(t, "week")
	today := sheet.Date(2026, 11, 13)
	s, err := summary(today)
	if err != nil || s.Lines[0] != "Today 0h00 · this week 0h00 / 40h" || s.Lines[1] != "Mo· Tu· We· Th· Fr· Sa· Su·" {
		t.Fatalf("empty week: %q %v", s.Lines, err)
	}
	logTime(t, cfg, today, 60)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s, err := summary(today); err != nil || !strings.HasPrefix(s.Lines[0], "Today 1h00") {
				t.Errorf("concurrent call: %q %v", s.Lines, err)
			}
		}()
	}
	wg.Wait()
}

func TestDayBar(t *testing.T) {
	got := dayBar([7]int{1, 60, 61, 240, 480, 600, 0})
	if want := "Mo▁ Tu▁ We▂ Th▄ Fr█ Sa█ Su·"; got != want {
		t.Fatalf("dayBar = %q, want %q", got, want)
	}
}
