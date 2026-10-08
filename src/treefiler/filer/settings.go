package filer

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"atomicfile"

	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// settings.go は設定の板 (`,`。spec §7・§8.2) と保存 (spec §0.5)。
//
// 保存先は ~/.config/glogx/treefiler.toml (ユーザー回答 2026-10-07)。TREEFILER_CONFIG_DIR で差し替える (テストが本物を触らないため)。
// 形式は treebeard と同じフラットな `key = value`。変えた 1 行だけ書き換え、他の行とコメントは保つ。
//
// 写さない項目 (spec §0.1): Pipes の別方式・River tracks・Sort by / Reverse (ソートは後回し)・Step through (j k は兄弟の中と決めた)・
// Mouse・Wheel speed・Momentum・Image previews・Block glyphs (画像は後回し)・Text preview (glow / bat は呼ばない)。
// 未対応: Wrap lines (タイルの長い行は今は切る)。

// Settings は設定の値。
type Settings struct {
	RowSpacing    int
	ColumnGap     int
	MaxName       int
	Columns       string // fit / equal
	Details       string // off / age / size / both
	FoldersFirst  bool
	NaturalSort   bool
	ShowHidden    bool
	Ground        string
	Accent        string
	Palette       string
	HeatRange     string
	FocusDim      int
	Lines         string // double / heavy / rounded / square / ascii
	BranchOffset  int
	Legend        bool
	Speed         string // slow / normal / fast / instant
	Live          bool
	Ripples       bool
	Git           bool
	DimIgnored    bool
	DimFloor      int
	ExplodeIgnore bool
	Remember      bool
	Wrap          bool
}

func defaultSettings() Settings {
	return Settings{RowSpacing: 0, ColumnGap: 3, MaxName: 28, Columns: "fit", Details: "off", NaturalSort: true,
		Ground: "dark", Accent: "indigo", Palette: "ember", HeatRange: "5y", FocusDim: 6, Lines: "double", BranchOffset: 1,
		Legend: true, Speed: "normal", Live: true, Ripples: true, Git: true, DimIgnored: true, DimFloor: 5, Wrap: true}
}

type itemKind int

const (
	kindInt itemKind = iota
	kindChoice
	kindBool
)

type item struct {
	section, label, key, help string
	kind                      itemKind
	min, max, step            int
	choices                   func(s *Settings) []string
	get                       func(s *Settings) string
	set                       func(s *Settings, v string) bool
}

func intItem(section, label, key, help string, lo, hi, step int, p func(s *Settings) *int) item {
	return item{section: section, label: label, key: key, help: help, kind: kindInt, min: lo, max: hi, step: step,
		get: func(s *Settings) string { return strconv.Itoa(*p(s)) },
		set: func(s *Settings, v string) bool {
			n, err := strconv.Atoi(v)
			if err != nil || n < lo || n > hi {
				return false
			}
			*p(s) = n
			return true
		}}
}

func choiceItem(section, label, key, help string, cs func(s *Settings) []string, p func(s *Settings) *string) item {
	return item{section: section, label: label, key: key, help: help, kind: kindChoice, choices: cs,
		get: func(s *Settings) string { return *p(s) },
		set: func(s *Settings, v string) bool {
			if !contains2(cs(s), v) {
				return false
			}
			*p(s) = v
			return true
		}}
}

func boolItem(section, label, key, help string, p func(s *Settings) *bool) item {
	return item{section: section, label: label, key: key, help: help, kind: kindBool,
		get: func(s *Settings) string { return strconv.FormatBool(*p(s)) },
		set: func(s *Settings, v string) bool {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return false
			}
			*p(s) = b
			return true
		}}
}

func fixed(vs ...string) func(*Settings) []string { return func(*Settings) []string { return vs } }

var items = []item{
	intItem("LAYOUT", "Row spacing", "row_spacing", "項目の間の空行 (0〜3)", 0, 3, 1, func(s *Settings) *int { return &s.RowSpacing }),
	intItem("LAYOUT", "Column gap", "column_gap", "次の列までの空白 (3〜12)", 3, 12, 1, func(s *Settings) *int { return &s.ColumnGap }),
	intItem("LAYOUT", "Column width", "max_name", "列の最大幅。長い名前は … で切る (12〜60)", 12, 60, 2, func(s *Settings) *int { return &s.MaxName }),
	choiceItem("LAYOUT", "Columns", "columns", "fit = 最長の名前に合わせる / equal = どの列も Column width", fixed("fit", "equal"), func(s *Settings) *string { return &s.Columns }),
	choiceItem("LAYOUT", "Name details", "details", "名前の後ろに更新からの時間・大きさを薄く出す", fixed("off", "age", "size", "both"), func(s *Settings) *string { return &s.Details }),
	boolItem("ORDER", "Folders first", "folders_first", "フォルダをファイルより上に並べる", func(s *Settings) *bool { return &s.FoldersFirst }),
	boolItem("ORDER", "Natural sort", "natural_sort", "file2 を file10 より前に並べる", func(s *Settings) *bool { return &s.NaturalSort }),
	boolItem("ORDER", "Dotfiles", "show_hidden", ". で始まる項目を見せる (. キーと同じ)", func(s *Settings) *bool { return &s.ShowHidden }),
	choiceItem("LOOK", "Ground", "ground", "地の色。dark か紙 (parchment / vellum)", fixed("dark", "parchment", "vellum"), func(s *Settings) *string { return &s.Ground }),
	choiceItem("LOOK", "Accent", "accent", "線・選択・強調の色 (紙の地では固定)", fixed(accentOrder...), func(s *Settings) *string { return &s.Accent }),
	choiceItem("LOOK", "Heat colors", "palette", "更新の新しさの色 (地ごとに選べる組が違う)", func(s *Settings) []string { return findGround(s.Ground).palettes }, func(s *Settings) *string { return &s.Palette }),
	choiceItem("LOOK", "Heat range", "heat_range", "いちばん冷たい色になる古さ", fixed("day", "week", "month", "year", "5y"), func(s *Settings) *string { return &s.HeatRange }),
	intItem("LOOK", "Off-line dim", "focus_dim", "選択線から外れた名前の明るさ (0 = ほぼ消える / 10 = そのまま)", 0, 10, 1, func(s *Settings) *int { return &s.FocusDim }),
	choiceItem("LOOK", "Tree lines", "lines", "線の形", fixed("double", "heavy", "rounded", "square", "ascii"), func(s *Settings) *string { return &s.Lines }),
	intItem("LOOK", "Branch offset", "branch_offset", "枝の分かれ目から名前までの線の長さ (0〜4)", 0, 4, 1, func(s *Settings) *int { return &s.BranchOffset }),
	boolItem("LOOK", "Legend", "legend", "ステータスバーに色の凡例を出す", func(s *Settings) *bool { return &s.Legend }),
	choiceItem("LOOK", "Motion", "speed", "動きの速さ (instant = 動かさない)", fixed("slow", "normal", "fast", "instant"), func(s *Settings) *string { return &s.Speed }),
	boolItem("BEHAVIOR", "Live updates", "live", "開いているフォルダの変化を 1 秒ごとに映す", func(s *Settings) *bool { return &s.Live }),
	boolItem("BEHAVIOR", "Ripples", "ripples", "変化した項目から上へ光を昇らせる", func(s *Settings) *bool { return &s.Ripples }),
	boolItem("BEHAVIOR", "Git status", "git", "git の印とブランチを出す", func(s *Settings) *bool { return &s.Git }),
	boolItem("BEHAVIOR", "Dim ignored", "dim_ignored", "git が無視するものを灰色に沈める", func(s *Settings) *bool { return &s.DimIgnored }),
	intItem("BEHAVIOR", "Dim floor", "dim_floor", "無視するものの見えやすさ (0 = ほぼ背景 / 10 = 灰色)", 0, 10, 1, func(s *Settings) *int { return &s.DimFloor }),
	boolItem("BEHAVIOR", "Explode ignored", "explode_ignored", "e で git が無視するフォルダも開く", func(s *Settings) *bool { return &s.ExplodeIgnore }),
	boolItem("BEHAVIOR", "Wrap lines", "wrap", "タイルの長い行を折り返す (off なら右端で切る)", func(s *Settings) *bool { return &s.Wrap }),
	boolItem("BEHAVIOR", "Remember place", "remember", "閉じたときの開いたフォルダとカーソルを、次に同じ場所で開いたときに戻す", func(s *Settings) *bool { return &s.Remember }),
}

func configDir() string {
	if d := os.Getenv("TREEFILER_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "glogx")
}

func configPath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "treefiler.toml")
}

// loadSettings は設定を読む。読めない行はその行だけ既定のまま残し、何行目が悪いかを返す (板の足元に出す)。
func loadSettings(path string) (Settings, []string) {
	s := defaultSettings()
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return s, []string{"設定を読めません: " + err.Error()}
	}
	// 項目の表の順に当てる (ground を palette より先に。palette の選択肢は ground で決まるので、ファイルの行の順に依らせない)
	type entry struct {
		line   int
		raw    string
		k, v   string
		ok     bool
		weight int
	}
	lines := strings.Split(string(b), "\n")
	es := make([]entry, 0, len(lines))
	for i, raw := range lines {
		line := raw
		if j := strings.Index(line, " #"); j >= 0 {
			line = line[:j]
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"`)
		es = append(es, entry{line: i + 1, raw: raw, k: k, v: v, ok: ok, weight: itemIndex(k)})
	}
	sort.SliceStable(es, func(i, j int) bool { return es[i].weight < es[j].weight })
	var badEs []entry
	for _, e := range es {
		it := findItem(e.k)
		if !e.ok || it == nil || !it.set(&s, e.v) {
			badEs = append(badEs, e)
		}
	}
	sort.Slice(badEs, func(i, j int) bool { return badEs[i].line < badEs[j].line }) // 何行目の順に出す
	bad := make([]string, 0, len(badEs))
	for _, e := range badEs {
		bad = append(bad, "line "+strconv.Itoa(e.line)+": "+termsafeLine(e.raw))
	}
	fitPalette(&s)
	return s, bad
}

func itemIndex(key string) int {
	for i := range items {
		if items[i].key == key {
			return i
		}
	}
	return -1
}

func findItem(key string) *item {
	if i := itemIndex(key); i >= 0 {
		return &items[i]
	}
	return nil
}

// saveSetting は key の行だけを書き換える (無ければ足す)。他の行・コメントは保つ。
func saveSetting(path, key, value string, quote bool) error {
	if path == "" {
		return errors.New("保存先が無い (ホームディレクトリが分からない)")
	}
	if quote {
		value = `"` + value + `"`
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	lines := []string{"# treefiler の設定。treefiler で , を押すと変えられる。手で直してもよい。"}
	if len(b) > 0 {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
	done := false
	for i, l := range lines {
		k, _, ok := strings.Cut(l, "=")
		if ok && strings.TrimSpace(k) == key { // コメント行の k は # で始まるので一致しない
			tail := ""
			if j := strings.Index(l, " #"); j >= 0 {
				tail = l[j:] // 行末のコメントは残す
			}
			lines[i] = key + " = " + value + tail
			done = true
		}
	}
	if !done {
		lines = append(lines, key+" = "+value)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// ---------- 板 ----------

type panelState struct {
	open bool
	row  int
}

// adjust は選んだ項目の値を d だけ動かす (数は step ずつ・選択肢は巡回・真偽は反転)。
func (m *Model) adjust(d int) {
	it := &items[m.panel.row]
	s := &m.set
	switch it.kind {
	case kindInt:
		n, _ := strconv.Atoi(it.get(s))
		n = max(it.min, min(it.max, n+d*it.step))
		it.set(s, strconv.Itoa(n))
	case kindChoice:
		cs := it.choices(s)
		i := indexOfStr(cs, it.get(s))
		it.set(s, cs[(i+d+len(cs))%len(cs)])
	case kindBool:
		v, _ := strconv.ParseBool(it.get(s))
		it.set(s, strconv.FormatBool(!v))
	}
	m.applySettings(it.key)
}

func (m *Model) resetItem() {
	it := &items[m.panel.row]
	def := defaultSettings()
	it.set(&m.set, it.get(&def))
	m.applySettings(it.key)
}

func indexOfStr(ss []string, s string) int {
	for i, x := range ss {
		if x == s {
			return i
		}
	}
	return 0
}

// applySettings は変えた設定を効かせて保存する。
func (m *Model) applySettings(key string) {
	errs := []error{}
	if fitPalette(&m.set) { // ground を変えたら、その ground で選べる組の先頭へ。直した palette も保存する (残すと次に読んだとき拒否される)
		errs = append(errs, m.save("palette"))
	}
	applyTheme(&m.set)
	labelMax, sortFoldersFirst, sortNatural = m.set.MaxName, m.set.FoldersFirst, m.set.NaturalSort
	m.showHidden = m.set.ShowHidden
	if key == "folders_first" || key == "natural_sort" {
		m.resort(m.root)
	}
	if key == "git" {
		m.gitSnap = gitSnapshot{}
		m.startGit(true)
	}
	if key == "live" {
		m.watch.pause(!m.set.Live)
	}
	m.moving = true
	m.saveErr = ""
	if err := errors.Join(append(errs, m.save(key))...); err != nil {
		m.saveErr = "保存できません: " + err.Error()
	}
}

func (m *Model) save(key string) error {
	it := findItem(key)
	if it == nil {
		return nil
	}
	return saveSetting(configPath(), key, it.get(&m.set), it.kind == kindChoice)
}

// fitPalette は palette が ground で選べる組に無ければ先頭へ直す (直したら true)。
func fitPalette(s *Settings) bool {
	g := findGround(s.Ground)
	if contains2(g.palettes, s.Palette) {
		return false
	}
	s.Palette = g.palettes[0]
	return true
}

func (m *Model) resort(n *node) {
	if !n.dir || !n.loaded {
		return
	}
	sortKids(n.kids, m.set.FoldersFirst, m.set.NaturalSort)
	for _, k := range n.kids {
		m.resort(k)
	}
}

func (m *Model) panelKey(k string) {
	switch k {
	case "esc", ",", "q", "ctrl+c":
		m.panel.open = false
	case "j", "down", "tab", "ctrl+n":
		m.panel.row = (m.panel.row + 1) % len(items)
	case "k", "up", "shift+tab", "ctrl+p":
		m.panel.row = (m.panel.row - 1 + len(items)) % len(items)
	case "g", "home":
		m.panel.row = 0
	case "G", "end":
		m.panel.row = len(items) - 1
	case "l", "right", "enter", "space", " ":
		m.adjust(1)
	case "h", "left":
		m.adjust(-1)
	case "r":
		m.resetItem()
	}
}

// drawPanel は右端の設定の板 (spec §8.2)。
func (m *Model) drawPanel(c *canvas) {
	w := min(52, m.w)
	h := m.h - 1
	if w < 24 || h < 6 {
		return
	}
	x0 := m.w - w
	c.clear(x0, 0, m.w-1, h-1, cPop)
	c.put(x0, 0, "╭"+strings.Repeat("─", w-2)+"╮", cAccRoute, false)
	for y := 1; y < h-1; y++ {
		c.put(x0, y, "│", cAccRoute, false)
		c.put(m.w-1, y, "│", cAccRoute, false)
	}
	c.put(x0, h-1, "╰"+strings.Repeat("─", w-2)+"╯", cAccRoute, false)
	c.put(x0+2, 0, " settings ", cText, true)
	inner := w - 4
	// 足元: 選んだ項目の説明・保存のエラー・設定ファイルの場所・キー
	type footLine struct {
		s string
		c rgb
	}
	foot := []footLine{{termwidthTrunc(items[m.panel.row].help, inner), cText}}
	if m.saveErr != "" {
		foot = append(foot, footLine{termwidthTrunc(m.saveErr, inner), cDot})
	}
	for _, e := range m.setErrs {
		foot = append(foot, footLine{termwidthTrunc(e, inner), cDot})
	}
	foot = append(foot, footLine{termwidthTrunc(configPath(), inner), cMuted}, footLine{"j k 移動  h l 変える  r 既定  esc 閉じる", cMuted})
	room := h - 2 - len(foot) - 1
	// 本文の行 (見出しと項目)
	type row struct {
		text  string
		item  int
		title bool
	}
	rows := make([]row, 0, 2*len(items))
	last := ""
	for i, it := range items {
		if it.section != last {
			if last != "" {
				rows = append(rows, row{item: -1})
			}
			rows = append(rows, row{text: it.section, item: -1, title: true})
			last = it.section
		}
		rows = append(rows, row{item: i})
	}
	sel := 0
	for i, r := range rows {
		if r.item == m.panel.row {
			sel = i
		}
	}
	skip := max(0, min(sel+1-room, len(rows)-room))
	y := 1
	for _, r := range rows[skip:] {
		if y > room {
			break
		}
		switch {
		case r.title:
			c.put(x0+2, y, r.text, cMuted, true)
		case r.item >= 0:
			it := items[r.item]
			val := it.get(&m.set)
			selected := r.item == m.panel.row
			if selected {
				c.fillBg(x0+1, y, m.w-2, y, mix(cPop, cCursorBg, 0.8))
				val = "‹ " + val + " ›"
			}
			lc := mix(cMuted, cText, 0.5)
			if selected {
				lc = cText
			}
			c.put(x0+2, y, it.label, lc, selected)
			c.put(m.w-2-widthOf(val), y, val, cAccRoute, selected)
		}
		y++
	}
	fy := h - 1 - len(foot)
	for _, f := range foot {
		c.put(x0+2, fy, f.s, f.c, false)
		fy++
	}
}

func termwidthTrunc(s string, w int) string { return termwidth.Truncate(s, w, "…") }

func termsafeLine(s string) string { return termsafe.PlainLine(s) }
