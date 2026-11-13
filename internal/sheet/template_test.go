package sheet

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dewald-L/lazysheet/internal/sheet/sheettest"
)

// The tests build a small template with the same structure as the real
// TimesheetUpload.xlsx (see sheettest). Set LAZYSHEET_TEMPLATE to also run
// them against a real template.

const bomStr = sheettest.BOM

func writeSynthTemplate(t *testing.T, p string) {
	t.Helper()
	if err := sheettest.WriteTemplate(p); err != nil {
		t.Fatal(err)
	}
}

// templates returns the templates to run a test against: the synthetic one,
// plus $LAZYSHEET_TEMPLATE when set (copied, never written to).
func templates(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	out := map[string]string{"synthetic": filepath.Join(dir, "synthetic.xlsx")}
	writeSynthTemplate(t, out["synthetic"])
	if real := os.Getenv("LAZYSHEET_TEMPLATE"); real != "" {
		data, err := os.ReadFile(real)
		if err != nil {
			t.Fatalf("LAZYSHEET_TEMPLATE: %v", err)
		}
		p := filepath.Join(dir, "real.xlsx")
		os.WriteFile(p, data, 0o644)
		out["real"] = p
	}
	return out
}

// --- workbook consistency ----------------------------------------------------

func zipParts(t *testing.T, p string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(p)
	if err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	defer r.Close()
	out := map[string]string{}
	for _, f := range r.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

var (
	reRowNum   = regexp.MustCompile(`<row r="(\d+)"`)
	reCellRef  = regexp.MustCompile(`<c r="([A-Z]+)(\d+)"`)
	reDimRef   = regexp.MustCompile(`<dimension ref="A1:([A-Z]+)(\d+)"`)
	reSqrefAll = regexp.MustCompile(`sqref="([A-Z]+)2:([A-Z]+)(\d+)"`)
	reRefAll   = regexp.MustCompile(` ref="([A-Z]+)1:([A-Z]+)(\d+)"`)
	reSIIndex  = regexp.MustCompile(`t="s"><v>(\d+)</v>`)
)

// checkConsistent asserts that every XML part parses and that the entry
// sheet's rows, dimension, validations, table refs and shared strings agree.
func checkConsistent(t *testing.T, p string, wantRows int) {
	t.Helper()
	parts := zipParts(t, p)
	for name, body := range parts {
		if !strings.HasSuffix(name, ".xml") && !strings.HasSuffix(name, ".rels") {
			continue
		}
		d := xml.NewDecoder(strings.NewReader(strings.TrimPrefix(body, bomStr)))
		for {
			if _, err := d.Token(); err != nil {
				if err != io.EOF {
					t.Errorf("%s: %s is not well-formed: %v", p, name, err)
				}
				break
			}
		}
	}
	wb, err := openWorkbook(p)
	if err != nil {
		t.Fatal(err)
	}
	sheetPath, tables, err := wb.entrySheetParts()
	if err != nil {
		t.Fatal(err)
	}
	sheetXML := parts[sheetPath]
	nums := reRowNum.FindAllStringSubmatch(sheetXML, -1)
	if len(nums) != wantRows {
		t.Errorf("%s: %d rows, want %d", p, len(nums), wantRows)
	}
	for i, m := range nums {
		if m[1] != itoa(i+1) {
			t.Errorf("%s: row %d is numbered %s", p, i+1, m[1])
		}
	}
	end := itoa(len(nums))
	if m := reDimRef.FindStringSubmatch(sheetXML); m == nil || m[2] != end {
		t.Errorf("%s: dimension %v, want rows to %s", p, m, end)
	}
	for _, m := range reSqrefAll.FindAllStringSubmatch(sheetXML, -1) {
		if m[1] == m[2] && m[3] != end {
			t.Errorf("%s: sqref %s2:%s%s, want end %s", p, m[1], m[2], m[3], end)
		}
	}
	for _, tbl := range tables {
		for _, m := range reRefAll.FindAllStringSubmatch(parts[tbl], -1) {
			if m[3] != end {
				t.Errorf("%s: %s ref ends at %s, want %s", p, tbl, m[3], end)
			}
		}
	}
	// every cell's row matches its <row>
	for _, row := range regexp.MustCompile(`(?s)<row r="(\d+)"[^>]*?(?:/>|>.*?</row>)`).FindAllStringSubmatch(sheetXML, -1) {
		for _, c := range reCellRef.FindAllStringSubmatch(row[0], -1) {
			if c[2] != row[1] {
				t.Errorf("%s: cell %s%s in row %s", p, c[1], c[2], row[1])
			}
		}
	}
	sst := parts["xl/sharedStrings.xml"]
	n := strings.Count(sst, "<si>")
	if !strings.Contains(sst, `uniqueCount="`+itoa(n)+`"`) || !strings.Contains(sst, ` count="`+itoa(n)+`"`) {
		t.Errorf("%s: sharedStrings counts don't match %d strings", p, n)
	}
	for _, m := range reSIIndex.FindAllStringSubmatch(sheetXML, -1) {
		if atoi(m[1]) >= n {
			t.Errorf("%s: shared string index %s out of range (%d)", p, m[1], n)
		}
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
func atoi(s string) int { var n int; json.Unmarshal([]byte(s), &n); return n }

// --- osheet bridge -----------------------------------------------------------

// osheet runs osheet's own Python code through testdata/osheet_interop.py.
// The test is skipped when python3 or osheet (OSHEET_SRC, default
// ~/Projects/osheet/osheet) isn't available.
func osheet(t *testing.T, env []string, args ...string) string {
	t.Helper()
	src := os.Getenv("OSHEET_SRC")
	if src == "" {
		src = ExpandUser("~/Projects/osheet/osheet")
	}
	if _, err := os.Stat(src); err != nil {
		t.Skipf("osheet not found at %s (set OSHEET_SRC)", src)
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found")
	}
	bridge, _ := filepath.Abs("../../testdata/osheet_interop.py")
	cmd := exec.Command(py, append([]string{"-I", bridge, src}, args...)...)
	home := t.TempDir()
	cmd.Env = []string{"HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "PATH=" + os.Getenv("PATH")}
	cmd.Env = append(cmd.Env, env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("osheet %v: %v\n%s", args, err, stderr.String())
	}
	return string(out)
}
