package filer

import "path/filepath"

// debug.go は動作の確かめ (tools/tfperf の -snap。Claude が実物を動かして状態の値で判定する) のための状態の写し。
// 書き出すのは bin/treefiler の main (TREEFILER_DEBUG)。ここはファイルを読み書きしない。

// DebugState は検証に使う内部の状態。値は JSON で 1 行に書く (名前は tfperf の読み手と揃える)。
type DebugState struct {
	Root      string `json:"root"`       // root の絶対パス
	Cursor    string `json:"cursor"`     // root からのカーソルの相対パス ("." = root)
	CursorDir bool   `json:"cursor_dir"` // カーソルがフォルダ
	Depth     int    `json:"depth"`      // カーソルの深さ (root = 0)
	Layer     string `json:"layer"`      // 全部のキーを取っている層 (help / panel / ask / search / prompt。無ければ "")
	Tiles     int    `json:"tiles"`      // 開いているタイルの枚数
	Animating bool   `json:"animating"`
	Busy      bool   `json:"busy"`
	TreeWidth int    `json:"tree_width"` // 木を描く幅 (右のプレビューの欄を除く)

	Holding bool `json:"holding"` // キーの押しっぱなしの最中 (pulse.go)
	Pulses  int  `json:"pulses"`  // 走っている経路の光の本数

	PaneWidth   int    `json:"pane_width"`   // 右のプレビューの欄の桁 (0 = 出していない)
	PaneGen     int    `json:"pane_gen"`     // 最後に頼んだ読み込みの世代
	PaneWaiting bool   `json:"pane_waiting"` // 頼んだ中身が届くのを待っている
	PaneShown   string `json:"pane_shown"`   // 描いている本文のファイル (root からの相対パス。無ければ "")
	PaneFresh   bool   `json:"pane_fresh"`   // 描いている本文が今のカーソル・大きさのもの (false なら薄く出している)
	PaneNote    string `json:"pane_note"`    // 本文の代わりに出している 1 行 (開けない理由・空のファイル)
	PaneLines   int    `json:"pane_lines"`   // 描いている本文の行の数
}

// DebugState は今の状態を写す。
func (m *Model) DebugState() DebugState {
	s := DebugState{
		Root: m.root.path(), Cursor: m.relPath(m.cur.path()), CursorDir: m.cur.dir, Depth: m.cur.depth() - m.root.depth(),
		Layer: m.debugLayer(), Tiles: len(m.tiles), Animating: m.Animating(), Busy: m.Busy(), TreeWidth: m.treeWidth(),
		Holding: m.holding, Pulses: len(m.pulses),
		PaneWidth: m.paneWidth(), PaneGen: m.pane.gen, PaneWaiting: m.pane.waiting,
	}
	if r := m.pane.shown; r != nil {
		s.PaneShown = m.relPath(r.key.path)
		s.PaneFresh = m.paneFresh(r)
		s.PaneNote = r.note
		s.PaneLines = len(r.lines)
	}
	return s
}

// relPath は root からの相対パス (root の外は ../ で始まる。Rel が失敗したときだけ絶対パスのまま)。
func (m *Model) relPath(p string) string {
	if r, err := filepath.Rel(m.root.path(), p); err == nil {
		return r
	}
	return p
}

// debugLayer は全部のキーを取っている層の名前 (HandleInput がキーを渡す順と同じ)。
func (m *Model) debugLayer() string {
	switch {
	case m.help:
		return "help"
	case m.panel.open:
		return "panel"
	case m.ask != nil:
		return "ask"
	case m.search.active:
		return "search"
	case m.prompt.active:
		return "prompt"
	}
	return ""
}
