package filer

import (
	"os"
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

func helpScreen(t *testing.T, w, h int) []string {
	t.Helper()
	m := newTest(t)
	m.Resize(w, h)
	settle(m)
	m.HandleKey("?")
	v := m.View()
	for i := range v {
		v[i] = termwidth.StripSGR(v[i])
	}
	return v
}

// 広い端末では、節ごとに見出しを付けて 2 段に組み、どの説明も切らずに出す。
func TestHelpGroupsKeysIntoSectionsInTwoColumns(t *testing.T) {
	v := helpScreen(t, 160, 50)
	all := strings.Join(v, "\n")
	for _, s := range helpSections {
		if !strings.Contains(all, "── "+s.title+" ") {
			t.Errorf("節の見出し %q が無い", s.title)
		}
		for _, kv := range s.keys {
			if !strings.Contains(all, kv[1]) {
				t.Errorf("%s の %q の説明が切れずに出ていない: %q", s.title, kv[0], kv[1])
			}
		}
	}
	twoCols := false
	for _, row := range v { // 最初の節と後半の先頭の節が同じ行に並ぶ (2 段)
		if strings.Contains(row, "── 移動 ") && strings.Contains(row, "── ファイル ") {
			twoCols = true
		}
	}
	if !twoCols {
		t.Errorf("160 桁で 2 段に組まれていない:\n%s", all)
	}
}

// 2 段が入らない幅では 1 段 (幅 64) に戻し、高さが足りなければ残りの数を出す。
func TestHelpFallsBackToOneColumnWhenNarrow(t *testing.T) {
	v := helpScreen(t, 80, 70)
	all := strings.Join(v, "\n")
	for _, row := range v {
		if strings.Contains(row, "── 移動 ") && strings.Contains(row, "── ファイル ") {
			t.Fatalf("80 桁で 2 段に組んだ (1 段は幅 64 まで):\n%s", all)
		}
	}
	if strings.Contains(all, "残り") || !strings.Contains(all, "このキー一覧") {
		t.Fatalf("70 行あるのに 1 段で全部出ない:\n%s", all)
	}
}

// 2 段は節の並びのまま前後に割り、高い方の段が最も低くなる所で割る。
func TestHelpColumnsSplitKeepsOrderAndBalances(t *testing.T) {
	cols := helpColumns(helpSections, 2)
	if len(cols) != 2 {
		t.Fatalf("段の数 %d", len(cols))
	}
	one := helpColumns(helpSections, 1)[0]
	if got := len(cols[0]) + 1 + len(cols[1]); got != len(one) { // 割った所の空行 1 つだけが減る
		t.Fatalf("2 段の行の合計 %d / 1 段 %d", got, len(one))
	}
	best := max(len(cols[0]), len(cols[1]))
	for k := 1; k < len(helpSections); k++ {
		a, b := helpColumns(helpSections[:k], 1)[0], helpColumns(helpSections[k:], 1)[0]
		if max(len(a), len(b)) < best {
			t.Fatalf("%d 節目で割る方が低い (%d < %d)", k, max(len(a), len(b)), best)
		}
	}
}

// 木のキー (treeKeys。Binds も同じ表を読む) は全部、? のキー一覧と README の「キー (木)」の表に載っている。
// キーを足して一覧か README を直し忘れると、押せば効くのに案内のどこにも無いキーができる (p が README に無いまま残った)。
// 検出しない形: 移動のキー (listnav.MotionOf が受ける j k f b など) と ctrl+c (HandleInput の入口) は treeKeys に無いので見ない /
// 同じキーが別の意味の行にも載っていると、片方の行を消しても通る (esc は「終了」と「> を止める」の 2 行) / 説明が動作と食い違うもの
func TestEveryTreeKeyIsListedInHelpAndReadme(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatalf("README が読めない: %v", err)
	}
	_, table, ok := strings.Cut(string(readme), "## キー (木)")
	if !ok {
		t.Fatal("README に「## キー (木)」の節が無い")
	}
	table, _, _ = strings.Cut(table, "\n## ")
	readmeKeys := map[string]bool{}
	for _, line := range strings.Split(table, "\n") {
		if col, _, ok := strings.Cut(strings.TrimPrefix(line, "|"), "|"); ok && strings.HasPrefix(line, "|") {
			for _, f := range strings.Split(col, "`") {
				readmeKeys[f] = true
			}
		}
	}
	helpKeys := map[string]bool{}
	sawTile := false
	for _, s := range helpSections {
		if s.title == "タイルの中" {
			sawTile = true
			continue // タイルのキーは treeKeys ではない (tile.go の tileKey)。混ぜると、木の d を消してもタイルの d で通る
		}
		for _, kv := range s.keys {
			fs := strings.Fields(kv[0])
			for _, f := range fs {
				if f == "/" && len(fs) > 1 {
					continue // "J K / ^D ^U" の区切り。キーの / (検索) と取り違えると、検索の行を消しても通る
				}
				helpKeys[f] = true
			}
		}
	}
	if !sawTile {
		t.Fatal("一覧に「タイルの中」の節が無い (題を変えたならここも直す。外さないとタイルのキーが木のキーの照合に混ざる)")
	}
	if len(readmeKeys) == 0 || len(helpKeys) == 0 {
		t.Fatalf("キーを 1 つも拾えない (README %d・一覧 %d)", len(readmeKeys), len(helpKeys))
	}
	checked := 0
	for k := range treeKeys {
		if k == " " {
			continue // space の別名 (bubbletea が " " で渡すことがある)
		}
		checked++
		if h := helpSpelling(k); !helpKeys[h] {
			t.Errorf("キー %q (一覧の表記 %q) が ? のキー一覧に無い", k, h)
		}
		if r := readmeSpelling(k); !readmeKeys[r] {
			t.Errorf("キー %q (README の表記 %q) が README の「キー (木)」の表に無い", k, r)
		}
	}
	if checked < 30 {
		t.Fatalf("treeKeys のキーが %d 個しか無い (表の読み方が壊れた)", checked)
	}
}

// helpSpelling は bubbletea のキー名を ? の一覧の表記にする (^X・矢印。他はそのまま)。
func helpSpelling(k string) string {
	if c, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return "^" + strings.ToUpper(c)
	}
	switch k {
	case "left":
		return "←"
	case "right":
		return "→"
	}
	return k
}

// readmeSpelling は bubbletea のキー名を README の表記にする (Ctrl-X・名前のあるキーは頭を大文字・矢印)。
func readmeSpelling(k string) string {
	if c, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return "Ctrl-" + strings.ToUpper(c)
	}
	switch k {
	case "left":
		return "←"
	case "right":
		return "→"
	case "esc", "enter", "space", "tab", "backspace":
		return strings.ToUpper(k[:1]) + k[1:]
	}
	return k
}
