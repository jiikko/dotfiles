package filer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 隠した dotfile のフォルダの中にカーソルがあるとき、そのフォルダの変化も光る (見える判定は kids と同じ visible を通す。issue 694 の 3)。
func TestChangeOnHiddenCursorPathLights(t *testing.T) {
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".cfg"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a.txt", ".cfg/in.txt", ".other"} {
		mustWrite(t, filepath.Join(dir, p), "")
	}
	m := newAt(t, dir)
	m.set.ShowHidden, m.set.Ripples = false, true
	in := m.loadPath(filepath.Join(dir, ".cfg", "in.txt"))
	if in == nil {
		t.Fatal("前提: .cfg/in.txt を引けない")
	}
	m.setCur(in)
	cfg, other := in.parent, m.findNode(filepath.Join(dir, ".other"))
	m.applyChange(dirChange{dir: dir, changed: []string{".cfg", ".other"}})
	if _, ok := m.ripples[cfg]; !ok {
		t.Fatal("カーソルの経路にある .cfg の変化が光らない")
	}
	if _, ok := m.ripples[other]; ok {
		t.Fatal("隠している .other の変化が光った")
	}
}

// 選択肢つきの設定は、どの選択肢にも写しの表の行がある (speed を足して speedFactor を忘れると動きが止まる。issue 694 の 2)。
// 選択肢の設定を足したら、ここに表を足すか、写しの表を持たない理由を exempt に書く。
func TestChoiceSettingsHaveTables(t *testing.T) {
	in := func(m any) func(string) bool {
		return func(v string) bool {
			switch m := m.(type) {
			case map[string]float64:
				_, ok := m[v]
				return ok
			case map[string][]string:
				_, ok := m[v]
				return ok
			case map[string]accent:
				_, ok := m[v]
				return ok
			case map[string][7]rgb:
				_, ok := m[v]
				return ok
			}
			t.Fatalf("表の型 %T を知らない", m)
			return false
		}
	}
	tables := map[string]func(string) bool{
		"speed":      in(speedFactor),
		"lines":      in(lineGlyphs),
		"heat_range": in(heatRanges),
		"accent":     in(accents),
		"palette":    in(palettes),
		"ground": func(v string) bool {
			for _, g := range grounds {
				if g.name == v {
					return true
				}
			}
			return false
		},
	}
	exempt := map[string]string{ // 表を持たず、描く所の switch / 比較で読む (知らない値は何も足さないだけで、動きは止まらない)
		"columns": "layout.go の equal との比較",
		"details": "model.go の Details の switch",
	}
	seen := 0
	for _, it := range items {
		if it.kind != kindChoice {
			continue
		}
		seen++
		has, ok := tables[it.key]
		if !ok {
			if _, ok := exempt[it.key]; !ok {
				t.Errorf("選択肢の設定 %q に写しの表の検査が無い (tables か exempt に足す)", it.key)
			}
			continue
		}
		for _, g := range grounds { // 選べる組が地で変わる (palette) ので、地ごとに選択肢を取る
			s := defaultSettings()
			s.Ground = g.name
			for _, v := range it.choices(&s) {
				if !has(v) {
					t.Errorf("%s = %q (地 %s) に写しの表の行が無い", it.key, v, g.name)
				}
			}
		}
	}
	if seen < 8 {
		t.Fatalf("選択肢の設定を %d 個しか見ていない (items の走査が壊れている)", seen)
	}
}

// 入力欄のキャレットは、描いた入力の末尾の直後にある (描画と CaretPos が別々に幅を計算するとずれる。issue 694 の 5)。
// 窓より長い入力 (横に流れる) と短い入力、検索と `!` の両方で、画面の文字列から位置を測る。
func TestCaretFollowsDrawnInput(t *testing.T) {
	for _, open := range []string{"/", "!"} {
		for _, n := range []int{3, 300} {
			m := newTest(t)
			m.HandleKey(open)
			typeText(m, strings.Repeat("x", n)+"日本END")
			x, y, ok := m.CaretPos()
			if !ok || y != m.h-1 {
				t.Fatalf("%s %d: キャレットが足元に無い (%d, %d, %v)", open, n, x, y, ok)
			}
			row := m.draw().plain()[m.h-1]
			i := strings.Index(row, "END")
			if i < 0 {
				t.Fatalf("%s %d: 足元に入力の末尾が描かれていない: %q", open, n, row)
			}
			if want := widthOf(row[:i]) + 3; x != want {
				t.Errorf("%s %d: キャレット x = %d、描いた入力の末尾の直後は %d", open, n, x, want)
			}
		}
	}
}

// Binds が false のキーは、木の画面で何もしない (表に無いキーが動作を持つと、glogx がそのキーを横断に取って filer の動作が消える)。
// 逆に、キー一覧 (?) を開いている間は全部のキーを filer が持つ (閉じるだけのキーで glogx が横断しない。issue 694 の 6)。
func TestBindsMatchesTreeKeys(t *testing.T) {
	keys := []string{"up", "down", "left", "right", "enter", "esc", "tab", "space", "backspace", "home", "end", "pgup", "pgdown",
		"shift+space", "ctrl+a", "ctrl+e", "ctrl+k", "ctrl+l", "ctrl+r", "ctrl+w", "delete", "insert", "f1"}
	for r := '!'; r <= '~'; r++ {
		keys = append(keys, string(r))
	}
	unbound := 0
	for _, k := range keys {
		m := newTest(t)
		if m.Binds(k) {
			continue
		}
		unbound++
		before := m.draw().plain()
		cur, root := m.cur, m.root
		if r := m.HandleKey(k); r != None {
			t.Errorf("Binds(%q) = false なのに HandleKey が %v を返した", k, r)
		}
		settle(m)
		if m.cur != cur || m.root != root || m.OwnsKeys() || len(m.tiles) > 0 || len(m.TakeNotices()) > 0 ||
			strings.Join(m.draw().plain(), "\n") != strings.Join(before, "\n") {
			t.Errorf("Binds(%q) = false なのに木の画面が変わった", k)
		}
	}
	if unbound < 20 {
		t.Fatalf("Binds が false のキーが %d 個しかない (走査が壊れている)", unbound)
	}
	for _, open := range []string{"?", "/", "!", ","} { // キー一覧・検索・! の入力・設定の板
		m := newTest(t)
		m.HandleKey(open)
		for _, k := range []string{"F", "i", "R", "D", "U", "X"} {
			if !m.Binds(k) {
				t.Errorf("%s を開いている間の Binds(%q) = false (glogx が横断して、開いたまま残る)", open, k)
			}
		}
	}
}

type exitCodeError struct{}

func (exitCodeError) Error() string { return "exit status 1" }
func (exitCodeError) ExitCode() int { return 1 }

// ExecDone は読み直し (シェルで作ったファイルを出す) と、起動できなかったときだけの知らせを 1 か所で持つ (issue 694 の 7)。
func TestExecDoneRefreshesAndWarnsOnlyOnStartFailure(t *testing.T) {
	m := newTest(t)
	dir := m.root.path()
	mustWrite(t, filepath.Join(dir, "made-in-shell.txt"), "")
	if w := m.ExecDone(nil); w != "" {
		t.Fatalf("正常に戻ったのに知らせ %q", w)
	}
	if m.findNode(filepath.Join(dir, "made-in-shell.txt")) == nil {
		t.Fatal("シェルで作ったファイルが木に出ない (読み直していない)")
	}
	if w := m.ExecDone(exitCodeError{}); w != "" {
		t.Fatalf("終了コードで戻ったのに知らせ %q (シェルは最後のコマンドの終了コードで抜けるのが普通)", w)
	}
	if w := m.ExecDone(errors.New("chdir: no such file")); !strings.Contains(w, "起動できません") {
		t.Fatalf("起動できなかったときの知らせ = %q", w)
	}
}

// git の状態は gitKinds の 1 行に性質を持ち、色はどの地にもある (状態を足して色を忘れると芽が黒く描かれる。issue 694 の 9)。
// classifyXY が返しうる状態は全部 gitKinds にある。
func TestGitKindsComplete(t *testing.T) {
	for _, xy := range []string{"??", "!!", "UU", "AA", "DD", "AU", " M", "M ", "MM", "A ", "D ", "R ", " D"} {
		if _, ok := gitKinds[classifyXY(xy)]; !ok {
			t.Errorf("classifyXY(%q) = %q が gitKinds に無い", xy, classifyXY(xy))
		}
	}
	defer func() { // 地を既定へ戻す (配色はパッケージ変数なので、後のテストに残さない)
		d := defaultSettings()
		applyTheme(&d)
	}()
	for _, g := range grounds {
		s := defaultSettings()
		s.Ground = g.name
		fitPalette(&s)
		applyTheme(&s)
		for st, k := range gitKinds {
			if _, ok := gitColor[st]; k.rank > 0 && !ok {
				t.Errorf("地 %s に状態 %q の色が無い", g.name, st)
			}
		}
	}
}

// 設定の板の移動は listnav.MotionOf の語彙 (ctrl+d / end も効く。issue 700 の 3)。
func TestPanelUsesMotionVocabulary(t *testing.T) {
	m := newTest(t)
	m.HandleKey(",")
	m.HandleKey("end")
	if m.panel.row != len(items)-1 {
		t.Fatalf("end で末尾へ: %d", m.panel.row)
	}
	m.HandleKey("ctrl+d")
	if m.panel.row != 0 {
		t.Fatalf("ctrl+d で 1 行巡らない: %d", m.panel.row)
	}
}
