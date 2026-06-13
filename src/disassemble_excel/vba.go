package main

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/richardlehane/mscfb"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"

	"github.com/jiikko/disassemble_excel/ovba"
)

// VBA identifiers may contain non-ASCII letters (e.g. Japanese: `Sub gh用()`),
// so the name class uses Unicode \p{L}/\p{N}, not ASCII-only \w.
var procRe = regexp.MustCompile(`(?i)^[ \t]*(?:public[ \t]+|private[ \t]+|friend[ \t]+)?(?:static[ \t]+)?(sub|function|property[ \t]+(?:get|let|set))[ \t]+([\p{L}_][\p{L}\p{N}_]*)`)

// ExtractVBA pulls every VBA module's source out of a raw vbaProject.bin.
// It is pure-Go: mscfb opens the OLE2 container, ovba decompresses the streams,
// and the project code page decodes module text (handles Japanese, etc.).
func ExtractVBA(bin []byte) ([]Module, error) {
	doc, err := mscfb.New(bytes.NewReader(bin))
	if err != nil {
		return nil, fmt.Errorf("open CFB: %w", err)
	}

	streams := map[string][]byte{}
	for entry, err := doc.Next(); err != io.EOF; entry, err = doc.Next() {
		if err != nil {
			break
		}
		// Module streams and "dir" live under the "VBA" storage. Keep those;
		// other top-level streams (PROJECT, PROJECTwm, ...) are not needed.
		if len(entry.Path) == 0 || entry.Path[len(entry.Path)-1] != "VBA" {
			continue
		}
		data, _ := io.ReadAll(entry)
		streams[entry.Name] = data
	}

	dirCompressed, ok := streams["dir"]
	if !ok {
		return nil, fmt.Errorf("VBA/dir stream not found")
	}
	dir, err := ovba.Decompress(dirCompressed)
	if err != nil {
		return nil, fmt.Errorf("decompress dir: %w", err)
	}

	codePage, dirModules := ovba.ParseDir(dir)
	dec := decoderFor(codePage)

	var mods []Module
	for _, dm := range dirModules {
		name := decodeBytes(dec, dm.Name)
		stream := decodeBytes(dec, dm.StreamName)
		data := streams[stream]
		if data == nil {
			data = streams[name]
		}
		if data == nil || int(dm.TextOffset) > len(data) {
			continue
		}
		srcBytes, err := ovba.Decompress(data[dm.TextOffset:])
		if err != nil {
			continue
		}
		// Drop the leading module-level Attribute header, then trim trailing
		// blank lines so the output matches what a developer sees in the editor.
		src := strings.TrimRight(stripModuleAttributes(normalizeNewlines(decodeBytes(dec, srcBytes))), "\n")

		ext := ".bas"
		if dm.Type == "class" {
			ext = ".cls"
		}
		empty := strings.TrimSpace(src) == ""
		mods = append(mods, Module{
			Name:       name,
			StreamName: stream,
			Type:       dm.Type,
			Ext:        ext,
			TextOffset: dm.TextOffset,
			Source:     src,
			Procs:      findProcs(src),
			Empty:      empty,
		})
	}
	return mods, nil
}

// stripModuleAttributes removes the leading module-level "Attribute VB_*" header
// lines that the VBA compiler stores at the top of every module stream. They are
// not part of the code a developer writes (the VBA editor hides them), so we drop
// them — this matches olevba's output and makes attribute-only modules (e.g. empty
// sheet/class modules) come out empty.
func stripModuleAttributes(src string) string {
	lines := strings.Split(src, "\n")
	i := 0
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "Attribute ") {
			i++
			continue
		}
		break
	}
	return strings.Join(lines[i:], "\n")
}

func findProcs(src string) []Proc {
	var procs []Proc
	for i, line := range strings.Split(src, "\n") {
		m := procRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		kind := strings.Title(strings.ToLower(strings.Fields(m[1])[0]))
		if strings.HasPrefix(strings.ToLower(m[1]), "property") {
			kind = "Property"
		}
		procs = append(procs, Proc{Name: m[2], Kind: kind, Line: i + 1})
	}
	return procs
}

// decoderFor maps a VBA project code page to a text decoder. nil means UTF-8/raw.
func decoderFor(codePage int) *encoding.Decoder {
	switch codePage {
	case 65001:
		return nil // UTF-8
	case 932:
		return japanese.ShiftJIS.NewDecoder()
	case 936:
		return simplifiedchinese.GBK.NewDecoder()
	case 949:
		return korean.EUCKR.NewDecoder()
	case 950:
		return traditionalchinese.Big5.NewDecoder()
	case 1250:
		return charmap.Windows1250.NewDecoder()
	case 1251:
		return charmap.Windows1251.NewDecoder()
	case 1252:
		return charmap.Windows1252.NewDecoder()
	default:
		return charmap.Windows1252.NewDecoder()
	}
}

func decodeBytes(dec *encoding.Decoder, b []byte) string {
	if dec == nil {
		return string(b)
	}
	out, _, err := transform.Bytes(dec, b)
	if err != nil {
		return string(b) // fall back to raw on malformed input
	}
	return string(out)
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}
