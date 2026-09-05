package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// --- ヘルパー ---

// newTestStatusView は「開いた状態で porcelain を読み込んだ」viewer を返す。
// 🚨 toggle() は通さない: 返り値の tea.Batch には tea.Tick (自動更新 1.5s / プレビュー 120ms) が
// 混ざっており、テストで実行すると本当に待つことになる。読み込みは loadCmd だけを同期実行する。
func newTestStatusView(t *testing.T, porcelain string) *statusView {
	t.Helper()
	v := newStatusView()
	v.closeAnimOff = true
	v.shown = true
	stubWorktreeStatus(t, porcelain, nil)
	applyStatusLoad(t, &v)
	return &v
}

// stubWorktreeStatus は loadWorktreeStatus を差し替える (返す porcelain を後から差し替えられる
// ように、ポインタ経由で読む)。返り値のポインタへ書くと次の読み込みの結果が変わる。
func stubWorktreeStatus(t *testing.T, porcelain string, err error) *struct {
	out   string
	err   error
	calls int
} {
	t.Helper()
	state := &struct {
		out   string
		err   error
		calls int
	}{out: porcelain, err: err}
	orig := loadWorktreeStatus
	loadWorktreeStatus = func() (worktreeStatus, error) {
		state.calls++
		if state.err != nil {
			return worktreeStatus{}, state.err
		}
		return parseWorktreeStatus(state.out), nil
	}
	t.Cleanup(func() { loadWorktreeStatus = orig })
	return state
}

// applyStatusLoad は v.loadCmd() を同期実行して結果を receive へ渡す (tick を含まない Cmd なので
// そのまま実行できる)。
func applyStatusLoad(t *testing.T, v *statusView) {
	t.Helper()
	cmd := v.loadCmd()
	if cmd == nil {
		t.Fatal("loadCmd() = nil (取得中のまま?)")
	}
	msg, ok := cmd().(statusLoadMsg)
	if !ok {
		t.Fatalf("loadCmd() の msg = %T, want statusLoadMsg", msg)
	}
	v.receive(msg)
}

// deliverStatus は「git status が返ってきた」ことにして viewer へ流す (browseModel 経由の
// テスト用)。🚨 Update から返った Cmd を実行する経路は使えない: toggle の Batch には tick が
// 混ざっており、実行すると本当に待つ (newTestStatusView の doc と同じ理由)。
func deliverStatus(t *testing.T, v *statusView, porcelain string) {
	t.Helper()
	v.receive(statusLoadMsg{st: parseWorktreeStatus(porcelain), gen: v.gen})
}

// applyCmd は操作が返した Cmd (loadCmd か nil) を反映する。
func applyCmd(t *testing.T, v *statusView, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	if msg, ok := cmd().(statusLoadMsg); ok {
		v.receive(msg)
	}
}

// stubGitOps は stage 系 git を差し替え、呼ばれた引数を記録する。
type gitOpCalls struct {
	add           [][]string
	restoreStaged [][]string
	restoreWork   [][]string
	clean         [][]string
}

func stubGitOps(t *testing.T, err error) *gitOpCalls {
	t.Helper()
	calls := &gitOpCalls{}
	origAdd, origStaged, origWork, origClean := runGitAdd, runGitRestoreStaged, runGitRestoreWorktree, runGitCleanUntracked
	runGitAdd = func(paths []string) error { calls.add = append(calls.add, paths); return err }
	runGitRestoreStaged = func(paths []string) error {
		calls.restoreStaged = append(calls.restoreStaged, paths)
		return err
	}
	runGitRestoreWorktree = func(paths []string) error {
		calls.restoreWork = append(calls.restoreWork, paths)
		return err
	}
	runGitCleanUntracked = func(paths []string) error { calls.clean = append(calls.clean, paths); return err }
	t.Cleanup(func() {
		runGitAdd, runGitRestoreStaged, runGitRestoreWorktree, runGitCleanUntracked = origAdd, origStaged, origWork, origClean
	})
	return calls
}

func testViewport() statusViewport { return statusViewport{width: 120, page: 20} }

func testStatusOpts(width, page int) statusRenderOpts {
	return statusRenderOpts{width: width, page: page, spinner: "⠋"}
}

// cursorPathAt はカーソル行の (セクション, パス)。
func cursorPathAt(t *testing.T, v *statusView) (worktreeSection, string) {
	t.Helper()
	row, ok := v.current()
	if !ok {
		t.Fatal("カーソル行がない")
	}
	return row.section, row.path
}

// --- stage / unstage ---

func TestStatusSpaceStagesUnstagedRow(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	calls := stubGitOps(t, nil)
	applyCmd(t, v, v.handleKey(" ", testViewport()))
	if len(calls.add) != 1 || calls.add[0][0] != worktreePathspec("a.go") {
		t.Fatalf("git add の呼び出し = %v, want [[a.go]]", calls.add)
	}
	if len(calls.restoreStaged) != 0 {
		t.Errorf("unstaged 行の Space で restore --staged が走った: %v", calls.restoreStaged)
	}
}

func TestStatusSpaceUnstagesStagedRow(t *testing.T) {
	v := newTestStatusView(t, statusRec("M  a.go"))
	calls := stubGitOps(t, nil)
	applyCmd(t, v, v.handleKey(" ", testViewport()))
	if len(calls.restoreStaged) != 1 || calls.restoreStaged[0][0] != worktreePathspec("a.go") {
		t.Fatalf("restore --staged の呼び出し = %v, want [[a.go]]", calls.restoreStaged)
	}
	if len(calls.add) != 0 {
		t.Errorf("staged 行の Space で git add が走った: %v", calls.add)
	}
}

func TestStatusSpaceStagesUntrackedRow(t *testing.T) {
	v := newTestStatusView(t, statusRec("?? new.txt"))
	calls := stubGitOps(t, nil)
	applyCmd(t, v, v.handleKey(" ", testViewport()))
	if len(calls.add) != 1 || calls.add[0][0] != worktreePathspec("new.txt") {
		t.Fatalf("git add の呼び出し = %v, want [[new.txt]]", calls.add)
	}
}

func TestStatusConflictRowRejectsStageAndDiscard(t *testing.T) {
	v := newTestStatusView(t, statusRec("UU c.go"))
	calls := stubGitOps(t, nil)
	applyCmd(t, v, v.handleKey(" ", testViewport()))
	if len(calls.add) != 0 || len(calls.restoreStaged) != 0 {
		t.Fatalf("conflict 行で git が走った: add=%v staged=%v", calls.add, calls.restoreStaged)
	}
	if notice, ok := v.takeNotice(); ok || !strings.Contains(notice, "conflict") {
		t.Errorf("notice = %q (ok=%v), want conflict の案内を失敗として出す", notice, ok)
	}
	v.handleKey("X", testViewport())
	if v.discarding {
		t.Error("conflict 行で X の確認モーダルが開いた")
	}
}

func TestStatusStageAllStagesUnstagedAndUntracked(t *testing.T) {
	v := newTestStatusView(t, statusRec("M  staged.go", " M a.go", "?? b.txt"))
	calls := stubGitOps(t, nil)
	applyCmd(t, v, v.handleKey("a", testViewport()))
	if len(calls.add) != 1 {
		t.Fatalf("git add の呼び出し回数 = %d, want 1 (まとめて 1 回)", len(calls.add))
	}
	got := strings.Join(calls.add[0], ",")
	want := strings.Join([]string{worktreePathspec("a.go"), worktreePathspec("b.txt")}, ",")
	if got != want {
		t.Fatalf("git add のパス = %q, want %q (staged 済みを含めない)", got, want)
	}
}

func TestStatusStageAllOnCleanTreeDoesNothing(t *testing.T) {
	v := newTestStatusView(t, statusRec("M  staged.go"))
	calls := stubGitOps(t, nil)
	v.handleKey("a", testViewport())
	if len(calls.add) != 0 {
		t.Fatalf("stage 対象が無いのに git add が走った: %v", calls.add)
	}
	if notice, ok := v.takeNotice(); !ok || notice == "" {
		t.Errorf("notice = %q (ok=%v), want 「stage するものがありません」", notice, ok)
	}
}

// --- カーソル (spec 4 節の不変条件 2 / 5 節) ---

// stage したファイルは別セクションへ移るが、カーソルは追いかけず「同じ位置の次のファイル」に
// 留まる (Space 連打で上から順に stage できる)。
func TestStatusCursorStaysInSectionAfterStage(t *testing.T) {
	state := stubWorktreeStatus(t, statusRec(" M a.go", " M b.go", " M c.go"), nil)
	v := newStatusView()
	v.closeAnimOff = true
	v.shown = true
	applyStatusLoad(t, &v)
	v.handleKey("j", testViewport()) // b.go へ
	if _, path := cursorPathAt(t, &v); path != "b.go" {
		t.Fatalf("移動後のカーソル = %q, want b.go", path)
	}
	stubGitOps(t, nil)
	state.out = statusRec("M  b.go", " M a.go", " M c.go") // b.go が staged へ移った後の状態
	applyCmd(t, &v, v.handleKey(" ", testViewport()))
	sec, path := cursorPathAt(t, &v)
	if sec != sectionUnstaged || path != "c.go" {
		t.Fatalf("stage 後のカーソル = (section %d, %q), want (Unstaged, c.go)", sec, path)
	}
}

// 外部編集で行が増えても、カーソルは同じパスに残る (index 保持だと隣のファイルへ滑り、
// X が「見ていたのとは別のファイル」を捨てる事故になる)。
func TestStatusCursorFollowsPathOnExternalEdit(t *testing.T) {
	state := stubWorktreeStatus(t, statusRec(" M b.go"), nil)
	v := newStatusView()
	v.closeAnimOff = true
	v.shown = true
	applyStatusLoad(t, &v)
	if _, path := cursorPathAt(t, &v); path != "b.go" {
		t.Fatalf("初期カーソル = %q", path)
	}
	// 別プロセスが a.go を編集して b.go の上に挿入された
	state.out = statusRec(" M a.go", " M b.go")
	applyStatusLoad(t, &v)
	if _, path := cursorPathAt(t, &v); path != "b.go" {
		t.Fatalf("外部編集後のカーソル = %q, want b.go (パスで保持していない)", path)
	}
}

func TestStatusJumpSectionMovesToNextSection(t *testing.T) {
	v := newTestStatusView(t, statusRec("M  s.go", " M u.go", "?? n.txt"))
	v.handleKey("tab", testViewport()) // Unstaged (初期位置) → Untracked
	if sec, path := cursorPathAt(t, v); sec != sectionUntracked || path != "n.txt" {
		t.Fatalf("Tab 後 = (section %d, %q), want (Untracked, n.txt)", sec, path)
	}
	v.handleKey("tab", testViewport()) // 末尾からは先頭のセクションへ回る
	if sec, path := cursorPathAt(t, v); sec != sectionStaged || path != "s.go" {
		t.Fatalf("Tab 2 回目 = (section %d, %q), want (Staged, s.go) — 巡回していない", sec, path)
	}
	v.handleKey("tab", testViewport())
	if sec, _ := cursorPathAt(t, v); sec != sectionUnstaged {
		t.Fatalf("Tab 3 回目のセクション = %d, want Unstaged", sec)
	}
}

// 開いた直後に触りたいのは「まだ stage していない変更」(spec 2 節)。
func TestStatusInitialCursorIsFirstUnstagedRow(t *testing.T) {
	v := newTestStatusView(t, statusRec("M  s.go", " M u.go", "?? n.txt"))
	sec, path := cursorPathAt(t, v)
	if sec != sectionUnstaged || path != "u.go" {
		t.Fatalf("初期カーソル = (section %d, %q), want (Unstaged, u.go)", sec, path)
	}
	// Unstaged が無ければ先頭の行 (Staged) から始める
	v2 := newTestStatusView(t, statusRec("M  s.go"))
	if sec, path := cursorPathAt(t, v2); sec != sectionStaged || path != "s.go" {
		t.Fatalf("Unstaged 無しの初期カーソル = (section %d, %q), want (Staged, s.go)", sec, path)
	}
}

// 閉じた時点で走行中だった取得の札を降ろさないと、次に開いたとき loadCmd が「取得中」と判断して
// 二度と読み直さない (古い一覧が永久に居座る)。
func TestStatusReopenAfterCloseDuringLoadCanReload(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	if cmd := v.loadCmd(); cmd == nil { // 走行中にする (結果は届けない)
		t.Fatal("2 回目の loadCmd が nil")
	}
	v.close() // closeAnimOff なので即座に畳まれる
	v.shown = true
	if cmd := v.loadCmd(); cmd == nil {
		t.Fatal("開き直しても loadCmd が nil (loading の札が残っている)")
	}
}

// 取得に失敗しても、一度読めた一覧は消さない (last-good。spec 5 節)。
func TestStatusLoadErrorAfterLoadedKeepsLastGood(t *testing.T) {
	state := stubWorktreeStatus(t, statusRec(" M a.go"), nil)
	v := newStatusView()
	v.closeAnimOff = true
	v.shown = true
	applyStatusLoad(t, &v)
	state.err = errors.New("fatal: unable to read index")
	applyStatusLoad(t, &v)
	// 🚨 1 カラムの幅で見る: 2 カラムだと右のプレビュー欄がカーソル行のパスを出すため、
	// 一覧が消えていても "a.go" が画面に残ってしまい last-good の有無を区別できない
	body := strings.Join(v.lines(testStatusOpts(70, 20)), "\n")
	if !strings.Contains(body, "a.go") {
		t.Fatalf("失敗で last-good の一覧が消えた:\n%s", body)
	}
	if !strings.Contains(stripANSI(body), "status 取得失敗") {
		t.Fatalf("古い可能性を示す印がヘッダーに無い:\n%s", body)
	}
}

// hasTerminalControl は「端末が制御として解釈しうる文字が残っているか」。
//
// 🚨 ESC と BEL だけを見る判定にしないこと: それだと 8bit の CSI (U+009B) / OSC (U+009D) を
// 原理的に見逃し、「ESC と BEL だけ落とす」実装がテストを全部 green で通ってしまう
// (敵対的レビュー 2026-08-05 が実際にこの盲点を突いた)。許可した文字だけが残っているか、の
// allowlist 側で判定する。
func hasTerminalControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return true
		}
	}
	return false
}

// ファイル名の端末制御シーケンスは画面へ出さない。POSIX のファイル名は / と NUL 以外の任意
// バイトを許し、-z の git status はクォートせず生で返すので、第三者ブランチに ESC 入りの名前の
// untracked が 1 つあるだけで status viewer を開いた瞬間に端末が乗っ取られる。
// 🚨 git へ渡す側 (path / pathspecs) は実物のまま — 無害化するとファイルを見失う。
func TestStatusPathIsSanitizedForDisplayOnly(t *testing.T) {
	const esc, bel = "\x1b", "\a"
	evil := "evil" + esc + "]0;pwned" + bel + ".txt"
	row := worktreeRow{section: sectionUntracked, code: '?', x: '?', y: '?', path: evil}

	shown := stripANSI(statusPathText(row, 60, false))
	if hasTerminalControl(shown) {
		t.Errorf("一覧のパスに制御シーケンスが残った: %q", shown)
	}
	if strings.Contains(shown, "pwned") {
		t.Errorf("OSC の中身が一覧に残った: %q", shown)
	}
	if row.path != evil {
		t.Errorf("同一性のパスが書き換わった (git へ渡す値が実物とずれる): %q", row.path)
	}
	if got := row.pathspecs(); len(got) == 0 || !strings.Contains(got[0], evil) {
		t.Errorf("pathspec が実物のパスを保っていない: %q", got)
	}
	// rename 行は "元 → 先" の両方が対象
	rn := worktreeRow{section: sectionStaged, code: 'R', path: "new.txt", orig: evil}
	if got := stripANSI(statusPathText(rn, 60, false)); hasTerminalControl(got) {
		t.Errorf("rename 元に制御シーケンスが残った: %q", got)
	}
}

// 通知文はパスや git のエラー出力を素で埋め込むので、setNotice 自体を関門にする
// (呼び出しごとに包む方式だと必ずどこかが漏れる)。
func TestStatusNoticeIsSanitized(t *testing.T) {
	const esc, bel = "\x1b", "\a"
	v := newTestStatusView(t, statusRec("## master", "?? a.txt"))
	v.setNotice("evil"+esc+"]0;pwned"+bel+".txt の変更を捨てました", true)
	if hasTerminalControl(v.notice) {
		t.Errorf("通知に制御シーケンスが残った: %q", v.notice)
	}
}

// パスが幅を超えるときは basename を残す (末尾から切ると「どのファイルか」が分からない)。
func TestStatusPathTextKeepsBasename(t *testing.T) {
	row := worktreeRow{section: sectionUnstaged, path: "src/glogx/deeply/nested/dir/render.go"}
	got := stripANSI(statusPathText(row, 16, false))
	if dispWidth(got) > 16 {
		t.Fatalf("幅 = %d, want <= 16: %q", dispWidth(got), got)
	}
	if !strings.HasSuffix(got, "render.go") {
		t.Fatalf("basename が残っていない: %q", got)
	}
}

// --- X (変更を捨てる) ---

func TestStatusDiscardRejectsStagedRow(t *testing.T) {
	v := newTestStatusView(t, statusRec("M  a.go"))
	v.handleKey("X", testViewport())
	if v.discarding {
		t.Fatal("staged 行で X の確認が開いた (先に Space で unstage させる契約)")
	}
	if notice, ok := v.takeNotice(); ok || !strings.Contains(notice, "unstage") {
		t.Errorf("notice = %q (ok=%v), want unstage を促す案内", notice, ok)
	}
}

func TestStatusDiscardConfirmRunsRestore(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	calls := stubGitOps(t, nil)
	v.handleKey("X", testViewport())
	if !v.discarding {
		t.Fatal("X で確認モーダルが開いていない")
	}
	applyCmd(t, v, v.handleKey("y", testViewport()))
	if len(calls.restoreWork) != 1 || calls.restoreWork[0][0] != worktreePathspec("a.go") {
		t.Fatalf("git restore の呼び出し = %v, want [[a.go]]", calls.restoreWork)
	}
	if v.discarding {
		t.Error("実行後も確認モーダルが開いたまま")
	}
}

func TestStatusDiscardCancelKeepsFile(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	calls := stubGitOps(t, nil)
	v.handleKey("X", testViewport())
	v.handleKey("n", testViewport())
	if len(calls.restoreWork) != 0 {
		t.Fatalf("n でキャンセルしたのに restore が走った: %v", calls.restoreWork)
	}
	if v.discarding {
		t.Error("n の後も確認モーダルが開いたまま")
	}
}

func TestStatusDiscardUsesCleanForUntracked(t *testing.T) {
	v := newTestStatusView(t, statusRec("?? new.txt"))
	calls := stubGitOps(t, nil)
	v.handleKey("X", testViewport())
	applyCmd(t, v, v.handleKey("y", testViewport()))
	if len(calls.clean) != 1 || calls.clean[0][0] != worktreePathspec("new.txt") {
		t.Fatalf("git clean の呼び出し = %v, want [[new.txt]]", calls.clean)
	}
	if len(calls.restoreWork) != 0 {
		t.Errorf("untracked に restore が走った: %v", calls.restoreWork)
	}
}

// spec 4 節の不変条件 1: 確認に出した (パス, XY) と実行時の状態が違えば捨てない。
func TestStatusDiscardAbortsWhenStateChangedDuringConfirm(t *testing.T) {
	state := stubWorktreeStatus(t, statusRec(" M a.go"), nil)
	v := newStatusView()
	v.closeAnimOff = true
	v.shown = true
	applyStatusLoad(t, &v)
	calls := stubGitOps(t, nil)
	v.handleKey("X", testViewport())
	// 確認中に別プロセスが a.go を stage した (XY が " M" → "MM" へ変わる)
	state.out = statusRec("MM a.go")
	applyCmd(t, &v, v.handleKey("y", testViewport()))
	if len(calls.restoreWork) != 0 || len(calls.clean) != 0 {
		t.Fatalf("状態が変わったのに捨ててしまった: restore=%v clean=%v", calls.restoreWork, calls.clean)
	}
	notice, ok := v.takeNotice()
	if ok || !strings.Contains(notice, "変わった") {
		t.Errorf("notice = %q (ok=%v), want 中止したことを失敗として伝える", notice, ok)
	}
}

func TestStatusDiscardAbortsWhenFileDisappeared(t *testing.T) {
	state := stubWorktreeStatus(t, statusRec(" M a.go"), nil)
	v := newStatusView()
	v.closeAnimOff = true
	v.shown = true
	applyStatusLoad(t, &v)
	calls := stubGitOps(t, nil)
	v.handleKey("X", testViewport())
	state.out = statusRec() // 確認中に別プロセスが commit した
	applyCmd(t, &v, v.handleKey("y", testViewport()))
	if len(calls.restoreWork) != 0 {
		t.Fatalf("対象が消えたのに restore が走った: %v", calls.restoreWork)
	}
}

// --- 自動更新 ---

func TestStatusPollDropsStaleGeneration(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	gen := v.gen
	if cmd := v.receivePoll(statusPollMsg{gen: gen}); cmd == nil {
		t.Fatal("同世代の poll が読み直しを返さない")
	}
	v.close() // closeAnimOff なので即座に畳まれ、gen が進む
	if cmd := v.receivePoll(statusPollMsg{gen: gen}); cmd != nil {
		t.Error("閉じた後に古い世代の poll が生きている (次に開いた画面へ効く)")
	}
}

func TestStatusReceiveIgnoresStaleGeneration(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	stale := statusLoadMsg{st: parseWorktreeStatus(statusRec(" M zzz.go")), gen: v.gen - 1}
	v.receive(stale)
	if _, path := cursorPathAt(t, v); path != "a.go" {
		t.Fatalf("古い世代の結果で上書きされた: カーソル = %q", path)
	}
}

func TestStatusLoadErrorShowsMessageAndKeepsViewOpen(t *testing.T) {
	v := newStatusView()
	v.closeAnimOff = true
	v.shown = true
	stubWorktreeStatus(t, "", errors.New("fatal: not a git repository"))
	applyStatusLoad(t, &v)
	if !v.shown {
		t.Fatal("読み取り失敗で viewer が閉じた (画面は開いたまま案内を出す契約)")
	}
	body := strings.Join(v.lines(testStatusOpts(120, 20)), "\n")
	if !strings.Contains(body, "git status に失敗") {
		t.Fatalf("失敗の案内が出ていない:\n%s", body)
	}
}

// --- 描画 ---

func TestStatusLinesTwoColumnsWhenWide(t *testing.T) {
	// 🚨 ブランチ名は長いものを使う: 短いとヘッダーが一覧カラム幅 (48 桁ほど) に収まってしまい、
	// 「全幅で描く」ことの検証にならない (件数が切れる条件を作れない)
	v := newTestStatusView(t, statusRec("## feature/a-very-long-branch-name-for-header-width...origin/x", " M a.go"))
	lines := v.lines(testStatusOpts(120, 20))
	if len(lines) != 20 {
		t.Fatalf("行数 = %d, want 20 (常にちょうど page 行)", len(lines))
	}
	if !strings.Contains(lines[1], "│") {
		t.Fatalf("2 カラムの区切りが無い: %q", lines[1])
	}
	// ヘッダーは全幅で描く (一覧カラムの中に入れると件数が切り落とされる)
	head := stripANSI(lines[0])
	if strings.Contains(head, "│") {
		t.Fatalf("ヘッダーが 2 カラムに割られている: %q", head)
	}
	if !strings.Contains(head, "staged 0") {
		t.Fatalf("ヘッダーの件数が切れている: %q", head)
	}
}

func TestStatusLinesOneColumnWhenNarrow(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	lines := v.lines(testStatusOpts(70, 20))
	body := strings.Join(lines, "\n")
	if strings.Contains(body, "│") {
		t.Fatalf("狭い端末で 2 カラムになっている:\n%s", body)
	}
	if !strings.Contains(body, "a.go") {
		t.Fatalf("一覧が出ていない:\n%s", body)
	}
}

func TestStatusListWidthFallsBackToOneColumn(t *testing.T) {
	if w := statusListWidth(80); w != 80 {
		t.Errorf("statusListWidth(80) = %d, want 80 (1 カラム)", w)
	}
	if w := statusListWidth(120); w >= 120 || w < statusListMinWidth {
		t.Errorf("statusListWidth(120) = %d, want 一覧カラムの幅 (< 120)", w)
	}
	if w := statusListWidth(400); w > statusListMaxWidth {
		t.Errorf("statusListWidth(400) = %d, want <= %d (上限で止める)", w, statusListMaxWidth)
	}
}

func TestStatusLinesShowsSectionsAndPartialBadge(t *testing.T) {
	v := newTestStatusView(t, statusRec("MM both.go", "?? new.txt"))
	body := strings.Join(v.lines(testStatusOpts(120, 20)), "\n")
	for _, want := range []string{"Staged (1)", "Unstaged (1)", "Untracked (1)", "◐"} {
		if !strings.Contains(body, want) {
			t.Errorf("画面に %q が無い:\n%s", want, body)
		}
	}
}

func TestStatusLinesCleanTreeMessage(t *testing.T) {
	v := newTestStatusView(t, statusRec())
	body := strings.Join(v.lines(testStatusOpts(120, 20)), "\n")
	if !strings.Contains(body, "クリーン") {
		t.Fatalf("クリーンの案内が無い:\n%s", body)
	}
}

func TestStatusHeaderShowsBranchAndCounts(t *testing.T) {
	v := newTestStatusView(t, statusRec("## topic...origin/topic [ahead 2]", "M  a.go", " M b.go"))
	head := stripANSI(v.lines(testStatusOpts(120, 20))[0])
	for _, want := range []string{"topic", "ahead 2", "staged 1", "unstaged 1"} {
		if !strings.Contains(head, want) {
			t.Errorf("ヘッダーに %q が無い: %q", want, head)
		}
	}
}

// カーソル行の強調は一覧カラムの幅で完結する (プレビュー側まで背景が伸びない)。
func TestStatusCursorPaintStaysInListColumn(t *testing.T) {
	painted := statusCursorPaint("→ M a.go", 30, true)
	if w := dispWidth(stripANSI(painted)); w != 30 {
		t.Fatalf("カーソル行の表示幅 = %d, want 30 (カラム幅で止める)", w)
	}
	if !strings.HasSuffix(painted, ansiReset) {
		t.Errorf("行末で色をリセットしていない: %q", painted)
	}
}

// 板が左端から生えてくる演出: 途中では各行の左側だけが出ていて、全行が同じ幅 (板が 1 枚)。
func TestSlideLeftWindow(t *testing.T) {
	window := []string{"0123456789", "0123456789", "0123456789", "0123456789"}
	// colored=false: 動く右端のボーダーは ▒ (NO_COLOR の影と同じ語彙)
	got := slideLeftWindow(window, 0.5, 10, false, false)
	if len(got) != 4 {
		t.Fatalf("行数 = %d, want 4", len(got))
	}
	for i, ln := range got {
		// 右端 1 桁は動く端のボーダー (透明だと板の終端が見えない。ユーザー要望 2026-08-07)
		body, hasEdge := strings.CutSuffix(ln, "▒")
		if !hasEdge {
			t.Errorf("行 %d の右端にボーダーが無い: %q", i, ln)
		}
		if !strings.HasPrefix(window[i], body) {
			t.Errorf("行 %d が左端アンカーの prefix になっていない: %q", i, body)
		}
		if ln != got[0] {
			t.Errorf("行 %d の幅が先頭行と違う (板が 1 枚になっていない): %q vs %q", i, ln, got[0])
		}
	}
	// 開きは easeOutCubic: 折り返し地点で半分 (等速の 5 桁) より先へ進んでいる
	if dispWidth(got[0]) <= 5 {
		t.Errorf("終端減速が効いていない (0.5 時点で %q = %d 桁。等速なら 5 桁)", got[0], dispWidth(got[0]))
	}
	// 着地 (進捗 1) ではボーダーごと消えて元の行に戻る
	if full := slideLeftWindow(window, 1, 10, false, false); full[0] != "0123456789" || full[3] != "0123456789" {
		t.Errorf("進捗 1 で変形している: %q", full)
	}
	// 進捗 0 は中身 0 桁 + 左端のボーダーだけ (動き出しから端が見える)
	if none := slideLeftWindow(window, 0, 10, false, false); none[0] != "▒" || none[3] != "▒" {
		t.Errorf("進捗 0 がボーダー 1 桁になっていない: %q", none)
	}
	// 閉じは全行同時・等速 (rowOffsetRatio の closing 分岐)。5 桁 = 中身 4 + ボーダー 1
	closing := slideLeftWindow(window, 0.5, 10, true, false)
	if closing[0] != closing[3] {
		t.Errorf("閉じで行ごとに差が出ている: %q vs %q", closing[0], closing[3])
	}
	if closing[0] != "0123▒" {
		t.Errorf("閉じ 0.5 の残り = %q, want %q", closing[0], "0123▒")
	}
	// 全角の境界をまたぐ切りは文字を割らず幅内に収める (中身 4 桁に 3 文字目の「う」は入らない)
	jp := slideLeftWindow([]string{"あいうえお"}, 0.5, 10, true, false)
	if jp[0] != "あい▒" {
		t.Errorf("全角境界の切り = %q, want %q", jp[0], "あい▒")
	}
	// 空行にもボーダーを立てる (行ごとに欠けると縦 1 本の端に見えない)
	blank := slideLeftWindow([]string{"", "0123456789"}, 0.5, 10, true, false)
	if dispWidth(blank[0]) != dispWidth(blank[1]) || !strings.HasSuffix(blank[0], "▒") {
		t.Errorf("空行のボーダーが欠けている: %q vs %q", blank[0], blank[1])
	}
	// colored=true は近黒の █ (ドロップシャドウと同じ色)
	col := slideLeftWindow(window, 0.5, 10, true, true)
	if !strings.HasSuffix(col[0], ansiShadowFg+"█"+ansiReset) {
		t.Errorf("colored のボーダーが近黒 █ になっていない: %q", col[0])
	}
}

func TestStatusOpenAnimationThenSettles(t *testing.T) {
	advance := stubClock(t)
	v := newStatusView()
	v.shown = true
	v.animStart = timeNow()
	// 経過 0 だけでなく「開いて僅かに経った」正の経過でも見る (0 は境界の退化ケースで、
	// 「経過 > 0 で即 1 に跳ぶ」型のミスを素通りさせる)
	advance(time.Millisecond)
	if !v.animating() {
		t.Fatal("開いた直後に animating() = false")
	}
	if p := v.animProgress(); p <= 0 || p >= 1 {
		t.Fatalf("開いた直後の進捗 = %v, want 0 < p < 1", p)
	}
	advance(statusOpenDuration)
	if v.animating() {
		t.Error("所要を過ぎても animating() = true")
	}
	v.settleAnim()
	if !v.animStart.IsZero() {
		t.Error("settleAnim が時計を捨てていない")
	}
}

// --- 全画面 diff (d) ---

func TestStatusPagerOpensAndScrolls(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	row, _ := v.current()
	key := previewKey(row)
	v.preview.store(key, []string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9", "l10",
		"l11", "l12", "l13", "l14", "l15", "l16", "l17", "l18", "l19", "l20"}, key)
	v.handleKey("d", testViewport())
	if v.pagerKey != key {
		t.Fatalf("pagerKey = %q, want %q", v.pagerKey, key)
	}
	v.handleKey("j", testViewport())
	if v.pagerOffset != 1 {
		t.Fatalf("j 後の offset = %d, want 1", v.pagerOffset)
	}
	v.handleKey("d", testViewport()) // toggle で閉じる
	if v.pagerKey != "" {
		t.Fatalf("d で閉じていない: %q", v.pagerKey)
	}
}

func TestStatusPagerKeysDoNotMoveList(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go", " M b.go"))
	row, _ := v.current()
	v.preview.store(previewKey(row), []string{"x"}, previewKey(row))
	v.handleKey("d", testViewport())
	v.handleKey("j", testViewport())
	if _, path := cursorPathAt(t, v); path != "a.go" {
		t.Fatalf("pager 中の j で一覧のカーソルが動いた: %q", path)
	}
}

func TestStatusDiscardConfirmSwallowsListKeys(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go", " M b.go"))
	stubGitOps(t, nil)
	v.handleKey("X", testViewport())
	v.handleKey("j", testViewport()) // 確認中の j は「キャンセル」で、カーソルは動かない
	if _, path := cursorPathAt(t, v); path != "a.go" {
		t.Fatalf("確認中の j で一覧のカーソルが動いた: %q (確認に出した行と食い違う)", path)
	}
}

// --- browseModel との統合 ---

func TestStatusViewerKeyTogglesFromList(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	stubWorktreeStatus(t, statusRec(" M a.go"), nil)
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if !m.statusOv.visible() {
		t.Fatal("s で status viewer が開かない")
	}
	releaseKey(m)
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if m.statusOv.visible() {
		t.Fatal("s の 2 回目で閉じない (toggle でない)")
	}
}

func TestStatusViewerNotOpenedWhileJobPanelOpen(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	withJobs(m, 0)
	m.panelSHA = m.commits[0].SHA
	stubWorktreeStatus(t, statusRec(" M a.go"), nil)
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if m.statusOv.visible() {
		t.Fatal("job パネル表示中に s で status viewer が開いた (パネルの語彙を優先する契約)")
	}
}

// u は viewer 内では pull を開かず「pull は p」と案内する (spec 3 節。押しても無言にしない)。
// 🚨 「確認が開かない」だけでは不十分: ガードを外しても viewer のキーに無い u は素通りして
// 何も起きないため、トーストが出ることまで固定する (ミューテーション検証 2026-08-03)。
func TestStatusViewerUKeyGuidesToP(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	m.statusOv.shown = true
	m.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	if m.actModal.pullConfirm {
		t.Fatal("u が status viewer 中に pull 確認を開いた (pull は p の契約)")
	}
	if !m.statusOv.visible() {
		t.Fatal("u で viewer が閉じた")
	}
	if !m.toast.visible() || !strings.Contains(m.toast.text, "p") {
		t.Fatalf("u の案内が出ていない: %q", m.toast.text)
	}
}

// p で pull --rebase の確認を開く (ユーザー要望 2026-08-05)。
//
// 🚨 spec 3 節は「staging の途中から remote 操作へ滑る導線を作らない」として b/u を遮断して
// いた。p を通すのはその判断の一部を覆すもので、キーを一覧の u と分けているのが折衷点:
// 誤爆しやすい隣接キーではなく、明示的に p を押したときだけ remote に触る。
func TestStatusViewerPullKeyOpensConfirm(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	m.statusOv.shown = true
	m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if !m.actModal.pullConfirm {
		t.Fatal("p で pull --rebase の確認が開かない")
	}
	if !m.statusOv.visible() {
		t.Error("p で viewer が閉じた (確認は viewer の上に重ねる)")
	}
	// 🚨 確認モーダルが viewer の上に描かれること。キーは viewer より先に actModal が捌くので、
	// 描かれないと「見えないモーダルが y/N を持つ」= 画面の指示と行き先が食い違う。
	out := stripANSI(m.View().Content)
	if !strings.Contains(out, "pull") {
		t.Fatalf("pull 確認が viewer の上に出ていない:\n%s", out)
	}
	// y は viewer でなく確認モーダルへ届く (actModal が先に捌く契約)
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if m.actModal.pullConfirm {
		t.Error("y が確認モーダルに届いていない")
	}
}

// 🚨 回帰防止 (perf): 整形するのは可視の窓の分だけ。全行を整形してから切ると、画面に出る
// 行数と無関係に変更ファイル数へ比例したコストになる (実測 40 件 103µs / 2000 件 1.65ms)。
//
// 🚨 出力では検出できない: 全行を整形しても「返す」のは窓の分だけなので、返り値は同じになる
// (最初この形で書いて変異が green のまま通った)。違うのは行った仕事量なので、alloc 数が
// 件数に比例しないことを見る。時間は CI のノイズで flake するため、そちらの番人は
// tests/glogx/bench_budgets.ci の status_view_2000 に置く。
func TestStatusListFormatsOnlyVisibleWindow(t *testing.T) {
	listAllocs := func(files int) float64 {
		recs := []string{"## master"}
		for i := range files {
			recs = append(recs, " M file"+strconv.Itoa(i)+".go")
		}
		v := newTestStatusView(t, statusRec(recs...))
		o := testStatusOpts(120, 10) // 10 行ぶんの窓
		if out := strings.Join(v.listLines(o, 120), "\n"); !strings.Contains(out, "file0.go") {
			t.Fatalf("窓の先頭が出ていない (前提が崩れた):\n%s", out)
		}
		return testing.AllocsPerRun(20, func() { v.listLines(o, 120) })
	}
	small, large := listAllocs(20), listAllocs(2000)
	// 件数が 100 倍でも alloc は数倍に収まること (行数え上げの index だけが伸びる)。
	// 全行整形だと 1 行 1 文字列で線形に増えるので、この上限を軽く超える。
	if large > small*3 {
		t.Errorf("alloc が件数に比例している (窓の外まで整形している): 20 件 %.0f / 2000 件 %.0f",
			small, large)
	}
}

// pull の成功で status viewer をその場で読み直す。自動更新は 1.5 秒周期なので、待たせると
// ヘッダーの ahead/behind が古いまま残り「p が効いていない」ように見える。
func TestPullReloadsOpenStatusViewer(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	stubWorktreeStatus(t, statusRec("## master...origin/master [behind 1]", " M a.go"), nil)
	m.statusOv.shown = true
	m.Update(pullMsg{})
	if !m.statusOv.loading {
		t.Error("pull 後に status viewer を読み直していない (1.5 秒間ヘッダーが古いまま残る)")
	}
}

// b で push の確認を開く (ユーザー要望 2026-08-07。pull の p と同じく確認は viewer の上に重ねる)。
func TestStatusViewerPushKeyOpensConfirm(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.statuses[m.commits[0].SHA] = StateUnpushed
	m.statusOv.shown = true
	m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if !m.actModal.pushConfirm {
		t.Fatal("b で push の確認が開かない")
	}
	if !m.statusOv.visible() {
		t.Error("b で viewer が閉じた (確認は viewer の上に重ねる)")
	}
	out := stripANSI(m.View().Content)
	if !strings.Contains(out, "push") {
		t.Fatalf("push 確認が viewer の上に出ていない:\n%s", out)
	}
	// y は viewer でなく確認モーダルへ届く (actModal が先に捌く契約)
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if m.actModal.pushConfirm {
		t.Error("y が確認モーダルに届いていない")
	}
}

// 未 push が無いときの b は確認を開かず、理由をトーストで返す (一覧側と同じ no-op 通知)。
func TestStatusViewerPushKeyNoUnpushedShowsToast(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	m.statusOv.shown = true
	m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if m.actModal.pushConfirm {
		t.Fatal("未 push なしで push 確認が開いた")
	}
	if !m.toast.visible() {
		t.Fatal("未 push なしの理由がトーストで出ていない")
	}
}

// push の成功で status viewer をその場で読み直し、push 演出は出さない (viewer が全画面で
// コミット一覧が見えないため。ヘッダーの ahead を即消すのは pull と同じ理由)。
func TestPushReloadsOpenStatusViewerWithoutAnim(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.statuses[m.commits[0].SHA] = StateUnpushed
	stubWorktreeStatus(t, statusRec("## master...origin/master [ahead 1]", " M a.go"), nil)
	m.statusOv.shown = true
	m.Update(pushMsg{})
	if !m.statusOv.loading {
		t.Error("push 後に status viewer を読み直していない (ヘッダーの ahead が古いまま残る)")
	}
	if m.pushAnimating {
		t.Error("viewer 表示中に push 演出が始まった (画面に無い一覧を演出しても再取得が遅れるだけ)")
	}
}

func TestStatusViewerRendersFullScreenAndHint(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	stubWorktreeStatus(t, statusRec(" M a.go"), nil)
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	deliverStatus(t, &m.statusOv, statusRec(" M a.go"))
	view := m.View().Content
	if !strings.Contains(view, "a.go") {
		t.Fatalf("status viewer の内容が描かれていない:\n%s", view)
	}
	if strings.Contains(view, "subject") {
		t.Fatalf("全画面のはずが commit 一覧が残っている:\n%s", view)
	}
	if !strings.Contains(view, "stage/unstage") {
		t.Fatalf("hint 行が status viewer のものになっていない:\n%s", view)
	}
	// remote 操作キー (b/p) は hint に出す (発見性。ユーザー要望 2026-08-07)。
	// 🚨 view でなく hint() を直接、しかも**広い幅**で見る: hint は幅に応じて優先度の低い項目を
	// 落とすので (issue 155)、テストの 80 桁では b/p は出ない (出るのは「今の幅で確実に読める
	// もの」だけ。全キーの正本は --help / README)
	hint := m.statusOv.hint(200)
	if !strings.Contains(hint, "b: push") || !strings.Contains(hint, "p: pull") {
		t.Fatalf("hint に remote 操作キー (b: push / p: pull) が出ていない: %q", hint)
	}
}

func TestStatusViewerNoticeBecomesToast(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	stubWorktreeStatus(t, statusRec("M  a.go"), nil)
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	deliverStatus(t, &m.statusOv, statusRec("M  a.go"))
	m.Update(tea.KeyPressMsg{Code: 'X', Text: "X", Mod: tea.ModShift})
	if !m.toast.visible() && m.lastWarning == "" {
		t.Fatal("viewer の notice がトースト/警告に出ていない")
	}
}

// --- 頑健性 (壊しにいく観点) ---

// 極端な端末サイズ (resize 直後・1 桁 pane) でも panic せず、契約どおり page 行を返すこと。
func TestStatusLinesSurvivesExtremeSizes(t *testing.T) {
	v := newTestStatusView(t, statusRec("M  s.go", " M src/very/deep/path/render.go", "?? tmp/"))
	for _, size := range [][2]int{{0, 0}, {1, 1}, {2, 3}, {20, 3}, {70, 2}, {200, 60}} {
		width, page := size[0], size[1]
		lines := v.lines(testStatusOpts(width, page))
		if len(lines) != page {
			t.Fatalf("%dx%d の行数 = %d, want %d", width, page, len(lines), page)
		}
	}
	// pager を開いた状態でも同じ (枠の描画が幅 0 を踏む経路)
	v.handleKey("d", testViewport())
	for _, size := range [][2]int{{0, 0}, {1, 1}, {20, 6}} {
		if lines := v.lines(testStatusOpts(size[0], size[1])); len(lines) != size[1] {
			t.Fatalf("pager 表示中 %dx%d の行数 = %d, want %d", size[0], size[1], len(lines), size[1])
		}
	}
}

// プレビューのキャッシュは無制限に育たない (長時間セッションでファイルを見た数だけ溜まる)。
func TestStatusPreviewCacheIsBounded(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	for i := range lineCacheLimit + 20 {
		v.receivePreview(statusPreviewMsg{key: "k" + strconv.Itoa(i), lines: []string{"x"}})
	}
	if len(v.preview.entries) > lineCacheLimit+1 { // +1 = 表示中のキー (keep) の分
		t.Fatalf("キャッシュ数 = %d, want <= %d", len(v.preview.entries), lineCacheLimit+1)
	}
	if len(v.preview.order) != len(v.preview.entries) {
		t.Fatalf("order = %d, cache = %d (捨て漏れ)", len(v.preview.order), len(v.preview.entries))
	}
}

// 外部編集で内容が変わったら、プレビューは捨てたうえで取り直しを予約する
// (捨てるだけだとカーソルを動かすまでプレビュー欄が空のまま = 眺める用途で死ぬ)。
// 外部編集で古い diff を捨てても、走行中の取得は二重発行にならない。
//
// 🚨 回帰防止 (セルフレビュー 2026-08-05): キャッシュを lineCache へ集約したとき、この経路を
// reset() にして走行中の札まで降ろしてしまった。この経路は直後に取り直しを予約する
// (previewTickCmd) ので、札が無いと同じキーへ git diff が 2 本走る。移行前は cache と order
// だけを捨てていたので、移行で入れた挙動変化だった。
func TestStatusExternalEditKeepsInFlightPreview(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	row, _ := v.current()
	key := previewKey(row)
	if cmd := v.fetchDiff(row, false); cmd == nil {
		t.Fatal("最初の fetchDiff が nil (前提が崩れた)")
	}

	// 外部編集で行集合が変わった = 古い diff を捨てる経路 (changed が true になる入力にする)
	v.receive(statusLoadMsg{st: parseWorktreeStatus(statusRec(" M a.go", " M b.go")), gen: v.gen})
	if v.preview.has(key) {
		t.Fatal("前提が崩れた: 古い diff が捨てられていない (この経路を通っていない)")
	}

	if !v.preview.loading(key) {
		t.Fatal("走行中の取得の札が降りた (直後の取り直し予約と二重に git diff が走る)")
	}
	if cmd := v.fetchDiff(row, false); cmd != nil {
		t.Error("走行中なのに 2 本目の fetchDiff が発行された")
	}
}

// applyFresh (X の実行時検証で「変わっていた」と分かった直後の反映) も同じ契約。
// 走行中の取得の札を降ろさない — プレビューの tick チェーンは張られたままなので、降ろすと
// 次の満了で同じキーを二重に取りに行く。
func TestStatusApplyFreshKeepsInFlightPreview(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	row, _ := v.current()
	key := previewKey(row)
	if cmd := v.fetchDiff(row, false); cmd == nil {
		t.Fatal("最初の fetchDiff が nil (前提が崩れた)")
	}
	v.preview.store("other", []string{"stale"}, "")

	v.applyFresh(parseWorktreeStatus(statusRec(" M a.go", " M b.go")))

	if v.preview.has("other") {
		t.Error("古いキャッシュが残った (取り直しの契機が無いまま古い diff を出す)")
	}
	if !v.preview.loading(key) {
		t.Fatal("走行中の取得の札が降りた (次の tick 満了で二重に git diff が走る)")
	}
}

func TestStatusReceiveSchedulesPreviewRefetchOnChange(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	row, _ := v.current()
	v.receivePreview(statusPreviewMsg{key: previewKey(row), lines: []string{"old"}})
	same := v.receive(statusLoadMsg{st: parseWorktreeStatus(statusRec(" M a.go")), gen: v.gen})
	if same != nil {
		t.Error("内容が変わっていないのに取り直しを予約した (毎 1.5 秒 git diff が走る)")
	}
	if len(v.preview.entries) == 0 {
		t.Error("内容が変わっていないのにキャッシュを捨てた")
	}
	changed := v.receive(statusLoadMsg{st: parseWorktreeStatus(statusRec("MM a.go")), gen: v.gen})
	if changed == nil {
		t.Fatal("内容が変わったのに取り直しを予約していない")
	}
	if len(v.preview.entries) != 0 {
		t.Errorf("古いプレビューが残っている: %v", v.preview)
	}
}

// untracked のプレビューはファイル全体を読まない (巨大な生成物を掴まない)。
func TestUntrackedPreviewReadsOnlyHead(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/huge.bin"
	// 改行を含まない巨大ファイル: 行数上限では止まらないので、バイト上限が効いているかを見る
	if err := os.WriteFile(path, []byte(strings.Repeat("a", statusPreviewMaxBytes*3)), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := untrackedPreview(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("行数 = %d, want 1", len(lines))
	}
	if len(lines[0]) > statusPreviewMaxBytes {
		t.Fatalf("読んだ長さ = %d bytes, want <= %d (全体を読んでいる)", len(lines[0]), statusPreviewMaxBytes)
	}
}

// untracked の symlink はリンク先を読まない。読むと、カーソルを合わせただけで
// リンク先 (~/.ssh/id_rsa 等) の中身がプレビューに出る。
func TestUntrackedPreviewDoesNotFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	secret := dir + "/secret.txt"
	if err := os.WriteFile(secret, []byte("SECRET-CONTENT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := dir + "/innocent.txt"
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("この環境では symlink を作れない: %v", err)
	}
	lines, err := untrackedPreview(link, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(lines, "\n"), "SECRET-CONTENT") {
		t.Fatalf("symlink 先の中身がプレビューに出た: %v", lines)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "リンク") {
		t.Fatalf("lines = %v, want シンボリックリンクの案内 1 行", lines)
	}
}

func TestUntrackedPreviewDirDoesNotListContents(t *testing.T) {
	lines, err := untrackedPreview("tmp/", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "ディレクトリ") {
		t.Fatalf("lines = %v, want ディレクトリの案内 1 行", lines)
	}
}

// git へ渡すパスは repo root 相対 + glob 無効の pathspec であること。
//
// glogx は tmux popup から任意のサブディレクトリで起動される (`-d '#{pane_current_path}'`) が、
// git status のパスは repo root 相対。素のパスを渡すと cwd 相対に解釈され、実測 (2026-08-03) では
// diff が常に空・add が "did not match any files"・clean が無言で何もしない状態になった。
func TestStatusOpsUseTopLevelLiteralPathspec(t *testing.T) {
	if got := worktreePathspec("src/a.go"); got != ":(top,literal)src/a.go" {
		t.Fatalf("worktreePathspec = %q", got)
	}
	// * や ? を含むファイル名を glob として解釈させない (literal)
	row := worktreeRow{section: sectionUnstaged, path: "tmp/a?b.txt"}
	specs := row.pathspecs()
	if len(specs) != 1 || !strings.HasPrefix(specs[0], ":(top,literal)") {
		t.Fatalf("pathspecs = %v", specs)
	}
	// rename は先と元の両方を pathspec 化する
	renamed := worktreeRow{section: sectionStaged, path: "new.go", orig: "old.go"}
	got := renamed.pathspecs()
	if len(got) != 2 || got[0] != worktreePathspec("new.go") || got[1] != worktreePathspec("old.go") {
		t.Fatalf("rename の pathspecs = %v", got)
	}
}

// プレビュー欄は狭いので diff のファイルヘッダーを飛ばして最初の hunk から見せる
// (全画面 diff では飛ばさない: 全文を読む場なので mode 変更等も情報)。
func TestStatusPreviewSkipsDiffHeader(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	row, _ := v.current()
	full := []string{
		"diff --git a/a.go b/a.go", "index 111..222 100644", "--- a/a.go", "+++ b/a.go",
		"@@ -1,3 +1,3 @@", "-old", "+new",
	}
	v.receivePreview(statusPreviewMsg{key: previewKey(row), lines: full})
	pane := strings.Join(v.previewPane(testStatusOpts(60, 12), 60), "\n")
	if strings.Contains(pane, "diff --git") || strings.Contains(pane, "index 111") {
		t.Fatalf("プレビューに diff ヘッダーが残っている:\n%s", pane)
	}
	if !strings.Contains(pane, "@@ -1,3 +1,3 @@") || !strings.Contains(pane, "+new") {
		t.Fatalf("プレビューに hunk が出ていない:\n%s", pane)
	}
	// 全画面 diff (d) は全文を出す
	v.handleKey("d", testViewport())
	box := strings.Join(v.pagerBox(testStatusOpts(80, 20)), "\n")
	if !strings.Contains(box, "diff --git") {
		t.Fatalf("全画面 diff でヘッダーまで飛ばしている:\n%s", box)
	}
}

// @@ を持たない diff (mode 変更だけ・binary) は飛ばさない (飛ばすと空になる)。
func TestPreviewSkipDiffHeaderKeepsHunklessDiff(t *testing.T) {
	lines := []string{"diff --git a/x b/x", "old mode 100644", "new mode 100755"}
	if got := previewSkipDiffHeader(lines); got != 0 {
		t.Fatalf("previewSkipDiffHeader = %d, want 0", got)
	}
}

// 解釈できない出力を「クリーン」と同じ絵にしない (沈黙を成功にしない)。
func TestStatusUnparsableOutputIsNotReportedAsClean(t *testing.T) {
	v := newTestStatusView(t, statusRec("garbage-line", "another-garbage"))
	body := stripANSI(strings.Join(v.lines(testStatusOpts(70, 20)), "\n"))
	if strings.Contains(body, "クリーン") {
		t.Fatalf("解釈できなかったのに「クリーン」と表示した:\n%s", body)
	}
	if !strings.Contains(body, "解釈できませんでした") {
		t.Fatalf("解釈できなかったことが画面に出ていない:\n%s", body)
	}
	// 一部だけ読めたときは一覧を出しつつヘッダーで取りこぼしを知らせる
	v2 := newTestStatusView(t, statusRec(" M a.go", "garbage"))
	head := stripANSI(v2.lines(testStatusOpts(120, 20))[0])
	if !strings.Contains(head, "解釈不能") {
		t.Fatalf("取りこぼしがヘッダーに出ていない: %q", head)
	}
}

// -z の末尾 NUL が生む空要素は「解釈できなかった」に数えない (常に警告が出てしまう)。
func TestStatusTrailingNulIsNotCountedAsSkipped(t *testing.T) {
	st := parseWorktreeStatus(statusRec(" M a.go"))
	if st.skipped != 0 {
		t.Fatalf("skipped = %d, want 0 (末尾の空要素を数えている)", st.skipped)
	}
}

// 全行が幅に収まる不変条件を width 1 から全数で掃く (issues 側の
// TestIssuesViewLinesAlwaysExactlyPageRows と対。issue 053: 掃き始めが広い幅だけだと、
// 極小幅でしか出ない枠越え (clipToWidth の width<=0 素通し・scrollbarColumn の床上げ) が
// 眠る)。
func TestStatusViewLinesFitWidthDownToOne(t *testing.T) {
	recs := make([]string, 0, 30)
	for i := range 30 {
		p := "src/deeply/nested/module" + strconv.Itoa(i) + "/handler.go"
		if i%2 == 0 {
			recs = append(recs, "M  "+p)
		} else {
			recs = append(recs, " M "+p)
		}
	}
	v := newTestStatusView(t, statusRec(recs...))
	for width := 1; width <= 80; width++ {
		for _, page := range []int{3, 12} {
			for _, ln := range v.lines(testStatusOpts(width, page)) {
				if w := dispWidth(ln); w > width {
					t.Fatalf("width=%d page=%d の行が幅を超えた (w=%d): %q", width, page, w, ln)
				}
			}
		}
	}
}

// 明示的な再読込 (r) はプレビューのキャッシュも捨てる (issue 114)。
//
// キー (section+XY+path) は内容を一意に決めないので、同じ ` M` のまま中身だけ変わる
// 「保存し直し」では receive の changed 判定を通り抜ける。自動更新での据え置きは意図的だが、
// r まで据え置くと「再読込したのに編集前の diff が出続ける」になる (silent)。
func TestStatusReloadKeyDropsPreviewCache(t *testing.T) {
	// 🚨 2 行以上の fixture にすること。1 行だと `len(entries) != 0` では
	//   「全部捨てる」と「カーソル行だけ捨てる」を区別できない (敵対的レビューで実測)
	stubWorktreeStatus(t, statusRec(" M a.go", " M b.go"), nil)
	v := newStatusView()
	v.closeAnimOff = true
	v.shown = true
	applyStatusLoad(t, &v)
	for _, r := range v.rows {
		v.preview.store(previewKey(r), []string{"OLD-DIFF"}, "")
	}
	if len(v.preview.entries) < 2 {
		t.Fatalf("前提が崩れた: キャッシュに 2 件仕込めていない (%d 件)", len(v.preview.entries))
	}
	// 🚨 予約と再読込は別々に見る。`cmd != nil` では判別力が無い — loadCmd だけでも
	//   previewTickCmd だけでも非 nil が返るので、どちらを落とす変異も green のまま通る (実測)
	// 🚨 再読込の有無は v.loading で見る。loadCmd は呼ばれた時点で同期に loading を立てるので、
	//   tea.Batch を実行しなくても判る (Batch を実行しても子は走らない = 数えられない)
	seqBefore := v.previewSeq
	v.loading = false

	v.listKey("r", testViewport())

	if len(v.preview.entries) != 0 {
		t.Errorf("r でプレビューのキャッシュが残った (編集前の diff が出続ける): %v", v.preview.entries)
	}
	// 🚨 捨てるだけだとプレビュー欄が空のまま残る (receive 側と同じ理由で取り直しを予約する)
	if v.previewSeq == seqBefore {
		t.Error("r で取り直しが予約されていない (プレビュー欄が空のまま戻らない)")
	}
	if !v.loading {
		t.Error("r が git status を読み直していない (再読込そのものが走らない)")
	}
}

// r と閉じで **reset() でなく clearEntries()** を使うこと (issue 114)。
//
// reset() は取得中の札まで降ろすので、走行中の取得と、直後に張り直した予約が
// **同じキーを二重に取りに行く** (lineCache.clearEntries の doc が名指しで禁じている形)。
func TestStatusReloadKeepsInFlightMark(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	row, _ := v.current()
	key := previewKey(row)
	if !v.preview.begin(key) {
		t.Fatal("前提が崩れた: 取得を始められない")
	}

	v.listKey("r", testViewport())

	if !v.preview.busy[key] {
		t.Error("r が取得中の札まで降ろした (走行中の取得と予約が同じキーを二重に取りに行く)")
	}
}

// 無効化 (閉じ) を跨いで着地した取得が、古い内容を復活させない (issue 114 の敵対的レビュー)。
//
// statusPreviewMsg は 4 兄弟で唯一 gen を持っておらず、閉じた瞬間に飛んでいた取得が
// clearEntries の**後**に着地してキャッシュを復活させていた。復活すると begin() に弾かれ、
// 開き直しても取り直しが一度も走らない = 編集前の diff が永久に出続ける。
func TestStatusLatePreviewAfterCloseIsDropped(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	row, _ := v.current()
	key := previewKey(row)
	if !v.preview.begin(key) {
		t.Fatal("前提が崩れた: 取得を始められない")
	}
	staleGen := v.gen

	v.closing = true
	if !v.finishClose() {
		t.Fatal("前提が崩れた: finishClose が閉じ切っていない")
	}
	v.receivePreview(statusPreviewMsg{gen: staleGen, key: key, lines: []string{"OLD-DIFF"}})

	if len(v.preview.entries) != 0 {
		t.Errorf("閉じた後に着地した取得がキャッシュを復活させた: %v", v.preview.entries)
	}
	// 🚨 捨てるときも札は降ろすこと。残すと begin() がこのキーを永久に弾く
	if v.preview.busy[key] {
		t.Error("捨てた結果の取得中の札が残った (取り直しが二度と走らない)")
	}
}

// r でも同じ (r は gen を進めるので、飛んでいた取得は世代違いで捨てられる)。
func TestStatusLatePreviewAfterReloadIsDropped(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	row, _ := v.current()
	key := previewKey(row)
	if !v.preview.begin(key) {
		t.Fatal("前提が崩れた: 取得を始められない")
	}
	staleGen := v.gen

	v.listKey("r", testViewport())
	v.receivePreview(statusPreviewMsg{gen: staleGen, key: key, lines: []string{"OLD-DIFF"}})

	if len(v.preview.entries) != 0 {
		t.Errorf("r の後に着地した取得が古い内容を復活させた (押しても何も変わらない): %v", v.preview.entries)
	}
	// 🚨 札はここで降ろすこと。r は clearEntries なので札が残っており (それは意図的)、
	//   捨てた結果の札を降ろさないと begin() がこのキーを永久に弾いて取り直しが走らない。
	//   閉じる経路は clearBusy が先に走るので、この主張はこちらでしか立たない
	if v.preview.busy[key] {
		t.Error("捨てた結果の取得中の札が残った (取り直しが二度と走らない)")
	}
}

// viewer を閉じるとプレビューのキャッシュを捨てる (issue 114)。
//
// 閉じているあいだに外部編集されても XY が動かなければキーが一致するので、捨てないと
// **開き直しても編集前の diff が出る**。しかも finishClose は clearBusy しか呼んでおらず、
// プロセスが生きている限り消えなかった。
func TestStatusCloseDropsPreviewCache(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	row, _ := v.current()
	v.receivePreview(statusPreviewMsg{key: previewKey(row), lines: []string{"OLD-DIFF"}})
	if len(v.preview.entries) == 0 {
		t.Fatal("前提が崩れた: キャッシュに仕込めていない")
	}

	// 別の行の取得が飛んでいる状態も作る (閉じた瞬間に in-flight があるのが実際の形)
	if !v.preview.begin("in-flight-key") {
		t.Fatal("前提が崩れた: 取得を始められない")
	}

	v.closing = true
	if !v.finishClose() {
		t.Fatal("前提が崩れた: finishClose が閉じ切っていない")
	}

	if len(v.preview.entries) != 0 {
		t.Errorf("閉じてもプレビューのキャッシュが残った (開き直しで編集前の diff が出る): %v", v.preview.entries)
	}
	// 🚨 隣の clearBusy も守る。clearEntries を隣に足したことで「2 行まとめて reset() で
	//   よくない?」という編集が誘発されやすくなったが、**片方だけ消す編集は無音で通る**。
	//   札が残ると fetching() が true のままフレーム tick を回し続ける (元の 🚨 コメント)
	if len(v.preview.busy) != 0 {
		t.Errorf("閉じても取得中の札が残った (フレーム tick が回り続ける): %v", v.preview.busy)
	}
}

// hint の語が動作と一致すること (issue 121)。
//
// 一覧モードの q は **glogx ごと終了**する (ユーザー要望 2026-08-06。git log へは戻らない) のに、
// hint は長らく「q: 閉じる」と出していた。README は「i/s で閉じて一覧へ戻る」「q/Esc は glogx ごと
// 終了」と 2 語を使い分けており、画面上の案内だけが古い語のまま残っていた。
//
// 🚨 全画面 pager の「d/q: 閉じる」は**正しい** (そこでの q は pager を閉じる)。両方を pin して
// 取り違えを防ぐ — 片方だけ見ていると「まとめて閉じるに戻す」変更が通ってしまう。
func TestStatusHintWordsMatchBehavior(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))

	list := v.hint(testPopupWidth)
	if !strings.Contains(list, "q: 終了") {
		t.Errorf("一覧の hint が「終了」と案内していない (q は glogx ごと終了する): %q", list)
	}
	if strings.Contains(list, "q: 閉じる") {
		t.Errorf("一覧の hint が「閉じる」と案内している (押すと glogx が終わる): %q", list)
	}
	if !strings.Contains(list, "s: 一覧へ") {
		t.Errorf("一覧の hint に戻り方 (s) が出ていない: %q", list)
	}

	// pager 表示中は「閉じる」で正しい
	v.pagerKey = "dummy"
	pager := v.hint(testPopupWidth)
	if !strings.Contains(pager, "d/q: 閉じる") {
		t.Errorf("pager の hint が「閉じる」でない (そこでの q は pager を閉じる): %q", pager)
	}
}

// R は ratelimit ダッシュボードへ横断する (i と対。ユーザー要望 2026-09-01)。viewer は閉じ、
// 実際に開くのは browseModel (takeWantRatelimit)。
// 🚨 hint の案内は**広い端末でだけ**出る (issue 155 で幅に応じて落とすようにしたため。popup の
// 実幅では優先度の低い R は落ちる)。狭い幅での正本は --help / README。
func TestStatusRSwitchesToRatelimitDash(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))

	v.handleKey("R", statusViewport{width: testPopupWidth, page: 20})

	if !v.takeWantRatelimit() {
		t.Error("R でダッシュボードへの横断を要求しない")
	}
	if v.takeWantRatelimit() {
		t.Error("takeWantRatelimit が 2 回 true を返す (横断が二重に起きる)")
	}
	if !v.closing && v.shown {
		t.Error("R で viewer が閉じない (全画面は同時に 1 枚)")
	}
	if !strings.Contains(v.hint(200), "R: 残量") {
		t.Errorf("hint が R を案内していない: %q", v.hint(200))
	}
}

// hint は与えられた幅に必ず収まる。🚨 issues viewer と同じ検査を status にも置く (issue 155:
// 片方だけ無検査だったので、キーを足すたびに末尾が黙って削られていた)。
//
// 🚨 幅を 1 点だけ見ない。popup の実幅 (84) は代表値でしかなく、端末は任意の幅を取る。
func TestStatusViewHintFitsWidth(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	for w := 10; w <= 200; w++ {
		for _, mode := range []struct {
			name  string
			setup func()
		}{
			{"一覧", func() { v.pagerKey, v.discarding = "", false }},
			{"pager", func() { v.pagerKey, v.discarding = "dummy", false }},
			{"破棄確認", func() { v.pagerKey, v.discarding = "", true }},
		} {
			mode.setup()
			if got := dispWidth(v.hint(w)); got > w && mode.name == "一覧" {
				t.Errorf("w=%d %s: hint が %d 桁 (収まっていない): %q", w, mode.name, got, v.hint(w))
			}
		}
	}
}

// 狭い端末でも「抜ける手段」は必ず案内する。🚨 ここが issue 155 の実害:
// 末尾から切る実装では、並びの最後にある s/q が幅に関係なく最初に消えていた。
func TestStatusViewHintKeepsExitKeys(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	for _, w := range []int{40, 60, testPopupWidth, 120} {
		h := v.hint(w)
		for _, want := range []string{"s: 一覧へ", "q: 終了"} {
			if !strings.Contains(h, want) {
				t.Errorf("w=%d: 抜ける手段 %q が消えた: %q", w, want, h)
			}
		}
	}
	// 広い端末では全部出る (落とす条件が広すぎないこと)
	full := v.hint(200)
	for _, want := range []string{"j/k: 移動", "Space: stage/unstage", "X: 変更を捨てる", "b: push", "R: 残量"} {
		if !strings.Contains(full, want) {
			t.Errorf("広い幅なのに %q が出ていない: %q", want, full)
		}
	}
}

// hint を組む予算と、描画側が切る幅は同じ 1 か所から取る (browseModel.hintWidth)。
// 🚨 2 か所に式を書くと「収まるつもりで組んだ hint が黙って切られる」形でずれる (issue 155)。
func TestStatusHintUsesRenderBudget(t *testing.T) {
	// 🚨 幅を 1 点で見ない。項目単位で採るので、ある幅では予算のずれ 2 桁が余白に吸われて
	// 表に出ない。ずれが「切り詰めの …」として現れる幅は幅の刻みでしか見つからない
	// (変異検証で実測 2026-09-01: 1 点だけの検査は、組む側だけ予算をずらす変異を素通りした)。
	for w := frameMinWidth; w <= 140; w++ {
		m := newTestBrowse(t, 1, map[string]CIState{}, nil)
		// newTestBrowse は NoFrame なので、フレームぶん 2 桁引く経路を明示的に有効化する
		m.showFrame, m.width, m.height = true, w, 40
		if !m.frameActive() {
			t.Fatalf("w=%d: 前提が崩れた (フレームが有効でない)", w)
		}
		m.handleKey("s")
		releaseKey(m)
		line := m.hintLine()
		if got := dispWidth(stripANSI(line)); got > w {
			t.Errorf("w=%d: hint 行が端末幅を超えた (%d 桁): %q", w, got, line)
		}
		// 切り詰めの … が出ていない = 組む側の予算と描画側の clip 幅が一致している
		if strings.Contains(line, "…") {
			t.Errorf("w=%d: hint が切り詰められている (予算がずれている): %q", w, line)
		}
	}
}

// 全画面 diff を開いたまま J/K で隣のファイルへ差し替える (docs/glogx-ui-guide.md §6)。
// セクションをまたいでも一覧と同じ順で進み、端では止まって案内する。
func TestStatusPagerNeighborKeysSwapFile(t *testing.T) {
	v := newTestStatusView(t, statusRec("M  a.go", " M b.go", "?? c.txt")) // staged / unstaged / untracked
	if len(v.rows) != 3 {
		t.Fatalf("前提: 3 行でない: %d", len(v.rows))
	}
	long := make([]string, 60) // 窓より長くしないと j で送れず、先頭へ戻る効果が観測できない
	for i := range long {
		long[i] = "l"
	}
	for _, r := range v.rows {
		v.preview.store(previewKey(r), long, previewKey(r))
	}
	v.handleKey("k", testViewport()) // 初期カーソルは先頭の unstaged (b.go) なので staged の a.go へ上げる
	v.handleKey("d", testViewport())
	v.handleKey("j", testViewport()) // 途中まで送る (差し替えで先頭へ戻ることを見る)
	if v.pagerKey != previewKey(v.rows[0]) || v.pagerOffset != 1 {
		t.Fatalf("前提: a.go の diff を 1 行送った状態でない: key=%q off=%d", v.pagerKey, v.pagerOffset)
	}

	v.handleKey("J", testViewport())
	if v.pagerKey != previewKey(v.rows[1]) || v.cursor != 1 {
		t.Fatalf("J でセクションをまたいで b.go へ移らない: key=%q cursor=%d", v.pagerKey, v.cursor)
	}
	if v.pagerOffset != 0 || v.pagerTitle != v.rows[1].dispPath() {
		t.Errorf("J で先頭へ戻らない / タイトルが替わらない: off=%d title=%q", v.pagerOffset, v.pagerTitle)
	}
	v.handleKey("shift+down", testViewport())
	if v.pagerKey != previewKey(v.rows[2]) || v.cursor != 2 {
		t.Fatalf("shift+↓ で c.txt へ移らない: key=%q cursor=%d", v.pagerKey, v.cursor)
	}
	v.handleKey("J", testViewport())
	if msg, _ := v.takeNotice(); v.pagerKey != previewKey(v.rows[2]) || !strings.Contains(msg, "最後") {
		t.Errorf("末尾の J で止まらない / 案内が出ない: key=%q notice=%q", v.pagerKey, msg)
	}
	v.handleKey("K", testViewport())
	v.handleKey("K", testViewport())
	if v.pagerKey != previewKey(v.rows[0]) || v.cursor != 0 {
		t.Fatalf("K ×2 で a.go へ戻らない: key=%q cursor=%d", v.pagerKey, v.cursor)
	}
	v.handleKey("K", testViewport())
	if msg, _ := v.takeNotice(); v.pagerKey == "" || !strings.Contains(msg, "最初") {
		t.Errorf("先頭の K で閉じた / 案内が出ない: key=%q notice=%q", v.pagerKey, msg)
	}
}
