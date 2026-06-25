package main

import (
	"archive/zip"
	"encoding/xml"
	"path"
	"strconv"
	"strings"
)

// extractObjects collects sheet objects (pictures, shapes, form-control buttons)
// that have a macro assigned, across the selected sheets. Two storage forms exist:
//
//   - DrawingML (xl/drawings/drawingN.xml): pictures/shapes carry the macro in the
//     `macro` attribute of <xdr:sp>/<xdr:pic>/... e.g. macro="[0]!MyMacro".
//   - VML (xl/drawings/vmlDrawingN.vml): legacy form-control buttons carry it in
//     the <x:FmlaMacro> element inside <x:ClientData>.
//
// Each sheet is mapped to its drawings via xl/worksheets/_rels/<sheet>.xml.rels.
func extractObjects(zr *zip.Reader, sheets []wbSheet, only map[string]bool) []DrawingObject {
	var out []DrawingObject
	for _, ws := range sheets {
		if only != nil && !only[ws.Name] {
			continue
		}
		drawings, vmls := sheetDrawingRels(zr, ws.Target)
		for _, t := range drawings {
			out = append(out, parseDrawingObjects(zr, t, ws.Name)...)
		}
		for _, t := range vmls {
			out = append(out, parseVMLObjects(zr, t, ws.Name)...)
		}
	}
	return out
}

// sheetDrawingRels resolves the drawing and vmlDrawing targets referenced by one
// worksheet, via its .rels file. Targets are returned as full zip paths.
func sheetDrawingRels(zr *zip.Reader, wsTarget string) (drawings, vmls []string) {
	dir := path.Dir(wsTarget)
	relsPath := dir + "/_rels/" + path.Base(wsTarget) + ".rels"
	f := openZip(zr, relsPath)
	if f == nil {
		return nil, nil
	}
	defer f.Close()
	var rels struct {
		Rel []struct {
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if xml.NewDecoder(f).Decode(&rels) != nil {
		return nil, nil
	}
	for _, r := range rels.Rel {
		switch {
		case strings.HasSuffix(r.Type, "/drawing"):
			drawings = append(drawings, resolveRelTarget(dir, r.Target))
		case strings.HasSuffix(r.Type, "/vmlDrawing"):
			vmls = append(vmls, resolveRelTarget(dir, r.Target))
		}
	}
	return drawings, vmls
}

// resolveRelTarget turns a relationship Target (relative like
// "../drawings/drawing3.xml" or absolute like "/xl/drawings/...") into a zip path.
func resolveRelTarget(baseDir, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(target, "/")
	}
	return path.Join(baseDir, target)
}

// --- DrawingML (drawingN.xml) ---

type xdrMarker struct {
	Col int `xml:"col"`
	Row int `xml:"row"`
}

// cNvPr lives under a different non-visual parent per shape kind
// (nvSpPr/nvPicPr/...), so all variants are declared and the first non-nil wins.
type xdrShape struct {
	Macro string `xml:"macro,attr"`
	NvSp  *nvPr  `xml:"nvSpPr>cNvPr"`
	NvPic *nvPr  `xml:"nvPicPr>cNvPr"`
	NvGF  *nvPr  `xml:"nvGraphicFramePr>cNvPr"`
	NvCxn *nvPr  `xml:"nvCxnSpPr>cNvPr"`
	NvGrp *nvPr  `xml:"nvGrpSpPr>cNvPr"`
}

type nvPr struct {
	Name string `xml:"name,attr"`
}

func (s *xdrShape) name() string {
	for _, n := range []*nvPr{s.NvSp, s.NvPic, s.NvGF, s.NvCxn, s.NvGrp} {
		if n != nil {
			return n.Name
		}
	}
	return ""
}

type xdrAnchor struct {
	From *xdrMarker `xml:"from"`
	Sp   *xdrShape  `xml:"sp"`
	Pic  *xdrShape  `xml:"pic"`
	Cxn  *xdrShape  `xml:"cxnSp"`
	GF   *xdrShape  `xml:"graphicFrame"`
	Grp  *xdrShape  `xml:"grpSp"`
}

func parseDrawingObjects(zr *zip.Reader, target, sheet string) []DrawingObject {
	f := openZip(zr, target)
	if f == nil {
		return nil
	}
	defer f.Close()

	var out []DrawingObject
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
		case "twoCellAnchor", "oneCellAnchor", "absoluteAnchor":
			var a xdrAnchor
			if dec.DecodeElement(&a, &se) != nil {
				continue
			}
			anchor := ""
			if a.From != nil {
				anchor = numToCol(a.From.Col+1) + strconv.Itoa(a.From.Row+1)
			}
			// At most one shape kind is present per anchor; fixed order keeps
			// output deterministic.
			for _, s := range []struct {
				kind string
				sh   *xdrShape
			}{
				{"pic", a.Pic}, {"sp", a.Sp}, {"cxnSp", a.Cxn},
				{"graphicFrame", a.GF}, {"grpSp", a.Grp},
			} {
				if s.sh == nil {
					continue
				}
				macro := normalizeMacro(s.sh.Macro)
				if macro == "" {
					continue // object without an assigned macro (e.g. a plain chart)
				}
				out = append(out, DrawingObject{
					Sheet: sheet, Name: s.sh.name(), Kind: s.kind,
					Anchor: anchor, Macro: macro,
				})
			}
		}
	}
	return out
}

// --- VML (vmlDrawingN.vml) form controls ---
//
// Spec-based (MS-VML / [MS-OSHARED]); the sample workbook used during development
// had no form-control buttons, so this path is exercised by unit tests rather
// than real-file verification.

func parseVMLObjects(zr *zip.Reader, target, sheet string) []DrawingObject {
	f := openZip(zr, target)
	if f == nil {
		return nil
	}
	defer f.Close()

	var out []DrawingObject
	dec := xml.NewDecoder(f)
	dec.Strict = false // VML is not strict XML
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity

	var curID string
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
		case "shape":
			curID = attrValue(se, "id")
		case "ClientData":
			var cd struct {
				ObjectType string `xml:"ObjectType,attr"`
				FmlaMacro  string `xml:"FmlaMacro"`
				Anchor     string `xml:"Anchor"`
			}
			if dec.DecodeElement(&cd, &se) != nil {
				continue
			}
			macro := normalizeMacro(cd.FmlaMacro)
			if macro == "" {
				continue
			}
			out = append(out, DrawingObject{
				Sheet: sheet, Name: curID, Kind: vmlKind(cd.ObjectType),
				Anchor: vmlAnchor(cd.Anchor), Macro: macro,
			})
		}
	}
	return out
}

func vmlKind(objType string) string {
	if objType == "" {
		return "control"
	}
	return strings.ToLower(objType) // Button -> button, Drop -> drop, ...
}

// vmlAnchor reads the top-left cell from a VML ClientData <x:Anchor>, whose
// fields are leftCol, leftOffset, topRow, topOffset, rightCol, ... (cols/rows
// 0-based).
func vmlAnchor(s string) string {
	parts := strings.Split(s, ",")
	if len(parts) < 3 {
		return ""
	}
	col, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	row, err2 := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err1 != nil || err2 != nil {
		return ""
	}
	return numToCol(col+1) + strconv.Itoa(row+1)
}

func attrValue(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// normalizeMacro strips the "[0]!" self-reference prefix Excel writes for macros
// that live in this workbook, leaving just the macro name. External-workbook
// references ("[1]!" or "'Other.xlsm'!...") are left intact.
func normalizeMacro(m string) string {
	m = strings.TrimSpace(m)
	return strings.TrimPrefix(m, "[0]!")
}
