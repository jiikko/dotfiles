package main

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"doctor/svc"
	"glogx/internal/doctorview"
	"glogx/internal/doctorview/doctortest"
	"glogx/internal/lanview"
	"glogx/internal/term"
	"glogx/internal/testenv"
)

// installInertDoctor は doctor の走査口を**何も触らない fake** に差し替える。
//
// 🚨 差さないと、キー操作の先で **実マシンに対する走査**が始まる: `D` で開く経路だけでなく、
// 削除の結果パネルを閉じる (= doctorview.DoctorRescan) のような**間接的な経路**でも起きる。
// 実測 2026-09-03 (issue 216): glogx のテスト全体で実 `xcrun` / `brew` / `pgrep` を 29 回叩き、
// しかも `t.Cleanup(m.cancel)` は join しないので goroutine が次のテストへ跨いでいた
// (issue 214 のデータ競合はその跨ぎが踏んだ 1 個)。
//
// カタログを空にするのが要点: エントリが 0 件なら走査は即座に終わり、Run も呼ばれない。
// doctor 自体を検査するテストは `doctorTestView` を使うか、この後で seam を上書きする。
func installInertDoctor(t *testing.T, v *doctorview.DoctorView) {
	t.Helper()
	home := t.TempDir()
	inert := func(_ context.Context, name string, _ ...string) (string, string, int, error) {
		if name == "launchctl" {
			return "PID\tStatus\tLabel\n", "", 0, nil
		}
		return "", "", 0, nil
	}
	v.SetDeps(doctorview.Deps{
		DiskOpts: func() disk.Options {
			return disk.Options{
				Env:     disk.Env{Home: home, TmpDir: home + "/", Getenv: func(string) string { return "" }},
				Run:     inert,
				Catalog: []disk.Entry{}, // 空 = 走査対象なし (nil にすると本物のカタログが使われる)
				// 固定時刻 (カタログを足したときに「全部が boot 後」の偏った fixture にしない)
				BootTime: func() (time.Time, error) { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil },
			}
		},
		SvcOpts:    func() svc.Options { return svc.Options{Run: inert} }, // Dirs 空 = 走査対象なし
		BrewRun:    inert,
		DockerOpts: doctortest.NoDockerOptions, // Docker Desktop が入っていない扱い (本物の docker system df を叩かない)
		SSDOpts:    doctortest.FakeSSDOptions(true),
	})
}

// joinDoctorCleanup は走査の終了を**上限つきで**待つ。
//
// ⚠️ production の `doctorview.WaitDoctorCleanup()` を直接呼ばない: あれは上限が無く、止まらない走査が
// あると go test の 10 分タイムアウトまでハングして goroutine dump で終わる (何が悪いのか
// 読めない)。判定不能は**第 3 の結果**として Fatal で出す (doctor_cleanup_test.go の
// drainDoctorCleanup と同じ規律)。
func joinDoctorCleanup(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); doctorview.WaitDoctorCleanup() }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("判定不能: doctor の走査が 30 秒で帰らない (cancel が届いていないか、seam が差さっていない)")
	}
}

// TestMain は隔離した環境でテストを走らせる (前処理の中身と理由は internal/testenv の Main が正本)。
// main のパッケージ固有の前処理: mac-lan-manager (L) の本物のネットワークの読み取り (networksetup) と Bonjour の走査
// (終わらない dns-sd) を起こさない (fullscreen_test は横断キーで全部の板を開く。issue 747)。
func TestMain(m *testing.M) {
	testenv.Main(m, "glogx-cache", func(string) { lanview.FixtureSetOptions(fakeLanOptions) })
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
	installInertDoctor(t, &m.doctorOv)
	t.Cleanup(func() {
		// 🚨 `m.cancel` ではなく `m.cancelAll`。前者は browseModel 自身の fetch ctx だけで
		// **doctorOv.stop() を呼ばない**ので、走っている走査は止まらない。上限なしの join と
		// 組み合わさると**パッケージ全体がハングする** (敵対レビュー 2026-09-03 が再現)。
		// production の quit / main.go と同じ後始末にすることで fidelity も上がる
		m.cancelAll()
		// ⚠️ cancel は**子の死を待たない** (doctorview.DoctorView.Stop() のコメントが正本。issue 211)。
		// join しないと走査 goroutine が次のテストへ跨ぎ、package 変数を踏む (issue 214 / 216)
		joinDoctorCleanup(t)
	})
	// 🚨 開閉の演出はテストでは切る (zoom.go)。View の期待値が「中央から開く途中の姿」に
	// なると全テストが読めなくなるため。演出そのものは zoom_test.go が直接検査する。
	m.zoom.off = true
	// issues viewer の閉じる演出も同じ理由で切る (既存テストは i / q で即座に閉じる前提)。
	// 演出そのものは issues_close_anim_test.go が明示的に on にして検査する。
	m.issuesOv.Fixture().SetCloseAnim(false)
	// status viewer の閉じる演出も同じ理由で切る (s の toggle が「即座に閉じている」前提で読める
	// ように)。演出そのものは status_view_test.go の TestSlideLeftWindow 等が直接検査する。
	m.statusOv.Fixture().SetCloseAnim(false)
	return m
}

// releaseKey は「指を離した」ことにする (キーリピート判定をリセットする。swallowKeyRepeat)。
// 🚨 テストは同じキーを瞬間的に 2 回押すが、実機ではありえない速さなので自動リピート扱いに
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

// testPopupTermWidth は testenv.PopupWidth の content 幅を出す**端末の幅** (frame の 7 桁を足し戻した値)。
// hint 行の予算はこちらから決まる (hintWidth は m.width から引く)。
const testPopupTermWidth = testenv.PopupWidth + frameHOverhead

// testHintBudget は hint 文字列が収まるべき桁数。🚨 production の browseModel.hintWidth() から
// 導く (数字を書かない)。以前は issues のテストが 84 (content 幅)、hint_width_test が 84-2 と
// 別々の数字を持っており、実効 (端末 91 桁 - 2 = 89) とも一致していなかった。予算が 3 通りあると
// hint を 1 つ足したとき「どのテストが正本か」で迷う。
func testHintBudget(t *testing.T) int {
	t.Helper()
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.showFrame, m.width, m.height = true, testPopupTermWidth, 40
	if !m.frameActive() {
		t.Fatal("前提が崩れた: popup 幅でフレームが有効にならない")
	}
	return m.hintWidth()
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
// maybeTick と束ねられていても副作用 (extcmd.OpenInBrowser など) が発火する。
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
	w := term.DispWidth(term.StripANSI(box[0]))
	for i, l := range box {
		if got := term.DispWidth(term.StripANSI(l)); got != w {
			t.Fatalf("行 %d の表示幅 = %d, 他の行 = %d: %q", i, got, w, l)
		}
	}
	return w
}

// 画面のパッケージのテストが使う hint の予算 (testenv.PopupHintBudget) は、main の hintWidth と同じ値 (issue 817)。
// 🚨 frame の寸法や hint の余白を変えてここが落ちたら、testenv の定数を直す (画面のテストが古い予算で緑のまま通らないように)。
func TestPopupHintBudgetMatchesBrowse(t *testing.T) {
	if got := testHintBudget(t); got != testenv.PopupHintBudget {
		t.Fatalf("popup 幅の hint の予算が %d (testenv.PopupHintBudget = %d)。testenv の定数を直す", got, testenv.PopupHintBudget)
	}
}
