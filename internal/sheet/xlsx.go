package sheet

// Excel I/O, ported from osheet. The workbook is treated as a zip of XML and
// edited as text, never re-serialised, so the template's validations, table
// and styles survive untouched. Keep these consistent on every write or Excel
// reports the file as corrupt: the entry sheet's rows, sharedStrings.xml, the
// <dimension> ref, the data-validation sqrefs, the table's ref, and dates as
// serial numbers in a date style.

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const relNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

// ExcelEpoch is day 0 of Excel's date serials.
var ExcelEpoch = Date(1899, 12, 30)

// Serial is d as an Excel date serial number.
func Serial(d time.Time) int { return int(d.Sub(ExcelEpoch).Hours() / 24) }

// FromSerial is the date for an Excel serial number.
func FromSerial(n int) time.Time { return ExcelEpoch.AddDate(0, 0, n) }

// Values is one row keyed by column header: time.Time for dates, int for
// numbers, string for text. Empty strings and nil are left out of the row.
type Values map[string]any

type workbook struct {
	order []string // zip member order
	mtime map[string]time.Time
	files map[string][]byte
}

func openWorkbook(src string) (*workbook, error) {
	r, err := zip.OpenReader(src)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	wb := &workbook{mtime: map[string]time.Time{}, files: map[string][]byte{}}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		wb.order = append(wb.order, f.Name)
		wb.mtime[f.Name] = f.Modified
		wb.files[f.Name] = data
	}
	return wb, nil
}

// loadWorkbook opens path, or template when path doesn't exist yet.
func loadWorkbook(p, template string) (*workbook, error) {
	src := p
	if _, err := os.Stat(p); err != nil {
		src = template
	}
	return openWorkbook(src)
}

func (wb *workbook) part(name string) ([]byte, error) {
	b, ok := wb.files[name]
	if !ok {
		return nil, fmt.Errorf("workbook has no %s", name)
	}
	return b, nil
}

var bom = []byte("\xef\xbb\xbf")

func unmarshal(data []byte, v any) error {
	return xml.Unmarshal(bytes.TrimPrefix(data, bom), v)
}

type xRels struct {
	Rels []struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
		Type   string `xml:"Type,attr"`
	} `xml:"Relationship"`
}

type xWorkbook struct {
	Sheets []struct {
		Name string `xml:"name,attr"`
		RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	} `xml:"sheets>sheet"`
}

type xCell struct {
	R  string  `xml:"r,attr"`
	T  string  `xml:"t,attr"`
	V  *string `xml:"v"`
	Is *struct {
		Inner []byte `xml:",innerxml"`
	} `xml:"is"`
}

type xRow struct {
	R     string  `xml:"r,attr"`
	Cells []xCell `xml:"c"`
}

type xSheet struct {
	Rows []xRow `xml:"sheetData>row"`
}

// entrySheetParts is the timesheet entry sheet (the first sheet that isn't
// Lookup or Validation) and its tables' parts.
func (wb *workbook) entrySheetParts() (string, []string, error) {
	relsData, err := wb.part("xl/_rels/workbook.xml.rels")
	if err != nil {
		return "", nil, err
	}
	var rels xRels
	if err := unmarshal(relsData, &rels); err != nil {
		return "", nil, err
	}
	targets := map[string]string{}
	for _, r := range rels.Rels {
		targets[r.ID] = r.Target
	}
	wbData, err := wb.part("xl/workbook.xml")
	if err != nil {
		return "", nil, err
	}
	var book xWorkbook
	if err := unmarshal(wbData, &book); err != nil {
		return "", nil, err
	}
	for _, s := range book.Sheets {
		if s.Name == "Lookup" || s.Name == "Validation" {
			continue
		}
		target := strings.TrimLeft(targets[s.RID], "/")
		sheet := target
		if !strings.HasPrefix(target, "xl/") {
			sheet = path.Join("xl", target)
		}
		folder, name := path.Dir(sheet), path.Base(sheet)
		var tables []string
		if data, ok := wb.files[path.Join(folder, "_rels", name+".rels")]; ok {
			var srels xRels
			if err := unmarshal(data, &srels); err != nil {
				return "", nil, err
			}
			for _, r := range srels.Rels {
				if strings.HasSuffix(r.Type, "/table") {
					tables = append(tables, path.Clean(path.Join(folder, r.Target)))
				}
			}
		}
		return sheet, tables, nil
	}
	return "", nil, fmt.Errorf("no timesheet entry sheet found")
}

// textInT concatenates the text of every <t> element in data (osheet joins
// all <t> descendants of an <si> or <is>).
func textInT(data []byte) string {
	d := xml.NewDecoder(bytes.NewReader(data))
	var b strings.Builder
	inT := 0
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				inT++
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inT--
			}
		case xml.CharData:
			if inT > 0 {
				b.Write(t)
			}
		}
	}
	return b.String()
}

// sharedStrings lists sharedStrings.xml's strings in index order.
func sharedStrings(data []byte) ([]string, error) {
	d := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, bom)))
	var out []string
	var cur strings.Builder
	inSI, inT := false, 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("sharedStrings.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI = true
				cur.Reset()
			case "t":
				inT++
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				out = append(out, cur.String())
				inSI = false
			case "t":
				inT--
			}
		case xml.CharData:
			if inSI && inT > 0 {
				cur.Write(t)
			}
		}
	}
}

var reColLetters = regexp.MustCompile(`^[A-Z]+`)

func colOf(ref string) string { return reColLetters.FindString(ref) }

// parseRows lists the entry sheet's <row>s.
func parseRows(data []byte) ([]xRow, error) {
	var s xSheet
	if err := unmarshal(data, &s); err != nil {
		return nil, err
	}
	return s.Rows, nil
}

// rowValues maps column letter to value for the cells of a row that have one
// (osheet's read_xlsx).
func rowValues(r xRow, strs []string) (map[string]string, error) {
	row := map[string]string{}
	for _, c := range r.Cells {
		var val string
		switch {
		case c.V != nil:
			val = *c.V
			if c.T == "s" {
				i, err := strconv.Atoi(strings.TrimSpace(val))
				if err != nil || i < 0 || i >= len(strs) {
					return nil, fmt.Errorf("cell %s: bad shared string index %q", c.R, val)
				}
				val = strs[i]
			}
		case c.Is != nil:
			val = textInT(append(append([]byte("<is>"), c.Is.Inner...), "</is>"...))
		default:
			continue
		}
		row[colOf(c.R)] = val
	}
	return row, nil
}

// headerLetters maps column header to column letter from the first row.
func headerLetters(header xRow, strs []string) map[string]string {
	letters := map[string]string{}
	for _, c := range header.Cells {
		if c.V == nil || c.T != "s" {
			continue
		}
		i, err := strconv.Atoi(strings.TrimSpace(*c.V))
		if err != nil || i < 0 || i >= len(strs) {
			continue
		}
		letters[strs[i]] = colOf(c.R)
	}
	return letters
}

func colLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// sstCache looks up and appends shared strings (osheet's shared_string_index).
type sstCache struct {
	built bool
	index map[string]int
	count int
}

func (c *sstCache) indexOf(wb *workbook, text string) (int, error) {
	data, err := wb.part("xl/sharedStrings.xml")
	if err != nil {
		return 0, err
	}
	if !c.built {
		strs, err := sharedStrings(data)
		if err != nil {
			return 0, err
		}
		c.index = map[string]int{}
		for i, s := range strs {
			if _, ok := c.index[s]; !ok {
				c.index[s] = i
			}
		}
		c.count = len(strs)
		c.built = true
	}
	if i, ok := c.index[text]; ok {
		return i, nil
	}
	x := strings.ReplaceAll(string(data), "</sst>",
		`<si><t xml:space="preserve">`+xmlEscape(text)+`</t></si></sst>`)
	wb.files["xl/sharedStrings.xml"] = []byte(x)
	c.index[text] = c.count
	c.count++
	return c.index[text], nil
}

var (
	reCellXfs     = regexp.MustCompile(`(?s)<cellXfs[^>]*>(.*?)</cellXfs>`)
	reXf          = regexp.MustCompile(`(?s)<xf\b[^>]*?(?:/>|>.*?</xf>)`)
	reCellXfCount = regexp.MustCompile(`(<cellXfs[^>]*count=")\d+`)
)

// dateStyleIndex is the index of a cellXfs style with the built-in date
// format (numFmtId 14), adding one if the workbook has none.
func (wb *workbook) dateStyleIndex() (int, error) {
	data, err := wb.part("xl/styles.xml")
	if err != nil {
		return 0, err
	}
	x := string(data)
	m := reCellXfs.FindStringSubmatchIndex(x)
	if m == nil {
		return 0, fmt.Errorf("styles.xml has no cellXfs")
	}
	xfs := reXf.FindAllString(x[m[2]:m[3]], -1)
	for i, xf := range xfs {
		if strings.Contains(xf, `numFmtId="14"`) {
			return i, nil
		}
	}
	const add = `<xf numFmtId="14" applyNumberFormat="1" fontId="0" applyFont="1" xfId="0" />`
	x = x[:m[3]] + add + x[m[3]:]
	x = replaceFirst(reCellXfCount, x, func(sub []string) string {
		return sub[1] + strconv.Itoa(len(xfs)+1)
	})
	wb.files["xl/styles.xml"] = []byte(x)
	return len(xfs), nil
}

// replaceFirst replaces the first match of re in s with repl(submatches).
func replaceFirst(re *regexp.Regexp, s string, repl func([]string) string) string {
	m := re.FindStringSubmatchIndex(s)
	if m == nil {
		return s
	}
	subs := make([]string, len(m)/2)
	for i := range subs {
		if m[2*i] >= 0 {
			subs[i] = s[m[2*i]:m[2*i+1]]
		}
	}
	return s[:m[0]] + repl(subs) + s[m[1]:]
}

// rowXML is the <row> for v as row n.
func (wb *workbook) rowXML(letters map[string]string, n int, v Values, cache *sstCache, dateStyle int) (string, error) {
	headers := make([]string, 0, len(letters))
	for h := range letters {
		headers = append(headers, h)
	}
	sort.Slice(headers, func(i, j int) bool { return colLess(letters[headers[i]], letters[headers[j]]) })
	var b strings.Builder
	fmt.Fprintf(&b, `<row r="%d">`, n)
	for _, h := range headers {
		ref := fmt.Sprintf("%s%d", letters[h], n)
		switch val := v[h].(type) {
		case nil:
		case time.Time:
			fmt.Fprintf(&b, `<c r="%s" s="%d"><v>%d</v></c>`, ref, dateStyle, Serial(val))
		case int:
			fmt.Fprintf(&b, `<c r="%s" s="0"><v>%d</v></c>`, ref, val)
		case string:
			if val == "" {
				continue
			}
			idx, err := cache.indexOf(wb, val)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, `<c r="%s" s="0" t="s"><v>%d</v></c>`, ref, idx)
		default:
			return "", fmt.Errorf("column %s: unsupported value %T", h, val)
		}
	}
	b.WriteString("</row>")
	return b.String(), nil
}

var (
	reDimension = regexp.MustCompile(`(<dimension ref=")[^"]*`)
	reSqref     = regexp.MustCompile(`sqref="([A-Z]+)2:([A-Z]+)\d+"`)
	reTableRef  = regexp.MustCompile(`ref="([A-Z]+)1:([A-Z]+)\d+"`)
	reUnique    = regexp.MustCompile(`uniqueCount="\d+"`)
	reSstCount  = regexp.MustCompile(`(<sst[^>]*?) count="\d+"`)
	reRenumber  = regexp.MustCompile(`(<row r="|<c r="[A-Z]+)(\d+)"`)
)

func rowPattern(n int) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf(`(?s)<row r="%d"[^>]*?(?:/>|>.*?</row>)`, n))
}

// save writes the entry sheet x, with its data running to row end: it
// updates the dimension, the data-validation ranges, the table refs and the
// shared string counts, then writes the zip atomically to p.
func (wb *workbook) save(p, sheetPath string, tables []string, x string, letters map[string]string, end int) error {
	lastCol := ""
	for _, l := range letters {
		if lastCol == "" || colLess(lastCol, l) {
			lastCol = l
		}
	}
	x = reDimension.ReplaceAllString(x, fmt.Sprintf("${1}A1:%s%d", lastCol, end))
	x = reSqref.ReplaceAllStringFunc(x, func(s string) string {
		m := reSqref.FindStringSubmatch(s)
		if m[1] != m[2] {
			return s
		}
		return fmt.Sprintf(`sqref="%s2:%s%d"`, m[1], m[1], end)
	})
	wb.files[sheetPath] = []byte(x)
	for _, t := range tables {
		data, err := wb.part(t)
		if err != nil {
			return err
		}
		wb.files[t] = []byte(reTableRef.ReplaceAllString(string(data), fmt.Sprintf(`ref="${1}1:${2}%d"`, end)))
	}

	data, err := wb.part("xl/sharedStrings.xml")
	if err != nil {
		return err
	}
	sst := string(data)
	total := strconv.Itoa(strings.Count(sst, "<si>"))
	sst = replaceFirst(reUnique, sst, func([]string) string { return `uniqueCount="` + total + `"` })
	sst = replaceFirst(reSstCount, sst, func(m []string) string { return m[1] + ` count="` + total + `"` })
	wb.files["xl/sharedStrings.xml"] = []byte(sst)

	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, name := range wb.order {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: wb.mtime[name]})
		if err == nil {
			_, err = w.Write(wb.files[name])
		}
		if err != nil {
			zw.Close()
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, p)
}

// entrySheet loads the parts every write needs.
func (wb *workbook) entrySheet() (sheetPath string, tables []string, rows []xRow, letters map[string]string, err error) {
	sheetPath, tables, err = wb.entrySheetParts()
	if err != nil {
		return
	}
	data, err := wb.part(sheetPath)
	if err != nil {
		return
	}
	if rows, err = parseRows(data); err != nil {
		return
	}
	if len(rows) == 0 {
		err = fmt.Errorf("%s has no header row", sheetPath)
		return
	}
	sstData, err := wb.part("xl/sharedStrings.xml")
	if err != nil {
		return
	}
	strs, err := sharedStrings(sstData)
	if err != nil {
		return
	}
	letters = headerLetters(rows[0], strs)
	if len(letters) == 0 {
		err = fmt.Errorf("%s has no column headers", sheetPath)
	}
	return
}

// appendRows appends entries to the workbook at p, creating it from
// template if it doesn't exist yet (osheet's append_rows).
func appendRows(p, template string, entries []Values) error {
	wb, err := loadWorkbook(p, template)
	if err != nil {
		return err
	}
	sheetPath, tables, rows, letters, err := wb.entrySheet()
	if err != nil {
		return err
	}
	dateCol := letters["Date"]
	if dateCol == "" {
		dateCol = "A"
	}
	x := string(wb.files[sheetPath])
	lastRow := rows[len(rows)-1]
	last, err := strconv.Atoi(lastRow.R)
	if err != nil {
		return fmt.Errorf("bad row number %q", lastRow.R)
	}
	// The template ships with a placeholder row 2 (defaults only, no date): replace it
	if last == 2 {
		hasDate := false
		for _, c := range lastRow.Cells {
			if c.R == dateCol+"2" {
				hasDate = true
			}
		}
		if !hasDate {
			x = rowPattern(2).ReplaceAllString(x, "")
			last = 1
		}
	}
	cache := &sstCache{}
	dateStyle, err := wb.dateStyleIndex()
	if err != nil {
		return err
	}
	var b strings.Builder
	for i, e := range entries {
		r, err := wb.rowXML(letters, last+1+i, e, cache, dateStyle)
		if err != nil {
			return err
		}
		b.WriteString(r)
	}
	x = strings.ReplaceAll(x, "</sheetData>", b.String()+"</sheetData>")
	return wb.save(p, sheetPath, tables, x, letters, last+len(entries))
}

// updateRow replaces the entry at index (its Row from ReadEntries) with v,
// or deletes it when v is nil; the rows below it move up (osheet's
// update_row).
func updateRow(p, template string, index int, v Values) error {
	wb, err := loadWorkbook(p, template)
	if err != nil {
		return err
	}
	sheetPath, tables, rows, letters, err := wb.entrySheet()
	if err != nil {
		return err
	}
	if index < 1 || index >= len(rows) {
		return fmt.Errorf("%s has no row %d", p, index)
	}
	n, err1 := strconv.Atoi(rows[index].R)
	last, err2 := strconv.Atoi(rows[len(rows)-1].R)
	if err1 != nil || err2 != nil {
		return fmt.Errorf("%s: bad row numbers", p)
	}
	x := string(wb.files[sheetPath])
	pat := rowPattern(n)
	switch {
	case v != nil:
		dateStyle, err := wb.dateStyleIndex()
		if err != nil {
			return err
		}
		row, err := wb.rowXML(letters, n, v, &sstCache{}, dateStyle)
		if err != nil {
			return err
		}
		x = replaceFirst(pat, x, func([]string) string { return row })
	case len(rows) <= 2:
		// It was the only entry: back to a fresh copy of the template (a table needs a data row)
		return copyFile(template, p)
	default:
		x = replaceFirst(pat, x, func([]string) string { return "" })
		x = reRenumber.ReplaceAllStringFunc(x, func(s string) string {
			m := reRenumber.FindStringSubmatch(s)
			k, _ := strconv.Atoi(m[2])
			if k > n {
				k--
			}
			return fmt.Sprintf(`%s%d"`, m[1], k)
		})
		last--
	}
	return wb.save(p, sheetPath, tables, x, letters, last)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// readSheetRows reads the entry sheet of the workbook at p as rows of
// column letter → value (the first row holds the headers).
func readSheetRows(p string) ([]map[string]string, error) {
	wb, err := openWorkbook(p)
	if err != nil {
		return nil, err
	}
	sheetPath, _, err := wb.entrySheetParts()
	if err != nil {
		return nil, err
	}
	var strs []string
	if data, ok := wb.files["xl/sharedStrings.xml"]; ok {
		if strs, err = sharedStrings(data); err != nil {
			return nil, err
		}
	}
	data, err := wb.part(sheetPath)
	if err != nil {
		return nil, err
	}
	rows, err := parseRows(data)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]string, 0, len(rows))
	for _, r := range rows {
		vals, err := rowValues(r, strs)
		if err != nil {
			return nil, err
		}
		out = append(out, vals)
	}
	return out, nil
}
