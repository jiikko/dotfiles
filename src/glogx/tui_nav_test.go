package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

func TestBrowseCursorNavigation(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	m.handleKey("j")
	if m.cursor != 1 {
		t.Errorf("j 後の cursor = %d; want 1", m.cursor)
	}
	m.handleKey("k")
	m.handleKey("k") // 先頭で止まる
	if m.cursor != 0 {
		t.Errorf("k 連打後の cursor = %d; want 0", m.cursor)
	}
	// emacs 風の Ctrl-N / Ctrl-P でも移動できる
	m.handleKey("ctrl+n")
	if m.cursor != 1 {
		t.Errorf("ctrl+n 後の cursor = %d; want 1", m.cursor)
	}
	m.handleKey("ctrl+p")
	if m.cursor != 0 {
		t.Errorf("ctrl+p 後の cursor = %d; want 0", m.cursor)
	}
	m.handleKey("G")
	if m.cursor != 2 {
		t.Errorf("G 後の cursor = %d; want 2", m.cursor)
	}
	m.handleKey("g")
	if m.cursor != 0 || m.offset != 0 {
		t.Errorf("g 後の cursor/offset = %d/%d; want 0/0", m.cursor, m.offset)
	}
}

func TestBrowseWrapUsesFullWidth(t *testing.T) {
	// カーソル溝の廃止 (git log と左マージンを揃える) 後は端末の全幅で折り返す。
	// 折り返し幅と clip 幅がずれると全幅の行の末尾が「…」に食われる
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess
	m.commits[0].Message = strings.Repeat("あ", 38) // 表示幅 76 (旧実装だと 1 行に収まり溝で溢れる)
	m.height = 30
	view := m.View()
	if got := strings.Count(view, "あ"); got != 38 {
		t.Errorf("折り返しで文字が欠けた: あ が %d 文字 (want 38)\n%s", got, view)
	}
	for line := range strings.SplitSeq(view, "\n") {
		if w := runewidth.StringWidth(stripANSI(line)); w > m.width {
			t.Errorf("幅超過 (%d > %d): %q", w, m.width, line)
		}
	}
}

func TestBrowseCursorScrollsViewport(t *testing.T) {
	// medium 形式 1 コミット ≈ 6 行 × 5 件 > 高さ 10 なので、下へ移動すると offset が進む
	m := newTestBrowse(t, 5, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	for range 4 {
		m.handleKey("j")
	}
	if m.offset == 0 {
		t.Errorf("末尾コミットへ移動しても offset が 0 のまま")
	}
}

// j/k でビューポートがコミット単位に動くとき、表示 offset を glide させる (ユーザー要望)。
func TestBrowseScrollAnim(t *testing.T) {
	m := newTestBrowse(t, 6, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	m.height = 10 // pageSize 9。medium 1 コミット ~5 行 + 行間で数 j 目にスクロールが起きる
	scrolled := false
	for range 5 {
		prev := m.offset
		_, cmd := m.handleKey("j")
		if m.offset == prev {
			if m.scrollAnim {
				t.Fatal("画面内のカーソル移動で scrollAnim が立った")
			}
			continue
		}
		// ビューポートが動いた最初の j: glide 開始
		if !m.scrollAnim || m.offsetShown != prev || cmd == nil {
			t.Fatalf("スクロール開始で glide が仕込まれない: scrollAnim=%v offsetShown=%d prev=%d cmd=%v",
				m.scrollAnim, m.offsetShown, prev, cmd != nil)
		}
		scrolled = true
		break
	}
	if !scrolled {
		t.Fatal("6 コミットで一度もスクロールしなかった (テスト前提の破れ)")
	}
	// 連打: glide 中の次の j は積まず即スナップ (押した分だけ遅延する体感を避ける)
	m.handleKey("j")
	if m.scrollAnim {
		t.Fatal("glide 中の j で scrollAnim が積まれた (即スナップのはず)")
	}
}

// 背高コミット (大きな offset ジャンプ) でも 1 コミット移動なら glide する
// (行数キャップ撤去の回帰: 以前は >12 行で snap してコミット高で挙動が変わっていた)。
func TestBrowseScrollAnimNoHeightCap(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	m.offset = 40 // ensureCursorVisible が背高コミットで飛ばした後を想定した大ジャンプ
	cmd := m.startScrollAnim(0)
	if !m.scrollAnim || m.offsetShown != 0 || cmd == nil {
		t.Fatalf("大ジャンプが animate されない: scrollAnim=%v offsetShown=%d cmd=%v",
			m.scrollAnim, m.offsetShown, cmd != nil)
	}
}

// advanceScroll は上下どちらの向きでも tick で表示 offset を論理 offset へ寄せ、
// 有限フレームで着地して scrollAnim を下ろす (geometry 非依存に決定的検証)。
func TestBrowseScrollAnimConverges(t *testing.T) {
	for _, tc := range []struct{ from, to int }{
		{from: 0, to: 7}, // 下スクロール (1 コミット ~7 行)
		{from: 7, to: 0}, // 上スクロール
		{from: 3, to: 4}, // 残り 1 行
		{from: 5, to: 5}, // 動きなし → 即座に scrollAnim を下ろす
	} {
		m := newTestBrowse(t, 6, map[string]CIState{}, nil)
		m.statuses = statusesFor(m, StateSuccess)
		m.offsetShown = tc.from
		m.scrollFrom = tc.from
		m.scrollFrame = 0
		m.offset = tc.to
		m.scrollAnim = true
		prevShown := m.offsetShown
		frames := 0
		for m.scrollAnim {
			m.advanceScroll()
			frames++
			// ease-in: 表示 offset は目標を通り越さず単調に近づく
			if (tc.to > tc.from && (m.offsetShown < prevShown || m.offsetShown > tc.to)) ||
				(tc.to < tc.from && (m.offsetShown > prevShown || m.offsetShown < tc.to)) {
				t.Fatalf("from=%d to=%d: 非単調/行き過ぎ (offsetShown=%d)", tc.from, tc.to, m.offsetShown)
			}
			prevShown = m.offsetShown
			if frames > 20 {
				t.Fatalf("from=%d to=%d: 収束しない (offsetShown=%d)", tc.from, tc.to, m.offsetShown)
			}
		}
		if m.offsetShown != tc.to {
			t.Errorf("from=%d to=%d: 着地 offsetShown=%d, want %d", tc.from, tc.to, m.offsetShown, tc.to)
		}
		if frames > scrollAnimFrames {
			t.Errorf("from=%d to=%d: %d フレーム (scrollAnimFrames=%d 以内のはず)", tc.from, tc.to, frames, scrollAnimFrames)
		}
	}
}

// 80ms tick は single-flight: チェーンは常に高々 1 本 (レビュー C1)。
func TestBrowseTickSingleFlight(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.usageOv.visible = false // usage 取得中も spinnerActive になるため、tick 単体の検証から隔離する
	if m.maybeTick() == nil || !m.ticking {
		t.Fatal("初回 maybeTick が tick を返さない / ticking が立たない")
	}
	if m.maybeTick() != nil {
		t.Fatal("チェーンが生きているのに 2 本目の maybeTick が非 nil (二重チェーン)")
	}
	// tickMsg 到着で 1 拍消費 → spinnerActive なら 1 本だけ再アーム
	m.fetching = true
	if _, cmd := m.Update(tickMsg{}); cmd == nil || !m.ticking {
		t.Fatal("spinnerActive 中の tickMsg で再アームされない")
	}
	// spinnerActive でなくなれば再アームせずチェーンは死ぬ
	m.fetching = false
	if _, cmd := m.Update(tickMsg{}); cmd != nil || m.ticking {
		t.Fatalf("非 spinnerActive で tick が止まらない: cmd=%v ticking=%v", cmd != nil, m.ticking)
	}
}

// pull アニメは 1 tickMsg = offset 1 減算 (複数チェーン併存による 2 倍速化の回帰・C1)。
func TestBrowseTickPullAnimOncePerTick(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.height = 6 // 短すぎない (offset を持てる)
	m.pullAnimating = true
	m.offset = 3
	m.Update(tickMsg{})
	if m.offset != 2 {
		t.Fatalf("1 tickMsg で offset=%d (want 2 = 1 回だけ減算)", m.offset)
	}
	m.Update(tickMsg{})
	if m.offset != 1 {
		t.Fatalf("2 tickMsg 後 offset=%d (want 1)", m.offset)
	}
}

// tickMsg の list 無効化は fetch/pushPoll のときだけ (レビュー C7)。
func TestBrowseTickInvalidateGate(t *testing.T) {
	// fetching 中はリストの loading スピナーが動くので無効化する
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.fetching = true
	m.linesValid = true
	m.Update(tickMsg{})
	if m.linesValid {
		t.Error("fetching 中の tickMsg でリストが無効化されない")
	}
	// pullAnimating だけ (fetch/pushPoll 無し) では list 内容は不変なので無効化しない
	m2 := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m2.height = 6
	m2.pullAnimating = true
	m2.offset = 3
	m2.linesValid = true
	m2.Update(tickMsg{})
	if !m2.linesValid {
		t.Error("pullAnimating のみで list が無効化された (C7 gate 破れ = 毎フレーム全行再構築)")
	}
}

func TestBrowseQuitFillsUnknown(t *testing.T) {
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	_, cmd := m.handleKey("q")
	if cmd == nil {
		t.Fatalf("q で Quit が返らない")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("q の Cmd が tea.Quit でない: %v", msg)
	}
	if !m.done {
		t.Errorf("done が立っていない")
	}
	if m.statuses[shas[0]] != StateUnknown {
		t.Errorf("取得中断で unknown に落ちていない: %v", m.statuses[shas[0]])
	}
	if m.View() != "" {
		t.Errorf("done 後の View が空でない (TUI 領域が残る): %q", m.View())
	}
}

func TestBrowseBatchedRunesKeyMsg(t *testing.T) {
	// 高速連打で複数キーが 1 つの KeyMsg (Runes="hhq") にまとまっても 1 文字ずつ
	// 処理される (未対応だと q が無視されて終了できない: pty スモークで実測した回帰)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	m.openJobDetail()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hhq")})
	if !m.done {
		t.Fatalf("hhq のまとめ配送で終了しない (detailOpen=%v panelSHA=%q)", m.detailOv.open, m.panelSHA)
	}
	if cmd == nil {
		t.Errorf("q の Quit Cmd が返らない")
	}
}

func TestBrowseQPopsViewStack(t *testing.T) {
	// q はビューのスタックを 1 段戻る (詳細 → job 一覧 → コミット一覧 → 終了)。
	// 即終了は Ctrl-C (ユーザー要望: tig 流)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	m.detailOv.cache[m.detailKey()] = []string{"line"}
	m.openJobDetail()
	m.handleKey("q")
	if m.detailOv.open || m.panelSHA == "" || m.done {
		t.Fatalf("q 1回目: 詳細だけ閉じるべき (detailOpen=%v panelSHA=%q done=%v)", m.detailOv.open, m.panelSHA, m.done)
	}
	m.handleKey("q")
	if m.panelSHA != "" || m.done {
		t.Fatalf("q 2回目: パネルだけ閉じるべき (panelSHA=%q done=%v)", m.panelSHA, m.done)
	}
	_, cmd := m.handleKey("q")
	if cmd == nil || !m.done {
		t.Errorf("q 3回目 (一覧) で終了しない")
	}
}

func TestBrowseCtrlCQuitsAnywhere(t *testing.T) {
	// Ctrl-C はどの階層からでも即終了
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	m.detailOv.cache[m.detailKey()] = []string{"line"}
	m.openJobDetail()
	_, cmd := m.handleKey("ctrl+c")
	if cmd == nil || !m.done {
		t.Errorf("詳細表示中の Ctrl-C で即終了しない")
	}
}

func TestBrowseLinesMemoized(t *testing.T) {
	// 行リストはカーソル移動やパネル開閉で再構築しない (メモ化)。-p の巨大 patch で
	// キー 1 打ごとに全行を組み直す計算量爆発を防ぐ
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	first := m.lines()
	m.handleKey("j")
	m.handleKey("ctrl+d")
	withJobs(m, 0)
	m.openPanel()
	if second := m.lines(); &first[0] != &second[0] {
		t.Errorf("カーソル移動・パネル開閉で行リストが再構築された")
	}
	// 状態を変える更新 (CI 結果のマージ) では再構築される
	sha := m.commits[0].SHA
	m.Update(ciResultMsg{batch: CIBatch{Statuses: map[string]CIState{sha: StateFailure}}})
	rebuilt := m.lines()
	if &first[0] == &rebuilt[0] {
		t.Errorf("CI 結果反映後も古い行リストのまま")
	}
	// 先頭は all pushed マークになりうるため、ヘッダー行 (CommitIdx 0) で状態を見る
	var header string
	for _, l := range rebuilt {
		if l.Header && l.CommitIdx == 0 {
			header = l.Text
			break
		}
	}
	if !strings.Contains(header, "✗") {
		t.Errorf("再構築後の行に新しい状態が反映されていない: %q", header)
	}
}

func TestJapaneseFullViewStaysInWidth(t *testing.T) {
	// subject・message・diff 本文・job 名・詳細ログの全部が日本語でも View の全行が幅内
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.height = 40
	sha := m.commits[0].SHA
	m.statuses[sha] = StateFailure
	m.commits[0].Subject = "日本語のサブジェクト: 表示崩れの検証"
	m.commits[0].Message = "日本語のサブジェクト: 表示崩れの検証\n\n" + strings.Repeat("本文の長い日本語テキスト。", 20)
	m.commits[0].Body = "+\t日本語のコード行 := \"値\"\n-\tもう一行の日本語\n"
	m.details[sha] = []CheckDetail{
		{Name: "テスト (ユニット)", State: StateFailure, URL: "https://github.com/o/r/runs/1"},
		{Name: strings.Repeat("長いジョブ名", 15), State: StateSuccess},
	}
	m.openPanel()
	m.handleKey("j")
	m.detailOv.cache[m.detailKey()] = []string{
		strings.Repeat("日本語のログ行です。", 12),
		"##[error]日本語のエラーメッセージ",
	}
	m.openJobDetail()
	for line := range strings.SplitSeq(m.View(), "\n") {
		if w := runewidth.StringWidth(stripANSI(line)); w > m.width {
			t.Errorf("幅超過 (%d > %d): %q", w, m.width, line)
		}
	}
	if !strings.Contains(m.View(), "テスト (ユニット)") {
		t.Errorf("日本語 job 名が表示されていない:\n%s", m.View())
	}
}

// git log --color は短縮形リセット "\x1b[m" を使う。bgLine (カーソル行の bg 塗り) は
// これでも bg を張り直して行末まで塗れる (literal "\x1b[0m" 一致だけだと途切れる回帰の防止)。
func TestBgLineReappliesAfterShortReset(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.colored = true
	got := m.bgLine("\x1b[33mcommit abc\x1b[m subject", ansiCursorBg)
	if !strings.Contains(got, "\x1b[m"+ansiCursorBg) {
		t.Fatalf("短縮形リセット後に bg が張り直されない: %q", got)
	}
	if !strings.HasSuffix(got, ansiReset) || !strings.Contains(got, "  ") {
		t.Fatalf("行末までの padding が無い: %q", got)
	}
}

// C-f は全ビューで → の別名。C-b の ← 別名は無い (push は b。glogx の独自仕様)。
func TestBrowseEmacsHorizontalAliases(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	withJobs(m, 0)
	// 一覧: C-f = → = パネルを開く
	m.handleKey("ctrl+f")
	if m.panelSHA == "" {
		t.Fatal("C-f でパネルが開かない (right の別名のはず)")
	}
	// C-b は left の別名ではない (未割当で何も起きない)
	m.handleKey("ctrl+b")
	if m.panelSHA == "" {
		t.Fatal("C-b でパネルが閉じた (glogx では未割当のはず)")
	}
}

// カーソル溝の廃止 (2026-07-19): 行頭 2 桁の溝を足さず git log と左マージンが一致し、
// カーソルはヘッダー行全体の bg 塗りで示す。
func TestBrowseCursorGutterArrowAndBgHighlight(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.usageOv.visible = false // 右上 usage モーダルは上部行を覆うため、カーソル強調の検証から隔離する
	m.statuses = statusesFor(m, StateSuccess)
	m.colored = true
	view := strings.Split(m.View(), "\n")
	var authorLine, cursorHeader, otherHeader string
	for _, l := range view {
		if strings.HasPrefix(stripANSI(l), cursorGutterBlank+"Author: ") && authorLine == "" {
			authorLine = l
		}
		if strings.Contains(l, "commit "+m.commits[0].SHA) {
			cursorHeader = l
		}
		if strings.Contains(l, "commit "+m.commits[1].SHA) {
			otherHeader = l
		}
	}
	if authorLine == "" || cursorHeader == "" || otherHeader == "" {
		t.Fatalf("期待行が見つからない:\n%s", m.View())
	}
	// 全行にカーソル溝 2 桁のマージンがあり、カーソル行だけ「→ 」が入る
	if !strings.HasPrefix(stripANSI(cursorHeader), cursorGutterMark) {
		t.Errorf("カーソル行が %q で始まっていない: %q", cursorGutterMark, cursorHeader)
	}
	if !strings.HasPrefix(stripANSI(otherHeader), cursorGutterBlank) {
		t.Errorf("非カーソル行に溝の空白マージンがない: %q", otherHeader)
	}
	if !strings.Contains(cursorHeader, ansiCursorBg) {
		t.Errorf("カーソル行に bg 塗りがない: %q", cursorHeader)
	}
	if strings.Contains(otherHeader, ansiCursorBg) {
		t.Errorf("非カーソル行に bg が付いている: %q", otherHeader)
	}
	// bg は行内のリセット後も維持される (途切れると sha の直後で塗りが切れる)
	if strings.Contains(cursorHeader, ansiReset+" ") && !strings.Contains(cursorHeader, ansiReset+ansiCursorBg) {
		t.Errorf("リセット後に bg が張り直されていない: %q", cursorHeader)
	}
	// 色なしは bg が使えないため「→ 」のみに degrade (溝は全行にあるのでずれない)
	m.colored = false
	m.invalidateLines()
	if !strings.Contains(m.View(), cursorGutterMark) {
		t.Errorf("NO_COLOR で %q マーカーが出ていない", cursorGutterMark)
	}
}

// ensureCursorVisible の上スクロール分岐: カーソルが窓の上へ戻ったとき offset がカーソル行の
// header index へ追従する (定数を変えても既存テストが green のままだった無防備な UI 不変条件)。
// push 境界マーカーで commit0 の header が index 0 とは限らないため、offset は lines() から引いた
// 実 header index と一致することで検証する。
func TestEnsureCursorVisibleScrollsUp(t *testing.T) {
	m := newTestBrowse(t, 10, nil, nil)
	m.statuses = statusesFor(m, StateSuccess)
	m.height = 8 // 小さい page で list をスクロール可能に
	m.cursor = 0
	m.offset = 12 // カーソル (先頭) より下へスクロールした状態
	m.ensureCursorVisible()
	hdr := -1
	for i, l := range m.lines() {
		if l.Header && l.CommitIdx == 0 {
			hdr = i
			break
		}
	}
	if hdr < 0 {
		t.Fatal("commit0 の header 行が見つからない")
	}
	if m.offset != hdr {
		t.Errorf("上スクロールで offset が header に追従しない: offset=%d header=%d", m.offset, hdr)
	}
}

// advancePullAnim の終端: offset が 0 に達したら pullAnimating を必ず下ろす (負に振れない)。
// 下ろし損ねると spinnerActive が真のまま tick が 80ms 毎に永久に回る実害があるため termination
// invariant を対で固定する (offset=1 は 1 回、offset=2 は 2 回で終端)。
func TestAdvancePullAnimTerminates(t *testing.T) {
	m := newTestBrowse(t, 5, nil, nil)
	m.statuses = statusesFor(m, StateSuccess)

	m.pullAnimating, m.offset = true, 1
	m.advancePullAnim()
	if m.offset != 0 || m.pullAnimating {
		t.Errorf("offset=1 から 1 回で終端しない: offset=%d animating=%v", m.offset, m.pullAnimating)
	}

	m.pullAnimating, m.offset = true, 2
	m.advancePullAnim()
	if m.offset != 1 || !m.pullAnimating {
		t.Errorf("offset=2 の 1 回目: offset=%d animating=%v; want 1/true", m.offset, m.pullAnimating)
	}
	m.advancePullAnim()
	if m.offset != 0 || m.pullAnimating {
		t.Errorf("offset=2 の 2 回目で終端しない (負に振れた?): offset=%d animating=%v", m.offset, m.pullAnimating)
	}
}
