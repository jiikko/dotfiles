package statusview

import (
	"errors"
	"fmt"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"glogx/internal/app"
	"glogx/internal/gitexec"
	"glogx/internal/linecache"
	"glogx/internal/term"
	"glogx/internal/testenv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// applyCmd は操作が返した Cmd (loadCmd か nil) を反映する。
func applyCmd(t *testing.T, v *StatusView, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	if msg, ok := cmd().(StatusLoadMsg); ok {
		v.Receive(msg)
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
	origAdd, origStaged, origWork, origClean := gitexec.RunGitAdd, gitexec.RunGitRestoreStaged, gitexec.RunGitRestoreWorktree, gitexec.RunGitCleanUntracked
	gitexec.RunGitAdd = func(paths []string) error { calls.add = append(calls.add, paths); return err }
	gitexec.RunGitRestoreStaged = func(paths []string) error {
		calls.restoreStaged = append(calls.restoreStaged, paths)
		return err
	}
	gitexec.RunGitRestoreWorktree = func(paths []string) error {
		calls.restoreWork = append(calls.restoreWork, paths)
		return err
	}
	gitexec.RunGitCleanUntracked = func(paths []string) error { calls.clean = append(calls.clean, paths); return err }
	t.Cleanup(func() {
		gitexec.RunGitAdd, gitexec.RunGitRestoreStaged, gitexec.RunGitRestoreWorktree, gitexec.RunGitCleanUntracked = origAdd, origStaged, origWork, origClean
	})
	return calls
}

func testViewport() StatusViewport { return StatusViewport{page: 20} }

func testStatusOpts(width, page int) StatusRenderOpts {
	return StatusRenderOpts{Width: width, Page: page, Spinner: "⠋"}
}

// cursorPathAt はカーソル行の (セクション, パス)。
func cursorPathAt(t *testing.T, v *StatusView) (worktreeSection, string) {
	t.Helper()
	row, ok := v.current()
	if !ok {
		t.Fatal("カーソル行がない")
	}
	return row.section, row.path
}

func TestStatusSpaceStagesUnstagedRow(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	calls := stubGitOps(t, nil)
	applyCmd(t, v, v.HandleKey(" ", testViewport()))
	if len(calls.add) != 1 || calls.add[0][0] != worktreePathspec("a.go") {
		t.Fatalf("git add の呼び出し = %v, want [[a.go]]", calls.add)
	}
	if len(calls.restoreStaged) != 0 {
		t.Errorf("unstaged 行の Space で restore --staged が走った: %v", calls.restoreStaged)
	}
}

func TestStatusSpaceUnstagesStagedRow(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec("M  a.go"))
	calls := stubGitOps(t, nil)
	applyCmd(t, v, v.HandleKey(" ", testViewport()))
	if len(calls.restoreStaged) != 1 || calls.restoreStaged[0][0] != worktreePathspec("a.go") {
		t.Fatalf("restore --staged の呼び出し = %v, want [[a.go]]", calls.restoreStaged)
	}
	if len(calls.add) != 0 {
		t.Errorf("staged 行の Space で git add が走った: %v", calls.add)
	}
}

func TestStatusSpaceStagesUntrackedRow(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec("?? new.txt"))
	calls := stubGitOps(t, nil)
	applyCmd(t, v, v.HandleKey(" ", testViewport()))
	if len(calls.add) != 1 || calls.add[0][0] != worktreePathspec("new.txt") {
		t.Fatalf("git add の呼び出し = %v, want [[new.txt]]", calls.add)
	}
}

func TestStatusConflictRowRejectsStageAndDiscard(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec("UU c.go"))
	calls := stubGitOps(t, nil)
	applyCmd(t, v, v.HandleKey(" ", testViewport()))
	if len(calls.add) != 0 || len(calls.restoreStaged) != 0 {
		t.Fatalf("conflict 行で git が走った: add=%v staged=%v", calls.add, calls.restoreStaged)
	}
	if notice, ok := testenv.NoticeOKOf(v.TakeNotice()); ok || !strings.Contains(notice, "conflict") {
		t.Errorf("notice = %q (ok=%v), want conflict の案内を失敗として出す", notice, ok)
	}
	v.HandleKey("X", testViewport())
	if v.discarding {
		t.Error("conflict 行で X の確認モーダルが開いた")
	}
}

func TestStatusStageAllStagesUnstagedAndUntracked(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec("M  staged.go", " M a.go", "?? b.txt"))
	calls := stubGitOps(t, nil)
	applyCmd(t, v, v.HandleKey("a", testViewport()))
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
	v := newTestStatusView(t, testenv.StatusRec("M  staged.go"))
	calls := stubGitOps(t, nil)
	v.HandleKey("a", testViewport())
	if len(calls.add) != 0 {
		t.Fatalf("stage 対象が無いのに git add が走った: %v", calls.add)
	}
	if notice, kind := v.TakeNotice(); kind != app.NoticeRefused || notice == "" {
		t.Errorf("notice = %q (kind=%v), want 断りの「stage するものがありません」", notice, kind)
	}
}

// stage したファイルは別セクションへ移るが、カーソルは追いかけず「同じ位置の次のファイル」に
// 留まる (Space 連打で上から順に stage できる)。
func TestStatusCursorStaysInSectionAfterStage(t *testing.T) {
	state := stubWorktreeStatus(t, testenv.StatusRec(" M a.go", " M b.go", " M c.go"), nil)
	v := NewStatusView()
	v.closeAnimOff = true
	v.shown = true
	applyStatusLoad(t, &v)
	v.HandleKey("j", testViewport()) // b.go へ
	if _, path := cursorPathAt(t, &v); path != "b.go" {
		t.Fatalf("移動後のカーソル = %q, want b.go", path)
	}
	stubGitOps(t, nil)
	state.out = testenv.StatusRec("M  b.go", " M a.go", " M c.go") // b.go が staged へ移った後の状態
	applyCmd(t, &v, v.HandleKey(" ", testViewport()))
	sec, path := cursorPathAt(t, &v)
	if sec != sectionUnstaged || path != "c.go" {
		t.Fatalf("stage 後のカーソル = (section %d, %q), want (Unstaged, c.go)", sec, path)
	}
}

// 外部編集で行が増えても、カーソルは同じパスに残る (index 保持だと隣のファイルへ滑り、
// X が「見ていたのとは別のファイル」を捨てる事故になる)。
func TestStatusCursorFollowsPathOnExternalEdit(t *testing.T) {
	state := stubWorktreeStatus(t, testenv.StatusRec(" M b.go"), nil)
	v := NewStatusView()
	v.closeAnimOff = true
	v.shown = true
	applyStatusLoad(t, &v)
	if _, path := cursorPathAt(t, &v); path != "b.go" {
		t.Fatalf("初期カーソル = %q", path)
	}
	// 別プロセスが a.go を編集して b.go の上に挿入された
	state.out = testenv.StatusRec(" M a.go", " M b.go")
	applyStatusLoad(t, &v)
	if _, path := cursorPathAt(t, &v); path != "b.go" {
		t.Fatalf("外部編集後のカーソル = %q, want b.go (パスで保持していない)", path)
	}
}

func TestStatusJumpSectionMovesToNextSection(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec("M  s.go", " M u.go", "?? n.txt"))
	v.HandleKey("tab", testViewport()) // Unstaged (初期位置) → Untracked
	if sec, path := cursorPathAt(t, v); sec != sectionUntracked || path != "n.txt" {
		t.Fatalf("Tab 後 = (section %d, %q), want (Untracked, n.txt)", sec, path)
	}
	v.HandleKey("tab", testViewport()) // 末尾からは先頭のセクションへ回る
	if sec, path := cursorPathAt(t, v); sec != sectionStaged || path != "s.go" {
		t.Fatalf("Tab 2 回目 = (section %d, %q), want (Staged, s.go) — 巡回していない", sec, path)
	}
	v.HandleKey("tab", testViewport())
	if sec, _ := cursorPathAt(t, v); sec != sectionUnstaged {
		t.Fatalf("Tab 3 回目のセクション = %d, want Unstaged", sec)
	}
}

// 開いた直後に触りたいのは「まだ stage していない変更」(spec 2 節)。
func TestStatusInitialCursorIsFirstUnstagedRow(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec("M  s.go", " M u.go", "?? n.txt"))
	sec, path := cursorPathAt(t, v)
	if sec != sectionUnstaged || path != "u.go" {
		t.Fatalf("初期カーソル = (section %d, %q), want (Unstaged, u.go)", sec, path)
	}
	// Unstaged が無ければ先頭の行 (Staged) から始める
	v2 := newTestStatusView(t, testenv.StatusRec("M  s.go"))
	if sec, path := cursorPathAt(t, v2); sec != sectionStaged || path != "s.go" {
		t.Fatalf("Unstaged 無しの初期カーソル = (section %d, %q), want (Staged, s.go)", sec, path)
	}
}

// 閉じた時点で走行中だった取得の札を降ろさないと、次に開いたとき loadCmd が「取得中」と判断して
// 二度と読み直さない (古い一覧が永久に居座る)。
func TestStatusReopenAfterCloseDuringLoadCanReload(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	if cmd := v.LoadCmd(); cmd == nil { // 走行中にする (結果は届けない)
		t.Fatal("2 回目の loadCmd が nil")
	}
	v.close() // closeAnimOff なので即座に畳まれる
	v.shown = true
	if cmd := v.LoadCmd(); cmd == nil {
		t.Fatal("開き直しても loadCmd が nil (loading の札が残っている)")
	}
}

// 取得に失敗しても、一度読めた一覧は消さない (last-good。spec 5 節)。
func TestStatusLoadErrorAfterLoadedKeepsLastGood(t *testing.T) {
	state := stubWorktreeStatus(t, testenv.StatusRec(" M a.go"), nil)
	v := NewStatusView()
	v.closeAnimOff = true
	v.shown = true
	applyStatusLoad(t, &v)
	state.err = errors.New("fatal: unable to read index")
	applyStatusLoad(t, &v)
	// 🚨 1 カラムの幅で見る: 2 カラムだと右のプレビュー欄がカーソル行のパスを出すため、
	// 一覧が消えていても "a.go" が画面に残ってしまい last-good の有無を区別できない
	body := strings.Join(v.Lines(testStatusOpts(70, 20)), "\n")
	if !strings.Contains(body, "a.go") {
		t.Fatalf("失敗で last-good の一覧が消えた:\n%s", body)
	}
	if !strings.Contains(term.StripANSI(body), "status 取得失敗") {
		t.Fatalf("古い可能性を示す印がヘッダーに無い:\n%s", body)
	}
}

// ファイル名の端末制御シーケンスは画面へ出さない。POSIX のファイル名は / と NUL 以外の任意
// バイトを許し、-z の git status はクォートせず生で返すので、第三者ブランチに ESC 入りの名前の
// untracked が 1 つあるだけで status viewer を開いた瞬間に端末が乗っ取られる。
// 🚨 git へ渡す側 (path / pathspecs) は実物のまま — 無害化するとファイルを見失う。
func TestStatusPathIsSanitizedForDisplayOnly(t *testing.T) {
	const esc, bel = "\x1b", "\a"
	evil := "evil" + esc + "]0;pwned" + bel + ".txt"
	row := worktreeRow{section: sectionUntracked, code: '?', x: '?', y: '?', path: evil}

	shown := term.StripANSI(statusPathText(row, 60, false))
	if testenv.HasTerminalControl(shown) {
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
	if got := term.StripANSI(statusPathText(rn, 60, false)); testenv.HasTerminalControl(got) {
		t.Errorf("rename 元に制御シーケンスが残った: %q", got)
	}
}

// 通知文はパスや git のエラー出力を素で埋め込むので、setNotice 自体を関門にする
// (呼び出しごとに包む方式だと必ずどこかが漏れる)。
func TestStatusNoticeIsSanitized(t *testing.T) {
	const esc, bel = "\x1b", "\a"
	v := newTestStatusView(t, testenv.StatusRec("## master", "?? a.txt"))
	v.setNotice("evil"+esc+"]0;pwned"+bel+".txt の変更を捨てました", app.NoticeOK)
	if testenv.HasTerminalControl(v.notice) {
		t.Errorf("通知に制御シーケンスが残った: %q", v.notice)
	}
}

// パスが幅を超えるときは basename を残す (末尾から切ると「どのファイルか」が分からない)。
func TestStatusPathTextKeepsBasename(t *testing.T) {
	row := worktreeRow{section: sectionUnstaged, path: "src/glogx/deeply/nested/dir/render.go"}
	got := term.StripANSI(statusPathText(row, 16, false))
	if term.DispWidth(got) > 16 {
		t.Fatalf("幅 = %d, want <= 16: %q", term.DispWidth(got), got)
	}
	if !strings.HasSuffix(got, "render.go") {
		t.Fatalf("basename が残っていない: %q", got)
	}
}

func TestStatusDiscardRejectsStagedRow(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec("M  a.go"))
	v.HandleKey("X", testViewport())
	if v.discarding {
		t.Fatal("staged 行で X の確認が開いた (先に Space で unstage させる契約)")
	}
	if notice, ok := testenv.NoticeOKOf(v.TakeNotice()); ok || !strings.Contains(notice, "unstage") {
		t.Errorf("notice = %q (ok=%v), want unstage を促す案内", notice, ok)
	}
}

func TestStatusDiscardConfirmRunsRestore(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	calls := stubGitOps(t, nil)
	v.HandleKey("X", testViewport())
	if !v.discarding {
		t.Fatal("X で確認モーダルが開いていない")
	}
	applyCmd(t, v, v.HandleKey("y", testViewport()))
	if len(calls.restoreWork) != 1 || calls.restoreWork[0][0] != worktreePathspec("a.go") {
		t.Fatalf("git restore の呼び出し = %v, want [[a.go]]", calls.restoreWork)
	}
	if v.discarding {
		t.Error("実行後も確認モーダルが開いたまま")
	}
}

// 大文字 Y も実行する (confirm.IsYes)。issues viewer の next 目印は Y で取り消す側で、
// 揃えない理由は discardKey の注記。
func TestStatusDiscardConfirmAcceptsUpperY(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	calls := stubGitOps(t, nil)
	v.HandleKey("X", testViewport())
	applyCmd(t, v, v.HandleKey("Y", testViewport()))
	if len(calls.restoreWork) != 1 {
		t.Fatalf("Y で git restore が走らない: %v", calls.restoreWork)
	}
}

func TestStatusDiscardCancelKeepsFile(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	calls := stubGitOps(t, nil)
	v.HandleKey("X", testViewport())
	v.HandleKey("n", testViewport())
	if len(calls.restoreWork) != 0 {
		t.Fatalf("n でキャンセルしたのに restore が走った: %v", calls.restoreWork)
	}
	if v.discarding {
		t.Error("n の後も確認モーダルが開いたまま")
	}
}

func TestStatusDiscardUsesCleanForUntracked(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec("?? new.txt"))
	calls := stubGitOps(t, nil)
	v.HandleKey("X", testViewport())
	applyCmd(t, v, v.HandleKey("y", testViewport()))
	if len(calls.clean) != 1 || calls.clean[0][0] != worktreePathspec("new.txt") {
		t.Fatalf("git clean の呼び出し = %v, want [[new.txt]]", calls.clean)
	}
	if len(calls.restoreWork) != 0 {
		t.Errorf("untracked に restore が走った: %v", calls.restoreWork)
	}
}

// spec 4 節の不変条件 1: 確認に出した (パス, XY) と実行時の状態が違えば捨てない。
func TestStatusDiscardAbortsWhenStateChangedDuringConfirm(t *testing.T) {
	state := stubWorktreeStatus(t, testenv.StatusRec(" M a.go"), nil)
	v := NewStatusView()
	v.closeAnimOff = true
	v.shown = true
	applyStatusLoad(t, &v)
	calls := stubGitOps(t, nil)
	v.HandleKey("X", testViewport())
	// 確認中に別プロセスが a.go を stage した (XY が " M" → "MM" へ変わる)
	state.out = testenv.StatusRec("MM a.go")
	applyCmd(t, &v, v.HandleKey("y", testViewport()))
	if len(calls.restoreWork) != 0 || len(calls.clean) != 0 {
		t.Fatalf("状態が変わったのに捨ててしまった: restore=%v clean=%v", calls.restoreWork, calls.clean)
	}
	notice, ok := testenv.NoticeOKOf(v.TakeNotice())
	if ok || !strings.Contains(notice, "変わった") {
		t.Errorf("notice = %q (ok=%v), want 中止したことを失敗として伝える", notice, ok)
	}
}

func TestStatusDiscardAbortsWhenFileDisappeared(t *testing.T) {
	state := stubWorktreeStatus(t, testenv.StatusRec(" M a.go"), nil)
	v := NewStatusView()
	v.closeAnimOff = true
	v.shown = true
	applyStatusLoad(t, &v)
	calls := stubGitOps(t, nil)
	v.HandleKey("X", testViewport())
	state.out = testenv.StatusRec() // 確認中に別プロセスが commit した
	applyCmd(t, &v, v.HandleKey("y", testViewport()))
	if len(calls.restoreWork) != 0 {
		t.Fatalf("対象が消えたのに restore が走った: %v", calls.restoreWork)
	}
}

func TestStatusPollDropsStaleGeneration(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	gen := v.gen
	if cmd := v.ReceivePoll(StatusPollMsg{gen: gen}); cmd == nil {
		t.Fatal("同世代の poll が読み直しを返さない")
	}
	v.close() // closeAnimOff なので即座に畳まれ、gen が進む
	if cmd := v.ReceivePoll(StatusPollMsg{gen: gen}); cmd != nil {
		t.Error("閉じた後に古い世代の poll が生きている (次に開いた画面へ効く)")
	}
}

func TestStatusReceiveIgnoresStaleGeneration(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	stale := StatusLoadMsg{st: parseWorktreeStatus(testenv.StatusRec(" M zzz.go")), gen: v.gen - 1}
	v.Receive(stale)
	if _, path := cursorPathAt(t, v); path != "a.go" {
		t.Fatalf("古い世代の結果で上書きされた: カーソル = %q", path)
	}
}

func TestStatusLoadErrorShowsMessageAndKeepsViewOpen(t *testing.T) {
	v := NewStatusView()
	v.closeAnimOff = true
	v.shown = true
	stubWorktreeStatus(t, "", errors.New("fatal: not a git repository"))
	applyStatusLoad(t, &v)
	if !v.shown {
		t.Fatal("読み取り失敗で viewer が閉じた (画面は開いたまま案内を出す契約)")
	}
	body := strings.Join(v.Lines(testStatusOpts(120, 20)), "\n")
	if !strings.Contains(body, "git status に失敗") {
		t.Fatalf("失敗の案内が出ていない:\n%s", body)
	}
}

func TestStatusLinesTwoColumnsWhenWide(t *testing.T) {
	// 🚨 ブランチ名は長いものを使う: 短いとヘッダーが一覧カラム幅 (48 桁ほど) に収まってしまい、
	// 「全幅で描く」ことの検証にならない (件数が切れる条件を作れない)
	v := newTestStatusView(t, testenv.StatusRec("## feature/a-very-long-branch-name-for-header-width...origin/x", " M a.go"))
	lines := v.Lines(testStatusOpts(120, 20))
	if len(lines) != 20 {
		t.Fatalf("行数 = %d, want 20 (常にちょうど page 行)", len(lines))
	}
	if !strings.Contains(lines[1], "│") {
		t.Fatalf("2 カラムの区切りが無い: %q", lines[1])
	}
	// ヘッダーは全幅で描く (一覧カラムの中に入れると件数が切り落とされる)
	head := term.StripANSI(lines[0])
	if strings.Contains(head, "│") {
		t.Fatalf("ヘッダーが 2 カラムに割られている: %q", head)
	}
	if !strings.Contains(head, "staged 0") {
		t.Fatalf("ヘッダーの件数が切れている: %q", head)
	}
}

func TestStatusLinesOneColumnWhenNarrow(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	lines := v.Lines(testStatusOpts(70, 20))
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
	v := newTestStatusView(t, testenv.StatusRec("MM both.go", "?? new.txt"))
	body := strings.Join(v.Lines(testStatusOpts(120, 20)), "\n")
	testenv.ContainsAll(t, "画面", body, "Staged (1)", "Unstaged (1)", "Untracked (1)", "◐")
}

func TestStatusLinesCleanTreeMessage(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec())
	body := strings.Join(v.Lines(testStatusOpts(120, 20)), "\n")
	if !strings.Contains(body, "クリーン") {
		t.Fatalf("クリーンの案内が無い:\n%s", body)
	}
}

func TestStatusHeaderShowsBranchAndCounts(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec("## topic...origin/topic [ahead 2]", "M  a.go", " M b.go"))
	head := term.StripANSI(v.Lines(testStatusOpts(120, 20))[0])
	testenv.ContainsAll(t, "ヘッダー", head, "topic", "ahead 2", "staged 1", "unstaged 1")
}

// カーソル行の強調は一覧カラムの幅で完結する (プレビュー側まで背景が伸びない)。
func TestStatusCursorPaintStaysInListColumn(t *testing.T) {
	painted := statusCursorPaint("→ M a.go", 30, true)
	if w := term.DispWidth(term.StripANSI(painted)); w != 30 {
		t.Fatalf("カーソル行の表示幅 = %d, want 30 (カラム幅で止める)", w)
	}
	if !strings.HasSuffix(painted, term.AnsiReset) {
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
	if term.DispWidth(got[0]) <= 5 {
		t.Errorf("終端減速が効いていない (0.5 時点で %q = %d 桁。等速なら 5 桁)", got[0], term.DispWidth(got[0]))
	}
	// 着地 (進捗 1) ではボーダーごと消えて元の行に戻る
	if full := slideLeftWindow(window, 1, 10, false, false); full[0] != "0123456789" || full[3] != "0123456789" {
		t.Errorf("進捗 1 で変形している: %q", full)
	}
	// 進捗 0 は中身 0 桁 + 左端のボーダーだけ (動き出しから端が見える)
	if none := slideLeftWindow(window, 0, 10, false, false); none[0] != "▒" || none[3] != "▒" {
		t.Errorf("進捗 0 がボーダー 1 桁になっていない: %q", none)
	}
	// 閉じは全行同時・等速 (layout.RowOffsetRatio の closing 分岐)。5 桁 = 中身 4 + ボーダー 1
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
	if term.DispWidth(blank[0]) != term.DispWidth(blank[1]) || !strings.HasSuffix(blank[0], "▒") {
		t.Errorf("空行のボーダーが欠けている: %q vs %q", blank[0], blank[1])
	}
	// colored=true は近黒の █ (ドロップシャドウと同じ色)
	col := slideLeftWindow(window, 0.5, 10, true, true)
	if !strings.HasSuffix(col[0], term.AnsiShadowFg+"█"+term.AnsiReset) {
		t.Errorf("colored のボーダーが近黒 █ になっていない: %q", col[0])
	}
}

func TestStatusOpenAnimationThenSettles(t *testing.T) {
	advance := testenv.StubClock(t)
	v := NewStatusView()
	v.shown = true
	v.animStart = app.TimeNow()
	// 経過 0 だけでなく「開いて僅かに経った」正の経過でも見る (0 は境界の退化ケースで、
	// 「経過 > 0 で即 1 に跳ぶ」型のミスを素通りさせる)
	advance(time.Millisecond)
	if !v.Animating() {
		t.Fatal("開いた直後に animating() = false")
	}
	if p := v.animProgress(); p <= 0 || p >= 1 {
		t.Fatalf("開いた直後の進捗 = %v, want 0 < p < 1", p)
	}
	advance(statusOpenDuration)
	if v.Animating() {
		t.Error("所要を過ぎても animating() = true")
	}
	v.settleAnim()
	if !v.animStart.IsZero() {
		t.Error("settleAnim が時計を捨てていない")
	}
}

func TestStatusPagerOpensAndScrolls(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	key := previewKey(row)
	v.preview.Store(key, []string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9", "l10",
		"l11", "l12", "l13", "l14", "l15", "l16", "l17", "l18", "l19", "l20"}, key)
	v.HandleKey("d", testViewport())
	if v.pagerKey != key {
		t.Fatalf("pagerKey = %q, want %q", v.pagerKey, key)
	}
	v.HandleKey("j", testViewport())
	if v.pager.Offset != 1 {
		t.Fatalf("j 後の offset = %d, want 1", v.pager.Offset)
	}
	v.HandleKey("d", testViewport()) // toggle で閉じる
	if v.pagerKey != "" {
		t.Fatalf("d で閉じていない: %q", v.pagerKey)
	}
}

func TestStatusPagerKeysDoNotMoveList(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go", " M b.go"))
	row, _ := v.current()
	v.preview.Store(previewKey(row), []string{"x"}, previewKey(row))
	v.HandleKey("d", testViewport())
	v.HandleKey("j", testViewport())
	if _, path := cursorPathAt(t, v); path != "a.go" {
		t.Fatalf("pager 中の j で一覧のカーソルが動いた: %q", path)
	}
}

func TestStatusDiscardConfirmSwallowsListKeys(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go", " M b.go"))
	stubGitOps(t, nil)
	v.HandleKey("X", testViewport())
	v.HandleKey("j", testViewport()) // 確認中の j は「キャンセル」で、カーソルは動かない
	if _, path := cursorPathAt(t, v); path != "a.go" {
		t.Fatalf("確認中の j で一覧のカーソルが動いた: %q (確認に出した行と食い違う)", path)
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
		v := newTestStatusView(t, testenv.StatusRec(recs...))
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

// 極端な端末サイズ (resize 直後・1 桁 pane) でも panic せず、契約どおり page 行を返すこと。
func TestStatusLinesSurvivesExtremeSizes(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec("M  s.go", " M src/very/deep/path/render.go", "?? tmp/"))
	for _, size := range [][2]int{{0, 0}, {1, 1}, {2, 3}, {20, 3}, {70, 2}, {200, 60}} {
		width, page := size[0], size[1]
		lines := v.Lines(testStatusOpts(width, page))
		if len(lines) != page {
			t.Fatalf("%dx%d の行数 = %d, want %d", width, page, len(lines), page)
		}
	}
	// pager を開いた状態でも同じ (枠の描画が幅 0 を踏む経路)
	v.HandleKey("d", testViewport())
	for _, size := range [][2]int{{0, 0}, {1, 1}, {20, 6}} {
		if lines := v.Lines(testStatusOpts(size[0], size[1])); len(lines) != size[1] {
			t.Fatalf("pager 表示中 %dx%d の行数 = %d, want %d", size[0], size[1], len(lines), size[1])
		}
	}
}

// プレビューのキャッシュは無制限に育たない (長時間セッションでファイルを見た数だけ溜まる)。
func TestStatusPreviewCacheIsBounded(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	for i := range linecache.Limit + 20 {
		v.ReceivePreview(StatusPreviewMsg{key: "k" + strconv.Itoa(i), lines: []string{"x"}})
	}
	if v.preview.Len() > linecache.Limit+1 { // +1 = 表示中のキー (keep) の分
		t.Fatalf("キャッシュ数 = %d, want <= %d", v.preview.Len(), linecache.Limit+1)
	}
	if n, order := v.preview.Counts(); order != n {
		t.Fatalf("order = %d, cache = %d (捨て漏れ)", order, n)
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
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	key := previewKey(row)
	if cmd := v.fetchDiff(row, false); cmd == nil {
		t.Fatal("最初の fetchDiff が nil (前提が崩れた)")
	}

	// 外部編集で行集合が変わった = 古い diff を捨てる経路 (changed が true になる入力にする)
	v.Receive(StatusLoadMsg{st: parseWorktreeStatus(testenv.StatusRec(" M a.go", " M b.go")), gen: v.gen})
	if v.preview.Has(key) {
		t.Fatal("前提が崩れた: 古い diff が捨てられていない (この経路を通っていない)")
	}

	if !v.preview.Loading(key) {
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
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	key := previewKey(row)
	if cmd := v.fetchDiff(row, false); cmd == nil {
		t.Fatal("最初の fetchDiff が nil (前提が崩れた)")
	}
	v.preview.Store("other", []string{"stale"}, "")

	v.applyFresh(parseWorktreeStatus(testenv.StatusRec(" M a.go", " M b.go")))

	if v.preview.Has("other") {
		t.Error("古いキャッシュが残った (取り直しの契機が無いまま古い diff を出す)")
	}
	if !v.preview.Loading(key) {
		t.Fatal("走行中の取得の札が降りた (次の tick 満了で二重に git diff が走る)")
	}
}

func TestStatusReceiveSchedulesPreviewRefetchOnChange(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	v.ReceivePreview(StatusPreviewMsg{key: previewKey(row), lines: []string{"old"}})
	same := v.Receive(StatusLoadMsg{st: parseWorktreeStatus(testenv.StatusRec(" M a.go")), gen: v.gen})
	if same != nil {
		t.Error("内容が変わっていないのに取り直しを予約した (毎 1.5 秒 git diff が走る)")
	}
	if v.preview.Len() == 0 {
		t.Error("内容が変わっていないのにキャッシュを捨てた")
	}
	changed := v.Receive(StatusLoadMsg{st: parseWorktreeStatus(testenv.StatusRec("MM a.go")), gen: v.gen})
	if changed == nil {
		t.Fatal("内容が変わったのに取り直しを予約していない")
	}
	if v.preview.Len() != 0 {
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
		testenv.SkipEnv(t, "この環境では symlink を作れない: %v", err)
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
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	full := []string{
		"diff --git a/a.go b/a.go", "index 111..222 100644", "--- a/a.go", "+++ b/a.go",
		"@@ -1,3 +1,3 @@", "-old", "+new",
	}
	v.ReceivePreview(StatusPreviewMsg{key: previewKey(row), lines: full})
	pane := strings.Join(v.previewPane(testStatusOpts(60, 12), 60), "\n")
	if strings.Contains(pane, "diff --git") || strings.Contains(pane, "index 111") {
		t.Fatalf("プレビューに diff ヘッダーが残っている:\n%s", pane)
	}
	if !strings.Contains(pane, "@@ -1,3 +1,3 @@") || !strings.Contains(pane, "+new") {
		t.Fatalf("プレビューに hunk が出ていない:\n%s", pane)
	}
	// 全画面 diff (d) は全文を出す
	v.HandleKey("d", testViewport())
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
	v := newTestStatusView(t, testenv.StatusRec("garbage-line", "another-garbage"))
	body := term.StripANSI(strings.Join(v.Lines(testStatusOpts(70, 20)), "\n"))
	if strings.Contains(body, "クリーン") {
		t.Fatalf("解釈できなかったのに「クリーン」と表示した:\n%s", body)
	}
	if !strings.Contains(body, "解釈できませんでした") {
		t.Fatalf("解釈できなかったことが画面に出ていない:\n%s", body)
	}
	// 一部だけ読めたときは一覧を出しつつヘッダーで取りこぼしを知らせる
	v2 := newTestStatusView(t, testenv.StatusRec(" M a.go", "garbage"))
	head := term.StripANSI(v2.Lines(testStatusOpts(120, 20))[0])
	if !strings.Contains(head, "解釈不能") {
		t.Fatalf("取りこぼしがヘッダーに出ていない: %q", head)
	}
}

// -z の末尾 NUL が生む空要素は「解釈できなかった」に数えない (常に警告が出てしまう)。
func TestStatusTrailingNulIsNotCountedAsSkipped(t *testing.T) {
	st := parseWorktreeStatus(testenv.StatusRec(" M a.go"))
	if st.skipped != 0 {
		t.Fatalf("skipped = %d, want 0 (末尾の空要素を数えている)", st.skipped)
	}
}

// 全行が幅に収まる不変条件を width 1 から全数で掃く (issues 側の
// TestIssuesViewLinesAlwaysExactlyPageRows と対。掃き始めが広い幅だけだと、
// 極小幅でしか出ない枠越え (term.ClipToWidth の width<=0 素通し・scrollbarColumn の床上げ) が
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
	v := newTestStatusView(t, testenv.StatusRec(recs...))
	for width := 1; width <= 80; width++ {
		for _, page := range []int{3, 12} {
			for _, ln := range v.Lines(testStatusOpts(width, page)) {
				if w := term.DispWidth(ln); w > width {
					t.Fatalf("width=%d page=%d の行が幅を超えた (w=%d): %q", width, page, w, ln)
				}
			}
		}
	}
}

// 明示的な再読込 (r) はプレビューのキャッシュも捨てる。
//
// キー (section+XY+path) は内容を一意に決めないので、同じ ` M` のまま中身だけ変わる
// 「保存し直し」では receive の changed 判定を通り抜ける。自動更新での据え置きは意図的だが、
// r まで据え置くと「再読込したのに編集前の diff が出続ける」になる (silent)。
func TestStatusReloadKeyDropsPreviewCache(t *testing.T) {
	// 🚨 2 行以上の fixture にすること。1 行だと `len(entries) != 0` では
	//   「全部捨てる」と「カーソル行だけ捨てる」を区別できない
	stubWorktreeStatus(t, testenv.StatusRec(" M a.go", " M b.go"), nil)
	v := NewStatusView()
	v.closeAnimOff = true
	v.shown = true
	applyStatusLoad(t, &v)
	for _, r := range v.rows {
		v.preview.Store(previewKey(r), []string{"OLD-DIFF"}, "")
	}
	if v.preview.Len() < 2 {
		t.Fatalf("前提が崩れた: キャッシュに 2 件仕込めていない (%d 件)", v.preview.Len())
	}
	// 🚨 予約と再読込は別々に見る。`cmd != nil` では判別力が無い — loadCmd だけでも
	//   previewTickCmd だけでも非 nil が返るので、どちらを落とす変異も green のまま通る (実測)
	// 🚨 再読込の有無は v.loading で見る。loadCmd は呼ばれた時点で同期に loading を立てるので、
	//   tea.Batch を実行しなくても判る (Batch を実行しても子は走らない = 数えられない)
	seqBefore := v.previewSeq
	v.loading = false

	v.listKey("r", testViewport())

	if v.preview.Len() != 0 {
		t.Errorf("r でプレビューのキャッシュが残った (編集前の diff が出続ける): %d 件", v.preview.Len())
	}
	// 🚨 捨てるだけだとプレビュー欄が空のまま残る (receive 側と同じ理由で取り直しを予約する)
	if v.previewSeq == seqBefore {
		t.Error("r で取り直しが予約されていない (プレビュー欄が空のまま戻らない)")
	}
	if !v.loading {
		t.Error("r が git status を読み直していない (再読込そのものが走らない)")
	}
}

// r と閉じで **reset() でなく clearEntries()** を使うこと。
//
// reset() は取得中の札まで降ろすので、走行中の取得と、直後に張り直した予約が
// **同じキーを二重に取りに行く** (lineCache.clearEntries の doc が名指しで禁じている形)。
func TestStatusReloadKeepsInFlightMark(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	key := previewKey(row)
	if !v.preview.Begin(key) {
		t.Fatal("前提が崩れた: 取得を始められない")
	}

	v.listKey("r", testViewport())

	if !v.preview.Loading(key) {
		t.Error("r が取得中の札まで降ろした (走行中の取得と予約が同じキーを二重に取りに行く)")
	}
}

// 無効化 (閉じ) を跨いで着地した取得が、古い内容を復活させない。
//
// statusPreviewMsg は 4 兄弟で唯一 gen を持っておらず、閉じた瞬間に飛んでいた取得が
// clearEntries の**後**に着地してキャッシュを復活させていた。復活すると begin() に弾かれ、
// 開き直しても取り直しが一度も走らない = 編集前の diff が永久に出続ける。
func TestStatusLatePreviewAfterCloseIsDropped(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	key := previewKey(row)
	if !v.preview.Begin(key) {
		t.Fatal("前提が崩れた: 取得を始められない")
	}
	staleGen := v.gen

	v.closing = true
	if !v.FinishClose() {
		t.Fatal("前提が崩れた: finishClose が閉じ切っていない")
	}
	v.ReceivePreview(StatusPreviewMsg{gen: staleGen, key: key, lines: []string{"OLD-DIFF"}})

	if v.preview.Len() != 0 {
		t.Errorf("閉じた後に着地した取得がキャッシュを復活させた: %d 件", v.preview.Len())
	}
	// 🚨 捨てるときも札は降ろすこと。残すと begin() がこのキーを永久に弾く
	if v.preview.Loading(key) {
		t.Error("捨てた結果の取得中の札が残った (取り直しが二度と走らない)")
	}
}

// r でも同じ (r は gen を進めるので、飛んでいた取得は世代違いで捨てられる)。
func TestStatusLatePreviewAfterReloadIsDropped(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	key := previewKey(row)
	if !v.preview.Begin(key) {
		t.Fatal("前提が崩れた: 取得を始められない")
	}
	staleGen := v.gen

	v.listKey("r", testViewport())
	v.ReceivePreview(StatusPreviewMsg{gen: staleGen, key: key, lines: []string{"OLD-DIFF"}})

	if v.preview.Len() != 0 {
		t.Errorf("r の後に着地した取得が古い内容を復活させた (押しても何も変わらない): %d 件", v.preview.Len())
	}
	// 🚨 札はここで降ろすこと。r は clearEntries なので札が残っており (それは意図的)、
	//   捨てた結果の札を降ろさないと begin() がこのキーを永久に弾いて取り直しが走らない。
	//   閉じる経路は clearBusy が先に走るので、この主張はこちらでしか立たない
	if v.preview.Loading(key) {
		t.Error("捨てた結果の取得中の札が残った (取り直しが二度と走らない)")
	}
}

// viewer を閉じるとプレビューのキャッシュを捨てる。
//
// 閉じているあいだに外部編集されても XY が動かなければキーが一致するので、捨てないと
// **開き直しても編集前の diff が出る**。しかも finishClose は clearBusy しか呼んでおらず、
// プロセスが生きている限り消えなかった。
func TestStatusCloseDropsPreviewCache(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	v.ReceivePreview(StatusPreviewMsg{key: previewKey(row), lines: []string{"OLD-DIFF"}})
	if v.preview.Len() == 0 {
		t.Fatal("前提が崩れた: キャッシュに仕込めていない")
	}

	// 別の行の取得が飛んでいる状態も作る (閉じた瞬間に in-flight があるのが実際の形)
	if !v.preview.Begin("in-flight-key") {
		t.Fatal("前提が崩れた: 取得を始められない")
	}

	v.closing = true
	if !v.FinishClose() {
		t.Fatal("前提が崩れた: finishClose が閉じ切っていない")
	}

	if v.preview.Len() != 0 {
		t.Errorf("閉じてもプレビューのキャッシュが残った (開き直しで編集前の diff が出る): %d 件", v.preview.Len())
	}
	// 🚨 隣の clearBusy も守る。clearEntries を隣に足したことで「2 行まとめて reset() で
	//   よくない?」という編集が誘発されやすくなったが、**片方だけ消す編集は無音で通る**。
	//   札が残ると fetching() が true のままフレーム tick を回し続ける (元の 🚨 コメント)
	if v.preview.Fetching() {
		t.Errorf("閉じても取得中の札が残った (フレーム tick が回り続ける)")
	}
}

// R は ratelimit ダッシュボードへ横断する (i と対。ユーザー要望 2026-09-01)。viewer は閉じ、
// 実際に開くのは browseModel (takeWantCross)。
// 🚨 hint の案内は**広い端末でだけ**出る (幅に応じて落とすため。popup の
// 実幅では優先度の低い R は落ちる)。狭い幅での正本は --help / README。
func TestStatusRSwitchesToRatelimitDash(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))

	v.HandleKey("R", StatusViewport{page: 20})

	if v.TakeWantCross() != "R" {
		t.Error("R でダッシュボードへの横断を要求しない")
	}
	if v.TakeWantCross() != "" {
		t.Error("takeWantCross が 2 回横断先を返す (横断が二重に起きる)")
	}
	if !v.closing && v.shown {
		t.Error("R で viewer が閉じない (全画面は同時に 1 枚)")
	}
	if !strings.Contains(v.Hint(200), "R: 残量") {
		t.Errorf("hint が R を案内していない: %q", v.Hint(200))
	}
}

// hint は与えられた幅に必ず収まる。🚨 issues viewer と同じ検査を status にも置く (片方だけ無検査だと、
// キーを足すたびに末尾が黙って削られる)。
//
// 🚨 幅を 1 点だけ見ない。popup の実幅 (84) は代表値でしかなく、端末は任意の幅を取る。
func TestStatusViewHintFitsWidth(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
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
			if got := term.DispWidth(v.Hint(w)); got > w && mode.name == "一覧" {
				t.Errorf("w=%d %s: hint が %d 桁 (収まっていない): %q", w, mode.name, got, v.Hint(w))
			}
		}
	}
}

// 狭い端末でも「抜ける手段」は必ず案内する。🚨 ここが実害になる:
// 末尾から切る実装では、並びの最後にある s/q が幅に関係なく最初に消える。
func TestStatusViewHintKeepsExitKeys(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	for _, w := range []int{40, 60, testenv.PopupWidth, 120} {
		h := v.Hint(w)
		for _, want := range []string{"s: 一覧へ", "q: 終了"} {
			if !strings.Contains(h, want) {
				t.Errorf("w=%d: 抜ける手段 %q が消えた: %q", w, want, h)
			}
		}
	}
	// 広い端末では全部出る (落とす条件が広すぎないこと)
	full := v.Hint(200)
	for _, want := range []string{"j/k: 移動", "Space: stage/unstage", "X: 変更を捨てる", "b: push", "R: 残量"} {
		if !strings.Contains(full, want) {
			t.Errorf("広い幅なのに %q が出ていない: %q", want, full)
		}
	}
}

// 全画面 diff を開いたまま J/K で隣のファイルへ差し替える (docs/glogx-ui-guide.md §6)。
// セクションをまたいでも一覧と同じ順で進み、端では止まって案内する。
func TestStatusPagerNeighborKeysSwapFile(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec("M  a.go", " M b.go", "?? c.txt")) // staged / unstaged / untracked
	if len(v.rows) != 3 {
		t.Fatalf("前提: 3 行でない: %d", len(v.rows))
	}
	long := make([]string, 60) // 窓より長くしないと j で送れず、先頭へ戻る効果が観測できない
	for i := range long {
		long[i] = "l"
	}
	for _, r := range v.rows {
		v.preview.Store(previewKey(r), long, previewKey(r))
	}
	v.HandleKey("k", testViewport()) // 初期カーソルは先頭の unstaged (b.go) なので staged の a.go へ上げる
	v.HandleKey("d", testViewport())
	v.HandleKey("j", testViewport()) // 途中まで送る (差し替えで先頭へ戻ることを見る)
	if v.pagerKey != previewKey(v.rows[0]) || v.pager.Offset != 1 {
		t.Fatalf("前提: a.go の diff を 1 行送った状態でない: key=%q off=%d", v.pagerKey, v.pager.Offset)
	}

	v.HandleKey("J", testViewport())
	if v.pagerKey != previewKey(v.rows[1]) || v.cursor != 1 {
		t.Fatalf("J でセクションをまたいで b.go へ移らない: key=%q cursor=%d", v.pagerKey, v.cursor)
	}
	if v.pager.Offset != 0 || v.pagerTitle != v.rows[1].dispPath() {
		t.Errorf("J で先頭へ戻らない / タイトルが替わらない: off=%d title=%q", v.pager.Offset, v.pagerTitle)
	}
	v.HandleKey("shift+down", testViewport())
	if v.pagerKey != previewKey(v.rows[2]) || v.cursor != 2 {
		t.Fatalf("shift+↓ で c.txt へ移らない: key=%q cursor=%d", v.pagerKey, v.cursor)
	}
	v.HandleKey("J", testViewport())
	if msg, _ := testenv.NoticeOKOf(v.TakeNotice()); v.pagerKey != previewKey(v.rows[2]) || !strings.Contains(msg, "最後") {
		t.Errorf("末尾の J で止まらない / 案内が出ない: key=%q notice=%q", v.pagerKey, msg)
	}
	v.HandleKey("K", testViewport())
	v.HandleKey("K", testViewport())
	if v.pagerKey != previewKey(v.rows[0]) || v.cursor != 0 {
		t.Fatalf("K ×2 で a.go へ戻らない: key=%q cursor=%d", v.pagerKey, v.cursor)
	}
	v.HandleKey("K", testViewport())
	if msg, _ := testenv.NoticeOKOf(v.TakeNotice()); v.pagerKey == "" || !strings.Contains(msg, "最初") {
		t.Errorf("先頭の K で閉じた / 案内が出ない: key=%q notice=%q", v.pagerKey, msg)
	}
}

// hint の語が動作と一致すること。
//
// 一覧モードの q は **glogx ごと終了**する (ユーザー要望 2026-08-06。git log へは戻らない) のに、
// hint は長らく「q: 閉じる」と出していた。README は「i/s で閉じて一覧へ戻る」「q/Esc は glogx ごと
// 終了」と 2 語を使い分けており、画面上の案内だけが古い語のまま残っていた。
//
// 🚨 全画面 pager の「d/q: 閉じる」は**正しい** (そこでの q は pager を閉じる)。両方を pin して
// 取り違えを防ぐ — 片方だけ見ていると「まとめて閉じるに戻す」変更が通ってしまう。
func TestStatusHintWordsMatchBehavior(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))

	list := v.Hint(testenv.PopupHintBudget)
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
	pager := v.Hint(testenv.PopupHintBudget)
	if !strings.Contains(pager, "d/q: 閉じる") {
		t.Errorf("pager の hint が「閉じる」でない (そこでの q は pager を閉じる): %q", pager)
	}
}

// ヘッダーの件数: unstaged は未追跡を含み、conflict は 1 件以上のときだけ先頭に出す。
func TestStatusHeaderCountsAndConflict(t *testing.T) {
	plainHeader := func(v *StatusView) string {
		return term.StripANSI(v.headerLine(StatusRenderOpts{Width: 200}, 200))
	}
	v := newTestStatusView(t, testenv.StatusRec("M  s.go", " M a.go", "?? n.txt", "UU c.go"))
	if got := plainHeader(v); !strings.Contains(got, "conflict 1 / unstaged 2 / staged 1") {
		t.Errorf("header = %q, want conflict 1 / unstaged 2 / staged 1", got)
	}
	v = newTestStatusView(t, testenv.StatusRec("M  s.go", " M a.go"))
	if got := plainHeader(v); strings.Contains(got, "conflict") || !strings.Contains(got, "unstaged 1 / staged 1") {
		t.Errorf("header = %q (conflict が無いときは conflict を出さない)", got)
	}
}

// まだ一度も読めていないうちに失敗したら、理由を一覧の列で切らずに全幅で出し、件数 (分からない数) は出さない。
func TestStatusFirstLoadFailureShowsTheWholeReason(t *testing.T) {
	v := NewStatusView()
	v.closeAnimOff, v.shown = true, true
	reason := "fatal: not a git repository (or any of the parent directories): .git — " + strings.Repeat("x", 40) + " end"
	stubWorktreeStatus(t, "", errors.New(reason))
	applyStatusLoad(t, &v)
	out := term.StripANSI(strings.Join(v.Lines(StatusRenderOpts{Width: 200, Page: 10}), "\n"))
	if !strings.Contains(out, " end") {
		t.Errorf("理由が一覧の列で切れた:\n%s", out)
	}
	if strings.Contains(out, "unstaged 0") {
		t.Errorf("読めていないのに件数を 0 と出した:\n%s", out)
	}
	if head := term.StripANSI(v.headerLine(StatusRenderOpts{Width: 200}, 200)); strings.HasSuffix(strings.TrimSpace(head), "/") {
		t.Errorf("ヘッダーの末尾に区切りだけが残った: %q", head)
	}
}

// 一度読めた後の読み直しが失敗したら、一覧は残したまま理由をヘッダーの下に出す。
func TestStatusReloadFailureShowsTheReason(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	stubWorktreeStatus(t, "", errors.New("fatal: index file corrupt"))
	applyStatusLoad(t, v)
	out := term.StripANSI(strings.Join(v.Lines(StatusRenderOpts{Width: 120, Page: 10}), "\n"))
	if !strings.Contains(out, "🚨 status 取得失敗: fatal: index file corrupt") || !strings.Contains(out, "a.go") {
		t.Errorf("読み直しの失敗の理由と last-good の一覧の両方が無い:\n%s", out)
	}
}

// 読み直しの失敗の理由の行が出ている間は本文が 1 行減るので、半ページの移動量もその本文の高さで決める。
func TestStatusHalfPageUsesTheBodyHeightWithReasonRow(t *testing.T) {
	recs := make([]string, 0, 20)
	for i := range 20 {
		recs = append(recs, fmt.Sprintf(" M f%02d.go", i))
	}
	v := newTestStatusView(t, testenv.StatusRec(recs...))
	stubWorktreeStatus(t, "", errors.New("fatal: index file corrupt"))
	applyStatusLoad(t, v)
	before := v.cursor
	v.listKey("ctrl+d", StatusViewport{page: 9})
	if got, want := v.cursor-before, listnav.Half(9-2); got != want {
		t.Errorf("半ページの移動 = %d, want %d (本文は page - ヘッダー 2 行)", got, want)
	}
}

// 確認の後に中身だけが変わった (XY は ` M` のまま。別プロセスが書き足した) ら捨てない。
func TestStatusDiscardAbortsWhenContentChangedDuringConfirm(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("a.go", []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	calls := stubGitOps(t, nil)
	v.HandleKey("X", testViewport())
	if !v.discarding {
		t.Fatal("X で確認モーダルが開いていない")
	}
	if err := os.WriteFile("a.go", []byte("v1\nB (確認の後に書き足した)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	applyCmd(t, v, v.HandleKey("y", testViewport()))
	if len(calls.restoreWork) != 0 {
		t.Fatalf("確認の後に中身が変わったのに捨てた: %v", calls.restoreWork)
	}
	if notice, ok := testenv.NoticeOKOf(v.TakeNotice()); ok || !strings.Contains(notice, "変わった") {
		t.Errorf("notice = %q (ok=%v), want 中止したことを伝える", notice, ok)
	}
}

// printOf はカーソル行の今の指紋 (テストが「プレビューを取ったときの指紋」を作るのに使う)。
func printOf(t *testing.T, v *StatusView) string {
	t.Helper()
	row, ok := v.current()
	if !ok {
		t.Fatal("カーソル行がない")
	}
	p, err := rowPrint(v.st.root, row)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// receiveAndShow はカーソル行の diff を受け取り、2 カラムで描く (= ユーザーに見せる。seenPrints は描いたときに進む)。
func receiveAndShow(t *testing.T, v *StatusView, lines []string, err error, print string) {
	t.Helper()
	row, _ := v.current()
	v.ReceivePreview(StatusPreviewMsg{gen: v.gen, key: previewKey(row), lines: lines, err: err, print: print})
	v.Lines(testStatusOpts(140, 20))
}

func writeA(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile("a.go", []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// プレビュー (画面に出した diff) を取った後に中身が変わったら、X は確認に入らずプレビューを読み直す
// (見せていない変更まで捨てない)。読み直した後なら確認に入れる。
func TestStatusDiscardRefreshesPreviewWhenContentChangedAfterPreview(t *testing.T) {
	t.Chdir(t.TempDir())
	writeA(t, "v1\n")
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	receiveAndShow(t, v, []string{"+v1"}, nil, printOf(t, v))
	writeA(t, "v1\nB (プレビューの後に書き足した)\n")
	cmd := v.HandleKey("X", testViewport())
	if v.discarding {
		t.Fatal("画面に出していない変更を含むのに、捨てる確認に入った")
	}
	if cmd == nil {
		t.Error("プレビューの読み直しを予約していない")
	}
	if notice, _ := testenv.NoticeOKOf(v.TakeNotice()); !strings.Contains(notice, "プレビューを出した後に") {
		t.Errorf("notice = %q", notice)
	}
	receiveAndShow(t, v, []string{"+v1", "+B"}, nil, printOf(t, v))
	v.HandleKey("X", testViewport())
	if !v.discarding {
		t.Error("プレビューを読み直した後でも確認に入らない")
	}
}

// 別の行の変化でプレビューのキャッシュを捨てても、最後に見せた diff の指紋は残す。捨てると取り直しが着く前の X が
// 比べるものを失い、見せていない中身で確認に入る。
func TestStatusDiscardKeepsPrintOfShownDiffAcrossCacheDrop(t *testing.T) {
	t.Chdir(t.TempDir())
	writeA(t, "v1\n")
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	stub := stubWorktreeStatus(t, testenv.StatusRec(" M a.go"), nil)
	row, _ := v.current()
	receiveAndShow(t, v, []string{"+v1"}, nil, printOf(t, v))
	stub.out = testenv.StatusRec(" M a.go", "?? new.txt") // 別の行が増えた = キャッシュを捨てる変化
	applyStatusLoad(t, v)
	if v.preview.Has(previewKey(row)) {
		t.Fatal("前提: 一覧の変化でキャッシュが捨てられていない")
	}
	writeA(t, "v1\nB (取り直しが着く前に書き足した)\n")
	v.HandleKey("X", testViewport())
	if v.discarding {
		t.Fatal("最後に見せた diff の後に変わったのに、キャッシュを捨てた後の X が確認に入った")
	}
}

// 受け取っただけで描いていない diff (1 カラムで欄が無い) は「見せた」に数えない: 全画面 diff で A を見せた後に B へ変わり、
// 裏の取り直しで B の diff が着いても、X は A と比べて断る。
func TestStatusDiscardIgnoresReceivedButUnshownDiff(t *testing.T) {
	t.Chdir(t.TempDir())
	writeA(t, "v1\n")
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	v.pagerKey = previewKey(row)
	v.ReceivePreview(StatusPreviewMsg{gen: v.gen, key: previewKey(row), lines: []string{"+v1"}, print: printOf(t, v)})
	v.Lines(testStatusOpts(80, 20)) // 1 カラム。全画面 diff で A を見せた
	v.pagerKey = ""
	writeA(t, "v1\nB (全画面 diff を閉じた後に書き足した)\n")
	v.dropPreviews()
	v.ReceivePreview(StatusPreviewMsg{gen: v.gen, key: previewKey(row), lines: []string{"+v1", "+B"}, print: printOf(t, v)})
	v.Lines(testStatusOpts(80, 20)) // 1 カラム: プレビュー欄が無いので B は描かれない
	v.HandleKey("X", testViewport())
	if v.discarding {
		t.Fatal("描いていない diff を「見せた」と数えて確認に入った")
	}
}

// diff を取れなかったときは指紋を進めない: A を見せた後に B へ変わり、取り直しが失敗して「取れなかった」が出ても、X は A と比べて断る。
func TestStatusDiscardDoesNotAdvanceSeenPrintOnFailedPreview(t *testing.T) {
	t.Chdir(t.TempDir())
	writeA(t, "v1\n")
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	receiveAndShow(t, v, []string{"+v1"}, nil, printOf(t, v))
	writeA(t, "v1\nB (取り直しが失敗する前に書き足した)\n")
	v.dropPreviews()
	receiveAndShow(t, v, nil, errors.New("git diff: timeout"), printOf(t, v))
	v.HandleKey("X", testViewport())
	if v.discarding {
		t.Fatal("取り直しが失敗した (B を見せていない) のに確認に入った")
	}
}

// tracked のパスが作業ツリーではディレクトリ (submodule・ファイルをディレクトリに置き換えた) なら X を断る: restore は中を
// 丸ごと消すのに、指紋は中のファイルの変化を見ない。
func TestStatusDiscardRefusesTrackedPathThatIsDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join("a.go", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	v := newTestStatusView(t, testenv.StatusRec(" D a.go"))
	v.HandleKey("X", testViewport())
	if v.discarding {
		t.Fatal("作業ツリーでディレクトリになっている tracked の行で確認に入った")
	}
}

// 未追跡のディレクトリの中を調べるのに時間の上限を越えたら、X を断る (stat の遅いマウントで UI を止めない)。
func TestStatusDiscardRefusesUntrackedDirOverPrintTime(t *testing.T) {
	t.Chdir(t.TempDir())
	v := newTestStatusView(t, testenv.StatusRec("?? junk/"))
	listUntrackedFiles = func(string) (string, error) { return "junk/f\x00", nil } // 1 件でも (256 件ごとの判定にしない)
	testenv.Restore(t, &rowPrintMaxDuration)
	rowPrintMaxDuration = 0
	v.HandleKey("X", testViewport())
	if v.discarding {
		t.Fatal("時間の上限を越えたのに確認に入った")
	}
}

// r は走行中の読み込みと自動更新の札も降ろして張り直す (gen を進めると、飛んでいる結果と旧い tick は着いても捨てられる)。
func TestStatusReloadRearmsLoadAndPoll(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	v.loading, v.pollArmed = true, true // 読み込み中・自動更新を張った状態で r
	cmd := v.HandleKey("r", testViewport())
	if cmd == nil {
		t.Fatal("r が何も予約しない")
	}
	// 予約の中に読み込み (StatusLoadMsg を返す Cmd) があるか。tick の Cmd は待つので、各 Cmd を goroutine で走らせ、
	// 読み込みの結果だけを待つ (札が立ったままだと LoadCmd が nil を返し、読み込みが来ない)
	got := make(chan tea.Msg, 8)
	run := func(c tea.Cmd) {
		if c != nil {
			go func() { got <- c() }()
		}
	}
	if b, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range b {
			run(c)
		}
	} else {
		run(cmd)
	}
	// 自動更新の tick (statusPollInterval) も新しい世代で届くこと。旧い世代の tick は捨てられるので、張り直さないとチェーンが切れる
	deadline := time.After(statusPollInterval + 5*time.Second) // hang guard
	load, poll := false, false
	for !load || !poll {
		select {
		case msg := <-got:
			switch m := msg.(type) {
			case StatusLoadMsg:
				load = m.gen == v.gen
			case StatusPollMsg:
				poll = m.gen == v.gen
			}
		case <-deadline:
			t.Fatalf("r の後の予約が足りない: 読み込み=%v 自動更新=%v (取得中・自動更新の札が立ったまま)", load, poll)
		}
	}
}

// 指紋: 作業ツリーは stat (書き直せば変わる)、symlink はリンク先、無いものは absent。
func TestStatPrintTracksChanges(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	if got := statPrint(f); got != "absent" {
		t.Errorf("無いファイルの指紋 = %q", got)
	}
	if err := os.WriteFile(f, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	p1 := statPrint(f)
	if err := os.WriteFile(f, []byte("b"), 0o644); err != nil { // 大きさは同じ
		t.Fatal(err)
	}
	if statPrint(f) == p1 {
		t.Error("同じ大きさで書き直しても指紋が同じ")
	}
	l := filepath.Join(dir, "l")
	if err := os.Symlink("f", l); err != nil {
		t.Fatal(err)
	}
	if got := statPrint(l); !strings.HasSuffix(got, ` link:"f"`) {
		t.Errorf("symlink の指紋 = %q, want リンク先 f (辿らない)", got)
	}
}

// 指紋は index 側の行も含む (MM の行で index だけが変わると、restore が書き戻す中身が変わる)。
func TestRowPrintIncludesIndexEntry(t *testing.T) {
	t.Chdir(t.TempDir())
	v := newTestStatusView(t, testenv.StatusRec("MM a.go"))
	index := "100644 aaaa 0\ta.go"
	readIndexEntries = func([]string) (string, error) { return index, nil }
	v.cursor = firstRowOfSection(v.rows, sectionUnstaged)
	before := printOf(t, v)
	index = "100644 bbbb 0\ta.go"
	if printOf(t, v) == before {
		t.Error("index の blob が変わっても指紋が同じ")
	}
}

// 未追跡のディレクトリの中が上限を超えたら、先頭だけで「同じ」と言わずに X を断る。
func TestStatusDiscardRefusesUntrackedDirOverPrintLimit(t *testing.T) {
	t.Chdir(t.TempDir())
	v := newTestStatusView(t, testenv.StatusRec("?? junk/"))
	listUntrackedFiles = func(string) (string, error) {
		return strings.Repeat("junk/f\x00", rowPrintMaxEntries+1), nil
	}
	v.HandleKey("X", testViewport())
	if v.discarding {
		t.Fatal("上限を超えたディレクトリで確認に入った")
	}
	if notice, _ := testenv.NoticeOKOf(v.TakeNotice()); !strings.Contains(notice, "確かめられない") {
		t.Errorf("notice = %q", notice)
	}
}

// repo root が分からないときは X を断る (rows は root 相対。cwd に落とすと指紋が別のファイルを見る)。
func TestStatusDiscardRefusesWithoutRepoRoot(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	v.st.root = ""
	v.HandleKey("X", testViewport())
	if v.discarding {
		t.Fatal("root が分からないのに確認に入った")
	}
}

// fetchDiff の指紋は repo root と結合したパスで採り (サブディレクトリから起動しても root のファイルを見る)、diff の取得の
// 前後で食い違えば "" (確かめられなかった) にする (取得中に変わって戻る形で、画面の diff が指紋の状態のものでなくなる)。
func TestStatusFetchDiffPrintIsRootJoinedAndEmptyWhenChangedDuringDiff(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	v.st.root = root
	t.Chdir(t.TempDir()) // cwd は root と別 (サブディレクトリから起動した形)
	fetch := func(during func()) StatusPreviewMsg {
		t.Helper()
		testenv.Restore(t, &loadWorktreeDiff)
		loadWorktreeDiff = func([]string, bool, bool) ([]string, error) { during(); return []string{"+v1"}, nil }
		v.dropPreviews()
		row, _ := v.current()
		cmd := v.fetchDiff(row, false)
		if cmd == nil {
			t.Fatal("fetchDiff が取得を発行しない")
		}
		msg, ok := cmd().(StatusPreviewMsg)
		if !ok {
			t.Fatalf("msg = %T", msg)
		}
		v.ReceivePreview(msg) // 取得中の札を降ろす (次の fetch が発行されるように)
		return msg
	}
	want := statPrint(path) // rowPrint を通さずに絶対パスで採る (結合を壊すと、期待値も一緒に absent になる形を避ける)
	if want == "absent" || !strings.Contains(fetch(func() {}).print, want) {
		t.Errorf("指紋に root のファイルの stat (%q) が入っていない", want)
	}
	msg := fetch(func() {
		if err := os.WriteFile(path, []byte("v1\nB (diff の取得中に書き足した)\n"), 0o644); err != nil {
			t.Error(err)
		}
	})
	if msg.print != "" {
		t.Errorf("diff の取得中に変わったのに指紋を付けた: %q", msg.print)
	}
}

// 未追跡ファイルのプレビューは行数の上限ちょうどなら省略しない。1 行超えたら上限で切って案内を付ける。
func TestUntrackedPreviewLineCapBoundary(t *testing.T) {
	dir := t.TempDir()
	write := func(n int) string {
		p := filepath.Join(dir, fmt.Sprintf("f%d.txt", n))
		if err := os.WriteFile(p, []byte(strings.Repeat("x\n", n)), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	got, err := untrackedPreview(write(statusPreviewMaxLines), false)
	if err != nil || len(got) != statusPreviewMaxLines || strings.Contains(got[len(got)-1], "省略") {
		t.Errorf("ちょうど上限: %d 行 / 末尾 %q / err %v (省略しないはず)", len(got), got[len(got)-1], err)
	}
	got, err = untrackedPreview(write(statusPreviewMaxLines+1), false)
	if err != nil || len(got) != statusPreviewMaxLines+1 || !strings.Contains(got[statusPreviewMaxLines], "省略") {
		t.Errorf("上限 + 1: %d 行 / err %v (上限で切って案内を付けるはず)", len(got), err)
	}
}

// 元のパスが続かないまま出力が終わった rename / copy のレコード (壊れた出力) は、元なしの行として読む (範囲の外を読まない)。
func TestParseWorktreeStatusRenameAtEndWithoutOrigin(t *testing.T) {
	st := parseWorktreeStatus("R  new.txt") // 末尾の NUL も無い (出力が途中で切れた)
	if len(st.rows) != 1 || st.rows[0].path != "new.txt" || st.rows[0].orig != "" {
		t.Fatalf("rows = %+v (元なしの rename 1 行のはず)", st.rows)
	}
}

// 確認の前に別プロセスが stage して XY が変わっても (` M` → `MM`)、最後に見せた diff の指紋で比べる (キーに XY を含めると引けなくなる)。
func TestStatusDiscardComparesShownPrintAcrossXYChange(t *testing.T) {
	t.Chdir(t.TempDir())
	writeA(t, "v1\n")
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	stub := stubWorktreeStatus(t, testenv.StatusRec(" M a.go"), nil)
	receiveAndShow(t, v, []string{"+v1"}, nil, printOf(t, v))
	stub.out = testenv.StatusRec("MM a.go") // 別プロセスが stage して、さらに書き足した
	applyStatusLoad(t, v)
	v.cursor = firstRowOfSection(v.rows, sectionUnstaged)
	writeA(t, "v1\nB (stage の後に書き足した)\n")
	v.HandleKey("X", testViewport())
	if v.discarding {
		t.Fatal("XY が変わった後に、最後に見せた diff と比べずに確認に入った")
	}
}

// 本文が 1 行も入らない高さで描いても、見せたことにしない (見出しだけが描かれる)。
func TestStatusPreviewTooShortIsNotSeen(t *testing.T) {
	t.Chdir(t.TempDir())
	writeA(t, "v1\n")
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	v.ReceivePreview(StatusPreviewMsg{gen: v.gen, key: previewKey(row), lines: []string{"+v1"}, print: printOf(t, v)})
	v.Lines(testStatusOpts(140, 3)) // ヘッダー 1 行 + 本文 2 行 = プレビューの見出しと空行だけ
	if _, ok := v.seenPrints[seenKey(previewKey(row))]; ok {
		t.Error("diff の本文を 1 行も描いていないのに、見せたことにした")
	}
	v.Lines(testStatusOpts(140, 20))
	if _, ok := v.seenPrints[seenKey(previewKey(row))]; !ok {
		t.Error("前提: 本文が入る高さで描いても見せたことにならない")
	}
}

// プレビューの取得中に r を押すと、旧い世代の結果は捨てるが、捨てたときに取り直しを予約する (r が張った取り直しは
// 取得中の札で弾かれているので、予約しないとプレビューが空のまま残る)。
func TestStatusReloadDuringPreviewFetchStillRefetches(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	fetch := v.fetchDiff(row, false)
	if fetch == nil {
		t.Fatal("前提: 取得が発行されない")
	}
	v.HandleKey("r", testViewport())
	msg, ok := fetch().(StatusPreviewMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	if cmd := v.ReceivePreview(msg); cmd == nil {
		t.Fatal("旧い世代の結果を捨てたのに、取り直しを予約しない")
	}
	if cmd := v.fetchDiff(row, false); cmd == nil {
		t.Error("札が降りていない (取り直しが弾かれる)")
	}
}

// 全画面 diff は窓が低くても本文が残る (箱を中央に重ねる) ので、見せたことにする。
func TestStatusPagerOnShortWindowIsSeen(t *testing.T) {
	t.Chdir(t.TempDir())
	writeA(t, "v1\n")
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	v.pagerKey = previewKey(row)
	v.ReceivePreview(StatusPreviewMsg{gen: v.gen, key: previewKey(row), lines: []string{"+v1"}, print: printOf(t, v)})
	v.Lines(testStatusOpts(80, 1))
	if _, ok := v.seenPrints[seenKey(previewKey(row))]; ok {
		t.Error("高さ 1 の全画面 diff (上辺の見出しだけ) で、見せたことにした")
	}
	v.Lines(testStatusOpts(80, 2)) // 上辺と本文の 1 行が見える境界
	if _, ok := v.seenPrints[seenKey(previewKey(row))]; !ok {
		t.Error("高さ 2 の全画面 diff で本文を見せたのに、見せたことにしていない")
	}
}

// 閉じて開き直した後に旧い世代の結果が着いても、新しい世代の取得の札は降ろさず、取り直しも予約しない (同じキーを二重に取らない)。
func TestStatusStalePreviewDoesNotReleaseNewGenerationMark(t *testing.T) {
	v := newTestStatusView(t, testenv.StatusRec(" M a.go"))
	row, _ := v.current()
	old := v.fetchDiff(row, false)
	if old == nil {
		t.Fatal("前提: 取得が発行されない")
	}
	staleMsg, ok := old().(StatusPreviewMsg)
	if !ok {
		t.Fatalf("msg = %T", staleMsg)
	}
	v.close()
	v.FinishClose()
	v.shown = true
	if v.fetchDiff(row, false) == nil {
		t.Fatal("前提: 開き直した後の取得が発行されない")
	}
	if cmd := v.ReceivePreview(staleMsg); cmd != nil {
		t.Error("旧い世代の結果で取り直しを予約した (新しい世代の取得と二重になる)")
	}
	if !v.preview.Loading(previewKey(row)) {
		t.Error("旧い世代の結果が、新しい世代の取得の札を降ろした")
	}
}

// 時間の上限は 1 件ごとの stat の後にも見る (一覧は速く、最後の stat で越えた分も数える)。
func TestStatusDiscardRefusesWhenLastStatExceedsPrintTime(t *testing.T) {
	t.Chdir(t.TempDir())
	v := newTestStatusView(t, testenv.StatusRec("?? junk/"))
	listUntrackedFiles = func(string) (string, error) { return "junk/f\x00", nil }
	testenv.Restore(t, &statOf)
	testenv.Restore(t, &rowPrintMaxDuration)
	rowPrintMaxDuration = 10 * time.Millisecond
	statOf = func(p string) string {
		time.Sleep(50 * time.Millisecond) // sleep-ok: window: 遅い stat (マウントの遅さ) を演じる入力
		return statPrint(p)
	}
	v.HandleKey("X", testViewport())
	if v.discarding {
		t.Fatal("最後の stat で時間の上限を越えたのに確認に入った")
	}
}
