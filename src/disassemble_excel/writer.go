package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// sanitizeFilename makes a sheet/module name safe to use as a file name while
// keeping it human-readable (Japanese kept as-is).
func sanitizeFilename(name string) string {
	repl := func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		if r < 0x20 {
			return '_'
		}
		return r
	}
	out := strings.Map(repl, name)
	out = strings.TrimSpace(out)
	if out == "" {
		out = "_"
	}
	return out
}

// tsvEsc keeps one cell on one line: tabs/newlines/backslashes are escaped.
func tsvEsc(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\t", "\\t")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

func formulaField(c Cell) string {
	if c.Formula == "" {
		return ""
	}
	return "=" + c.Formula
}

// writeCellsTSV writes the full, one-cell-per-line representation.
func writeCellsTSV(path string, sh *Sheet) error {
	var b strings.Builder
	b.WriteString("cell\ttype\tformula\tvalue\n")
	for _, c := range sh.Cells {
		b.WriteString(c.Addr)
		b.WriteByte('\t')
		b.WriteString(c.Type)
		b.WriteByte('\t')
		b.WriteString(tsvEsc(formulaField(c)))
		b.WriteByte('\t')
		b.WriteString(tsvEsc(c.Value))
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// writeSlimTSV writes only formula-bearing cells plus a summary of the
// value-only cells that were omitted. Used for sheets above the cell threshold.
func writeSlimTSV(path string, sh *Sheet) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# slim view of %q\n", sh.Name)
	fmt.Fprintf(&b, "# dimension=%s  cells=%d  formulas=%d  value_only=%d\n",
		sh.Dimension, len(sh.Cells), sh.FormulaN, sh.ValueN)
	fmt.Fprintf(&b, "# value-only cells (%d) are omitted here; see the .cells.tsv for the full dump\n", sh.ValueN)
	b.WriteString("cell\ttype\tformula\tvalue\n")
	for _, c := range sh.Cells {
		if c.Formula == "" {
			continue
		}
		b.WriteString(c.Addr)
		b.WriteByte('\t')
		b.WriteString(c.Type)
		b.WriteByte('\t')
		b.WriteString(tsvEsc(formulaField(c)))
		b.WriteByte('\t')
		b.WriteString(tsvEsc(c.Value))
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// writeValuesCSV writes the grid view (cached values) for human eyes.
func writeValuesCSV(path string, sh *Sheet) error {
	maxRow, maxCol := 0, 0
	for _, c := range sh.Cells {
		if c.Row > maxRow {
			maxRow = c.Row
		}
		if c.Col > maxCol {
			maxCol = c.Col
		}
	}
	grid := make(map[[2]int]string, len(sh.Cells))
	for _, c := range sh.Cells {
		grid[[2]int{c.Row, c.Col}] = c.Value
	}

	fp, err := os.Create(path)
	if err != nil {
		return err
	}
	defer fp.Close()
	w := csv.NewWriter(fp)
	defer w.Flush()

	header := make([]string, maxCol+1)
	header[0] = ""
	for col := 1; col <= maxCol; col++ {
		header[col] = numToCol(col)
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for row := 1; row <= maxRow; row++ {
		rec := make([]string, maxCol+1)
		rec[0] = strconv.Itoa(row)
		for col := 1; col <= maxCol; col++ {
			rec[col] = grid[[2]int{row, col}]
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	return nil
}

func writeDefinedNames(path string, names []DefinedName) error {
	var b strings.Builder
	b.WriteString("name\tscope\trefers_to\n")
	for _, n := range names {
		b.WriteString(tsvEsc(n.Name))
		b.WriteByte('\t')
		b.WriteString(tsvEsc(n.Scope))
		b.WriteByte('\t')
		b.WriteString(tsvEsc(n.RefersTo))
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeModule(dir string, m Module) error {
	return os.WriteFile(filepath.Join(dir, sanitizeFilename(m.Name)+m.Ext), []byte(m.Source+"\n"), 0o644)
}

func writeVBAIndex(path string, mods []Module) error {
	var b strings.Builder
	b.WriteString("module\tproc\tkind\tmodule_line\n")
	for _, m := range mods {
		for _, p := range m.Procs {
			fmt.Fprintf(&b, "%s\t%s\t%s\t%d\n", m.Name, p.Name, p.Kind, p.Line)
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeManifest(path string, man Manifest) error {
	data, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
