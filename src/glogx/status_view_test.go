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
// ⚠️ toggle() は通さない: 返り値の tea.Batch には tea.Tick (自動更新 1.5s / プレビュー 120ms) が
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
// テスト用)。⚠️ Update から返った Cmd を実行する経路は使えない: toggle の Batch には tick が
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
	// ⚠️ 1 カラムの幅で見る: 2 カラムだと右のプレビュー欄がカーソル行のパスを出すため、
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
// ⚠️ ESC と BEL だけを見る判定にしないこと: それだと 8bit の CSI (U+009B) / OSC (U+009D) を
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
// ⚠️ git へ渡す側 (path / pathspecs) は実物のまま — 無害化するとファイルを見失う。
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
	// ⚠️ ブランチ名は長いものを使う: 短いとヘッダーが一覧カラム幅 (48 桁ほど) に収まってしまい、
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

// 下からせり上がる演出: 途中では上側が空で、下側に窓の先頭が出る。
func TestSlideUpWindow(t *testing.T) {
	window := []string{"1", "2", "3", "4"}
	got := slideUpWindow(window, 0.5)
	if len(got) != 4 {
		t.Fatalf("行数 = %d, want 4", len(got))
	}
	if got[0] != "" || got[1] != "" {
		t.Errorf("上側が埋まっている: %q", got)
	}
	if got[2] != "1" || got[3] != "2" {
		t.Errorf("下側 = %q, want 窓の先頭 2 行", got[2:])
	}
	if full := slideUpWindow(window, 1); full[0] != "1" || full[3] != "4" {
		t.Errorf("進捗 1 で変形している: %q", full)
	}
}

func TestStatusOpenAnimationThenSettles(t *testing.T) {
	v := newStatusView()
	v.shown = true
	v.animStart = timeNow()
	if !v.animating() {
		t.Fatal("開いた直後に animating() = false")
	}
	if p := v.animProgress(); p >= 1 {
		t.Fatalf("開いた直後の進捗 = %v, want < 1", p)
	}
	restore := timeNow
	timeNow = func() time.Time { return restore().Add(statusOpenDuration + time.Millisecond) }
	defer func() { timeNow = restore }()
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
	v.preview[key] = []string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9", "l10",
		"l11", "l12", "l13", "l14", "l15", "l16", "l17", "l18", "l19", "l20"}
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
	v.preview[previewKey(row)] = []string{"x"}
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

// staging の画面から remote 操作へ滑る導線を作らない (spec 3 節)。
func TestStatusViewerBlocksPushAndPullKeys(t *testing.T) {
	for _, key := range []string{"b", "u"} {
		m := newTestBrowse(t, 3, nil, nil)
		m.statusOv.shown = true
		m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		if m.actModal.pushConfirm || m.actModal.pullConfirm {
			t.Fatalf("%s が status viewer 中に push/pull 確認を開いた", key)
		}
		if !m.statusOv.visible() {
			t.Fatalf("%s で viewer が閉じた", key)
		}
		// ⚠️ 「確認が開かない」だけでは不十分: ガードを外しても viewer のキーに無い b/u は
		// 素通りして何も起きないため、この assertion だけではガードの有無を区別できない
		// (ミューテーション検証 2026-08-03 で green のままだった)。契約は「無効である理由を
		// 返す」なので、トーストが出ることまで固定する。
		if !m.toast.visible() {
			t.Fatalf("%s が黙って無視された (無効である旨のトーストが出ていない)", key)
		}
	}
}

// p で pull --rebase の確認を開く (ユーザー要望 2026-08-05)。
//
// ⚠️ spec 3 節は「staging の途中から remote 操作へ滑る導線を作らない」として b/u を遮断して
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
	// ⚠️ 確認モーダルが viewer の上に描かれること。キーは viewer より先に actModal が捌くので、
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

// ⚠️ 回帰防止 (perf): 整形するのは可視の窓の分だけ。全行を整形してから切ると、画面に出る
// 行数と無関係に変更ファイル数へ比例したコストになる (実測 40 件 103µs / 2000 件 1.65ms)。
//
// ⚠️ 出力では検出できない: 全行を整形しても「返す」のは窓の分だけなので、返り値は同じになる
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

// push は従来どおり遮断し、u は「pull は p」と案内する (押しても無言にしない)。
func TestStatusViewerPushStaysBlockedAndUGuides(t *testing.T) {
	for _, tc := range []struct{ key, want string }{
		{"b", "push"},
		{"u", "p"},
	} {
		m := newTestBrowse(t, 3, nil, nil)
		m.statusOv.shown = true
		m.Update(tea.KeyPressMsg{Code: rune(tc.key[0]), Text: tc.key})
		if m.actModal.pushConfirm || m.actModal.pullConfirm {
			t.Fatalf("%s が status viewer 中に確認を開いた", tc.key)
		}
		if !m.toast.visible() || !strings.Contains(m.toast.text, tc.want) {
			t.Fatalf("%s の案内が出ていない: %q", tc.key, m.toast.text)
		}
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
	for i := range overlayCacheLimit + 20 {
		v.receivePreview(statusPreviewMsg{key: "k" + strconv.Itoa(i), lines: []string{"x"}})
	}
	if len(v.preview) > overlayCacheLimit+1 { // +1 = 表示中のキー (keep) の分
		t.Fatalf("キャッシュ数 = %d, want <= %d", len(v.preview), overlayCacheLimit+1)
	}
	if len(v.order) != len(v.preview) {
		t.Fatalf("order = %d, cache = %d (捨て漏れ)", len(v.order), len(v.preview))
	}
}

// 外部編集で内容が変わったら、プレビューは捨てたうえで取り直しを予約する
// (捨てるだけだとカーソルを動かすまでプレビュー欄が空のまま = 眺める用途で死ぬ)。
func TestStatusReceiveSchedulesPreviewRefetchOnChange(t *testing.T) {
	v := newTestStatusView(t, statusRec(" M a.go"))
	row, _ := v.current()
	v.receivePreview(statusPreviewMsg{key: previewKey(row), lines: []string{"old"}})
	same := v.receive(statusLoadMsg{st: parseWorktreeStatus(statusRec(" M a.go")), gen: v.gen})
	if same != nil {
		t.Error("内容が変わっていないのに取り直しを予約した (毎 1.5 秒 git diff が走る)")
	}
	if len(v.preview) == 0 {
		t.Error("内容が変わっていないのにキャッシュを捨てた")
	}
	changed := v.receive(statusLoadMsg{st: parseWorktreeStatus(statusRec("MM a.go")), gen: v.gen})
	if changed == nil {
		t.Fatal("内容が変わったのに取り直しを予約していない")
	}
	if len(v.preview) != 0 {
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
