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
func (f Fixture) Show() { f.v.Shown = true }

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

// Delete は削除の相を置く。下見中・実行中は進捗 (1/1 件目) も置き、cancel は Ctrl-C の 2 回目で呼ばれる中断。
func (f Fixture) Delete(st DeleteState, cancel func()) {
	prog := DoctorProgress{I: 1, Total: 1, Label: "…", Phase: disk.PhaseScanning, Known: true}
	switch st {
	case DeleteIdle:
		f.v.Del = DoctorDelete{}
	case DeleteConfirm:
		f.v.Del = DoctorDelete{Confirm: true}
	case DeleteConfirmPlan:
		f.v.Del = DoctorDelete{Confirm: true, Plan: &disk.DeleteReport{}}
	case DeletePreparing:
		f.v.Del = DoctorDelete{Preparing: true, Progress: prog, Cancel: cancel}
	case DeleteRunning:
		f.v.Del = DoctorDelete{Running: true, Progress: prog, Cancel: cancel}
	case DeleteResult:
		f.v.Del = DoctorDelete{Result: &disk.DeleteReport{}}
	case DeleteFailed:
		f.v.Del = DoctorDelete{Err: "だめでした"}
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
	f.v.Tab = tab
	f.v.Expanded = map[RowKey]bool{}
	f.v.DiskRep, f.v.DiskResults, f.v.SvcRep, f.v.Docker, f.v.Ssd = d, d.Results, s, dk, sd
	f.v.Brew = &BrewDoctorResult{Warnings: brewWarnings, Clean: len(brewWarnings) == 0}
}

// Rows は n 行の選べる行を置く (描画を経ずにキーの動きだけを見るテスト用)。
func (f Fixture) Rows(n int) {
	f.v.Rows = f.v.Rows[:0]
	for i := range n {
		f.v.Rows = append(f.v.Rows, DoctorRow{Text: strconv.Itoa(i), Selectable: true, Key: RowKey(strconv.Itoa(i))})
	}
}

// CursorIndex はカーソルの位置 (Rows の添字)。
func (f Fixture) CursorIndex() int { return f.v.Cur.Index }

// Tab は表示中のタブ。
func (f Fixture) Tab() DoctorTab { return f.v.Tab }

// SetTab は表示中のタブを置く。
func (f Fixture) SetTab(tab DoctorTab) { f.v.Tab = tab }

// DiskResults はディスクの走査の途中の結果を置く (描画で行を組み直させるテスト用)。
func (f Fixture) DiskResults(rs []disk.Result) { f.v.DiskResults = rs }

// CursorFellBack は選んでいた行が消えてカーソルを寄せた印が立っているか (取り出さずに見る)。
func (f Fixture) CursorFellBack() bool { return f.v.Cur.FellBack }

// ScanStopped は走査の ctx を手放したか (Stop の後は真)。
func (f Fixture) ScanStopped() bool { return f.v.Cancel == nil }
