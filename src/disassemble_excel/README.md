# disassemble_excel

Disassemble an Excel workbook (`.xlsx` / `.xlsm`) into a directory of plain,
diffable text files: every sheet as one-cell-per-line TSV (formula **and** cached
value), defined names, and VBA macro source — one file per module.

Built for reading and **diffing** spreadsheet logic in version control, and for
porting heavy Excel models (with formulas + macros) to real code.

## Why

Spreadsheets are zip archives of XML plus an OLE2 blob for macros, so they don't
diff and aren't greppable. `disassemble_excel` turns one workbook into stable,
line-oriented text you can `diff`, `grep`, and review:

- **Formula *and* value per cell.** The formula is the logic; the cached value is
  the expected output. Both are emitted, side by side.
- **Shared formulas are expanded.** A workbook can contain tens of thousands of
  shared-formula child cells whose formula text is empty in the raw XML. Each
  cell's real formula is resolved, so you see what every cell actually computes.
- **VBA macros, decompressed in pure Go.** No `olevba`, no Python, no external
  tool. Module source is extracted straight from `vbaProject.bin`.
- **Fully offline.** At runtime it only reads the input file and writes output
  files — no network, no subprocesses.

## Features

- Sheets → `sheets/<name>.cells.tsv` — `cell  type  formula  value`, sorted by
  (row, col), empty cells omitted.
- Sheets → `sheets/<name>.values.csv` — a plain grid of cached values for eyeballing.
- Huge sheets → `sheets/<name>.slim.tsv` — formula cells only plus a summary of
  the value-only cells (for sheets above a cell threshold).
- VBA → `vba/<module>.bas` / `.cls`, one file per module, plus `vba_index.tsv`
  (module / proc / kind / line) to navigate to each `Sub`/`Function`.
- Defined names (named ranges, LAMBDA args) → `defined_names.tsv`.
- `manifest.json` — source SHA-256, per-sheet dimensions and counts, module list.
- `README.md` — a human-readable provenance file written into the output dir:
  the source workbook's absolute path, SHA-256, and what every emitted file means.
- Shared strings are resolved to real text; non-ASCII (e.g. Japanese, via the
  project code page) is decoded in both cell strings and VBA source.

## Install

Requires Go 1.23+.

Install the latest version directly:

```sh
go install github.com/jiikko/disassemble_excel@latest
```

Or build from source:

```sh
git clone https://github.com/jiikko/disassemble_excel
cd disassemble_excel
go build -o disassemble_excel .
# optionally move ./disassemble_excel onto your PATH
```

The first build downloads the Go module dependencies; after that it builds and
runs entirely offline.

## Usage

```sh
disassemble_excel <file.xlsx|.xlsm> [options]
# flags may appear before or after the file argument
```

| flag | meaning |
|------|---------|
| `-o DIR` | output directory (default: the file name without extension) |
| `-sheets a,b,c` | only extract these sheets (default: all) |
| `-max-cells N` | sheets with more than N cells also get a `.slim.tsv` (default 100000; 0 disables) |
| `-no-grid` | skip the grid `values.csv` (the `cells.tsv` is always written) |
| `-no-vba` | skip VBA extraction |
| `-f` | overwrite the output directory if it already exists |

Examples:

```sh
# extract everything into ./report/
disassemble_excel report.xlsm

# choose the output directory and only two sheets
disassemble_excel report.xlsm -o out/report -sheets Summary,Data

# many files, one process each
for f in *.xlsm; do disassemble_excel "$f" -o "out/${f%.xlsm}" -f; done
```

## Output layout

```
<out>/
  README.md                      # provenance: source file path, SHA-256, and what each file means
  manifest.json
  defined_names.tsv              # name  scope  refers_to
  sheets/
    Summary.cells.tsv            # cell  type  formula  value   (one cell per line)
    Summary.values.csv           # grid of cached values
    Data.cells.tsv
    Data.values.csv
    Big.cells.tsv                # huge sheets keep the full dump...
    Big.slim.tsv                 # ...plus a compact formula-only view
  vba/
    Module1.bas                  # standard module
    ThisWorkbook.cls             # class/document module
  vba_index.tsv                  # module  proc  kind  module_line
```

`cells.tsv` example:

```
cell	type	formula	value
A1	n		1
E3	n	=Inputs!C2	46054
J12	n	=SUMIF($D$20:$II$20,J11,$D$59:$II$59)/SUMIF($D$20:$II$20,J11,$D$42:$II$42)	8.71798
C1	s		Start date
```

Values are the cached values stored in the workbook; tabs/newlines inside a
value are escaped so each cell stays on one line.

## How it works

- **Cells, values, types, inline formulas** are read by parsing the worksheet
  XML directly (fast, single pass, low memory).
- **Shared / array formulas** are expanded with
  [excelize](https://github.com/xuri/excelize); only the shared-formula child
  cells need it, not the value-only cells.
- **VBA** is extracted with a pure-Go pipeline: [mscfb](https://github.com/richardlehane/mscfb)
  opens the OLE2 container, and the bundled `ovba` package implements the
  [MS-OVBA] decompression and `dir`-stream parsing. Module source is decoded
  using the project code page.

## Limitations

- Formulas are **extracted, not evaluated**. The tool reports the formula text
  and the value Excel last cached; it does not recompute anything.
- Styles, charts, images, pivot tables and conditional formatting are not
  extracted (the focus is logic, not presentation).
- The workbook is loaded into memory; a ~70 MB macro workbook needs roughly
  1 GB of RAM and processes in a few seconds.

## License

[MIT](LICENSE)

## Acknowledgements

- [xuri/excelize](https://github.com/xuri/excelize)
- [richardlehane/mscfb](https://github.com/richardlehane/mscfb)
- VBA extraction follows the [MS-OVBA] specification.

[MS-OVBA]: https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-ovba/
