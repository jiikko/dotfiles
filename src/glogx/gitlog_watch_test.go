package main

// git log の見張り (gitlog_watch.go) の検査。
//
// 指紋の一致は実 git で 1 回通す (TestGitLogFingerprintBaseline...): BuildFingerprintArgs の
// 出力形式と fingerprintSHAs / Commit.SHA の突き合わせは「測定表の読み違い」が起きうる箇所で、
// フェイクだけでは自分の思い込みを検査できない。チェーンの規律 (single-flight / 世代 / 見送り) は
// フェイクで環境非依存に固定する。

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// realRepoBrowse は実 git repo (subjects のコミット付き) を作り、そこへ chdir した状態の
// browseModel を返す。このファイルの 10 テストが同じ 9 行のプロローグを繰り返していたのを
// 1 行にしたもの (issue 199)。
//
// ⚠️ 3 つとも返すこと。dir は追加コミット (commitLines) に、opts は BuildFingerprintArgs /
// LoadCommits に個別に要る。どれかを隠すと呼び出し側が結局自前で組み直す。
// ⚠️ newTempRepo は t.Chdir する副作用を持つので、LoadCommits より先に呼ぶ順序を変えない。
//
// ここに置くのは意図的で、tui_helpers_test.go へは上げない。実 git repo に依存するのは
// このファイルだけで、共有語彙に見せると読み手が「全 main テストの前提」と誤解する。
func realRepoBrowse(t *testing.T, height int, subjects ...string) (*browseModel, string, *Options) {
	t.Helper()
	dir := newTempRepo(t, subjects)
	opts := &Options{MaxCount: 20, NoFrame: true}
	commits, err := LoadCommits(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	m := newBrowseModel(commits, map[string]CIState{}, nil, Repo{}, false, opts, false, 80, height)
	t.Cleanup(m.cancel)
	m.zoom.off = true
	return m, dir, opts
}

func TestBuildFingerprintArgsSkipsBodyAndColor(t *testing.T) {
	opts := &Options{MaxCount: 7, Stat: true, Patch: true, Revs: []string{"HEAD"}, Paths: []string{"a.go"}}
	args := BuildFingerprintArgs(opts)
	for _, bad := range []string{"--stat", "--patch", "--color=always"} {
		if slices.Contains(args, bad) {
			t.Errorf("指紋の問い合わせに %s が混ざっている (本文まで読むと -p の巨大 patch を毎分取る): %v", bad, args)
		}
	}
	for _, want := range []string{fingerprintFormat, "--max-count=7", "--color=never", "HEAD", "--", "a.go"} {
		if !slices.Contains(args, want) {
			t.Errorf("指紋の問い合わせに %s が無い: %v", want, args)
		}
	}
}

func TestFingerprintSHAsMatchesCommits(t *testing.T) {
	fp := func(lines ...string) string { return strings.Join(lines, "\n") }
	commits := []Commit{{SHA: "aaa"}, {SHA: "bbb"}}
	cases := []struct {
		name string
		fp   string
		want bool
	}{
		{"一致 (decoration つき)", fp("aaa"+fieldSep+"HEAD -> main", "bbb"+fieldSep+""), true},
		{"末尾改行つきでも一致", fp("aaa"+fieldSep, "bbb"+fieldSep) + "\n", true},
		{"順序が違う", fp("bbb"+fieldSep, "aaa"+fieldSep), false},
		{"件数が違う", fp("aaa" + fieldSep), false},
		{"空 (コミットがある)", "", false},
	}
	for _, c := range cases {
		if got := gitLogFPMatchesCommits(c.fp, commits); got != c.want {
			t.Errorf("%s: gitLogFPMatchesCommits = %v, want %v", c.name, got, c.want)
		}
	}
	if !gitLogFPMatchesCommits("", nil) {
		t.Error("コミット 0 件の repo で空の指紋が不一致になった (起動直後に空振りの再読込が走る)")
	}
}

// ポップアップを開いている間は測らない (見送り)。測ると reloadLog が開いている内容の
// キャッシュを消すため、気づいても反映できない = fork が無駄になる。
func TestGitLogProbeSkipsMeasureWhileOverlayOpen(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.diffOv.open(m.commits[0].SHA)
	if !m.gitLogReloadDeferred() {
		t.Fatal("diff ポップアップを開いても見送り状態にならない")
	}
	if cmd := m.handleGitLogProbe(gitLogProbeMsg{}); cmd == nil {
		t.Fatal("見送っても次の観測は予約し続けること (予約を止めると閉じた後に気づけない)")
	}
	if m.logWatch.measuring {
		t.Error("見送るべき状態で指紋の測定を始めた")
	}
	if m.logWatch.hasSeen {
		t.Error("見送りで基準を触った (触ると閉じた後の観測で変化を見落とす)")
	}
	m.diffOv.close()
	m.handleGitLogProbe(gitLogProbeMsg{})
	if !m.logWatch.measuring {
		t.Error("ポップアップを閉じた後の観測で測定が始まらない")
	}
	// 測定中は二重に測らない (git を並行に起こさない)
	m.handleGitLogProbe(gitLogProbeMsg{})
	if !m.logWatch.measuring {
		t.Error("測定中の札が降りた")
	}
}

// 届けたチェーンの札だけを降ろす。両方降ろすと、まだブロックしている goroutine が居るのに
// 札が空いて 2 本目が張られる (観測 1 回ごとに goroutine が 1 本積み上がる)。
func TestGitLogProbeDropsOnlyDeliveringChain(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.diffOv.open(m.commits[0].SHA) // 測定を挟まず札の扱いだけを見る
	m.logWatch.w = nil              // イベント経路は張り直せない = 札の変化がそのまま見える
	m.logWatch.evArmed, m.logWatch.pollArmed = true, true
	if cmd := m.handleGitLogProbe(gitLogProbeMsg{fromEvent: true}); cmd != nil {
		t.Error("イベント経路の観測でポーリングのチェーンが二重に張られた")
	}
	if m.logWatch.evArmed {
		t.Error("届けたチェーン (イベント) の札が降りていない")
	}
	m.logWatch.evArmed, m.logWatch.pollArmed = true, true
	m.handleGitLogProbe(gitLogProbeMsg{})
	if !m.logWatch.evArmed {
		t.Error("ポーリングの観測でイベント経路の札まで降ろした")
	}
}

func TestGitLogWatchIgnoresStaleGeneration(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.logWatch.gen = 2
	m.logWatch.seen, m.logWatch.hasSeen = "base", true
	if cmd := m.handleGitLogProbe(gitLogProbeMsg{gen: 1}); cmd != nil {
		t.Error("古い世代の合図で観測が張り直された")
	}
	if m.logWatch.measuring {
		t.Error("古い世代の合図で測定が始まった")
	}
	if cmd := m.handleGitLogFP(gitLogFPMsg{fp: "changed", ok: true, gen: 1}); cmd != nil {
		t.Error("古い世代の測定結果が反映された")
	}
	if m.logWatch.seen != "base" || !m.logWatch.hasSeen {
		t.Errorf("古い世代の測定結果で基準が動いた: hasSeen=%v seen=%q", m.logWatch.hasSeen, m.logWatch.seen)
	}
}

// watcher が死んだらイベントを諦めてポーリングだけで続ける (無音にしない)。
func TestGitLogWatchClosedFallsBackToPolling(t *testing.T) {
	fw := fakeWatcherSeam(t)
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.logWatch.dirs = []string{t.TempDir()}
	m.startGitLogWatch()
	if m.logWatch.w != fw {
		t.Fatalf("startGitLogWatch が newDirWatcher seam を通っていない (w=%T)", m.logWatch.w)
	}
	m.logWatch.evArmed, m.logWatch.pollArmed = true, true
	m.logWatch.measuring = true // 測定が飛んでいる最中に watcher が死んだ
	gen := m.logWatch.gen
	cmd := m.handleGitLogProbe(gitLogProbeMsg{fromEvent: true, closed: true, gen: gen})
	if cmd == nil {
		t.Fatal("watcher が死んだ後に観測が予約されない (以降ずっと無音になる)")
	}
	if m.logWatch.w != nil {
		t.Error("死んだ watcher を掴んだまま")
	}
	if m.logWatch.gen == gen {
		t.Error("世代を進めていない (旧チェーンの観測が新しい状態へ効く)")
	}
	if !m.logWatch.pollArmed {
		t.Error("ポーリングのチェーンが張り直されていない")
	}
	if m.logWatch.measuring {
		t.Error("飛んでいる測定の札が降りていない (世代違いで結果が捨てられるので、以降ひとつも測らなくなる)")
	}
}

// 測れなかった (git の失敗 / timeout) は「変化なし」でも「変化あり」でもない: 基準を汚さない。
func TestGitLogFPUnmeasuredKeepsBaseline(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.logWatch.seen, m.logWatch.hasSeen, m.logWatch.measuring = "base", true, true
	if cmd := m.handleGitLogFP(gitLogFPMsg{}); cmd != nil {
		t.Error("測れなかった結果で何か起きた")
	}
	if !m.logWatch.hasSeen || m.logWatch.seen != "base" {
		t.Errorf("測れなかったのに基準を上書きした: hasSeen=%v seen=%q", m.logWatch.hasSeen, m.logWatch.seen)
	}
	if m.logWatch.measuring {
		t.Error("測定の札が降りていない (以降ずっと測れなくなる)")
	}
}

// 起動時の読み込み〜初回測定の窓に入った変化を「元からそう」と飲み込まない。
// 実 git で測るので、指紋の形式と Commit.SHA の突き合わせ自体も検査している。
func TestGitLogFingerprintBaselineCatchesStartupWindow(t *testing.T) {
	m, dir, opts := realRepoBrowse(t, 10, "c1", "c2")

	fp, err := runGit(BuildFingerprintArgs(opts)...)
	if err != nil {
		t.Fatal(err)
	}
	m.handleGitLogProbe(gitLogProbeMsg{}) // 測定が飛んでいる状態を作る (札を立てるのは probe の仕事)
	if cmd := m.handleGitLogFP(gitLogFPMsg{fp: fp, ok: true}); cmd != nil {
		t.Error("表示中のコミット列と一致する指紋で再読込が走った (起動直後に必ず空振りする)")
	}
	if !m.logWatch.hasSeen || m.logWatch.seen != fp {
		t.Errorf("一致した指紋が基準にならなかった: hasSeen=%v seen=%q", m.logWatch.hasSeen, m.logWatch.seen)
	}
	if m.toast.visible() {
		t.Errorf("変化していないのにトーストが出た: %q", m.toast.text)
	}

	// 「読み込みの後に commit された」= 基準がまだ無い状態で、手元と食い違う指紋が届く
	commitLines(t, dir, 3, "c3")
	fp2, err := runGit(BuildFingerprintArgs(opts)...)
	if err != nil {
		t.Fatal(err)
	}
	m.logWatch.hasSeen = false // reloadLog が基準を降ろした直後と同じ状態
	m.handleGitLogProbe(gitLogProbeMsg{})
	cmd := m.handleGitLogFP(gitLogFPMsg{fp: fp2, ok: true})
	if cmd == nil {
		t.Fatal("手元のコミット列と食い違う指紋を基準にして飲み込んだ")
	}
	runGitLogReload(t, m, cmd)
	if len(m.commits) != 3 || m.commits[0].Subject != "c3" {
		t.Fatalf("再読込されていない: %d 件 先頭=%q", len(m.commits), m.commits[0].Subject)
	}
	// 反映後は基準を降ろしたままにする契約 (次の測定が手元のコミット列と突き合わせて作り直す)。
	// 測定値を基準に置くと、読み直しが測定より新しい状態を読んだときに空振りの再読込が 1 回増える。
	if m.logWatch.hasSeen {
		t.Errorf("反映後に測定値を基準にした: %q", m.logWatch.seen)
	}
}

// 途中を読んでいるときは、見ているコミットが同じ画面行に残る (ユーザー選定 2026-09-01)。
func TestGitLogReflectKeepsAnchorRow(t *testing.T) {
	m, dir, _ := realRepoBrowse(t, 10, "c1", "c2", "c3", "c4")
	m.cursor = 2
	m.ensureCursorVisible()
	anchor := m.commits[2].SHA // ヘルパー化前は LoadCommits の戻り値を直接見ていた (同じもの)
	rowBefore := headerLineIndex(m.lines(), m.cursor) - m.offset

	commitLines(t, dir, 9, "c5")
	m.logWatch.seen, m.logWatch.hasSeen = "before", true
	m.handleGitLogProbe(gitLogProbeMsg{})
	cmd := m.handleGitLogFP(gitLogFPMsg{fp: "after", ok: true})
	if cmd == nil {
		t.Fatal("指紋が変わったのに反映されない")
	}
	runGitLogReload(t, m, cmd)
	if len(m.commits) != 5 || m.commits[0].Subject != "c5" {
		t.Fatalf("再読込されていない: %d 件 先頭=%q", len(m.commits), m.commits[0].Subject)
	}
	if m.commits[m.cursor].SHA != anchor {
		t.Errorf("カーソルが別のコミットへ移った: %s (錨 %s)", m.commits[m.cursor].SHA, anchor)
	}
	if row := headerLineIndex(m.lines(), m.cursor) - m.offset; row != rowBefore {
		t.Errorf("見ている行が動いた: 画面行 %d → %d", rowBefore, row)
	}
	// offset の算術と独立に、実際に描かれる行の中身で確かめる (同じ画面行に同じコミットが来ているか)
	if lines := m.lines(); m.offset+rowBefore < len(lines) {
		if got := stripANSI(lines[m.offset+rowBefore].Text); !strings.Contains(got, anchor) {
			t.Errorf("同じ画面行に別の内容が来た: %q (錨 %s)", got, anchor)
		}
	} else {
		t.Errorf("錨の画面行が範囲外になった: offset=%d row=%d 行数=%d", m.offset, rowBefore, len(lines))
	}
	if m.pullAnimating {
		t.Error("途中を読んでいるのに降らせる演出が始まった (行がずれる)")
	}
	if !m.toast.visible() || !strings.Contains(m.toast.text, "1 件") {
		t.Errorf("新規コミット件数のトーストが出ない: visible=%v text=%q", m.toast.visible(), m.toast.text)
	}
	// 反映後は基準を降ろしたままにする (測定値を基準にすると、読み直しが測定より新しい状態を
	// 読んだときに「表示は変わらないのにトーストだけ出る」再読込が 1 回増える)
	if m.logWatch.hasSeen {
		t.Errorf("反映後に測定値を基準にした: %q", m.logWatch.seen)
	}
}

// カーソルが先頭にいるときは pull と同じ演出 (新規行が上から降り、カーソルは先頭)。
func TestGitLogReflectAtTopFallsToPullAnim(t *testing.T) {
	m, dir, _ := realRepoBrowse(t, 24, "c1", "c2", "c3", "c4")
	m.cursor, m.offset = 0, 0

	commitLines(t, dir, 9, "c5")
	m.logWatch.seen, m.logWatch.hasSeen = "before", true
	m.handleGitLogProbe(gitLogProbeMsg{})
	runGitLogReload(t, m, m.handleGitLogFP(gitLogFPMsg{fp: "after", ok: true}))
	if m.cursor != 0 {
		t.Errorf("カーソルが先頭に残らない: %d", m.cursor)
	}
	if m.commits[0].Subject != "c5" {
		t.Fatalf("再読込されていない: 先頭=%q", m.commits[0].Subject)
	}
}

// 実 fsnotify との結合: git commit が見張り対象のディレクトリのイベントとして届くか。
//
// これが崩れる形は「イベントが来ないので 1 分ポーリングだけになる」= 無音ではなく静かに
// 即時性を失うだけなので、gitLogWatchDirs の対象漏れは他のどの検査でも観測できない。
// ⚠️ fsnotify を作れない環境では skip する (その環境ではポーリングが唯一の経路)。
func TestIntegrationGitLogWatchSeesCommitEvent(t *testing.T) {
	w, err := newDirWatcher()
	if err != nil {
		t.Skipf("この環境では fsnotify を作れない (%v)。実 fsnotify との結合は未検証", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	dir := newTempRepo(t, []string{"c1"})
	dirs := gitLogWatchDirs()
	if len(dirs) == 0 {
		t.Fatal("見張り対象のディレクトリを解決できない")
	}
	added := 0
	for _, d := range dirs {
		if err := w.Add(d); err == nil {
			added++
		}
	}
	if added == 0 {
		t.Fatalf("1 つも watch を張れなかった: %v", dirs)
	}
	commitLines(t, dir, 5, "c2")
	select {
	case ev, ok := <-w.Events():
		if !ok {
			t.Fatal("イベントチャネルが閉じた")
		}
		t.Logf("commit で届いたイベント: %s", ev)
	case err := <-w.Errors():
		t.Fatalf("fsnotify エラー: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatalf("git commit が %v のどのディレクトリのイベントにもならなかった", dirs)
	}
}

// Update の配線を通した反映 (case が 1 つ抜けると機能ごと死ぬのに、ハンドラの単体検査は全部
// 緑のまま通る)。Init が保険のチェーンを張ることも合わせて固定する。
func TestGitLogWatchWiredThroughUpdate(t *testing.T) {
	m, dir, _ := realRepoBrowse(t, 10, "c1", "c2")
	m.Init() // 返す Cmd は実行しない (札の副作用だけを見る)
	if !m.logWatch.pollArmed {
		t.Error("Init が保険のポーリングを張っていない (イベントが来ない環境で永久に気づけない)")
	}

	dirsMsg := gitLogWatchDirsCmd()() // 実 git で対象ディレクトリを解決
	if _, ok := dirsMsg.(gitLogDirsMsg); !ok {
		t.Fatalf("対象ディレクトリの解決が gitLogDirsMsg を返さない: %T", dirsMsg)
	}
	m.Update(dirsMsg)
	if len(m.logWatch.dirs) == 0 {
		t.Error("Update が対象ディレクトリを受け取っていない")
	}

	commitLines(t, dir, 9, "c3")
	m.logWatch.pollArmed = false // Init が張った tick は実行しないので札だけ空ける
	m.Update(gitLogProbeMsg{})
	if !m.logWatch.measuring {
		t.Fatal("Update が gitLogProbeMsg を受けていない (case の配線漏れ)")
	}
	fpMsg := m.gitLogMeasureCmd()() // 実 git で指紋を測る
	_, reloadCmd := m.Update(fpMsg)
	if reloadCmd == nil {
		t.Fatal("指紋の変化で読み直しの Cmd が返らない")
	}
	if len(m.commits) != 2 {
		t.Fatalf("Update の中で同期に読み直した (Cmd に出す契約。issue 146): %d 件", len(m.commits))
	}
	reloadMsg := reloadCmd() // 実 git で読み直す (goroutine 側の仕事)
	if _, ok := reloadMsg.(gitLogReloadMsg); !ok {
		t.Fatalf("読み直しの Cmd が gitLogReloadMsg を返さない: %T", reloadMsg)
	}
	m.Update(reloadMsg)
	if len(m.commits) != 3 || m.commits[0].Subject != "c3" {
		t.Fatalf("配線経由で反映されない: %d 件 先頭=%q", len(m.commits), m.commits[0].Subject)
	}
	if m.logWatch.measuring {
		t.Error("測定の札が降りていない")
	}
}

// 回帰 (fd 漏れ): restartSelf の syscall.Exec は fd テーブルを引き継ぐので、cancelAll が
// 見張りを閉じ忘れると r で再起動するたびに kqueue fd が漏れる。git log の見張りは起動から
// 終了まで開き続けるため、issues viewer より漏れやすい。
func TestCancelAllClosesGitLogWatcher(t *testing.T) {
	fakeWatcherSeam(t)
	m := newTestBrowse(t, 1, nil, nil)
	dir := t.TempDir()
	m.logWatch.dirs = []string{dir}
	m.startGitLogWatch()
	fw, ok := m.logWatch.w.(*fakeWatcher)
	if !ok {
		t.Fatalf("startGitLogWatch が newDirWatcher seam を通っていない (w=%T)", m.logWatch.w)
	}
	m.cancelAll()
	if m.logWatch.w != nil {
		t.Fatal("cancelAll 後も watcher が開いたまま (restart の exec で fd が漏れる)")
	}
	// スロットの nil 化だけでなく実体が Close されたことを固定する (Close を消して nil 代入
	// だけにする変異をすり抜けさせない)
	if err := fw.Add(dir); !errors.Is(err, fsnotify.ErrClosed) {
		t.Fatalf("cancelAll 後の watcher が Close されていない (Add err=%v)", err)
	}
}

// 測っている間にポップアップを開かれたら反映しない (開始時のチェックだけでは、開いている
// 内容のキャッシュを reloadLog が消してしまう)。
func TestGitLogFPDefersWhenOverlayOpensDuringMeasure(t *testing.T) {
	m, dir, _ := realRepoBrowse(t, 10, "c1", "c2")
	m.logWatch.seen, m.logWatch.hasSeen = "before", true
	m.handleGitLogProbe(gitLogProbeMsg{}) // ここでは開いていないので測定が始まる
	if !m.logWatch.measuring {
		t.Fatal("測定が始まらない")
	}
	commitLines(t, dir, 9, "c3")
	m.diffOv.open(m.commits[0].SHA) // 測定中に開かれた
	if cmd := m.handleGitLogFP(gitLogFPMsg{fp: "after", ok: true}); cmd != nil {
		t.Error("ポップアップを開いている状態で反映した (開いている内容のキャッシュが消える)")
	}
	if len(m.commits) != 2 {
		t.Fatalf("反映されてしまった: %d 件", len(m.commits))
	}
	if !m.logWatch.hasSeen || m.logWatch.seen != "before" {
		t.Errorf("見送ったのに基準が動いた: hasSeen=%v seen=%q (閉じた後の観測で変化を見落とす)",
			m.logWatch.hasSeen, m.logWatch.seen)
	}
	if m.logWatch.measuring {
		t.Error("測定の札が降りていない (以降ずっと測れなくなる)")
	}
}

// 自分で読み直した直後に、その前に測った指紋が届いても反映しない (pull / push の後に
// 無駄な再読込・トースト・CI 再取得が続けて走る形)。
func TestGitLogFPDiscardsMeasurementTakenBeforeSelfReload(t *testing.T) {
	m, dir, _ := realRepoBrowse(t, 10, "c1", "c2")
	m.logWatch.seen, m.logWatch.hasSeen = "before", true
	m.handleGitLogProbe(gitLogProbeMsg{})
	staleFP := "measured-before-pull"

	commitLines(t, dir, 9, "c3") // 自分の pull が持ってきた分に相当
	m.reloadAfterPull()
	if len(m.commits) != 3 {
		t.Fatalf("pull の読み直しが効いていない: %d 件", len(m.commits))
	}
	m.toast = toast{} // pull のトーストと区別する
	// ⚠️ pull の演出中は見送り (gitLogReloadDeferred) に入って何もしないため、演出を落として
	// 「古い測定値をどう扱うか」だけを判定に残す (落とさないと、この検査は演出のおかげで
	// 通ってしまい、古い測定値を採用する変異を検知できない — 実測 2026-09-01)。
	m.pullAnimating = false

	if cmd := m.handleGitLogFP(gitLogFPMsg{fp: staleFP, ok: true}); cmd != nil {
		t.Error("読み直し前に測った指紋で再読込が走った")
	}
	if m.toast.visible() {
		t.Errorf("自分の pull の直後に外部変更のトーストが出た: %q", m.toast.text)
	}
	if m.logWatch.hasSeen {
		t.Errorf("古い指紋を基準にした: %q (次の観測でも不一致になり、もう一度無駄に反映する)", m.logWatch.seen)
	}
}

// ctrl+d / pgdown はカーソルを動かさずビューポートだけ下げる。この状態を「先頭を見ている」と
// 扱うと、外部変更の反映で画面が先頭へ飛ぶ。
func TestGitLogReflectKeepsViewportScrolledWithCursorAtTop(t *testing.T) {
	m, dir, _ := realRepoBrowse(t, 10, "c1", "c2", "c3", "c4", "c5")
	m.cursor = 0
	m.offset = m.clampOffset(4) // ctrl+d 相当 (カーソルは先頭のまま下を覗いている)
	if m.offset == 0 {
		t.Fatal("ビューポートを下げられなかった (前提が作れていない)")
	}
	offsetBefore := m.offset

	commitLines(t, dir, 9, "c6")
	m.logWatch.seen, m.logWatch.hasSeen = "before", true
	m.handleGitLogProbe(gitLogProbeMsg{})
	runGitLogReload(t, m, m.handleGitLogFP(gitLogFPMsg{fp: "after", ok: true}))
	if m.commits[0].Subject != "c6" {
		t.Fatalf("反映されていない: 先頭=%q", m.commits[0].Subject)
	}
	if m.offset <= offsetBefore {
		t.Errorf("ビューポートが先頭へ戻った: offset %d → %d (読んでいた位置が失われる)", offsetBefore, m.offset)
	}
	if m.pullAnimating {
		t.Error("下を読んでいるのに降らせる演出が始まった")
	}
}

// 見送りの条件は 6 つある。1 つ外れても他が立っていると気づけないので、各条件を単独で固定する。
func TestGitLogReloadDeferredCoversEachState(t *testing.T) {
	cases := []struct {
		name string
		set  func(m *browseModel)
	}{
		{"push 確認モーダル", func(m *browseModel) { m.actModal.pushConfirm = true }},
		{"pull 実行中", func(m *browseModel) { m.actModal.pulling = true }},
		{"diff ポップアップ", func(m *browseModel) { m.diffOv.open(m.commits[0].SHA) }},
		{"PR 状態ポップアップ", func(m *browseModel) { m.prStatusOv.open(m.commits[0].SHA) }},
		{"job 詳細ログ", func(m *browseModel) { m.detailOv.startOpen("k", 5) }},
		{"pull の演出中", func(m *browseModel) { m.pullAnimating = true }},
		{"push の演出中", func(m *browseModel) { m.pushAnimating = true }},
		{"job パネル", func(m *browseModel) { m.panelSHA = m.commits[0].SHA }},
		{"issues viewer", func(m *browseModel) { m.issuesOv.shown = true }},
		{"status viewer", func(m *browseModel) { m.statusOv.shown = true }},
		{"残量ダッシュボード", func(m *browseModel) { m.rlDash.shown = true }},
	}
	for _, c := range cases {
		m := newTestBrowse(t, 3, map[string]CIState{}, nil)
		if m.gitLogReloadDeferred() {
			t.Fatalf("%s: 何も開いていないのに見送り状態", c.name)
		}
		c.set(m)
		if !m.gitLogReloadDeferred() {
			t.Errorf("%s: 反映してはいけない状態を見送らない", c.name)
		}
	}
}

// ctrl+d でページ送りしている最中に先頭コミットが書き換わっても (`--amend`)、見えている画面は
// 動かない。⚠️ カーソルだけを錨にすると、この状態のカーソルは先頭コミット = amend で最も
// 消えやすい SHA を指すため「先頭へ倒す」経路に落ちる (敵対レビューで実測 2026-09-01)。
func TestGitLogReflectSurvivesAmendOfTopCommit(t *testing.T) {
	m, dir, _ := realRepoBrowse(t, 10, "c1", "c2", "c3", "c4", "c5", "c6")
	m.cursor = 0
	m.offset = m.clampOffset(12) // ctrl+d 相当 (カーソルは先頭のまま下を読んでいる)
	if m.offset == 0 {
		t.Fatal("ビューポートを下げられなかった (前提が作れていない)")
	}
	topIdx := topVisibleCommitIdx(m.lines(), m.offset)
	if topIdx <= 0 {
		t.Fatalf("画面先頭が先頭コミットのまま (前提が作れていない): idx=%d", topIdx)
	}
	topSHA := m.commits[topIdx].SHA

	gitInRepo(t, dir, "commit", "-q", "--amend", "-m", "c6 amended")
	m.logWatch.seen, m.logWatch.hasSeen = "before", true
	m.handleGitLogProbe(gitLogProbeMsg{})
	runGitLogReload(t, m, m.handleGitLogFP(gitLogFPMsg{fp: "after", ok: true}))

	if m.commits[0].Subject != "c6 amended" {
		t.Fatalf("反映されていない: 先頭=%q", m.commits[0].Subject)
	}
	if m.offset == 0 {
		t.Error("ビューポートが先頭へ飛んだ (読んでいた位置が失われる)")
	}
	gotIdx := topVisibleCommitIdx(m.lines(), m.offset)
	if gotIdx < 0 || m.commits[gotIdx].SHA != topSHA {
		t.Errorf("画面先頭のコミットが変わった: idx=%d (錨 %s)", gotIdx, topSHA)
	}
}

// 見張り対象は refs / logs の入れ子まで辿る (スラッシュ入りのブランチ名・remote ごとの ref・
// reflog はサブディレクトリの中にあり、fsnotify は再帰しない)。
func TestGitLogWatchDirsCoversNestedRefs(t *testing.T) {
	dir := newTempRepo(t, []string{"c1"})
	gitInRepo(t, dir, "switch", "-q", "-c", "feature/x")
	commitLines(t, dir, 3, "c2")
	dirs := gitLogWatchDirs()
	// ⚠️ t.TempDir() のパスと git が返す --absolute-git-dir は macOS では symlink の解決で
	// 食い違う (/var/folders と /private/var/folders) ので、末尾で照合する。
	want := filepath.Join("refs", "heads", "feature")
	found := slices.ContainsFunc(dirs, func(d string) bool { return strings.HasSuffix(d, want) })
	if !found {
		t.Errorf("スラッシュ入りブランチの ref ディレクトリを見張っていない: *%c%s\n対象=%v",
			filepath.Separator, want, dirs)
	}
	if len(dirs) > gitLogWatchMaxDirs {
		t.Errorf("見張るディレクトリ数の上限を超えた: %d > %d", len(dirs), gitLogWatchMaxDirs)
	}
}

// コミット 0 件 (revs が空範囲) の指紋は正当に "" になる。これを「基準なし」と混同すると
// 変化の判定が狂う。
func TestGitLogEmptyFingerprintIsNotSentinel(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.logWatch.seen, m.logWatch.hasSeen, m.logWatch.measuring = "", true, true
	if cmd := m.handleGitLogFP(gitLogFPMsg{fp: "", ok: true}); cmd != nil {
		t.Error("基準 (空の指紋) と同じ測定値で反映が走った")
	}
	if !m.logWatch.hasSeen {
		t.Error("空の指紋を基準として保てていない")
	}
	if m.toast.visible() {
		t.Errorf("変化していないのにトーストが出た: %q", m.toast.text)
	}
}

// runGitLogReload は reflectGitLogChange が返した読み直しの Cmd を goroutine の代わりに実行し、
// その結果を handleGitLogReload へ渡す (反映は Update の外で読み、Msg で受ける契約。issue 146)。
func runGitLogReload(t *testing.T, m *browseModel, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("読み直しの Cmd が返らない")
	}
	msg, ok := cmd().(gitLogReloadMsg)
	if !ok {
		t.Fatalf("読み直しの Cmd が gitLogReloadMsg を返さない")
	}
	m.handleGitLogReload(msg)
}

// 読み直している間に pull が入ったら、届いた古い logData で pull の結果を上書きしない。
func TestGitLogReloadDiscardsWhenSelfReloadHappenedMeanwhile(t *testing.T) {
	m, dir, _ := realRepoBrowse(t, 10, "c1", "c2")
	commitLines(t, dir, 9, "c3")
	cmd := m.reflectGitLogChange()
	staleMsg := cmd() // c3 まで読んだ結果 (まだ届いていない)
	commitLines(t, dir, 10, "c4")
	m.reloadAfterPull() // 利用者の pull が先に反映された (c4 まで)
	m.pullAnimating = false
	m.toast = toast{}
	if cmd := m.handleGitLogReload(staleMsg.(gitLogReloadMsg)); cmd != nil {
		t.Error("古い読み直しの結果で何か起きた")
	}
	if len(m.commits) != 4 || m.commits[0].Subject != "c4" {
		t.Fatalf("古い logData が pull の結果を上書きした: %d 件 先頭=%q", len(m.commits), m.commits[0].Subject)
	}
	if m.toast.visible() {
		t.Errorf("捨てた読み直しでトーストが出た: %q", m.toast.text)
	}
	if m.logWatch.reloading {
		t.Error("reloading の札が降りていない (以降の観測が全部見送られる)")
	}
}

// 読み直している間にポップアップが開かれたら、届いた結果を入れない (開いている内容の
// キャッシュが消える)。基準は触らないので閉じた後の観測で反映される。
func TestGitLogReloadDefersWhenOverlayOpensDuringReload(t *testing.T) {
	m, dir, _ := realRepoBrowse(t, 10, "c1", "c2")
	m.logWatch.seen, m.logWatch.hasSeen = "before", true
	commitLines(t, dir, 9, "c3")
	cmd := m.reflectGitLogChange()
	msg := cmd()
	m.diffOv.open(m.commits[0].SHA) // 読み直し中に開かれた
	if cmd := m.handleGitLogReload(msg.(gitLogReloadMsg)); cmd != nil {
		t.Error("ポップアップを開いている状態で反映した")
	}
	if len(m.commits) != 2 {
		t.Fatalf("反映されてしまった: %d 件", len(m.commits))
	}
	if !m.logWatch.hasSeen || m.logWatch.seen != "before" {
		t.Errorf("見送ったのに基準が動いた: hasSeen=%v seen=%q", m.logWatch.hasSeen, m.logWatch.seen)
	}
	if m.logWatch.reloading {
		t.Error("reloading の札が降りていない")
	}
}

// 読み直しが飛んでいる間は測らない (結果は読み直しが基準を降ろした後に捨てられるだけの fork)。
func TestGitLogProbeSkipsMeasureWhileReloading(t *testing.T) {
	newTempRepo(t, []string{"c1"})
	opts := &Options{MaxCount: 20, NoFrame: true}
	commits, err := LoadCommits(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	m := newBrowseModel(commits, map[string]CIState{}, nil, Repo{}, false, opts, false, 80, 10)
	t.Cleanup(m.cancel)
	_ = m.reflectGitLogChange() // Cmd は実行しない = in-flight のまま
	if !m.logWatch.reloading {
		t.Fatal("reflectGitLogChange が reloading の札を立てない")
	}
	m.handleGitLogProbe(gitLogProbeMsg{})
	if m.logWatch.measuring {
		t.Error("読み直し中に測定を始めた")
	}
}

// 読み直しの git fork は Cmd の実行時 (goroutine) に走り、Update の中 (reflectGitLogChange の呼び出し時)
// では走らない (issue 146)。Cmd を作った後に git を使えなくして、Cmd の実行が初めて失敗することで固定する
// (Update 内で読んでしまう変異は、Cmd を作った時点で結果を握っているので成功してしまう)。
func TestGitLogReflectRunsGitInsideCmd(t *testing.T) {
	newTempRepo(t, []string{"c1"})
	opts := &Options{MaxCount: 20, NoFrame: true}
	commits, err := LoadCommits(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	m := newBrowseModel(commits, map[string]CIState{}, nil, Repo{}, false, opts, false, 80, 10)
	t.Cleanup(m.cancel)
	cmd := m.reflectGitLogChange()
	t.Setenv("PATH", "") // ここから git は起動できない
	msg, ok := cmd().(gitLogReloadMsg)
	if !ok {
		t.Fatalf("gitLogReloadMsg でない: %T", msg)
	}
	if msg.err == nil {
		t.Fatal("git を使えなくした後の Cmd 実行が成功した = Update の中で既に読んでいる")
	}
	if cmd := m.handleGitLogReload(msg); cmd != nil {
		t.Error("失敗した読み直しで何か起きた")
	}
	if m.logWatch.reloading {
		t.Error("失敗後に reloading の札が降りていない")
	}
}
