package ssd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"doctor/runner"
)

// smartctl の終了状態はビットの組み合わせ (smartctl(8) の EXIT STATUS)。
const (
	exitParse   = 1 << 0 // コマンドラインを解釈できなかった
	exitOpen    = 1 << 1 // デバイスを開けなかった
	exitCmdFail = 1 << 2 // SMART などのコマンドの一部が失敗した
	exitFailing = 1 << 3 // SMART の総合判定が DISK FAILING
	// 残りのビット (4〜7) は ATA の属性・エラー履歴・自己診断の履歴の警告。NVMe の Apple SSD で
	// 立つ形を実測していないので、立ったら判定不能に倒す (意味を推測して ok / 異常に振らない)
	exitKnown = exitParse | exitOpen | exitCmdFail | exitFailing
)

// smartJSON は `smartctl -j -a` の出力のうち読む欄だけ。
//
// 🚨 **serial_number の欄を置かない**。unmarshal は置いていない欄を捨てるので、シリアル番号は
// この package のどの値にも入らない (画面・コピー文・キャッシュへ出る経路を構造で断つ)。
// 数値は pointer で持つ: 欄が無い (0 と区別できない) を判定不能にするため。
type smartJSON struct {
	Smartctl struct {
		ExitStatus *int `json:"exit_status"`
	} `json:"smartctl"`
	SmartStatus *struct {
		Passed *bool `json:"passed"`
	} `json:"smart_status"`
	NVMe *struct {
		CriticalWarning  *int64 `json:"critical_warning"`
		AvailableSpare   *int64 `json:"available_spare"`
		SpareThreshold   *int64 `json:"available_spare_threshold"`
		PercentageUsed   *int64 `json:"percentage_used"`
		MediaErrors      *int64 `json:"media_errors"`
		DataUnitsWritten *int64 `json:"data_units_written"`
		PowerOnHours     *int64 `json:"power_on_hours"`
		UnsafeShutdowns  *int64 `json:"unsafe_shutdowns"`
	} `json:"nvme_smart_health_information_log"`
}

// wearWarnPercent は摩耗 (Percentage Used) を注意にする境界。
//
// 🚨 仮の値。NVMe の仕様が決めているのは「100 = メーカーの見積もった寿命に達した」ことだけで、
// 80 に根拠は無い (docs/macos-health-check.md の初期案)。異常ではなく注意に留めているのはそのため
const wearWarnPercent = 80

// criticalTemperature は Critical Warning のビット 1 (温度が閾値を越えた)。ほかのビットは
// 予備領域 (0)・信頼性の低下 (2)・読み取り専用化 (3)・揮発性メモリの退避の失敗 (4) で、どれも異常にする。
const criticalTemperature = 1 << 1

// dataUnitBytes は NVMe の Data Units 1 つの大きさ (512 バイト × 1000)。
const dataUnitBytes = 512000

// readSmartctl は smartctl の JSON を読んで行を組む。
func readSmartctl(ctx context.Context, run runner.Runner, path string) ([]Check, []string) {
	stdout, stderr, rc, err := runner.WithTimeout(ctx, run, scanTimeout, path, "-j", "-a", Device)
	if err != nil {
		return []Check{unknownCheck("smartctl", "実行できません", err.Error())}, nil
	}
	var j smartJSON
	if jerr := json.Unmarshal([]byte(stdout), &j); jerr != nil {
		checks := []Check{unknownCheck("smartctl", "出力を読めません",
			fmt.Sprintf("rc=%d %s", rc, firstLine(stderr)))}
		// 出力が読めなくても、プロセスの rc が故障を申告していれば落とさない
		if rc >= 0 && rc&exitFailing != 0 {
			checks = append(checks, failingCheck())
		}
		return checks, nil
	}
	return judgeSmart(j, rc)
}

// judgeSmart は読んだ欄と終了状態から行を組む (純関数。テストはここを直接叩く)。
func judgeSmart(j smartJSON, rc int) ([]Check, []string) {
	// 🚨 JSON の exit_status を優先する: プロセスの rc と同じ値だが、runner の fake と本物で
	// 食い違ったときに、smartctl 自身の申告の方を信じる
	if j.Smartctl.ExitStatus != nil {
		rc = *j.Smartctl.ExitStatus
	}
	var checks []Check
	var notes []string
	// 故障の申告は、ほかのビットで読めなかったときにも落とさない (先に積む)
	if rc >= 0 && rc&exitFailing != 0 {
		checks = append(checks, failingCheck())
	}
	if rc < 0 || rc&(exitParse|exitOpen) != 0 {
		return append(checks, unknownCheck("smartctl", "読めません",
			fmt.Sprintf("終了状態 0x%02x (デバイスを開けない / 引数の誤り)", max(rc, 0)))), nil
	}
	if unknownBits := rc &^ exitKnown; unknownBits != 0 {
		checks = append(checks, unknownCheck("smartctl の終了状態", fmt.Sprintf("0x%02x", rc),
			fmt.Sprintf("警告のビット 0x%02x が立っている (意味を実測していないので判定しない)", unknownBits)))
	}
	checks = append(checks, passedCheck(j))
	if j.NVMe == nil {
		checks = append(checks, unknownCheck("NVMe の健康情報", "ありません", "smartctl が NVMe の健康情報を返さなかった"))
		return checks, notes
	}
	n := j.NVMe
	checks = append(checks,
		thresholdCheck("Critical Warning", n.CriticalWarning, func(v int64) (string, Status) {
			switch v {
			case 0:
				return "0x00", StatusOK
			case criticalTemperature:
				// 温度だけは一時的に立つことがある (負荷の高い間)。故障とは言わず注意に留める
				return fmt.Sprintf("0x%02x (温度)", v), StatusWarn
			}
			return fmt.Sprintf("0x%02x", v), StatusFail
		}),
		thresholdCheck("整合性エラー", n.MediaErrors, func(v int64) (string, Status) {
			if v >= 1 {
				return strconv.FormatInt(v, 10), StatusFail
			}
			return strconv.FormatInt(v, 10), StatusOK
		}),
		spareCheck(n.AvailableSpare, n.SpareThreshold),
		thresholdCheck("摩耗", n.PercentageUsed, func(v int64) (string, Status) {
			if v >= wearWarnPercent {
				return fmt.Sprintf("%d%% 使用", v), StatusWarn
			}
			return fmt.Sprintf("%d%% 使用", v), StatusOK
		}),
		infoCheck("書き込み総量", n.DataUnitsWritten, func(v int64) string {
			// 1 単位 = 512 バイト × 1000 (NVMe の Data Units)。掛けると桁あふれする値は数字を出さない
			if v > math.MaxInt64/dataUnitBytes {
				return "値が大きすぎます (" + strconv.FormatInt(v, 10) + " 単位)"
			}
			return decimalBytes(v * dataUnitBytes)
		}),
		infoCheck("通電時間", n.PowerOnHours, func(v int64) string { return groupDigits(v) + " h" }),
		infoCheck("安全でない電源断", n.UnsafeShutdowns, func(v int64) string { return groupDigits(v) + " 回" }),
	)
	if rc == exitCmdFail {
		if st := overall(checks); st == StatusOK || st == StatusWarn {
			// Apple の内蔵 SSD はエラー履歴のページ (Error Information Log) を読ませず、ここが立つ。
			// 故障ではないので判定に使わない。
			// 🚨 「故障ではない」と書くのは、立っているのが 4 だけで、判定した行が異常・判定不能を
			// 1 つも含まないときだけ (異常の行の隣に「故障ではありません」を並べない。敵対レビュー 3 周目)
			notes = append(notes, "smartctl の rc=4 はエラー履歴のページを読めないだけです (Apple の SSD の仕様。故障ではありません)")
		}
	}
	return checks, notes
}

func failingCheck() Check {
	return Check{Label: "smartctl の判定", Value: "DISK FAILING", Status: StatusFail,
		Note: "終了状態のビット 3 (SMART の総合判定が故障)"}
}

func passedCheck(j smartJSON) Check {
	if j.SmartStatus == nil || j.SmartStatus.Passed == nil {
		return unknownCheck("総合判定", "欄がありません", "smartctl")
	}
	if *j.SmartStatus.Passed {
		return Check{Label: "総合判定", Value: "PASSED", Status: StatusOK, Note: "smartctl"}
	}
	return Check{Label: "総合判定", Value: "FAILED", Status: StatusFail, Note: "smartctl"}
}

// spareCheck は予備領域を閾値と比べる。
//
// 🚨 **閾値を「下回った」ときだけ異常** (spare < threshold)。NVMe の仕様の定義 (Critical Warning の
// ビット 0 と同じ条件) で、`<=` にすると閾値ちょうどを異常にする。実測の Apple SSD は
// 100% に対して閾値 99% なので、`<=` だと 1 目盛り減っただけで異常を出す
func spareCheck(spare, threshold *int64) Check {
	if spare == nil || threshold == nil {
		return unknownCheck("予備領域", "欄がありません", "smartctl")
	}
	if *spare < 0 || *threshold < 0 {
		return unknownCheck("予備領域", fmt.Sprintf("%d%% (閾値 %d%%)", *spare, *threshold), "負の値は読めません")
	}
	value := fmt.Sprintf("%d%% (閾値 %d%%)", *spare, *threshold)
	if *spare < *threshold {
		return Check{Label: "予備領域", Value: value, Status: StatusFail}
	}
	return Check{Label: "予備領域", Value: value, Status: StatusOK}
}

func thresholdCheck(label string, v *int64, judge func(int64) (string, Status)) Check {
	if v == nil {
		return unknownCheck(label, "欄がありません", "smartctl")
	}
	if *v < 0 {
		// smartctl は符号なしで出す。負の値は読み違い (0 や ok に丸めない)
		return unknownCheck(label, strconv.FormatInt(*v, 10), "負の値は読めません")
	}
	value, st := judge(*v)
	return Check{Label: label, Value: value, Status: st}
}

func infoCheck(label string, v *int64, format func(int64) string) Check {
	if v == nil {
		return Check{Label: label, Value: "欄がありません", Status: StatusInfo}
	}
	if *v < 0 {
		return Check{Label: label, Value: "読めません (負の値)", Status: StatusInfo}
	}
	return Check{Label: label, Value: format(*v), Status: StatusInfo}
}

func unknownCheck(label, value, note string) Check {
	return Check{Label: label, Value: value, Status: StatusUnknown, Note: note}
}

// decimalBytes は 10 進 (smartctl の [120 TB] と同じ) で丸める。
func decimalBytes(b int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	f := float64(b)
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if f < 10 && i > 0 {
		return fmt.Sprintf("%.1f %s", f, units[i])
	}
	return fmt.Sprintf("%.0f %s", f, units[i])
}

// groupDigits は 3 桁ごとにカンマを入れる (3993 → "3,993")。
func groupDigits(v int64) string {
	s := strconv.FormatInt(v, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
