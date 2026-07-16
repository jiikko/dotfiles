package main

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// --- A1 <-> (row, col) helpers ---

func colToNum(col string) int {
	n := 0
	for i := 0; i < len(col); i++ {
		n = n*26 + int(col[i]-'A'+1)
	}
	return n
}

func numToCol(n int) string {
	var b []byte
	for n > 0 {
		n--
		b = append([]byte{byte('A' + n%26)}, b...)
		n /= 26
	}
	return string(b)
}

func splitAddr(addr string) (row, col int) {
	i := 0
	for i < len(addr) && addr[i] >= 'A' && addr[i] <= 'Z' {
		i++
	}
	col = colToNum(addr[:i])
	row, _ = strconv.Atoi(addr[i:])
	return row, col
}

// --- shared strings ---

type siXML struct {
	T string     `xml:"t"`
	R []siRunXML `xml:"r"`
}
type siRunXML struct {
	T string `xml:"t"`
}

func (s siXML) text() string {
	if len(s.R) > 0 {
		var b strings.Builder
		for _, r := range s.R {
			b.WriteString(r.T)
		}
		return b.String()
	}
	return s.T
}

func parseSharedStrings(zr *zip.Reader) []string {
	f := openZip(zr, "xl/sharedStrings.xml")
	if f == nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	dec := xml.NewDecoder(f)
	var out []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "si" {
			var si siXML
			if dec.DecodeElement(&si, &se) == nil {
				out = append(out, si.text())
			}
		}
	}
	return out
}

// --- workbook sheet list (name -> worksheet target path) ---

type wbSheet struct {
	Name   string
	Target string // e.g. "xl/worksheets/sheet3.xml"
}

func parseWorkbookSheets(zr *zip.Reader) []wbSheet {
	type sheetEl struct {
		Name string `xml:"name,attr"`
		RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	}
	type relEl struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	}
	var wb struct {
		Sheets []sheetEl `xml:"sheets>sheet"`
	}
	if f := openZip(zr, "xl/workbook.xml"); f != nil {
		// 欠損/壊れた XML は best-effort (ゼロ値のまま続行し、参照側が空を扱う)
		_ = xml.NewDecoder(f).Decode(&wb)
		_ = f.Close()
	}
	var rels struct {
		Rel []relEl `xml:"Relationship"`
	}
	if f := openZip(zr, "xl/_rels/workbook.xml.rels"); f != nil {
		// 欠損/壊れた XML は best-effort (ゼロ値のまま続行し、参照側が空を扱う)
		_ = xml.NewDecoder(f).Decode(&rels)
		_ = f.Close()
	}
	ridTarget := map[string]string{}
	for _, r := range rels.Rel {
		ridTarget[r.ID] = r.Target
	}
	var out []wbSheet
	for _, s := range wb.Sheets {
		t := ridTarget[s.RID]
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "/") {
			t = "xl/" + t
		} else {
			t = strings.TrimPrefix(t, "/")
		}
		out = append(out, wbSheet{Name: s.Name, Target: t})
	}
	return out
}

// --- worksheet cell parsing ---

type cXML struct {
	R  string `xml:"r,attr"`
	T  string `xml:"t,attr"`
	F  *fXML  `xml:"f"`
	V  string `xml:"v"`
	Is *isXML `xml:"is"`
}
type fXML struct {
	T       string `xml:"t,attr"`
	Content string `xml:",chardata"`
}
type isXML struct {
	T string     `xml:"t"`
	R []siRunXML `xml:"r"`
}

func (is *isXML) text() string {
	if is == nil {
		return ""
	}
	if len(is.R) > 0 {
		var b strings.Builder
		for _, r := range is.R {
			b.WriteString(r.T)
		}
		return b.String()
	}
	return is.T
}

// extractSheet parses one worksheet XML and resolves shared/array formulas via xl.
func extractSheet(zr *zip.Reader, target, name string, shared []string, xl *excelize.File) *Sheet {
	f := openZip(zr, target)
	if f == nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	sh := &Sheet{Name: name}
	dec := xml.NewDecoder(f)
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "dimension":
			for _, a := range se.Attr {
				if a.Name.Local == "ref" {
					sh.Dimension = a.Value
				}
			}
		case "c":
			var c cXML
			if dec.DecodeElement(&c, &se) != nil || c.R == "" {
				continue
			}
			cell := buildCell(c, shared)
			if cell == nil {
				continue
			}
			sh.Cells = append(sh.Cells, *cell)
		}
	}

	// Expand shared-formula children (their formula text is empty in the XML).
	for i := range sh.Cells {
		if sh.Cells[i].needExpand {
			if fm, err := xl.GetCellFormula(name, sh.Cells[i].Addr); err == nil {
				sh.Cells[i].Formula = fm
			}
		}
	}

	sort.Slice(sh.Cells, func(a, b int) bool {
		if sh.Cells[a].Row != sh.Cells[b].Row {
			return sh.Cells[a].Row < sh.Cells[b].Row
		}
		return sh.Cells[a].Col < sh.Cells[b].Col
	})
	for _, c := range sh.Cells {
		if c.Formula != "" {
			sh.FormulaN++
		} else if c.Value != "" {
			sh.ValueN++
		}
	}
	return sh
}

func buildCell(c cXML, shared []string) *Cell {
	row, col := splitAddr(c.R)
	if row == 0 {
		return nil
	}

	var value string
	switch c.T {
	case "s": // shared string index
		if idx, err := strconv.Atoi(strings.TrimSpace(c.V)); err == nil && idx >= 0 && idx < len(shared) {
			value = shared[idx]
		}
	case "inlineStr":
		value = c.Is.text()
	default: // n, b, str, e, d ...
		value = c.V
	}

	formula := ""
	needExpand := false
	if c.F != nil {
		if strings.TrimSpace(c.F.Content) != "" {
			formula = c.F.Content
		} else if c.F.T == "shared" {
			needExpand = true
		}
	}

	if formula == "" && value == "" && !needExpand {
		return nil
	}
	typ := c.T
	if typ == "" {
		typ = "n"
	}
	return &Cell{Row: row, Col: col, Addr: c.R, Type: typ, Formula: formula, Value: value, needExpand: needExpand}
}

func openZip(zr *zip.Reader, name string) io.ReadCloser {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil
			}
			return rc
		}
	}
	return nil
}
