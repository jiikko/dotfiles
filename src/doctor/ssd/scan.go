// Package ssd は内蔵 SSD の健康状態を読む (issue 578)。**読むだけ**で、修復・sudo・設定変更の経路は持たない。
//
// 見るのは 2 層で、混ぜない:
//   - SMART (物理): `diskutil info disk0` の合否と、`smartctl -j -a disk0` の NVMe の健康情報
//   - APFS (論理): doctor は検査を実行しない。手で叩くコマンドを提示するだけ (Status は常に未検査)
//
// 🚨 **判定できなかったものを ok にしない**。コマンドが無い・失敗した・欄が無い・知らない終了状態は
// すべて StatusUnknown に倒す。SMART の総合が ok でも APFS は未検査のまま出す
// (「SMART が正常 = ディスクが正常」と総括させない)。
package ssd

import (
	"context"
	"time"

	"doctor/runner"
)

// Device は診断する内蔵の起動ディスク。外付けは初回の対象外 (issue 578 の非ゴール)。
const Device = "disk0"

// APFSCommand は APFS の整合性を手で確かめるコマンド。
//
// 🚨 doctor からは実行しない: 数分かかりディスクが重くなる上、sudo の要否・中断したときの挙動を
// 実測していない (ユーザー決定 2026-09-29)。実行経路を足すなら、先にそれらを測ること。
const APFSCommand = "diskutil verifyVolume /System/Volumes/Data"

// InstallHint は smartctl が無いときの案内 (doctor からは入れない)。
const InstallHint = "brew install smartmontools"

// scanTimeout は外部コマンド 1 回の上限 (実測 diskutil 0.07 秒 / smartctl 0.02 秒)。
const scanTimeout = 10 * time.Second

// Status は 1 項目の判定。
type Status string

const (
	StatusOK        Status = "ok"
	StatusWarn      Status = "warn"      // 故障ではないが確認対象 (摩耗が進んでいる)
	StatusFail      Status = "fail"      // 既知の異常条件に当たった
	StatusUnknown   Status = "unknown"   // コマンド不在 / 失敗 / 欄が無い / 知らない終了状態
	StatusUnchecked Status = "unchecked" // 検査していない (APFS)
	StatusInfo      Status = "info"      // 数字を出すだけで判定しない (書き込み総量など)
)

// Check は画面の 1 行。
type Check struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Status Status `json:"status"`
	Note   string `json:"note,omitempty"` // 判定の理由・出典 (判定不能のときは何が取れなかったか)
}

// Report は 1 回の診断結果。
type Report struct {
	// Model は SSD の型番 (例 "APPLE SSD AP1024Z")。取れなければ空。
	// 🚨 シリアル番号は持たない (smartctl の JSON から読む構造体に欄を置いていない)
	Model    string `json:"model,omitempty"`
	Internal bool   `json:"internal"`
	// SMART は物理の総合 (Checks のうち判定する行の最悪)
	SMART  Status  `json:"smart"`
	Checks []Check `json:"checks"`
	// SmartctlMissing は smartctl が PATH に無い (InstallHint を案内する)
	SmartctlMissing bool   `json:"smartctl_missing"`
	APFS            Status `json:"apfs"`
	// Notes は判定とは別に伝えること (rc=4 を無視した理由など)
	Notes     []string  `json:"notes,omitempty"`
	ScannedAt time.Time `json:"scanned_at"`
}

// Options は差し替え口 (zero value = 本番)。
type Options struct {
	Run      runner.Runner
	LookPath func(string) (string, error)
	Now      func() time.Time
}

// Scan は 1 回診断する。表示の関門を通した値を返す。
func Scan(ctx context.Context, o Options) Report {
	return SanitizeForDisplay(scan(ctx, o))
}

func scan(ctx context.Context, o Options) Report {
	run := o.Run
	if run == nil {
		run = runner.Exec
	}
	look := o.LookPath
	if look == nil {
		look = runner.LookPath
	}
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	rep := Report{APFS: StatusUnchecked, ScannedAt: now()}

	info := readDiskutil(ctx, run)
	rep.Model, rep.Internal = info.model, info.internal
	rep.Checks = append(rep.Checks, info.check)

	path, err := look("smartctl")
	if err != nil {
		rep.SmartctlMissing = true
		rep.Checks = append(rep.Checks, Check{
			Label: "smartctl", Value: "見つかりません", Status: StatusUnknown,
			Note: InstallHint + " で入れられます (y でコピー。摩耗・整合性エラーはこれで読みます)",
		})
	} else {
		checks, notes := readSmartctl(ctx, run, path)
		rep.Checks = append(rep.Checks, checks...)
		rep.Notes = append(rep.Notes, notes...)
	}
	rep.SMART = overall(rep.Checks)
	return rep
}

// overall は判定する行の最悪を返す。順位は fail > unknown > warn > ok。
// unknown を warn より重く置くのは、見えていないものは異常かもしれないから (ok 側に丸めない)。
func overall(checks []Check) Status {
	rank := func(s Status) int {
		switch s {
		case StatusFail:
			return 4
		case StatusUnknown:
			return 3
		case StatusWarn:
			return 2
		case StatusOK:
			return 1
		case StatusInfo, StatusUnchecked:
			return 0
		}
		return 3 // 知らない値は判定不能と同じ重さ
	}
	best, worst := 0, StatusUnknown
	for _, c := range checks {
		if r := rank(c.Status); r > best {
			best, worst = r, c.Status
		}
	}
	if best == 0 {
		return StatusUnknown // 判定する行が 1 つも無い
	}
	return worst
}
