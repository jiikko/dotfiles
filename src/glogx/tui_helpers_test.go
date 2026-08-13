package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestMain はパッケージ全体のキャッシュ置き場を一時ディレクトリへ逃がす。
//
// ⚠️ 実ユーザーの ~/.cache/glog を触らせないため: quit() は「最後に見ていた画面」を保存/削除する
// (issues_state.go) ので、隔離しないと make test が開発者の記憶を消す。CI キャッシュ
// (cache.go) と claude バージョンキャッシュも同じ base を使う。個別テストの
// t.Setenv("XDG_CACHE_HOME", ...) は従来どおり上書きできる。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "glogx-test-cache")
	if err == nil {
		if err := os.Setenv("XDG_CACHE_HOME", dir); err != nil {
			panic(err) // 隔離できないまま走らせると実ユーザーのキャッシュを触る
		}
	}
	code := m.Run() // ⚠️ os.Exit は defer を走らせないので、片付けは Run の後に手で書く
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}

func newTestBrowse(t *testing.T, n int, statuses map[string]CIState, toFetch []string) *browseModel {
	t.Helper()
	commits := make([]Commit, n)
	for i := range commits {
		sha := strings.Repeat(string(rune('a'+i)), 40)
		commits[i] = Commit{
			SHA: sha, ShortSHA: sha[:7], Subject: "subject", Author: "koji", AuthorEmail: "k@x",
			Date: "Thu Jul 16 19:12:47 2026 +0900", RelDate: "now", Message: "subject",
		}
	}
	// NoFrame: true = 最外周フレームを明示 OFF (issue 025)。既存の View/overlay/panel テストの
	// 期待値を変えない。現行 80×10 は frameMinHeight 未満で自動 OFF だが、途中で width/height を
	// 大きくするテストが誤ってフレームを踏まないよう明示 OFF を決定的にする。
	m := newBrowseModel(commits, statuses, toFetch, Repo{Owner: "o", Name: "r"}, true,
		&Options{NoFrame: true}, false, 80, 10)
	t.Cleanup(m.cancel)
	// ⚠️ 開閉の演出はテストでは切る (zoom.go)。View の期待値が「中央から開く途中の姿」に
	// なると全テストが読めなくなるため。演出そのものは zoom_test.go が直接検査する。
	m.zoom.off = true
	// issues viewer の閉じる演出も同じ理由で切る (既存テストは i / q で即座に閉じる前提)。
	// 演出そのものは issues_close_anim_test.go が明示的に on にして検査する。
	m.issuesOv.closeAnimOff = true
	// status viewer の閉じる演出も同じ理由で切る (s の toggle が「即座に閉じている」前提で読める
	// ように)。演出そのものは status_view_test.go の TestSlideLeftWindow 等が直接検査する。
	m.statusOv.closeAnimOff = true
	return m
}

// stubClock は timeNow を固定時刻 Unix(1000, 0) に差し替え、テスト終了時に戻す。返す advance で
// 時計を進める (演出の進捗・経過時間の判定を決定的にする)。実時間に戻したいテストは従来どおり
// 自前で退避する。⚠️ 基準時刻を変えないこと: tui_panel_test.go の ETA fixture (StartedAt に
// Unix(880/910/940) 等) が「Unix(1000) から見た相対時間」で組まれている。
func stubClock(t *testing.T) (advance func(time.Duration)) {
	t.Helper()
	orig := timeNow
	t.Cleanup(func() { timeNow = orig })
	now := time.Unix(1000, 0)
	timeNow = func() time.Time { return now }
	return func(d time.Duration) { now = now.Add(d) }
}

// releaseKey は「指を離した」ことにする (キーリピート判定をリセットする。swallowKeyRepeat)。
// ⚠️ テストは同じキーを瞬間的に 2 回押すが、実機ではありえない速さなので自動リピート扱いに
// なる。意図的な 2 回目であることをテスト側で明示する (実機では 300ms 空ければ同じ)。
func releaseKey(m *browseModel) {
	m.lastKey, m.lastKeyAt = "", time.Time{}
}

func statusesFor(m *browseModel, state CIState) map[string]CIState {
	s := map[string]CIState{}
	for _, c := range m.commits {
		s[c.SHA] = state
	}
	return s
}

// deliverMsgs は tea.Cmd の結果 msg を BatchMsg の入れ子ごと再帰展開し、match が true を
// 返した msg だけを m.Update へ届ける (tick 等の無関係な msg で状態を進めないためのフィルタ)。
func deliverMsgs(m *browseModel, msg tea.Msg, match func(tea.Msg) bool) {
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				deliverMsgs(m, c(), match)
			}
		}
		return
	}
	if match(msg) {
		m.Update(msg)
	}
}

// withJobs は commit idx の details を job 2 件で埋めるテストヘルパー。
func withJobs(m *browseModel, idx int) {
	m.details[m.commits[idx].SHA] = []CheckDetail{
		{Name: "build", State: StateSuccess, URL: "https://github.com/o/r/runs/1"},
		{Name: "lint", State: StateFailure, URL: ""},
	}
}

// stubClipboardFunc は copyToClipboard を fn に差し替え、テスト終了時に戻す (失敗系・独自記録用)。
func stubClipboardFunc(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := copyToClipboard
	copyToClipboard = fn
	t.Cleanup(func() { copyToClipboard = orig })
}

// stubClipboard は copyToClipboard を「記録して成功する」実装に差し替え、記録先を返す (多数派の形)。
func stubClipboard(t *testing.T) *string {
	t.Helper()
	var copied string
	stubClipboardFunc(t, func(text string) error { copied = text; return nil })
	return &copied
}

// stubBrowserFunc は openInBrowser を fn に差し替え、テスト終了時に戻す (ガード・複数記録用)。
func stubBrowserFunc(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := openInBrowser
	openInBrowser = fn
	t.Cleanup(func() { openInBrowser = orig })
}

// stubBrowser は openInBrowser を「最後に開いた URL を記録して成功する」実装に差し替え、記録先を返す。
func stubBrowser(t *testing.T) *string {
	t.Helper()
	var opened string
	stubBrowserFunc(t, func(url string) error { opened = url; return nil })
	return &opened
}

// stubEditorCapture は runEditorCmd を「起動せず *exec.Cmd を記録する」実装へ差し替える。
//
// ⚠️ エディタ連携のテストは全部これを使う。回数だけ数える stub も昔あったが、それだと
// 「何を開いたか」が見えず、渡す対象を取り違えた実装 (iss.Path → iss.Dir 等) を通してしまう。
// 差し替え点を 1 つに保つのは、runEditorCmd の契約 (非 nil な Cmd を返す) が変わったときに
// 直す箇所を 1 箇所にするため。
//
// エディタを起動するキーは 3 系統ある: issues viewer の e (別名 v) = その issue の実ファイル /
// git log 一覧の e = repo root を `nvim .` / job パネルの v = 標準入力の scratch。
func stubEditorCapture(t *testing.T) *[]*exec.Cmd {
	t.Helper()
	var cmds []*exec.Cmd
	orig := runEditorCmd
	runEditorCmd = func(c *exec.Cmd) tea.Cmd {
		cmds = append(cmds, c)
		return func() tea.Msg { return editorClosedMsg{} }
	}
	t.Cleanup(func() { runEditorCmd = orig })
	return &cmds
}

// stubDiff は loadCommitDiff を差し替え、呼び出し記録と固定行を返す。
func stubDiff(t *testing.T, lines []string, err error) *[]string {
	t.Helper()
	var calls []string
	orig := loadCommitDiff
	loadCommitDiff = func(sha string, colored bool) ([]string, error) {
		calls = append(calls, sha)
		return lines, err
	}
	t.Cleanup(func() { loadCommitDiff = orig })
	return &calls
}

// runCmd は tea.Cmd (tea.Batch 含む) を同期実行して diffMsg を探して Update へ流す。
func deliverDiffMsg(t *testing.T, m *browseModel, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("cmd が nil (diff 取得コマンドが返っていない)")
	}
	var deliver func(msg tea.Msg)
	deliver = func(msg tea.Msg) {
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				if c != nil {
					deliver(c())
				}
			}
		case diffMsg:
			m.Update(v)
		}
	}
	deliver(cmd())
}

// runCmdTree は cmd を再帰実行する。tea.Batch (BatchMsg) は各要素を辿るので、開く Cmd が
// maybeTick と束ねられていても副作用 (openInBrowser など) が発火する。
func runCmdTree(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if msg, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range msg {
			runCmdTree(c)
		}
	}
}

// withFailedJob は commit idx の details を「失敗 job (CheckID あり)」1 件で埋める。
func withFailedJob(m *browseModel, idx int, checkID int64, state CIState) {
	m.details[m.commits[idx].SHA] = []CheckDetail{
		{Name: "lint", State: state, URL: "https://github.com/o/r/runs/9", CheckID: checkID},
	}
}

// uniformWidth は box 描画行の表示幅が全行で揃っている (枠が崩れていない) ことを検証して
// その幅を返す。diff / job 詳細のスクロールバーテストが共有する (同一クロージャが 2 ファイルへ
// コピーされていた。issue 030)。
func uniformWidth(t *testing.T, box []string) int {
	t.Helper()
	w := dispWidth(stripANSI(box[0]))
	for i, l := range box {
		if got := dispWidth(stripANSI(l)); got != w {
			t.Fatalf("行 %d の表示幅 = %d, 他の行 = %d: %q", i, got, w, l)
		}
	}
	return w
}
