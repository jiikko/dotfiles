//go:build !nofixture

// fixture.go は main のテスト用の口なので、到達不能のコードの検査 (scripts/check_unused_excluding_tests.sh) は
// -tags=nofixture でこのファイルを外して走る (ここを根に数えると、Fixture からしか呼ばれない内部を見落とす。issue 817)。

package doctorview

import (
	"strconv"

	"doctor/disk"
	"doctor/docker"
	"doctor/ssd"
	"doctor/svc"
)

// Fixture は main のテスト (browseModel と組み合わせる配線・1 フレームの確保の予算・動きの語彙のテスト) が
// doctor の画面を特定の状態に置く・読むための口。本番のコードは使わない (structure_ratchet_test.go の
// TestNoFixtureCallsInProduction が止める)。browseModel が要らないテストは package doctorview の中に書き、
// この口を使わない。ここに足すのは「main のテストが本当に要る状態」だけにする (issue 817)。
type Fixture struct{ v *DoctorView }

// Fixture はテスト用の口を返す (上の型の注記)。
func (v *DoctorView) Fixture() Fixture { return Fixture{v} }

// Show は走査を始めずに開く。
func (f Fixture) Show() { f.v.shown = true }

// DeleteState は Delete で置く削除の相。
type DeleteState int

const (
	DeleteIdle        DeleteState = iota // 削除の語彙を持っていない
	DeleteConfirm                        // 確認中
	DeleteConfirmPlan                    // 下見の後の確認
	DeletePreparing                      // 下見の走査中
	DeleteRunning                        // 実行中
	DeleteResult                         // 結果の表示中
	DeleteFailed                         // 失敗の表示中
)

// Delete は削除の相を置く。cancel は下見中・実行中の Ctrl-C の 2 回目で呼ばれる中断。
// 🚨 進捗は置かない (known=false)。本物の削除も進捗が届く前の状態から始まる (StartDelete)。進捗を「既知」で置くと、
// 進捗の届く前の削除中を見落とす退行 (Deleting が progress.known を見る等) をテストが区別できない (codex の敵対的レビュー 2026-10-17)。
func (f Fixture) Delete(st DeleteState, cancel func()) {
	switch st {
	case DeleteIdle:
		f.v.del = doctorDelete{}
	case DeleteConfirm:
		f.v.del = doctorDelete{confirm: true}
	case DeleteConfirmPlan:
		f.v.del = doctorDelete{confirm: true, plan: &disk.DeleteReport{}}
	case DeletePreparing:
		f.v.del = doctorDelete{preparing: true, cancel: cancel}
	case DeleteRunning:
		f.v.del = doctorDelete{running: true, cancel: cancel}
	case DeleteResult:
		f.v.del = doctorDelete{result: &disk.DeleteReport{}}
	case DeleteFailed:
		f.v.del = doctorDelete{err: "だめでした"}
	}
}

// Reports は走査を終えた状態と表示中のタブを置く (nil の欄は空の結果)。描画を展開しない (Expanded は空)。
func (f Fixture) Reports(tab DoctorTab, d *disk.Report, s *svc.Report, brewWarnings []string, dk *docker.Report, sd *ssd.Report) {
	if d == nil {
		d = &disk.Report{}
	}
	if s == nil {
		s = &svc.Report{}
	}
	if dk == nil {
		dk = &docker.Report{}
	}
	if sd == nil {
		sd = &ssd.Report{}
	}
	f.v.tab = tab
	f.v.expanded = map[rowKey]bool{}
	f.v.diskRep, f.v.diskResults, f.v.svcRep, f.v.docker, f.v.ssd = d, d.Results, s, dk, sd
	f.v.brew = &brewDoctorResult{Warnings: brewWarnings, Clean: len(brewWarnings) == 0}
}

// Rows は n 行の選べる行を置く (描画を経ずにキーの動きだけを見るテスト用)。
func (f Fixture) Rows(n int) {
	f.v.rows = f.v.rows[:0]
	for i := range n {
		f.v.rows = append(f.v.rows, doctorRow{text: strconv.Itoa(i), selectable: true, key: rowKey(strconv.Itoa(i))})
	}
}

// CursorIndex はカーソルの位置 (Rows の添字)。
func (f Fixture) CursorIndex() int { return f.v.cur.index }

// Tab は表示中のタブ。
func (f Fixture) Tab() DoctorTab { return f.v.tab }

// SetTab は表示中のタブを置く。
func (f Fixture) SetTab(tab DoctorTab) { f.v.tab = tab }

// DiskResults はディスクの走査の途中の結果を置く (描画で行を組み直させるテスト用)。
func (f Fixture) DiskResults(rs []disk.Result) { f.v.diskResults = rs }

// CursorFellBack は選んでいた行が消えてカーソルを寄せた印が立っているか (取り出さずに見る)。
func (f Fixture) CursorFellBack() bool { return f.v.cur.fellBack }

// ScanStopped は走査の ctx を手放したか (Stop の後は真)。
func (f Fixture) ScanStopped() bool { return f.v.cancel == nil }
