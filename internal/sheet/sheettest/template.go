// Package sheettest builds a small synthetic osheet template for tests. It
// has the same structure as the real TimesheetUpload.xlsx (BOM-prefixed
// parts, a placeholder row 2, per-column data validations, a TimesheetEntry
// table, no date style yet) with one project, "Acme" (billable, category
// "Software Development"), and one location, "Home".
package sheettest

import (
	"archive/zip"
	"io"
	"os"
)

// BOM prefixes some of the template's XML parts, as in the real file.
const BOM = "\xef\xbb\xbf"

// Parts are the template's zip entries, in order.
var Parts = []struct{ Name, Body string }{
	{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default ContentType="application/xml" Extension="xml"/><Default ContentType="application/vnd.openxmlformats-package.relationships+xml" Extension="rels"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml" PartName="/xl/workbook.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml" PartName="/xl/worksheets/sheet1.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml" PartName="/xl/worksheets/sheet2.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml" PartName="/xl/worksheets/sheet3.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml" PartName="/xl/styles.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml" PartName="/xl/sharedStrings.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.table+xml" PartName="/xl/tables/table2.xml"/></Types>`},
	{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
	{"xl/workbook.xml", BOM + `<?xml version="1.0" encoding="utf-8"?><workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><bookViews><workbookView /></bookViews><sheets><sheet name="TimesheetUpload" sheetId="1" r:id="rId1" /><sheet name="Lookup" sheetId="2" r:id="rId2" /><sheet name="Validation" sheetId="3" r:id="rId4" state="veryHidden" /></sheets></workbook>`},
	{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/><Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet3.xml"/><Relationship Id="rId5" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/></Relationships>`},
	{"xl/worksheets/sheet1.xml", BOM + `<?xml version="1.0" encoding="utf-8"?><worksheet xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><dimension ref="A1:J2" /><sheetData><row r="1"><c r="A1" s="0" t="s"><v>0</v></c><c r="B1" s="0" t="s"><v>1</v></c><c r="C1" s="0" t="s"><v>2</v></c><c r="D1" s="0" t="s"><v>3</v></c><c r="E1" s="0" t="s"><v>4</v></c><c r="F1" s="0" t="s"><v>5</v></c><c r="G1" s="0" t="s"><v>6</v></c><c r="H1" s="0" t="s"><v>7</v></c><c r="I1" s="0" t="s"><v>8</v></c><c r="J1" s="0" t="s"><v>9</v></c></row><row r="2"><c r="F2" s="0" t="s"><v>10</v></c><c r="I2" s="0" t="s"><v>11</v></c></row></sheetData><dataValidations count="3"><dataValidation type="date" sqref="A2:A2" operator="between"><formula1>36526</formula1><formula2>73415</formula2></dataValidation><dataValidation type="whole" sqref="D2:D2" operator="between"><formula1>0</formula1><formula2>24</formula2></dataValidation><dataValidation type="list" sqref="J2:J2"><formula1>=INDIRECT($J$1)</formula1></dataValidation></dataValidations><tableParts><tablePart r:id="rId1" /></tableParts></worksheet>`},
	{"xl/worksheets/_rels/sheet1.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/table" Target="../tables/table2.xml"/></Relationships>`},
	{"xl/worksheets/sheet2.xml", BOM + `<?xml version="1.0" encoding="utf-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><dimension ref="A1:C3" /><sheetData><row r="1"><c r="A1" t="s"><v>1</v></c><c r="B1" t="s"><v>2</v></c><c r="C1" t="s"><v>5</v></c></row><row r="2"><c r="A2" t="s"><v>12</v></c><c r="B2" t="s"><v>13</v></c><c r="C2" t="s"><v>14</v></c></row></sheetData></worksheet>`},
	{"xl/worksheets/sheet3.xml", BOM + `<?xml version="1.0" encoding="utf-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><dimension ref="A1:A2" /><sheetData><row r="1"><c r="A1" t="s"><v>9</v></c></row><row r="2"><c r="A2" t="s"><v>15</v></c></row></sheetData></worksheet>`},
	{"xl/tables/table2.xml", BOM + "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n<table xmlns=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\" id=\"2\" name=\"TimesheetEntry\" displayName=\"TimesheetEntry\" ref=\"A1:J2\" headerRowCount=\"1\">\n  <autoFilter ref=\"A1:J2\" />\n  <tableColumns count=\"10\"><tableColumn id=\"1\" name=\"Date\" /><tableColumn id=\"2\" name=\"Project\" /><tableColumn id=\"3\" name=\"Category\" /><tableColumn id=\"4\" name=\"Hours\" /><tableColumn id=\"5\" name=\"Minutes\" /><tableColumn id=\"6\" name=\"Billable\" /><tableColumn id=\"7\" name=\"Description\" /><tableColumn id=\"8\" name=\"TicketNumber\" /><tableColumn id=\"9\" name=\"Sentiment\" /><tableColumn id=\"10\" name=\"WorkedFrom\" /></tableColumns>\n</table>"},
	{"xl/styles.xml", `<?xml version="1.0" encoding="utf-8"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="1"><font /></fonts><fills count="1"><fill /></fills><borders count="1"><border /></borders><cellStyleXfs count="1"><xf numFmtId="0" /></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" applyNumberFormat="1" fontId="0" applyFont="1" xfId="0" applyProtection="1" /><xf numFmtId="0" applyNumberFormat="1" fontId="0" applyFont="1" xfId="0" applyAlignment="1"><alignment vertical="top" /></xf></cellXfs></styleSheet>`},
	{"xl/sharedStrings.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="16" uniqueCount="16"><si><t>Date</t></si><si><t>Project</t></si><si><t>Category</t></si><si><t>Hours</t></si><si><t>Minutes</t></si><si><t>Billable</t></si><si><t>Description</t></si><si><t>TicketNumber</t></si><si><t>Sentiment</t></si><si><t>WorkedFrom</t></si><si><t>No</t></si><si><t>Neutral</t></si><si><t>Acme</t></si><si><t>Software Development</t></si><si><t>Yes</t></si><si><t>Home</t></si></sst>`},
}

// WriteTemplate writes the synthetic template to p.
func WriteTemplate(p string) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, part := range Parts {
		w, err := zw.Create(part.Name)
		if err != nil {
			f.Close()
			return err
		}
		if _, err := io.WriteString(w, part.Body); err != nil {
			f.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
