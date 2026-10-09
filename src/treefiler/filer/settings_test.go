package filer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveSettingKeepsOtherLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "treefiler.toml")
	orig := "# 手で書いたコメント\nground = \"vellum\" # 紙\n# row_spacing = 9\nrow_spacing = 1 # 手のメモ\n"
	mustWrite(t, p, orig)
	if err := saveSetting(p, "row_spacing", "2", false); err != nil {
		t.Fatal(err)
	}
	if err := saveSetting(p, "legend", "false", false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	want := "# 手で書いたコメント\nground = \"vellum\" # 紙\n# row_spacing = 9\nrow_spacing = 2 # 手のメモ\nlegend = false\n"
	if string(b) != want {
		t.Fatalf("保存の後:\n%s\nwant:\n%s", b, want)
	}
	s, bad := loadSettings(p)
	if len(bad) != 0 || s.Ground != "vellum" || s.RowSpacing != 2 || s.Legend {
		t.Fatalf("読み戻し: %+v bad=%v", s, bad)
	}
}

func TestLoadSettingsReportsBadLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "treefiler.toml")
	body := "row_spacing = 99\nnope = 1\ncolumns = \"fit\"\nground\n"
	mustWrite(t, p, body)
	s, bad := loadSettings(p)
	if len(bad) != 3 || !strings.HasPrefix(bad[0], "line 1:") || !strings.HasPrefix(bad[1], "line 2:") || !strings.HasPrefix(bad[2], "line 4:") {
		t.Fatalf("悪い行の報告: %q", bad)
	}
	if s.RowSpacing != defaultSettings().RowSpacing { // 範囲外の値は既定のまま
		t.Fatalf("row_spacing = %d", s.RowSpacing)
	}
}

func panelTo(t *testing.T, m *Model, key string) {
	t.Helper()
	if !m.panel.open {
		m.HandleKey(",")
	}
	if !m.panel.open {
		t.Fatal(", で設定の板が開かない")
	}
	for i := range items {
		if items[i].key == key {
			m.panel.row = i
			return
		}
	}
	t.Fatalf("項目 %s が無い", key)
}

func TestPanelAdjustSavesAndApplies(t *testing.T) {
	m := newTest(t)
	panelTo(t, m, "ground")
	m.HandleKey("l")
	if m.set.Ground != "parchment" || cBg != findGround("parchment").bg {
		t.Fatalf("ground = %s cBg = %v", m.set.Ground, cBg)
	}
	if m.set.Palette != "growth" { // 紙の地では dark の組を選べない
		t.Fatalf("palette = %s", m.set.Palette)
	}
	b, err := os.ReadFile(configPath())
	if err != nil || !strings.Contains(string(b), "ground = \"parchment\"") {
		t.Fatalf("保存されていない: %q %v", b, err)
	}
	m.HandleKey("r") // 既定へ戻す
	if m.set.Ground != "dark" || cBg != grounds[0].bg {
		t.Fatalf("r の後 ground = %s", m.set.Ground)
	}
	m.HandleKey("esc")
	if m.panel.open {
		t.Fatal("esc で閉じない")
	}
}

func TestPanelIntClampsAndBoolToggles(t *testing.T) {
	m := newTest(t)
	panelTo(t, m, "row_spacing")
	for range 10 {
		m.HandleKey("l")
	}
	if m.set.RowSpacing != 3 {
		t.Fatalf("row_spacing = %d (上限 3)", m.set.RowSpacing)
	}
	panelTo(t, m, "folders_first")
	m.HandleKey("enter")
	if !m.set.FoldersFirst {
		t.Fatal("folders_first が反転しない")
	}
	if k := m.root.kids; !k[0].dir { // 並べ直されている
		t.Fatalf("先頭 = %s", k[0].name)
	}
}

func TestPanelOwnsKeys(t *testing.T) {
	m := newTest(t)
	m.HandleKey(",")
	if !m.OwnsKeys() {
		t.Fatal("板が開いている間はキーを持つ (glogx の F などを横取りされないため)")
	}
	cur := m.cur
	m.HandleKey("j")
	if m.cur != cur {
		t.Fatal("板の j が木を動かした")
	}
}

func TestRememberPlace(t *testing.T) {
	m := newTest(t)
	panelTo(t, m, "remember")
	m.HandleKey("l")
	m.HandleKey("esc")
	if !m.set.Remember {
		t.Fatal("remember が on にならない")
	}
	b := m.loadPath(filepath.Join(m.root.abs, "b"))
	x := m.loadPath(filepath.Join(m.root.abs, "b", "deep", "x.txt"))
	if b == nil || x == nil {
		t.Fatal("fixture の b/deep/x.txt が無い")
	}
	for a := x.parent; a != nil; a = a.parent {
		a.expanded = true
	}
	m.setCur(x)
	m.Close()

	m2, err := New(m.root.abs, Options{Now: m.now})
	if err != nil {
		t.Fatal(err)
	}
	if m2.cur.path() != x.path() {
		t.Fatalf("カーソル = %s, want %s", m2.cur.path(), x.path())
	}
	if n := m2.loadPath(filepath.Join(m.root.abs, "b", "deep")); n == nil || !n.expanded {
		t.Fatal("b/deep が開いていない")
	}

	// 別のフォルダ (子の a) から開いて閉じた記録は、親から開いたときに使わない
	a, err := New(filepath.Join(m.root.abs, "a"), Options{Now: m.now})
	if err != nil {
		t.Fatal(err)
	}
	two := a.loadPath(filepath.Join(a.root.abs, "two.go"))
	a.setCur(two)
	a.Close()
	m3, err := New(m.root.abs, Options{Now: m.now})
	if err != nil {
		t.Fatal(err)
	}
	if m3.cur.path() != x.path() {
		t.Fatalf("親の起点で子の記録を使った: %s", m3.cur.path())
	}
}

func TestRememberOffDoesNotWrite(t *testing.T) {
	m := newTest(t)
	m.Close()
	if _, err := os.Stat(placesPath()); !os.IsNotExist(err) {
		t.Fatalf("remember が off なのに書いた: %v", err)
	}
}

func TestPaperGroundsShareColors(t *testing.T) {
	for _, g := range grounds[1:] {
		if g.dot == (rgb{}) || g.untracked == (rgb{}) || g.ripple == (rgb{}) {
			t.Fatalf("%s の共通色が空", g.name)
		}
	}
}

// palette の選択肢は ground で決まる。ファイルで palette が ground より前にあっても拒否しない (レビューの指摘 2026-10-09)。
func TestLoadSettingsAppliesGroundBeforePalette(t *testing.T) {
	p := filepath.Join(t.TempDir(), "treefiler.toml")
	mustWrite(t, p, "palette = \"growth\"\nground = \"parchment\"\n")
	s, bad := loadSettings(p)
	if len(bad) != 0 || s.Palette != "growth" {
		t.Fatalf("palette = %s bad = %v", s.Palette, bad)
	}
}

// ground を変えて palette を直したら、palette も保存する (残すと次に読んだとき板と実際の色が食い違う)。
func TestGroundChangeSavesFittedPalette(t *testing.T) {
	m := newTest(t)
	panelTo(t, m, "palette")
	m.HandleKey("l") // dark の組の 2 つ目 (magma) を保存する
	panelTo(t, m, "ground")
	m.HandleKey("l")
	s, bad := loadSettings(configPath()) // palette = magma が残っていると、紙の地では読めない行になる
	if len(bad) != 0 || s.Ground != "parchment" || s.Palette != "growth" {
		t.Fatalf("読み戻し: ground=%s palette=%s bad=%v", s.Ground, s.Palette, bad)
	}
}

// Live を off で始めても Changed は待てるチャネルを返し、on に戻すと同じチャネルに合図が届く
// (閉じたチャネルを返すと glogx の待ちが終わり、on に戻しても誰も待たない。レビューの指摘 2026-10-09)。
func TestLiveToggleKeepsChannel(t *testing.T) {
	m := newTest(t)
	panelTo(t, m, "live")
	m.HandleKey("l") // off
	if m.set.Live {
		t.Fatal("live が off にならない")
	}
	ch := m.Changed()
	defer m.Close()
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("Live が off のとき Changed が閉じたチャネルを返した")
		}
	default:
	}
	m.HandleKey("l") // on
	if m.WatchingChan() != ch {
		t.Fatal("on に戻したらチャネルが変わった (glogx は古いチャネルを待ったまま)")
	}
}

// explode_ignored を on にすると、git が無視するフォルダにも降りる。
func TestExplodeIgnoredSetting(t *testing.T) {
	for _, into := range []bool{false, true} {
		m := newTest(t)
		m.set.ExplodeIgnore = into
		setGit(m, "!! b/deep/\x00")
		b := m.findNode(m.root.path() + "/b")
		m.setCur(b)
		m.HandleKey("e")
		waitExplode(t, m)
		deep := m.findNode(b.path() + "/deep")
		if deep == nil || deep.expanded != into {
			t.Fatalf("explode_ignored=%v で b/deep の開き = %v", into, deep != nil && deep.expanded)
		}
	}
}

func waitExplode(t *testing.T, m *Model) {
	t.Helper()
	waitFor(t, "explode", func() bool {
		if m.exploding == nil {
			return true
		}
		m.Advance(fixedNow)
		return false
	})
}

// Git を off にしたら、off にする前に始めた取得の結果も取り込まず、読み直しでも取りに行かない。
func TestGitOffDropsInflightAndStopsFetching(t *testing.T) {
	needGit(t)
	dir := fixture(t)
	initRepo(t, dir, "trunk")
	m := newAt(t, dir) // New が git の取得を始める
	m.Resize(120, 40)
	panelTo(t, m, "git")
	m.HandleKey("l") // off (取得はまだ走っているか、結果が取り込まれずに残っている)
	if m.set.Git {
		t.Fatal("git が off にならない")
	}
	settleBackground(t, m)
	if len(m.gitSnap.repos) != 0 {
		t.Fatal("off の後に取得の結果を取り込んだ")
	}
	m.Refresh()
	if m.git.busy() {
		t.Fatal("off なのに読み直しで git を取りに行った")
	}
}

// 名前の後ろの詳細は、列の中で右揃え (名前の長さによらず同じ桁で終わる。spec §3.4)。
func TestDetailsRightAlignedInColumn(t *testing.T) {
	m := newTest(t)
	m.set.Details = "age"
	settle(m)
	ends := map[string]int{}
	for _, l := range m.draw().plain() {
		for _, name := range []string{"c.txt ", "file10.txt "} {
			i := strings.Index(l, name)
			if i < 0 {
				continue
			}
			rest := l[i:]
			j := strings.Index(rest, "1h")
			if j < 0 {
				t.Fatalf("%q の行に詳細が無い: %q", name, l)
			}
			ends[name] = widthOf(l[:i+j+2])
		}
	}
	if len(ends) != 2 || ends["c.txt "] != ends["file10.txt "] {
		t.Fatalf("詳細の終わりの桁が揃わない: %v", ends)
	}
}

// git が無視する枝は細線になる (double: 縦は二重のまま、その枝の横だけ細い。spec §2.1 の thin)。
func TestIgnoredBranchIsThin(t *testing.T) {
	m := newTest(t)
	setGit(m, "!! b/\x00")
	settle(m)
	var bRow, cRow string
	for _, l := range m.draw().plain() {
		if strings.Contains(l, "═b/") || strings.Contains(l, "─b/") {
			bRow = l
		}
		if strings.Contains(l, "c.txt") {
			cRow = l
		}
	}
	if !strings.Contains(bRow, "╟─b") {
		t.Fatalf("無視される b の枝が細くない: %q", bRow)
	}
	if !strings.Contains(cRow, "╠═") {
		t.Fatalf("ふつうの枝まで細くなった: %q", cRow)
	}
	m.set.DimIgnored = false
	for _, l := range m.draw().plain() {
		if strings.Contains(l, "─b/") {
			t.Fatalf("Dim ignored が off でも細い: %q", l)
		}
	}
}
