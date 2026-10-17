package main

import (
	"strings"
	"testing"

	"glogx/internal/doctorview"
)

// ---- 敵対的レビュー (2026-09-03) が開けた穴を塞ぐテスト ----

// 🚨 削除の実行中に Ctrl-C / Ctrl-G が **browseModel の側で** 先に捌かれ、1 回目でアプリごと
// 落ちていた。中断は ctx で伝える契約 (プロセスを殺すと記録が executing のまま残り、
// cli: の子が孤児になる) が、UI の配線で破れていた形。
//
// 🚨 doctorview.DoctorView.handleKey を直叩きするテストではこの穴を 1 mm も守れない (それが実際に
// 起きたこと)。**browseModel.handleKey 経由**で見る。
func TestBrowseCtrlCDuringDeleteDoesNotQuit(t *testing.T) {
	for _, key := range []string{"ctrl+c", "ctrl+g"} {
		t.Run(key, func(t *testing.T) {
			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			m.doctorOv.Fixture().Show()
			cancelled := false
			m.doctorOv.Fixture().Delete(doctorview.DeleteRunning, func() { cancelled = true })
			m.handleKey(key)
			if m.done || m.restartRequested {
				t.Fatalf("1 回目の %s でアプリを終了した (中断は ctx で伝えること)", key)
			}
			// 武装したかは「1 回目では中断せず、2 回目で中断する」で見る (武装を経ずに中断する経路は無い)
			if cancelled {
				t.Errorf("1 回目で中断した (誤爆を 1 段止める設計)")
			}
			m.handleKey(key)
			if !cancelled {
				t.Errorf("2 回目でも中断しない")
			}
			if m.done {
				t.Errorf("2 回目でアプリごと落ちた (ctx の cancel だけでよい)")
			}
		})
	}
}

// 削除の実行中は再起動ダイアログを出さない (出るとどのキーも吸われ、r は走行中の削除を殺す)。
func TestRestartPromptDefersWhileDeleting(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.doctorOv.Fixture().Show()
	m.doctorOv.Fixture().Delete(doctorview.DeleteRunning, nil)
	m.restartPending = true
	if m.restartPromptVisible() {
		t.Fatal("削除中に再起動ダイアログを出した")
	}
	m.handleKey("r")
	if m.restartRequested {
		t.Error("削除中の r で再起動した (走行中の削除を殺す)")
	}
}

// ---- ディレクトリ単位の選択 (ユーザー要望 2026-09-03) ----

// ---- doctor_view.go の通しレビュー (opus, 2026-09-03) で出た食い違いの回帰 ----

// 削除の**確認中**も再起動ダイアログを出さない (出るとどのキーも吸われ、y が食われる)。
func TestRestartPromptDefersWhileDeleteConfirm(t *testing.T) {
	for _, tc := range []struct {
		name string
		del  doctorview.DeleteState
	}{{"確認中", doctorview.DeleteConfirm}, {"結果の表示中", doctorview.DeleteResult}} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			m.doctorOv.Fixture().Show()
			m.doctorOv.Fixture().Delete(tc.del, nil)
			m.restartPending = true
			if m.restartPromptVisible() {
				t.Fatalf("%s に再起動ダイアログを出した", tc.name)
			}
		})
	}
}

// 中断に使えるキーと、案内に書いてあるキーが一致する (issue 244)。
//
// 🚨 案内は doctor_delete.go、キーの分岐は tui.go と**層が分かれている**ので、片側だけ変えても
// build も既存テストも通る。
//
// 🚨 期待値を deleteAbortKeys から作らない。それは production と同じ定数から期待値を作る形で、
// 「案内から ctrl+g を落とす」変異が green のまま通った (最初にそう書いて実測した)。
// **挙動を独立の出典にする**: 候補キーを 1 つずつ browseModel の入口から押して、実際に
// 中断できたかを見てから、案内にその名前が出ているかを突き合わせる。
func TestDeleteAbortGuidanceMatchesKeys(t *testing.T) {
	var pv doctorview.DoctorView
	pv.Fixture().Delete(doctorview.DeleteRunning, nil)
	panel := strings.Join(pv.Lines(doctorview.DoctorRenderOpts{Width: 100, Page: 20, Spinner: "⠋"}), "\n")
	// 中断に使えそうな候補。実際に使えるかはここでは決めず、押して確かめる
	for _, tc := range []struct{ key, word string }{
		{"ctrl+c", "Ctrl-C"},
		{"ctrl+g", "Ctrl-G"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			m.doctorOv.Fixture().Show()
			cancelled := false
			m.doctorOv.Fixture().Delete(doctorview.DeleteRunning, func() { cancelled = true })
			m.handleKey(tc.key)
			// 1 回目で中断していない = 武装で止まった (武装を経ずに中断する経路は無い)
			armed := !cancelled
			m.handleKey(tc.key)
			aborts := cancelled

			shown := strings.Contains(panel, tc.word)
			switch {
			case aborts && !shown:
				t.Errorf("%s で中断できるのに、案内に %q が無い:\n%s", tc.key, tc.word, panel)
			case !aborts && shown:
				t.Errorf("案内に %q が出ているのに、%s では中断できない:\n%s", tc.word, tc.key, panel)
			case aborts && !armed:
				t.Errorf("%s が 1 回目で武装していない (誤爆を 1 段止める設計)", tc.key)
			}
		})
	}
}

// 削除中は再描画の tick が回り続ける。回らないと engine のイベントの合間に 1 度も描き直されず、
// 進捗を出していても画面が静止する = 文字どおり固まる (ユーザー報告 2026-09-04)。
//
// 🚨 scanning() では代用できないことも同時に pin する: あちらは 3 つのレポートが nil のあいだ
// (= 初回スキャン中) しか true にならないので、削除に入る頃には必ず false になっている。
func TestDeleteKeepsRedrawAlive(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.width, m.height = 100, 30
	m.doctorOv.Fixture().Show()
	// 走査は終わっている状態を作る (5 つのレポートが揃うと scanning() は false)
	m.doctorOv.Fixture().Reports(doctorview.TabDisk, nil, nil, nil, nil, nil)
	// usage の枠は「開いていて未取得」だと単独で tick を立てるので落とす。
	// 🚨 これを落とさないと、削除の配線を外す変異を当てても緑のまま = 何も守らないテストになる
	m.usageOv.Visible = false
	if m.doctorOv.Scanning() {
		t.Fatal("前提が崩れている: 走査は終わっているはず")
	}
	if m.spinnerActive() {
		t.Fatal("前提が崩れている: 削除前は tick を回す理由が無いはず")
	}
	for _, tc := range []struct {
		name string
		del  doctorview.DeleteState
	}{
		{"preparing", doctorview.DeletePreparing},
		{"running", doctorview.DeleteRunning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.doctorOv.Fixture().Delete(tc.del, nil)
			if !m.spinnerActive() {
				t.Error("削除中なのに spinnerActive=false (再描画が止まり、固まって見える)")
			}
		})
	}
	// 🚨 **削除の後に実際に画面へ出る状態**を全部当てる。ゼロ値だけを試すと、
	// deleting() を blocking() から active() に取り違える退行を検知できない
	// (active() は confirm / result / err も真になるので、結果画面の上で 12.5fps の
	// 再描画が無限に回り続ける)。敵対レビュー 2026-09-04 が変異で実証した穴
	for _, tc := range []struct {
		name string
		del  doctorview.DeleteState
	}{
		{"ゼロ値", doctorview.DeleteIdle},
		{"下見の後の確認", doctorview.DeleteConfirmPlan},
		{"削除の結果", doctorview.DeleteResult},
		{"失敗の表示", doctorview.DeleteFailed},
	} {
		t.Run("止まる/"+tc.name, func(t *testing.T) {
			m.doctorOv.Fixture().Delete(tc.del, nil)
			if m.doctorOv.Deleting() {
				t.Error("走っていないのに deleting() が true")
			}
			if m.spinnerActive() {
				t.Error("tick が回り続けている (静止画面の上で 12.5fps の再描画が止まらない)")
			}
		})
	}
}
