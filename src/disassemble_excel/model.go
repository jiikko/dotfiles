package main

// Cell is one non-empty worksheet cell.
type Cell struct {
	Row, Col int    // 1-based
	Addr     string // A1-style
	Type     string // raw cell type: n, s, str, b, e, inlineStr ...
	Formula  string // without leading '=' ("" if none)
	Value    string // cached value (shared strings already resolved)

	needExpand bool // shared-formula child whose formula must be expanded via excelize
}

// Sheet holds the extracted cells of one worksheet.
type Sheet struct {
	Name      string
	Dimension string
	Cells     []Cell
	FormulaN  int
	ValueN    int
}

// Module is one VBA module extracted from vbaProject.bin.
type Module struct {
	Name       string
	StreamName string
	Type       string // standard | class
	Ext        string // .bas | .cls
	TextOffset uint32
	Source     string
	Procs      []Proc
	Empty      bool
}

// Proc is a Sub/Function/Property declaration found inside a module.
type Proc struct {
	Name string
	Kind string // Sub | Function | Property
	Line int    // 1-based line within the module source
}

// --- manifest.json ---

type Manifest struct {
	Source      string          `json:"source"`
	SHA256      string          `json:"sha256"`
	ExtractedAt string          `json:"extracted_at"`
	ToolVersion string          `json:"tool_version"`
	Sheets      []SheetManifest `json:"sheets"`
	VBAModules  []ModManifest   `json:"vba_modules"`
}

type SheetManifest struct {
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
	Cells     int    `json:"cells"`
	Formulas  int    `json:"formulas"`
	Slim      bool   `json:"slim"` // a .slim.tsv was also written
}

type ModManifest struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	File  string `json:"file,omitempty"`
	Empty bool   `json:"empty"`
	Procs int    `json:"procs"`
	Lines int    `json:"lines"`
}
