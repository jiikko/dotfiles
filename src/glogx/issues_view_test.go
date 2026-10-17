package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/anim"

	"glogx/internal/issuesview"
	"glogx/internal/issuesview/issuesviewtest"
	"glogx/internal/term"
	"glogx/internal/testenv"
	"glogx/issues"

	"github.com/jiikko/dotfiles/src/tuikit/editor"
)

// issuesView は browseModel を参照しないので、モデルを組まずに直接駆動できる
// (この独立性が「結合を最後にする」を成立させている。壊れたら結合コストが跳ね上がる)。

// loadedView は receive 済みの viewer を返す。
func loadedView(list ...*issues.Issue) *issuesview.IssuesView {
	v := newTestIssuesView()
	v.Shown = true
	v.Receive(issuesview.IssuesScanMsg{Dirs: []string{"/repo/issues"}, Issues: list})
	return &v
}

// vp は窓の寸法 (キー処理へ渡す)。幅は renderOpts と同じにする — テストだけ幅 0 で駆動すると、
// 幅で行数が変わるヘッダーを足したときに本番でだけ page 分割がずれる。
func vp(page int) issuesview.IssuesViewport { return renderOpts(page).Viewport() }

func renderOpts(page int) issuesview.IssuesRenderOpts {
	return issuesview.IssuesRenderOpts{Width: 80, Page: page, Colored: false}
}

// 選択範囲は溝で見える (どこを選んでいるか画面から分かる)。幅も超えない。
func TestIssuesViewMultiSelectIsVisible(t *testing.T) {
	v := loadedView(issuesviewtest.SampleIssues()...)
	v.Filter = issues.FilterAll
	v.Refresh()
	v.Cursor = 2
	v.HandleKey("shift+up", vp(10))
	out := v.Lines(renderOpts(10))
	marked := 0
	for _, ln := range out {
		if strings.HasPrefix(ln, issuesview.IssuesSelGutter) {
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
	if h := v.Hint(testHintBudget(t)); !strings.Contains(h, "2 件選択") {
		t.Fatalf("選択中の hint に件数が出ない: %q", h)
	}
}

func TestIssuesViewHintChangesByMode(t *testing.T) {
	v := loadedView(issuesviewtest.SampleIssues()...)
	if !strings.Contains(v.Hint(testHintBudget(t)), "カテゴリ") {
		t.Fatalf("一覧の hint が想定と違う: %q", v.Hint(testHintBudget(t)))
	}
	v.Open = v.Rows[0]
	if !strings.Contains(v.Hint(testHintBudget(t)), "隣へ") {
		t.Fatalf("本文の hint が想定と違う: %q", v.Hint(testHintBudget(t)))
	}
	// URL ピッカーは本文の上に重なる 3 つ目のモード。本文の案内を出したままにすると、案内した
	// キー (j/k/g/G/p/u/v/h/q) が全部 urlPicker の検索語に化けて 1 つも案内どおりに動かない。
	v.UrlPick.Open([]string{"https://example.com/"})
	if h := v.Hint(testHintBudget(t)); !strings.Contains(h, "絞り込み") || strings.Contains(h, "隣へ") {
		t.Fatalf("URL ピッカーの hint が想定と違う: %q", h)
	}

	// n の確認中は全キーを確認が飲む (Enter は実行)。一覧の案内 (Enter: 本文 / q: 終了) を残さない (issue 750)
	l := loadedView(issuesviewtest.SampleIssues()...)
	l.HandleKey("n", vp(10))
	if !l.MarkNext.Active {
		t.Fatal("前提が崩れた: n で確認が出ない")
	}
	if h := l.Hint(testHintBudget(t)); !strings.Contains(h, "y/Enter: 実行") || strings.Contains(h, "本文") || strings.Contains(h, "終了") {
		t.Fatalf("確認中の hint が想定と違う: %q", h)
	}
}

func TestIssuesViewHintFitsPopupWidth(t *testing.T) {
	// hint は term.FitHintItems で組むので「収まる」は構造で保証される (issue 264)。ここで見るのは
	// ①予算幅では全モードの全項目が入る (= 代表幅の端末で案内が欠けない) ②狭い幅でも
	// 優先度 1 の「抜ける手段」が最後まで残る、の 2 つ。
	budget := testHintBudget(t)
	v := loadedView(issuesviewtest.SampleIssues()...)
	type mode struct {
		name  string
		setup func()
		exit  string // 優先度 1 (抜ける手段)。狭い幅でもこれだけは残る
		full  string // 予算幅で最後に並ぶ項目 (= 全項目が入ったことの証拠)
	}
	modes := []mode{
		{"一覧 open", func() { v.Filter = issues.FilterOpen; v.Refresh() }, "q: 終了", "a: +"},
		{"一覧 pending", func() { v.Filter = issues.FilterPending; v.Refresh() }, "q: 終了", "a: +"},
		{"一覧 all", func() { v.Filter = issues.FilterAll; v.Refresh() }, "q: 終了", "のみ"},
		{"選択中 (3 桁)", func() {
			v.Filter = issues.FilterAll
			v.Refresh()
			v.Marked, v.MarkAt, v.Cursor = true, 0, len(v.Rows)-1
		}, "Esc: 解除", "Y: 参照"},
		{"番号絞り込み中", func() { v.ClearMark(); v.NumFilter.Active = true }, "Esc: 解除", "/: 絞り込み直す"},
		{"番号入力中", func() { v.NumFilter.Typing = true }, "Esc: 解除", "Enter: 確定"},
		{"本文", func() { v.NumFilter.Active, v.NumFilter.Typing = false, false; v.Open = v.Rows[0] }, "Enter/h/q: 戻る", "u: URL"},
		{"URL ピッカー", func() { v.UrlPick.Open([]string{"https://example.com/"}) }, "Esc: 戻る", "Enter: 開く"},
		{"パスのジャンプ", func() { v.UrlPick.Close(); v.LinkJump.Active = true }, "Esc: 戻る", "y: パス"},
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

// hint を組む予算と描画側が切る幅は同じ 1 か所 (browseModel.hintWidth) から取る。
// TestStatusHintUsesRenderBudget の issues viewer 版 (issue 264)。
func TestIssuesHintUsesRenderBudget(t *testing.T) {
	for w := frameMinWidth; w <= 140; w++ {
		m := newTestBrowse(t, 1, map[string]CIState{}, nil)
		m.showFrame, m.width, m.height = true, w, 40
		if !m.frameActive() {
			t.Fatalf("w=%d: 前提が崩れた (フレームが有効でない)", w)
		}
		m.issuesOv.Shown = true
		m.issuesOv.Receive(issuesview.IssuesScanMsg{Dirs: []string{"/repo/issues"}, Issues: issuesviewtest.SampleIssues()})
		line := m.hintLine()
		if got := term.DispWidth(term.StripANSI(line)); got > w {
			t.Errorf("w=%d: hint 行が端末幅を超えた (%d 桁): %q", w, got, line)
		}
		if strings.Contains(line, "…") {
			t.Errorf("w=%d: hint が切り詰められている (予算がずれている): %q", w, line)
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
		if v.Filter != w.filter {
			t.Fatalf("%d 打目: filter=%d want %d", i, v.Filter, w.filter)
		}
		if len(v.Rows) != w.rows {
			t.Errorf("%d 打目: 行数=%d want %d", i, len(v.Rows), w.rows)
		}
		if got := v.Filter.Badges(); got != w.badges {
			t.Errorf("%d 打目: バッジ=%q want %q", i, got, w.badges)
		}
		// hint は「次に何が増えるか」を出し、現在地は出さない (幅が 1 行しかない)
		if !strings.Contains(v.Hint(testHintBudget(t)), "a: ") {
			t.Errorf("%d 打目: hint に a の案内が無い: %q", i, v.Hint(testHintBudget(t)))
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
	if v.Open == nil {
		t.Fatal("本文モードに入れていない")
	}
	const advertised = "e: 編集"
	if h := v.Hint(testHintBudget(t)); !strings.Contains(h, advertised) {
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

// Msg 経路 (issuesScanMsg) からの notice 配達の回帰テスト (issue 059)。
// rebindOpen が置く「開いていた issue が見つかりません」は、打鍵を待たずスキャン結果を
// 受けたフレームで配達される (トースト + w でコピーできる lastWarning)。
// 以前は takeNotice が打鍵経路にしか無く、本文が無言で畳まれて見え、直後に q を押すと
// 理由が 1 度も描かれないままプロセスが終わった。
// 🚨 testenv.NoticeOKOf(v.takeNotice()) を直接 assert しない: それは配達経路 (browseModel の Update) を通らず、
// 配達ブロックを丸ごと削っても green のままになる (この issue 自身が見つけた false green)。
func TestIssuesScanMsgDeliversRebindNotice(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("i")
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# 001 feat: x\n\n本文の行\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := issuesviewtest.Path2Issue(dir, "001-feat-x.md")
	if err := iss.LoadMeta(); err != nil {
		t.Fatal(err)
	}
	m.issuesOv.Cwd = root
	m.Update(issuesview.IssuesScanMsg{Root: root, Dirs: []string{dir}, Issues: []*issues.Issue{iss}})
	m.issuesOv.FinishAnim()
	m.handleKey("enter")
	m.issuesOv.Drawer.Finish() // 本文モードへ

	// 別プロセスがファイルを消したスキャン結果が Msg 経路で届く (キーは押さない)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m.Update(issuesview.IssuesScanMsg{Root: root, Dirs: []string{dir}})

	if !strings.Contains(m.lastWarning, "見つかりません") {
		t.Errorf("畳んだ理由が lastWarning に届いていない (q で恒久喪失する): %q", m.lastWarning)
	}
	if !m.toast.Visible() {
		t.Error("畳んだ理由のトーストが積まれていない")
	}
	if text, _ := testenv.NoticeOKOf(m.issuesOv.TakeNotice()); text != "" {
		t.Errorf("notice が取り出されずに残っている (配達漏れ): %q", text)
	}
}

// browseModel の配線: issuesScanMsg の case が receive の戻り値 (取り直しの予約) を返すこと。
// 🚨 ここを捨てても issuesView 単体のテストは全部通り、lint も通らない (errcheck 系は error 以外の
// 戻り値放棄を見ない)。予約機構の可視効果はこの 1 行に乗っているので model 層で固定する。
func TestBrowseIssuesScanMsgPropagatesRescanCmd(t *testing.T) {
	iss := issuesviewtest.RealIssue(t)
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(iss)

	if cmd := m.issuesOv.ScanCmd(m.issuesOv.Cwd); cmd == nil {
		t.Fatal("前提が崩れた: 1 本目のスキャンが張れない")
	}
	if cmd := m.issuesOv.ReloadAfterEdit(); cmd != nil || !m.issuesOv.RescanPending {
		t.Fatalf("前提が崩れた: 予約が立っていない (cmd=%v pending=%v)", cmd != nil, m.issuesOv.RescanPending)
	}

	_, cmd := m.Update(issuesview.IssuesScanMsg{Dirs: []string{iss.Dir}, Issues: []*issues.Issue{iss}})
	if cmd == nil {
		t.Error("issuesScanMsg の case が取り直しの Cmd を捨てている (予約が失われる)")
	}
}

// newTestIssuesView は閉じる演出を切った viewer。🚨 既存テストは「close したら即座に畳まれて
// いる」前提で書かれているので、演出を挟むと全て 1 拍待ちになって読めなくなる。演出そのものは
// issues_close_anim_test.go が明示的に on にして検査する (newTestBrowse の zoom.off と同じ)。
func newTestIssuesView() issuesview.IssuesView {
	v := issuesview.NewIssuesView()
	v.CloseAnimOff = true
	return v
}

func TestIssuesViewBodyHintKeysAllRespond(t *testing.T) {
	// 案内キーごとの「押したら何が変わるか」。🚨 効果を観測できる状態を各自で作る
	// (本文の途中まで送ってから k を押す等。端で押して「変わらない」を通すと空振りする)
	probes := map[string]func(t *testing.T, e *bodyKeyEnv){
		"j": func(t *testing.T, e *bodyKeyEnv) {
			before := e.v.BodyPager.Offset
			e.press("j")
			if e.v.BodyPager.Offset <= before {
				t.Errorf("j で本文が下へ進まない: %d → %d", before, e.v.BodyPager.Offset)
			}
		},
		"k": func(t *testing.T, e *bodyKeyEnv) {
			// 🚨 G を押して状態を作らない。G が壊れると k の probe も red になり、
			// 壊れていないキーを名指しして原因追跡を遅らせる
			e.v.BodyPager.Offset = 5
			before := e.v.BodyPager.Offset
			e.press("k")
			if e.v.BodyPager.Offset >= before {
				t.Errorf("k で本文が上へ戻らない: %d → %d", before, e.v.BodyPager.Offset)
			}
		},
		"Space": func(t *testing.T, e *bodyKeyEnv) {
			before := e.v.BodyPager.Offset
			e.press(" ")
			// 🚨 方向だけでなく「半ページ」まで見る (1 行送りの実装を通さない)
			if want := before + e.rows()/2; e.v.BodyPager.Offset != want {
				t.Errorf("Space が半ページ送りでない: %d → %d (want %d)", before, e.v.BodyPager.Offset, want)
			}
		},
		"g": func(t *testing.T, e *bodyKeyEnv) {
			e.press("G")
			e.press("g")
			if e.v.BodyPager.Offset != 0 {
				t.Errorf("g で先頭へ戻らない: bodyOff=%d", e.v.BodyPager.Offset)
			}
		},
		"G": func(t *testing.T, e *bodyKeyEnv) {
			e.press("G")
			// 🚨 「0 でない」ではなく末尾そのものを見る (途中で止まる実装を通さない)
			if want := e.v.Body.Len() - e.rows(); e.v.BodyPager.Offset != want {
				t.Errorf("G が末尾へ飛ばない: bodyOff=%d want %d", e.v.BodyPager.Offset, want)
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
			if !e.v.LinkJump.Active {
				t.Error("Tab でジャンプモードに入らない")
			}
		},
		"u": func(t *testing.T, e *bodyKeyEnv) {
			e.press("u")
			if !e.v.UrlPick.Active {
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
			if args, want := (*e.cmds)[before].Args, []string{editor.Fallback, e.v.Open.Path}; !slices.Equal(args, want) {
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
	for _, tok := range strings.Split(e.v.Hint(testHintBudget(t)), "  ") {
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
	v      *issuesview.IssuesView
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
	v.Root = dir // Tab (ジャンプモード) は「repo の中」の範囲が決まらないとパスを開かない (fail-closed)
	landBody(t, v, 10)
	// 🚨 行数は描画で確定する (未描画だと body.Len() = 0 でスクロール上限が 0 になり、
	// j / Space / G が「動かない」ので効果を観測できない)
	v.Lines(renderOpts(20))
	if v.Body.Len() <= 10 {
		t.Fatalf("前提が崩れた: 本文が窓より短くスクロールできない (Len=%d)", v.Body.Len())
	}
	return &bodyKeyEnv{v: v, cmds: cmds, copied: copied, page: 10}
}

func (e *bodyKeyEnv) press(key string) { e.v.HandleKey(key, vp(e.page)) }

// rows は本文 pager の実際の行数 (ヘッダー行数を引いた値)。🚨 page とは違うので、
// 「半ページ」「末尾」を語義で検証する probe はこちらを使う。
func (e *bodyKeyEnv) rows() int { return e.v.VisibleRows(vp(e.page)) }

func (e *bodyKeyEnv) assertClosesBody(t *testing.T, key string) {
	t.Helper()
	e.press(key)
	// 🚨 本文は「閉じる演出の着地後」に捨てられるので、open == nil を待つと時刻の進め方に
	// 依存する。キーの直接の効果である「閉じる演出に入ったか」を見る。
	if e.v.Drawer.Phase() != anim.Closing {
		t.Errorf("%q で本文が閉じ始めない: phase=%v", key, e.v.Drawer.Phase())
	}
	// 次の打鍵で実際に畳まれる (handleKey 冒頭の drawer.finish → discardBody)
	e.press("j")
	if e.v.Open != nil {
		t.Errorf("%q の後に本文が捨てられない", key)
	}
}

// URL ピッカー入力中は、外側の U 横取りを止めて viewer へ委譲する (issue 113)。
//
// urlPicker は doc で「印字文字はすべて検索語に流す。ここで個別のキーを先に横取りすると、
// その文字を含む URL を検索できなくなる」と宣言しているのに、tui.go が委譲の 3 行前で
// 大文字 U だけを奪っていた。`github.com/Ueno/...` のような URL を絞り込めない (silent)。
func TestIssuesViewerOwnsKeysDuringURLPicker(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(issuesviewtest.RealIssue(t))
	if !m.issuesOv.UrlPick.Open([]string{"https://github.com/Ueno/repo", "https://example.com/x"}) {
		t.Fatal("前提が崩れた: URL ピッカーが開かない")
	}
	// 🚨 起動時グランスの既定を継承しないこと。継承すると片方向で vacuous になる (敵対的レビュー)
	m.usageOv.visible = false

	m.handleKey("U")

	// 本命の主張はこちら: キーが viewer へ届いているか
	if q := m.issuesOv.UrlPick.Query(); q != "U" {
		t.Errorf("U が検索語に届いていない (外側が横取りしている): query=%q (期待 \"U\")", q)
	}
	// 🚨 こちらは別の主張 (残量モーダルを開かない)。混ぜて 1 つの非難文にすると、dismiss の
	// 位置を変えただけで「キーを奪っている」と誤診する (敵対的レビューが実測)
	if m.usageOv.visible {
		t.Error("URL ピッカー入力中の U が残量モーダルを開いた")
	}
}

// 🚨 述語の 2 節目 (numFilter.typing) を pin する。これが無いと節ごと削る変異も、
// typing→active の 1 語変異も**全テスト green のまま通る** (敵対的レビューが実測)。
//
// active でなく typing を見る理由: 絞り込みを確定した後 (typing=false, active=true) は通常の
// ナビゲーションなので、そこで U を殺すと**絞り込みを解くまで U が恒久的に死ぬ**。
func TestIssuesViewerOwnsKeysWhileTypingNumberFilter(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(issuesviewtest.RealIssue(t))
	m.issuesOv.NumFilter.Start()
	if !m.issuesOv.OwnsKeys() {
		t.Fatal("番号入力中に ownsKeys() が false")
	}
	m.usageOv.visible = false
	m.handleKey("U")
	if m.usageOv.visible {
		t.Error("番号入力中の U が残量モーダルを開いた (入力モードのキーを外側が奪っている)")
	}

	// 確定後は通常のナビゲーションに戻る = U は外側で受ける
	// 🚨 数字を打ってから確定すること。空入力の確定は confirmNumberFilter が解除へ回すので active も
	//   false になり、typing→active の変異を素通りさせる (敵対的レビューの再現手順は / → 0 → Enter だった)
	if !m.issuesOv.NumFilter.Edit("0") {
		t.Fatal("前提が崩れた: 番号フィルタが数字を受け付けない")
	}
	m.issuesOv.NumFilter.Confirm()
	if !m.issuesOv.NumFilter.Active {
		t.Fatal("前提が崩れた: 確定後に絞り込みが残っていない")
	}
	if m.issuesOv.OwnsKeys() {
		t.Fatal("絞り込み確定後も ownsKeys() が true (U が恒久的に死ぬ)")
	}
	releaseKey(m) // 指を離す (上の U とキーリピート判定を跨ぐ)
	m.handleKey("U")
	if !m.usageOv.visible {
		t.Error("絞り込み確定後に U が効かない (typing でなく active を見ている)")
	}
}

// 🚨 述語の 3 節目 (markNext.active) を pin する。
//
// 変更前は確認中の U で usage が開き、**確認が armed のまま裏に残った** (続く y で実ファイル移動)。
// 変更後は U が「y/Enter 以外 = 取り消し」に落ちる (spec の「知らないキーを押したら実ファイルが
// 動いた、を作らない」と一致)。安全側の変化なので固定する。
func TestIssuesViewerOwnsKeysDuringMarkNextConfirm(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(issuesviewtest.RealIssue(t))
	m.issuesOv.MarkNext = issuesview.IssuesMarkConfirm{Active: true}
	if !m.issuesOv.OwnsKeys() {
		t.Fatal("確認中に ownsKeys() が false")
	}
	m.usageOv.visible = false

	m.handleKey("U")

	if m.usageOv.visible {
		t.Error("確認中の U が残量モーダルを開いた (確認が armed のまま裏に残る)")
	}
	if m.issuesOv.MarkNext.Active {
		t.Error("確認が取り消されていない (知らないキーで実ファイルが動く側に倒れている)")
	}
}

// 🚨 ガードが広すぎないこと: viewer を開いているだけ (どのモードにも入っていない) なら
// U は従来どおり外側で受ける (ユーザー要望 2026-08-01 の「viewer の上でも U は効く」)。
// これが無いと「ownsKeys() を常に true にする」変異が素通りする。
func TestIssuesViewerUsageStillWorksWhenNotOwningKeys(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(issuesviewtest.RealIssue(t))
	if m.issuesOv.OwnsKeys() {
		t.Fatal("前提が崩れた: 一覧モードで ownsKeys() が true")
	}
	m.usageOv.visible = false

	m.handleKey("U")

	if !m.usageOv.visible {
		t.Error("viewer の一覧モードで U が効かない (横取りを止めすぎている)")
	}
}

// term.IsCopySafe の境界: タブだけ通し、貼ったときに端末やシェルが解釈するものは断る (issue 753)。
func TestIsCopySafe(t *testing.T) {
	for s, want := range map[string]bool{
		"/repo/issues/001-feat-x.md": true,
		"drive\tsync":                true,  // group 名の実例 (生でコピーする決定)
		"a\nrm -rf x":                false, // 貼った瞬間にコマンドが走る
		"a\x1b[201~b":                false, // bracketed paste の終わり
		"a\u0085b":                   false, // C1
		"a\u202eb":                   false, // BiDi
		"a\rb":                       false,
		"\u26a0\ufe0f-notes":         true, // 絵文字の VS16 は制御文字ではない (無害化の結果との一致で判定すると断っていた)
		"epic-\u2764\ufe0f":          true,
	} {
		if got := term.IsCopySafe(s); got != want {
			t.Errorf("term.IsCopySafe(%q) = %v, want %v", s, got, want)
		}
	}
}
