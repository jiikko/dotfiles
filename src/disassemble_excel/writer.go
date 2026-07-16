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
	if out == "" || out == "." || out == ".." {
		// "." / ".." はパス区切りを含まないため上位の連結 (name+ext) では脱出しないが、
		// 拡張子が空の呼び出し (writeModule で Ext="") では filepath.Join(dir, "..") が
		// 親ディレクトリを指しうる。防御的に無害名へ置換する。
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
	// グリッドの次元は「セル数」ではなく最大セル座標で決まる。遠方に 1 セルあるだけ
	// (例 XFD1048576 → 16384x1048576 ≈ 170 億セル) で dense グリッドが巨大化し OOM /
	// ハングする (-max-cells はセル数 gate なので防げない)。グリッドは目視用の便宜ビューで
	// 完全データは常に .cells.tsv にあるため、次元が過大なときはグリッドを出さず注記だけ残す。
	const maxGridCells = 10_000_000
	if int64(maxRow)*int64(maxCol) > maxGridCells {
		note := fmt.Sprintf(
			"# grid skipped: dimension %dx%d (%d cells) exceeds %d — see the .cells.tsv for the full data\n",
			maxRow, maxCol, int64(maxRow)*int64(maxCol), maxGridCells)
		return os.WriteFile(path, []byte(note), 0o644)
	}
	grid := make(map[[2]int]string, len(sh.Cells))
	for _, c := range sh.Cells {
		grid[[2]int{c.Row, c.Col}] = c.Value
	}

	fp, err := os.Create(path)
	if err != nil {
		return err
	}
	// 早期 return 時の解放用。正常系は末尾の明示 Close が先に走り、こちらは無害な二重 Close になる
	defer func() { _ = fp.Close() }()
	w := csv.NewWriter(fp)

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
	// 書き込みファイルなので Flush / Close のエラーは握り潰さない (silent なデータ欠損防止)
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return fp.Close()
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

// writeObjectsTSV writes one line per sheet object that has a macro assigned,
// so "what runs when this picture/button is clicked" is diffable and greppable.
func writeObjectsTSV(path string, objs []DrawingObject) error {
	var b strings.Builder
	b.WriteString("sheet\tobject\tkind\tanchor\tmacro\n")
	for _, o := range objs {
		b.WriteString(tsvEsc(o.Sheet))
		b.WriteByte('\t')
		b.WriteString(tsvEsc(o.Name))
		b.WriteByte('\t')
		b.WriteString(o.Kind)
		b.WriteByte('\t')
		b.WriteString(o.Anchor)
		b.WriteByte('\t')
		b.WriteString(tsvEsc(o.Macro))
		b.WriteByte('\n')
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

// writeReadme writes a human-readable README.md into the output directory so the
// disassembled result is self-describing: which tool produced it, the source
// Excel file it came from, and what every file means. srcAbs is the absolute
// path of the source workbook; noGrid reports whether values.csv was skipped.
func writeReadme(path string, man Manifest, srcAbs string, noGrid bool) error {
	anySlim := false
	for _, s := range man.Sheets {
		if s.Slim {
			anySlim = true
			break
		}
	}
	hasVBA := len(man.VBAModules) > 0
	hasObjects := len(man.Objects) > 0

	var b strings.Builder
	b.WriteString("# disassemble_excel 出力ディレクトリ\n\n")
	b.WriteString("このディレクトリは Excel ワークブックを `disassemble_excel` で分解（解体）した結果です。\n")
	b.WriteString("中身はすべて行指向のプレーンテキストで、`diff` / `grep` / バージョン管理でそのまま比較できます。\n")
	b.WriteString("Excel を開かずに、シートのセル数式・キャッシュ値・定義名・VBA マクロのソースを読むためのものです。\n\n")

	b.WriteString("## 生成元\n\n")
	b.WriteString("| 項目 | 値 |\n")
	b.WriteString("|------|----|\n")
	fmt.Fprintf(&b, "| 生成ツール | disassemble_excel v%s (https://github.com/jiikko/disassemble_excel) |\n", man.ToolVersion)
	fmt.Fprintf(&b, "| 元 Excel ファイル（絶対パス） | `%s` |\n", srcAbs)
	fmt.Fprintf(&b, "| 元 Excel ファイル名 | `%s` |\n", man.Source)
	fmt.Fprintf(&b, "| 元ファイルの SHA-256 | `%s` |\n", man.SHA256)
	fmt.Fprintf(&b, "| 抽出日時 | %s |\n\n", man.ExtractedAt)
	b.WriteString("> 数式は **抽出のみで、評価（再計算）はしていません**。各セルの `value` 列は Excel が\n")
	b.WriteString("> 最後にファイルへ保存したキャッシュ値であり、このツールが計算した値ではありません。\n\n")

	b.WriteString("## ファイル構成と各ファイルの意味\n\n")
	b.WriteString("| パス | 内容 |\n")
	b.WriteString("|------|------|\n")
	b.WriteString("| `manifest.json` | 機械可読の目録。元ファイル名・SHA-256・抽出日時・ツールバージョン、シートごとの寸法/セル数/数式数、VBA モジュール一覧を持つ |\n")
	b.WriteString("| `README.md` | このファイル（出力の説明・元ソース・各ファイルの意味） |\n")
	b.WriteString("| `defined_names.tsv` | 定義名（名前付き範囲・LAMBDA 引数）。列は `name  scope  refers_to` |\n")
	b.WriteString("| `sheets/<シート名>.cells.tsv` | シートの全セルを 1 セル 1 行で。列は `cell  type  formula  value`、(行,列) でソート・空セルは省略。値中のタブ/改行は `\\t` `\\n` にエスケープ |\n")
	if !noGrid {
		b.WriteString("| `sheets/<シート名>.values.csv` | キャッシュ値だけをグリッド（行×列）に並べた目視確認用ビュー |\n")
	}
	if anySlim {
		b.WriteString("| `sheets/<シート名>.slim.tsv` | 巨大シート向けの圧縮ビュー。数式を持つセルだけを残し、値だけのセルは要約。`-max-cells` を超えたシートにのみ出力 |\n")
	}
	if hasVBA {
		b.WriteString("| `vba/<モジュール名>.bas` / `.cls` | VBA モジュールのソースコード（標準モジュール=`.bas`、クラス/ドキュメントモジュール=`.cls`）。コードを持つモジュールごとに 1 ファイル |\n")
		b.WriteString("| `vba_index.tsv` | VBA の Sub/Function/Property の索引。列は `module  proc  kind  module_line` |\n")
	}
	if hasObjects {
		b.WriteString("| `objects.tsv` | マクロが割り当てられたシート上のオブジェクト（画像・図形・ボタン）。クリック時に実行されるマクロが分かる。列は `sheet  object  kind  anchor  macro`。画像自体は復元しない |\n")
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## シート一覧（%d 枚）\n\n", len(man.Sheets))
	if len(man.Sheets) == 0 {
		b.WriteString("（抽出されたシートはありません）\n\n")
	} else {
		b.WriteString("| シート | 範囲 | セル数 | 数式数 | ファイル |\n")
		b.WriteString("|--------|------|-------:|-------:|----------|\n")
		for _, s := range man.Sheets {
			file := "sheets/" + sanitizeFilename(s.Name) + ".cells.tsv"
			if s.Slim {
				file += " (+slim)"
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %d | `%s` |\n",
				s.Name, s.Dimension, s.Cells, s.Formulas, file)
		}
		b.WriteString("\n")
	}

	if hasVBA {
		fmt.Fprintf(&b, "## VBA モジュール一覧（%d 個）\n\n", len(man.VBAModules))
		b.WriteString("| モジュール | 種別 | プロシージャ数 | 行数 | ファイル |\n")
		b.WriteString("|------------|------|---------------:|-----:|----------|\n")
		for _, m := range man.VBAModules {
			file := m.File
			if file == "" {
				file = "（空モジュール: ファイル化なし）"
			} else {
				file = "`" + file + "`"
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %s |\n",
				m.Name, m.Type, m.Procs, m.Lines, file)
		}
		b.WriteString("\n")
	}

	if hasObjects {
		fmt.Fprintf(&b, "## マクロ割り当てオブジェクト一覧（%d 個）\n\n", len(man.Objects))
		b.WriteString("シート上の画像・図形・ボタンに登録され、クリック時に実行されるマクロです。\n\n")
		b.WriteString("| シート | オブジェクト | 種別 | 位置 | マクロ |\n")
		b.WriteString("|--------|--------------|------|------|--------|\n")
		for _, o := range man.Objects {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | `%s` |\n",
				o.Sheet, o.Name, o.Kind, o.Anchor, o.Macro)
		}
		b.WriteString("\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}
