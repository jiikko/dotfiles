package main

import (
	"archive/zip"
	"bytes"
	"testing"
)

func zipReaderFrom(t *testing.T, files map[string]string) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip open: %v", err)
	}
	return zr
}

const drawingXML = `<xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
 <xdr:twoCellAnchor>
  <xdr:from><xdr:col>3</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>11</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from>
  <xdr:to><xdr:col>3</xdr:col><xdr:row>12</xdr:row></xdr:to>
  <xdr:sp macro="[0]!RunReport" textlink=""><xdr:nvSpPr><xdr:cNvPr id="4" name="Rect 3"/><xdr:cNvSpPr/></xdr:nvSpPr></xdr:sp>
 </xdr:twoCellAnchor>
 <xdr:twoCellAnchor>
  <xdr:from><xdr:col>0</xdr:col><xdr:row>0</xdr:row></xdr:from>
  <xdr:graphicFrame macro=""><xdr:nvGraphicFramePr><xdr:cNvPr id="5" name="Chart 1"/></xdr:nvGraphicFramePr></xdr:graphicFrame>
 </xdr:twoCellAnchor>
</xdr:wsDr>`

const vmlXML = `<xml xmlns:v="urn:schemas-microsoft-com:vml" xmlns:x="urn:schemas-microsoft-com:office:excel">
 <v:shape id="Button 1" type="#_x0000_t201">
  <x:ClientData ObjectType="Button">
   <x:Anchor>5, 0, 9, 0, 7, 0, 11, 0</x:Anchor>
   <x:FmlaMacro>DoExport</x:FmlaMacro>
  </x:ClientData>
 </v:shape>
</xml>`

const sheetRels = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
 <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/drawing1.xml"/>
 <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/vmlDrawing" Target="../drawings/vmlDrawing1.vml"/>
</Relationships>`

func TestExtractObjects(t *testing.T) {
	zr := zipReaderFrom(t, map[string]string{
		"xl/worksheets/_rels/sheet1.xml.rels": sheetRels,
		"xl/drawings/drawing1.xml":            drawingXML,
		"xl/drawings/vmlDrawing1.vml":         vmlXML,
	})
	sheets := []wbSheet{{Name: "S1", Target: "xl/worksheets/sheet1.xml"}}

	got := extractObjects(zr, sheets, nil)
	want := []DrawingObject{
		{Sheet: "S1", Name: "Rect 3", Kind: "sp", Anchor: "D12", Macro: "RunReport"},
		{Sheet: "S1", Name: "Button 1", Kind: "button", Anchor: "F10", Macro: "DoExport"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d objects, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("object[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestExtractObjectsSheetFilter(t *testing.T) {
	zr := zipReaderFrom(t, map[string]string{
		"xl/worksheets/_rels/sheet1.xml.rels": sheetRels,
		"xl/drawings/drawing1.xml":            drawingXML,
		"xl/drawings/vmlDrawing1.vml":         vmlXML,
	})
	sheets := []wbSheet{{Name: "S1", Target: "xl/worksheets/sheet1.xml"}}
	if got := extractObjects(zr, sheets, map[string]bool{"Other": true}); len(got) != 0 {
		t.Errorf("filtered-out sheet should yield no objects, got %+v", got)
	}
}

func TestNormalizeMacro(t *testing.T) {
	cases := map[string]string{
		"[0]!RunReport":    "RunReport",
		"  [0]!Foo  ":      "Foo",
		"[1]!External":     "[1]!External",
		"'Other.xlsm'!Run": "'Other.xlsm'!Run",
		"":                 "",
		"PlainName":        "PlainName",
	}
	for in, want := range cases {
		if got := normalizeMacro(in); got != want {
			t.Errorf("normalizeMacro(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVMLAnchor(t *testing.T) {
	cases := map[string]string{
		"5, 0, 9, 0, 7, 0, 11, 0": "F10",
		"0, 0, 0, 0":              "A1",
		"bad":                     "",
		"":                        "",
	}
	for in, want := range cases {
		if got := vmlAnchor(in); got != want {
			t.Errorf("vmlAnchor(%q) = %q, want %q", in, got, want)
		}
	}
}
