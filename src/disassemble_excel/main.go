// Command disassemble_excel takes one .xlsx/.xlsm file and writes its sheets
// (one cell per line, formula + cached value), defined names and VBA macro
// source into a directory, so the result can be diffed and read without Excel.
//
// It is specialized for Excel disassembly only and has no external-tool
// dependency: VBA is decompressed in pure Go (see package ovba).
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

const toolVersion = "0.1.0"

func main() {
	out := flag.String("o", "", "output directory (default: <file name without extension>/)")
	noVBA := flag.Bool("no-vba", false, "skip VBA macro extraction")
	noGrid := flag.Bool("no-grid", false, "skip the grid values.csv (cells.tsv is always written)")
	maxCells := flag.Int("max-cells", 100000, "sheets with more cells than this also get a .slim.tsv (0 disables slim)")
	sheetsCSV := flag.String("sheets", "", "comma-separated sheet names to extract (default: all)")
	force := flag.Bool("f", false, "overwrite the output directory if it already exists")
	flag.Usage = usage

	// Go's flag package stops parsing at the first non-flag argument, so accept
	// both "<file> -o out" and "-o out <file>" by pulling a leading file
	// argument out before parsing the rest.
	rawArgs := os.Args[1:]
	var src string
	if len(rawArgs) > 0 && !strings.HasPrefix(rawArgs[0], "-") {
		src, rawArgs = rawArgs[0], rawArgs[1:]
	}
	flag.CommandLine.Parse(rawArgs)
	if src == "" {
		src = flag.Arg(0)
	}
	if src == "" {
		usage()
		os.Exit(2)
	}

	if err := run(src, *out, *sheetsCSV, *maxCells, *noVBA, *noGrid, *force); err != nil {
		fmt.Fprintln(os.Stderr, "disassemble_excel:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "%s", `disassemble_excel `+toolVersion+` — Excel のシート・数式・VBA を diff 可能なテキストに分解する

.xlsx/.xlsm ワークブック 1 ファイルを、行指向のプレーンテキスト群に書き出します:
全セルの数式とキャッシュ値の両方、定義名、VBA マクロのソースコード。
数式は抽出のみで評価（再計算）はしません。共有数式は展開され、各セルが
実際に計算している数式が見えます。.xlsx/.xlsm の解体は完全オフラインで動作します。

.xlsb（バイナリ形式）は直接解体できませんが、LibreOffice (soffice) が
PATH かインストール先に見つかれば、自動で .xlsm に変換してから解体します
（この場合のみ外部プロセスを起動します）。

使い方:
  disassemble_excel <file.xlsx|.xlsm|.xlsb> [options]
  （フラグはファイル引数の前後どちらに置いてもよい）

オプション:
  -o DIR         出力先ディレクトリ（既定: <ファイル名から拡張子を除いたもの>/）
  -sheets a,b,c  指定したシートだけを抽出（既定: 全シート）
  -max-cells N   セル数が N を超えるシートには .slim.tsv も出力（既定 100000、0 で無効）
  -no-grid       グリッド形式の values.csv を出力しない
  -no-vba        VBA マクロを抽出しない
  -f             出力先ディレクトリが既存でも上書きする

出力レイアウト:
  <out>/
    manifest.json            元ファイルの SHA-256、シートごとの寸法・セル数、モジュール一覧
    defined_names.tsv        name  scope  refers_to（名前付き範囲、LAMBDA 引数）
    sheets/<name>.cells.tsv  cell  type  formula  value — 1 セル 1 行、
                             (行, 列) でソート、空セルは省略。値の中のタブ・改行は
                             \t \n にエスケープされる
    sheets/<name>.values.csv キャッシュ値のグリッド（目視確認用）
    sheets/<name>.slim.tsv   数式セルのみの圧縮ビュー（-max-cells 超のシートにだけ出力）
    vba/<module>.bas|.cls    コードを持つ VBA モジュールごとに 1 ファイル
    vba_index.tsv            module  proc  kind  module_line — Sub/Function/Property の索引
    objects.tsv              sheet  object  kind  anchor  macro — 画像・図形・ボタンに
                             割り当てられた（クリックで実行される）マクロ。画像自体は復元しない

  value は Excel がファイルに最後に保存したキャッシュ値であり、再計算結果ではない。
  空の VBA モジュール（シートクラスのスタブ等）はファイル化されず、
  manifest.json に "empty": true として記録される。

実行例:
  # すべてを ./report/ に抽出する
  disassemble_excel report.xlsm

  # 出力先を指定し、2 シートだけ抽出する
  disassemble_excel report.xlsm -o out/report -sheets Summary,Data

  # 複数ファイルを 1 ファイル 1 プロセスで処理する
  for f in *.xlsm; do disassemble_excel "$f" -o "out/${f%.xlsm}" -f; done

  # 同じワークブックのバージョン間・変種間を比較する
  diff -ru out/report_v1/sheets out/report_v2/sheets

終了ステータス:
  0 成功, 1 抽出エラー, 2 使い方の誤り
`)
}

func run(src, outDir, sheetsCSV string, maxCells int, noVBA, noGrid, force bool) error {
	start := time.Now()

	// .xlsb (Excel Binary Workbook) is a ZIP container of BIFF12 *binary* records,
	// not the OOXML (ZIP+XML) parts this tool reads, so excelize and the XML
	// parsers below cannot open it. If LibreOffice (soffice) is available, convert
	// it to .xlsm first (xlsm, not xlsx, to keep VBA). This is the ONLY place the
	// tool ever spawns a subprocess; the .xlsx/.xlsm path stays fully offline.
	origSrc := src
	if strings.EqualFold(filepath.Ext(src), ".xlsb") {
		converted, cleanup, err := convertXLSB(src)
		if err != nil {
			return err
		}
		defer cleanup()
		src = converted
	}

	if outDir == "" {
		base := filepath.Base(origSrc)
		outDir = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if err := prepareOutDir(outDir, force); err != nil {
		return err
	}

	xl, err := excelize.OpenFile(src)
	if err != nil {
		return fmt.Errorf("open workbook: %w", err)
	}
	defer xl.Close()

	zr, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	var only map[string]bool
	if sheetsCSV != "" {
		only = map[string]bool{}
		for _, s := range strings.Split(sheetsCSV, ",") {
			only[strings.TrimSpace(s)] = true
		}
	}

	shared := parseSharedStrings(&zr.Reader)
	wbSheets := parseWorkbookSheets(&zr.Reader)

	sheetsDir := filepath.Join(outDir, "sheets")
	if err := os.MkdirAll(sheetsDir, 0o755); err != nil {
		return err
	}

	var sheetManifests []SheetManifest
	for _, ws := range wbSheets {
		if only != nil && !only[ws.Name] {
			continue
		}
		sh := extractSheet(&zr.Reader, ws.Target, ws.Name, shared, xl)
		if sh == nil {
			continue
		}
		fileBase := filepath.Join(sheetsDir, sanitizeFilename(ws.Name))
		if err := writeCellsTSV(fileBase+".cells.tsv", sh); err != nil {
			return err
		}
		if !noGrid {
			if err := writeValuesCSV(fileBase+".values.csv", sh); err != nil {
				return err
			}
		}
		slim := maxCells > 0 && len(sh.Cells) > maxCells
		if slim {
			if err := writeSlimTSV(fileBase+".slim.tsv", sh); err != nil {
				return err
			}
		}
		sheetManifests = append(sheetManifests, SheetManifest{
			Name: ws.Name, Dimension: sh.Dimension, Cells: len(sh.Cells), Formulas: sh.FormulaN, Slim: slim,
		})
		fmt.Fprintf(os.Stderr, "  sheet %-22s cells=%-7d formulas=%-6d%s\n",
			ws.Name, len(sh.Cells), sh.FormulaN, slimNote(slim))
	}

	if err := writeDefinedNames(filepath.Join(outDir, "defined_names.tsv"), extractDefinedNames(xl)); err != nil {
		return err
	}

	var modManifests []ModManifest
	if !noVBA {
		mods, err := extractVBAFromZip(&zr.Reader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  vba: %v (skipped)\n", err)
		} else if len(mods) > 0 {
			vbaDir := filepath.Join(outDir, "vba")
			if err := os.MkdirAll(vbaDir, 0o755); err != nil {
				return err
			}
			for _, m := range mods {
				mm := ModManifest{Name: m.Name, Type: m.Type, Empty: m.Empty, Procs: len(m.Procs), Lines: lineCount(m.Source)}
				if !m.Empty {
					if err := writeModule(vbaDir, m); err != nil {
						return err
					}
					mm.File = "vba/" + sanitizeFilename(m.Name) + m.Ext
				}
				modManifests = append(modManifests, mm)
			}
			if err := writeVBAIndex(filepath.Join(outDir, "vba_index.tsv"), mods); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "  vba: %d modules\n", len(mods))
		}
	}

	objects := extractObjects(&zr.Reader, wbSheets, only)
	if len(objects) > 0 {
		if err := writeObjectsTSV(filepath.Join(outDir, "objects.tsv"), objects); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "  objects: %d with assigned macro\n", len(objects))
	}

	man := Manifest{
		Source:      filepath.Base(origSrc),
		SHA256:      fileSHA256(origSrc),
		ExtractedAt: start.Format(time.RFC3339),
		ToolVersion: toolVersion,
		Sheets:      sheetManifests,
		VBAModules:  modManifests,
		Objects:     objects,
	}
	if err := writeManifest(filepath.Join(outDir, "manifest.json"), man); err != nil {
		return err
	}

	srcAbs, err := filepath.Abs(origSrc)
	if err != nil {
		srcAbs = origSrc
	}
	if err := writeReadme(filepath.Join(outDir, "README.md"), man, srcAbs, noGrid); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "done: %s (%d sheets, %d VBA modules) in %s\n",
		outDir, len(sheetManifests), len(modManifests), time.Since(start).Round(time.Millisecond))
	return nil
}

// convertXLSB converts an .xlsb workbook to .xlsm via LibreOffice (soffice) so
// the OOXML disassembly path can read it. It returns the converted file path and
// a cleanup func that removes the temporary directory. The "Calc MS Excel 2007
// VBA" export filter is named explicitly so macros are preserved (a plain xlsx
// conversion would drop VBA, which is one of this tool's main outputs).
func convertXLSB(src string) (string, func(), error) {
	soffice := findSoffice()
	if soffice == "" {
		return "", nil, fmt.Errorf(`.xlsb は OOXML (ZIP+XML) 形式ではないため直接解体できません。
自動変換に使う LibreOffice (soffice) が見つかりませんでした。次のいずれかで対応してください:
  1. LibreOffice をインストールする (macOS: brew install --cask libreoffice)。
     インストール後はこのツールが自動で .xlsm に変換して解体します。
  2. Excel か LibreOffice で手動で .xlsm へ変換してから渡す
     (VBA マクロを残すため .xlsx ではなく .xlsm を選ぶこと)`)
	}

	tmpDir, err := os.MkdirTemp("", "disassemble_excel_xlsb_*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(tmpDir) }

	fmt.Fprintf(os.Stderr, "  xlsb: LibreOffice で .xlsm に変換中 (%s)\n", filepath.Base(soffice))
	cmd := exec.Command(soffice, "--headless",
		"--convert-to", "xlsm:Calc MS Excel 2007 VBA",
		"--outdir", tmpDir, src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("soffice での xlsb→xlsm 変換に失敗: %w\n%s", err, out)
	}

	converted := filepath.Join(tmpDir,
		strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))+".xlsm")
	if _, err := os.Stat(converted); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("変換後の .xlsm が見つかりません: %s\nsoffice 出力:\n%s", converted, out)
	}
	return converted, cleanup, nil
}

// findSoffice locates the LibreOffice CLI on PATH or in the macOS default
// install location. Returns "" if not found.
func findSoffice() string {
	for _, name := range []string{"soffice", "libreoffice"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	for _, p := range []string{
		"/Applications/LibreOffice.app/Contents/MacOS/soffice",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func extractVBAFromZip(zr *zip.Reader) ([]Module, error) {
	rc := openZip(zr, "xl/vbaProject.bin")
	if rc == nil {
		return nil, fmt.Errorf("no vbaProject.bin (not a macro workbook)")
	}
	defer rc.Close()
	bin, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return ExtractVBA(bin)
}

func prepareOutDir(dir string, force bool) error {
	info, err := os.Stat(dir)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s exists and is not a directory", dir)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) > 0 && !force {
			return fmt.Errorf("%s already exists and is not empty (use -f to overwrite)", dir)
		}
	}
	return os.MkdirAll(dir, 0o755)
}

func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func slimNote(slim bool) string {
	if slim {
		return "  (+slim)"
	}
	return ""
}
