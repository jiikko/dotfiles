package issuesview

import (
	"glogx/internal/issuesview/issuesviewtest"
	"glogx/internal/term"
	"glogx/internal/testenv"
	"glogx/issues"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/anim"
	"github.com/jiikko/dotfiles/src/tuikit/editor"
)

// 選択範囲は溝で見える (どこを選んでいるか画面から分かる)。幅も超えない。
func TestIssuesViewMultiSelectIsVisible(t *testing.T) {
	v := loadedView(issuesviewtest.SampleIssues()...)
	v.filter = issues.FilterAll
	v.refresh()
	v.cursor = 2
	v.HandleKey("shift+up", vp(10))
	out := v.Lines(renderOpts(10))
	marked := 0
	for _, ln := range out {
		if strings.HasPrefix(ln, issuesSelGutter) {
			marked++
		}
		if w := term.DispWidth(ln); w > 80 {
			t.Fatalf("選択表示で行が幅を超えた (w=%d): %q", w, ln)
		}
	}
	// 2 行選択のうちカーソル行は → で示すので、選択の溝が付くのは 1 行
	if marked != 1 {
		t.Fatalf("選択範囲の溝が出ていない (marked=%d):\n%s", marked, strings.Join(out, "\n"))
	}
	if h := v.Hint(testenv.PopupHintBudget); !strings.Contains(h, "2 件選択") {
		t.Fatalf("選択中の hint に件数が出ない: %q", h)
	}
}

func TestIssuesViewHintChangesByMode(t *testing.T) {
	v := loadedView(issuesviewtest.SampleIssues()...)
	if !strings.Contains(v.Hint(testenv.PopupHintBudget), "カテゴリ") {
		t.Fatalf("一覧の hint が想定と違う: %q", v.Hint(testenv.PopupHintBudget))
	}
	v.open = v.rows[0]
	if !strings.Contains(v.Hint(testenv.PopupHintBudget), "隣へ") {
		t.Fatalf("本文の hint が想定と違う: %q", v.Hint(testenv.PopupHintBudget))
	}
	// URL ピッカーは本文の上に重なる 3 つ目のモード。本文の案内を出したままにすると、案内した
	// キー (j/k/g/G/p/u/v/h/q) が全部 urlPicker の検索語に化けて 1 つも案内どおりに動かない。
	v.urlPick.open([]string{"https://example.com/"})
	if h := v.Hint(testenv.PopupHintBudget); !strings.Contains(h, "絞り込み") || strings.Contains(h, "隣へ") {
		t.Fatalf("URL ピッカーの hint が想定と違う: %q", h)
	}

	// n の確認中は全キーを確認が飲む (Enter は実行)。一覧の案内 (Enter: 本文 / q: 終了) を残さない (issue 750)
	l := loadedView(issuesviewtest.SampleIssues()...)
	l.HandleKey("n", vp(10))
	if !l.markNext.active {
		t.Fatal("前提が崩れた: n で確認が出ない")
	}
	if h := l.Hint(testenv.PopupHintBudget); !strings.Contains(h, "y/Enter: 実行") || strings.Contains(h, "本文") || strings.Contains(h, "終了") {
		t.Fatalf("確認中の hint が想定と違う: %q", h)
	}
}

func TestIssuesViewHintFitsPopupWidth(t *testing.T) {
	// hint は term.FitHintItems で組むので「収まる」は構造で保証される (issue 264)。ここで見るのは
	// ①予算幅では全モードの全項目が入る (= 代表幅の端末で案内が欠けない) ②狭い幅でも
	// 優先度 1 の「抜ける手段」が最後まで残る、の 2 つ。
	budget := testenv.PopupHintBudget
	v := loadedView(issuesviewtest.SampleIssues()...)
	type mode struct {
		name  string
		setup func()
		exit  string // 優先度 1 (抜ける手段)。狭い幅でもこれだけは残る
		full  string // 予算幅で最後に並ぶ項目 (= 全項目が入ったことの証拠)
	}
	modes := []mode{
		{"一覧 open", func() { v.filter = issues.FilterOpen; v.refresh() }, "q: 終了", "a: +"},
		{"一覧 pending", func() { v.filter = issues.FilterPending; v.refresh() }, "q: 終了", "a: +"},
		{"一覧 all", func() { v.filter = issues.FilterAll; v.refresh() }, "q: 終了", "のみ"},
		{"選択中 (3 桁)", func() {
			v.filter = issues.FilterAll
			v.refresh()
			v.marked, v.markAt, v.cursor = true, 0, len(v.rows)-1
		}, "Esc: 解除", "Y: 参照"},
		{"番号絞り込み中", func() { v.clearMark(); v.numFilter.active = true }, "Esc: 解除", "/: 絞り込み直す"},
		{"番号入力中", func() { v.numFilter.typing = true }, "Esc: 解除", "Enter: 確定"},
		{"本文", func() { v.numFilter.active, v.numFilter.typing = false, false; v.open = v.rows[0] }, "Enter/h/q: 戻る", "u: URL"},
		{"URL ピッカー", func() { v.urlPick.open([]string{"https://example.com/"}) }, "Esc: 戻る", "Enter: 開く"},
		{"パスのジャンプ", func() { v.urlPick.close(); v.linkJump.active = true }, "Esc: 戻る", "y: パス"},
	}
	for _, md := range modes {
		md.setup()
		h := v.Hint(budget)
		if w := term.DispWidth(h); w > budget {
			t.Errorf("%s: 予算 %d に収まらない (w=%d): %q", md.name, budget, w, h)
		}
		if !strings.Contains(h, md.full) {
			t.Errorf("%s: 予算幅で項目が落ちている (%q が無い): %q", md.name, md.full, h)
		}
		// 🚨 1 点だけ見ない。落ちる項目は幅の刻みごとに変わるので、下限から予算まで掃く
		// (下限は抜ける手段そのものが入る幅。それ未満は term.FitHintItems が末尾の項目を出す極端域)
		for w := term.DispWidth(md.exit); w <= budget; w++ {
			h := v.Hint(w)
			if term.DispWidth(h) > w {
				t.Errorf("%s: 幅 %d に収まらない (w=%d): %q", md.name, w, term.DispWidth(h), h)
			}
			if !strings.Contains(h, md.exit) {
				t.Errorf("%s: 幅 %d で抜ける手段 %q が消えた: %q", md.name, w, md.exit, h)
			}
		}
	}
}

// a キーは 3 段の巡回 (open → +pending → +done → open)。既定は open だけ
// (ユーザー要望 2026-07-31: 既定で pending を伏せる)。
func TestIssuesStatusFilterCycle(t *testing.T) {
	v := loadedView(issuesviewtest.SampleIssues()...)
	want := []struct {
		filter issues.StatusFilter
		rows   int
		badges string
	}{
		{issues.FilterOpen, 2, "○"},
		{issues.FilterPending, 3, "○⏸◌"},
		{issues.FilterAll, 5, "○⏸◌✓"},
		{issues.FilterOpen, 2, "○"}, // 巡回して既定へ戻る
	}
	for i, w := range want {
		if v.filter != w.filter {
			t.Fatalf("%d 打目: filter=%d want %d", i, v.filter, w.filter)
		}
		if len(v.rows) != w.rows {
			t.Errorf("%d 打目: 行数=%d want %d", i, len(v.rows), w.rows)
		}
		if got := v.filter.Badges(); got != w.badges {
			t.Errorf("%d 打目: バッジ=%q want %q", i, got, w.badges)
		}
		// hint は「次に何が増えるか」を出し、現在地は出さない (幅が 1 行しかない)
		if !strings.Contains(v.Hint(testenv.PopupHintBudget), "a: ") {
			t.Errorf("%d 打目: hint に a の案内が無い: %q", i, v.Hint(testenv.PopupHintBudget))
		}
		v.HandleKey("a", vp(10))
	}
}

// hint が案内するキーと実際の割当を繋ぐ。案内だけ書き換えて割当を忘れる (逆も) と、
// spec 5 節が既知の事故として挙げる「案内どおりに動かないキー」が静かに生まれる。
// 🚨 幅は TestIssuesViewHintFitsPopupWidth が別に見る (ここは対応だけを見る)。
func TestIssuesViewBodyHintAdvertisedEditorKeyWorks(t *testing.T) {
	// 🚨 回数だけ数える stubEditor では「渡す対象の取り違え」(iss.Path → iss.Dir 等) が通るので、
	// Args を捕まえる testenv.StubEditorCapture を使う。
	cmds := testenv.StubEditorCapture(t)
	testenv.PinFallbackEditor(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "028-refactor-x.md")
	if err := os.WriteFile(path, []byte("# 028 refactor: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(issuesviewtest.Path2Issue(dir, "028-refactor-x.md"))
	v.HandleKey("enter", vp(10)) // 本文モードへ
	if v.open == nil {
		t.Fatal("本文モードに入れていない")
	}
	const advertised = "e: 編集"
	if h := v.Hint(testenv.PopupHintBudget); !strings.Contains(h, advertised) {
		t.Fatalf("本文の hint が %q を案内していない: %q", advertised, h)
	}
	if cmd := v.HandleKey("e", vp(10)); cmd == nil {
		t.Fatal("hint が案内した e でエディタが開かない (cmd == nil)")
	}
	// 開く対象は「その issue の実ファイル」。末尾引数がパスであることまで見る
	if len(*cmds) != 1 {
		t.Fatalf("エディタの起動回数が 1 でない: %d", len(*cmds))
	}
	if args, want := (*cmds)[0].Args, []string{editor.Fallback, path}; !slices.Equal(args, want) {
		t.Errorf("e の起動コマンドが違う: args=%v want=%v", args, want)
	}
}

func TestIssuesViewBodyHintKeysAllRespond(t *testing.T) {
	// 案内キーごとの「押したら何が変わるか」。🚨 効果を観測できる状態を各自で作る
	// (本文の途中まで送ってから k を押す等。端で押して「変わらない」を通すと空振りする)
	probes := map[string]func(t *testing.T, e *bodyKeyEnv){
		"j": func(t *testing.T, e *bodyKeyEnv) {
			before := e.v.bodyPager.Offset
			e.press("j")
			if e.v.bodyPager.Offset <= before {
				t.Errorf("j で本文が下へ進まない: %d → %d", before, e.v.bodyPager.Offset)
			}
		},
		"k": func(t *testing.T, e *bodyKeyEnv) {
			// 🚨 G を押して状態を作らない。G が壊れると k の probe も red になり、
			// 壊れていないキーを名指しして原因追跡を遅らせる
			e.v.bodyPager.Offset = 5
			before := e.v.bodyPager.Offset
			e.press("k")
			if e.v.bodyPager.Offset >= before {
				t.Errorf("k で本文が上へ戻らない: %d → %d", before, e.v.bodyPager.Offset)
			}
		},
		"Space": func(t *testing.T, e *bodyKeyEnv) {
			before := e.v.bodyPager.Offset
			e.press(" ")
			// 🚨 方向だけでなく「半ページ」まで見る (1 行送りの実装を通さない)
			if want := before + e.rows()/2; e.v.bodyPager.Offset != want {
				t.Errorf("Space が半ページ送りでない: %d → %d (want %d)", before, e.v.bodyPager.Offset, want)
			}
		},
		"g": func(t *testing.T, e *bodyKeyEnv) {
			e.press("G")
			e.press("g")
			if e.v.bodyPager.Offset != 0 {
				t.Errorf("g で先頭へ戻らない: bodyOff=%d", e.v.bodyPager.Offset)
			}
		},
		"G": func(t *testing.T, e *bodyKeyEnv) {
			e.press("G")
			// 🚨 「0 でない」ではなく末尾そのものを見る (途中で止まる実装を通さない)
			if want := e.v.body.Len() - e.rows(); e.v.bodyPager.Offset != want {
				t.Errorf("G が末尾へ飛ばない: bodyOff=%d want %d", e.v.bodyPager.Offset, want)
			}
		},
		"p": func(t *testing.T, e *bodyKeyEnv) {
			*e.copied = ""
			e.press("p")
			if *e.copied == "" {
				t.Error("p で番号がコピーされない")
			}
		},
		"Tab": func(t *testing.T, e *bodyKeyEnv) {
			e.press("tab")
			if !e.v.linkJump.active {
				t.Error("Tab でジャンプモードに入らない")
			}
		},
		"u": func(t *testing.T, e *bodyKeyEnv) {
			e.press("u")
			if !e.v.urlPick.active {
				t.Error("u で URL ピッカーが開かない")
			}
		},
		"e": func(t *testing.T, e *bodyKeyEnv) {
			before := len(*e.cmds)
			e.press("e")
			if len(*e.cmds) != before+1 {
				t.Fatal("e でエディタが起動しない")
			}
			// 🚨 起動しただけでなく対象まで見る (専用テストと同じ強さに揃える。方向だけの
			// 弱い二重化にすると「壊れても片方しか落ちない」状態になる)
			if args, want := (*e.cmds)[before].Args, []string{editor.Fallback, e.v.open.Path}; !slices.Equal(args, want) {
				t.Errorf("e の起動コマンドが違う: args=%v want=%v", args, want)
			}
		},
		"J": func(t *testing.T, e *bodyKeyEnv) {
			// env は 1 件なので観測できる効果は「端の案内」。差し替えそのものは
			// TestIssuesViewBodyNeighborKeysSwapIssueWithoutReopening が見る
			e.press("J")
			if msg, _ := testenv.NoticeOKOf(e.v.TakeNotice()); !strings.Contains(msg, "最後") {
				t.Errorf("J が本文モードで効いていない (端の案内が出ない): %q", msg)
			}
		},
		"K": func(t *testing.T, e *bodyKeyEnv) {
			e.press("K")
			if msg, _ := testenv.NoticeOKOf(e.v.TakeNotice()); !strings.Contains(msg, "最初") {
				t.Errorf("K が本文モードで効いていない (端の案内が出ない): %q", msg)
			}
		},
		"Enter": func(t *testing.T, e *bodyKeyEnv) { e.assertClosesBody(t, "enter") },
		"h":     func(t *testing.T, e *bodyKeyEnv) { e.assertClosesBody(t, "h") },
		"q":     func(t *testing.T, e *bodyKeyEnv) { e.assertClosesBody(t, "q") },
	}

	advertised := advertisedHintKeys(t)
	if len(advertised) == 0 {
		t.Fatal("本文モードの hint から案内キーを取り出せていない (パースが壊れている)")
	}
	for _, name := range advertised {
		run, ok := probes[name]
		if !ok {
			t.Errorf("hint が案内している %q の効果を誰も検証していない (probes に足すこと)", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			run(t, newBodyKeyEnv(t))
		})
	}
	for name := range probes {
		if !slices.Contains(advertised, name) {
			t.Errorf("hint が案内していない %q を検証している (hint から消えたなら probes も消す)", name)
		}
	}
}

// advertisedHintKeys は本文モードの hint から案内キーを取り出す ("j/k/Space: 説明" → j, k, Space)。
func advertisedHintKeys(t *testing.T) []string {
	t.Helper()
	e := newBodyKeyEnv(t)
	var keys []string
	for _, tok := range strings.Split(e.v.Hint(testenv.PopupHintBudget), "  ") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		label, _, ok := strings.Cut(tok, ": ")
		if !ok {
			// 🚨 捨てずに落とす。黙って continue すると「コロン無しトークンでキーを案内する」
			// 書き方 (他モードの hint に "文字入力で絞り込み" 等が実在する) を body に持ち込んだ
			// 瞬間に、そのキーが検証から漏れて集合一致もすり抜ける。
			t.Errorf("hint のトークン %q を \"キー: 説明\" として読めない (案内キーが検証から漏れる)", tok)
			continue
		}
		keys = append(keys, strings.Split(label, "/")...)
	}
	return keys
}

// bodyKeyEnv は本文モードのキーを押せる状態 (長い本文 + URL + 実ファイル + 各種スタブ)。
type bodyKeyEnv struct {
	v      *IssuesView
	cmds   *[]*exec.Cmd
	copied *string
	page   int // キー処理へ渡す窓の高さ (実際の pager 行数は rows())
}

func newBodyKeyEnv(t *testing.T) *bodyKeyEnv {
	t.Helper()
	cmds := testenv.StubEditorCapture(t)
	copied := testenv.StubClipboard(t)
	testenv.PinFallbackEditor(t)

	dir := t.TempDir()
	rel := "001-feat-hintkeys.md"
	path := filepath.Join(dir, rel)
	// 🚨 本文は窓より十分長くする (短いと j / Space / G が「動かない」ので効果を観測できない)。
	// URL を 1 つ入れるのは u (ピッカー) の観測のため。
	// 🚨 空行で区切る。連続行は markdown で 1 段落に畳まれ、body.Len() が窓より短くなって
	// j / Space / G の効果が観測できない (実測: 80 行が 1 段落になり bodyOff が 0 のまま)
	// 自分自身の絶対パスを 1 つ入れるのは Tab (ジャンプモード) の観測のため (実在するファイルしかリンクにしない)
	body := "# 001 feat: hint keys\n\nhttps://example.com/x `" + path + "`\n\n" + strings.Repeat("本文の行。\n\n", 60)
	v := loadedView(issuesviewtest.WriteRealIssue(t, dir, rel, body))
	v.root = dir // Tab (ジャンプモード) は「repo の中」の範囲が決まらないとパスを開かない (fail-closed)
	landBody(t, v, 10)
	// 🚨 行数は描画で確定する (未描画だと body.Len() = 0 でスクロール上限が 0 になり、
	// j / Space / G が「動かない」ので効果を観測できない)
	v.Lines(renderOpts(20))
	if v.body.Len() <= 10 {
		t.Fatalf("前提が崩れた: 本文が窓より短くスクロールできない (Len=%d)", v.body.Len())
	}
	return &bodyKeyEnv{v: v, cmds: cmds, copied: copied, page: 10}
}

func (e *bodyKeyEnv) press(key string) { e.v.HandleKey(key, vp(e.page)) }

// rows は本文 pager の実際の行数 (ヘッダー行数を引いた値)。🚨 page とは違うので、
// 「半ページ」「末尾」を語義で検証する probe はこちらを使う。
func (e *bodyKeyEnv) rows() int { return e.v.visibleRows(vp(e.page)) }

func (e *bodyKeyEnv) assertClosesBody(t *testing.T, key string) {
	t.Helper()
	e.press(key)
	// 🚨 本文は「閉じる演出の着地後」に捨てられるので、open == nil を待つと時刻の進め方に
	// 依存する。キーの直接の効果である「閉じる演出に入ったか」を見る。
	if e.v.drawer.phase() != anim.Closing {
		t.Errorf("%q で本文が閉じ始めない: phase=%v", key, e.v.drawer.phase())
	}
	// 次の打鍵で実際に畳まれる (handleKey 冒頭の drawer.finish → discardBody)
	e.press("j")
	if e.v.open != nil {
		t.Errorf("%q の後に本文が捨てられない", key)
	}
}
