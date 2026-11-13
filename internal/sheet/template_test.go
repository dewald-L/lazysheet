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
)

// The tests build a small template with the same structure as the real
// TimesheetUpload.xlsx (BOM-prefixed parts, a placeholder row 2, per-column
// data validations, a TimesheetEntry table, no date style yet). Set
// LAZYSHEET_TEMPLATE to also run them against a real template.

const bomStr = "\xef\xbb\xbf"

var synthParts = []struct{ name, body string }{
	{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default ContentType="application/xml" Extension="xml"/><Default ContentType="application/vnd.openxmlformats-package.relationships+xml" Extension="rels"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml" PartName="/xl/workbook.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml" PartName="/xl/worksheets/sheet1.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml" PartName="/xl/worksheets/sheet2.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml" PartName="/xl/worksheets/sheet3.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml" PartName="/xl/styles.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml" PartName="/xl/sharedStrings.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.table+xml" PartName="/xl/tables/table2.xml"/></Types>`},
	{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
	{"xl/workbook.xml", bomStr + `<?xml version="1.0" encoding="utf-8"?><workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><bookViews><workbookView /></bookViews><sheets><sheet name="TimesheetUpload" sheetId="1" r:id="rId1" /><sheet name="Lookup" sheetId="2" r:id="rId2" /><sheet name="Validation" sheetId="3" r:id="rId4" state="veryHidden" /></sheets></workbook>`},
	{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/><Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet3.xml"/><Relationship Id="rId5" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/></Relationships>`},
	{"xl/worksheets/sheet1.xml", bomStr + `<?xml version="1.0" encoding="utf-8"?><worksheet xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><dimension ref="A1:J2" /><sheetData><row r="1"><c r="A1" s="0" t="s"><v>0</v></c><c r="B1" s="0" t="s"><v>1</v></c><c r="C1" s="0" t="s"><v>2</v></c><c r="D1" s="0" t="s"><v>3</v></c><c r="E1" s="0" t="s"><v>4</v></c><c r="F1" s="0" t="s"><v>5</v></c><c r="G1" s="0" t="s"><v>6</v></c><c r="H1" s="0" t="s"><v>7</v></c><c r="I1" s="0" t="s"><v>8</v></c><c r="J1" s="0" t="s"><v>9</v></c></row><row r="2"><c r="F2" s="0" t="s"><v>10</v></c><c r="I2" s="0" t="s"><v>11</v></c></row></sheetData><dataValidations count="3"><dataValidation type="date" sqref="A2:A2" operator="between"><formula1>36526</formula1><formula2>73415</formula2></dataValidation><dataValidation type="whole" sqref="D2:D2" operator="between"><formula1>0</formula1><formula2>24</formula2></dataValidation><dataValidation type="list" sqref="J2:J2"><formula1>=INDIRECT($J$1)</formula1></dataValidation></dataValidations><tableParts><tablePart r:id="rId1" /></tableParts></worksheet>`},
	{"xl/worksheets/_rels/sheet1.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/table" Target="../tables/table2.xml"/></Relationships>`},
	{"xl/worksheets/sheet2.xml", bomStr + `<?xml version="1.0" encoding="utf-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><dimension ref="A1:C3" /><sheetData><row r="1"><c r="A1" t="s"><v>1</v></c><c r="B1" t="s"><v>2</v></c><c r="C1" t="s"><v>5</v></c></row><row r="2"><c r="A2" t="s"><v>12</v></c><c r="B2" t="s"><v>13</v></c><c r="C2" t="s"><v>14</v></c></row></sheetData></worksheet>`},
	{"xl/worksheets/sheet3.xml", bomStr + `<?xml version="1.0" encoding="utf-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><dimension ref="A1:A2" /><sheetData><row r="1"><c r="A1" t="s"><v>9</v></c></row><row r="2"><c r="A2" t="s"><v>15</v></c></row></sheetData></worksheet>`},
	{"xl/tables/table2.xml", bomStr + "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n<table xmlns=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\" id=\"2\" name=\"TimesheetEntry\" displayName=\"TimesheetEntry\" ref=\"A1:J2\" headerRowCount=\"1\">\n  <autoFilter ref=\"A1:J2\" />\n  <tableColumns count=\"10\"><tableColumn id=\"1\" name=\"Date\" /><tableColumn id=\"2\" name=\"Project\" /><tableColumn id=\"3\" name=\"Category\" /><tableColumn id=\"4\" name=\"Hours\" /><tableColumn id=\"5\" name=\"Minutes\" /><tableColumn id=\"6\" name=\"Billable\" /><tableColumn id=\"7\" name=\"Description\" /><tableColumn id=\"8\" name=\"TicketNumber\" /><tableColumn id=\"9\" name=\"Sentiment\" /><tableColumn id=\"10\" name=\"WorkedFrom\" /></tableColumns>\n</table>"},
	{"xl/styles.xml", `<?xml version="1.0" encoding="utf-8"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="1"><font /></fonts><fills count="1"><fill /></fills><borders count="1"><border /></borders><cellStyleXfs count="1"><xf numFmtId="0" /></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" applyNumberFormat="1" fontId="0" applyFont="1" xfId="0" applyProtection="1" /><xf numFmtId="0" applyNumberFormat="1" fontId="0" applyFont="1" xfId="0" applyAlignment="1"><alignment vertical="top" /></xf></cellXfs></styleSheet>`},
	{"xl/sharedStrings.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="16" uniqueCount="16"><si><t>Date</t></si><si><t>Project</t></si><si><t>Category</t></si><si><t>Hours</t></si><si><t>Minutes</t></si><si><t>Billable</t></si><si><t>Description</t></si><si><t>TicketNumber</t></si><si><t>Sentiment</t></si><si><t>WorkedFrom</t></si><si><t>No</t></si><si><t>Neutral</t></si><si><t>Acme</t></si><si><t>Software Development</t></si><si><t>Yes</t></si><si><t>Home</t></si></sst>`},
}

func writeSynthTemplate(t *testing.T, p string) {
	t.Helper()
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, part := range synthParts {
		w, err := zw.Create(part.name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, part.body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
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
