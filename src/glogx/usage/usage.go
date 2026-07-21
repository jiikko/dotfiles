// Package usage は Claude Code の `/usage` 出力を取得・整形する。
//
// glogx / bubbletea には一切依存しない自己完結パッケージ。将来 単独コマンドへ
// 切り出す場合は Fetch + RenderLine を呼ぶだけの main を足せば済む (glogx 側の
// コード移動は不要)。ユーザー要望 2026-07-21: 「切り離しやすく設計」。
//
// データ源の注意: `/usage` の % は「このマシンのローカルセッションに基づく近似」で、
// 他デバイス・claude.ai の消費を含まない (出力自身がそう明記している)。リセット時刻は
// サーバのウィンドウ境界由来。`claude -p "/usage"` は LLM を呼ばない (num_turns=0・
// ゼロコスト) ため高速で、確認のために利用枠を減らさない。
package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Window は 1 つの利用枠 (5h セッション / weekly) の残量とリセット時刻。
type Window struct {
	Label   string    // 表示用ラベル ("5h" / "7d" / "7d(Fable)")
	Raw     string    // /usage の元ラベル ("Current session" 等)
	Percent int       // 使用率 0-100
	ResetAt time.Time // 枠がリセットされる時刻 (ローカルタイム)
}

// Snapshot は `/usage` 一回分のパース結果。
type Snapshot struct {
	Windows []Window
	Version string // Claude Code の CLI バージョン ("2.1.216" 等)。取得失敗時は空
}

// Find は label ("5h" / "7d" 等) に一致する Window を返す。
func (s *Snapshot) Find(label string) (Window, bool) {
	for _, w := range s.Windows {
		if w.Label == label {
			return w, true
		}
	}
	return Window{}, false
}

// claudeResult は `claude ... --output-format json` の必要フィールドだけ。
type claudeResult struct {
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
}

// Fetch は `claude -p "/usage"` を実行して結果をパースする。
//
// --model haiku を明示する: /usage はローカルコマンド処理で LLM を呼ばない
// (num_turns=0・total_cost_usd=0・duration_api_ms=0 を実測) ためモデル指定は結果に影響
// しないが、将来 /usage が推論を伴う実装に変わった場合に備え最小モデルへ固定しておく (保険)。
func Fetch(ctx context.Context) (*Snapshot, error) {
	// バージョンは /usage と独立なので並列取得して起動 fork の直列化を避ける。取得失敗は
	// 致命ではない (バージョン表示が消えるだけ) ため error は握りつぶし空文字にする。
	verCh := make(chan string, 1)
	go func() { verCh <- fetchVersion(ctx) }()

	cmd := exec.CommandContext(ctx, "claude", "-p", "/usage", "--model", "haiku", "--output-format", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude /usage 実行失敗: %w", err)
	}
	var res claudeResult
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("/usage 出力の JSON パース失敗: %w", err)
	}
	if res.IsError {
		return nil, errors.New("/usage がエラーを返した")
	}
	snap, err := Parse(res.Result, time.Now())
	if err != nil {
		return nil, err
	}
	snap.Version = <-verCh
	return snap, nil
}

// fetchVersion は `claude --version` から CLI バージョン番号だけを取り出す。
// 出力例: "2.1.216 (Claude Code)" → "2.1.216"。取得・パース失敗はすべて空文字を返す
// (バージョン表示は付加情報であり、欠けても usage 表示は成立させる)。
func fetchVersion(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "claude", "--version").Output()
	if err != nil {
		return ""
	}
	return parseVersion(string(out))
}

// parseVersion は `claude --version` の出力先頭トークンを返す純関数 (テスト容易性のため分離)。
// "2.1.216 (Claude Code)" → "2.1.216"。空・空白のみは空文字。
func parseVersion(out string) string {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// 例: "Current session: 2% used · resets Jul 22 at 3:09am (Asia/Tokyo)"
// 中点は環境により U+00B7 (·) のことがあるため \s+.\s+ で 1 文字だけ許容する。
// 時刻は "3:10am" (分あり) と "8am" (正時・分なし) の両形式があり、am/pm は大文字表記でも
// 1 枠だけ黙って欠落しないよう case-insensitive にする (parseResetTime 側で小文字化して parse)。
var lineRe = regexp.MustCompile(
	`(Current [^:]+):\s*(\d+)%\s+used\s+.\s+resets\s+([A-Z][a-z]{2}\s+\d{1,2})\s+at\s+(\d{1,2}(?::\d{2})?\s*(?i:[ap]m))`)

// Parse は `/usage` の result 文字列から利用枠を抽出する。now はリセット時刻の年補完に使う
// (`/usage` は年を出力しないため)。1 枠も取れなければエラー。
func Parse(result string, now time.Time) (*Snapshot, error) {
	matches := lineRe.FindAllStringSubmatch(result, -1)
	if len(matches) == 0 {
		return nil, errors.New("/usage 出力から利用枠を検出できず")
	}
	snap := &Snapshot{}
	for _, m := range matches {
		raw := strings.TrimSpace(m[1])
		pct, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		reset, err := parseResetTime(m[3], m[4], now)
		if err != nil {
			continue
		}
		snap.Windows = append(snap.Windows, Window{
			Label:   labelFor(raw),
			Raw:     raw,
			Percent: pct,
			ResetAt: reset,
		})
	}
	if len(snap.Windows) == 0 {
		return nil, errors.New("/usage 出力の利用枠をパースできず")
	}
	return snap, nil
}

// labelFor は `/usage` の元ラベルを短い表示ラベルへ写像する。
func labelFor(raw string) string {
	switch {
	case strings.HasPrefix(raw, "Current session"):
		return "5h"
	case strings.Contains(raw, "all models"):
		return "7d"
	case strings.HasPrefix(raw, "Current week"):
		// week(all models) 以外の週枠 (Fable 等)。括弧内をそのまま添える。
		if i := strings.Index(raw, "("); i >= 0 {
			return "7d" + strings.TrimSuffix(raw[i:], ")") + ")"
		}
		return "7d"
	}
	return raw
}

// parseResetTime は "Jul 22" + "3:09am" をローカルタイムの時刻へ変換する。`/usage` は年を
// 出さないため now の年を補い、過去に落ちた場合 (年末→年始境界) だけ翌年へ繰り上げる。
func parseResetTime(date, clock string, now time.Time) (time.Time, error) {
	clock = strings.ToLower(strings.ReplaceAll(clock, " ", "")) // "8PM" → "8pm" (Go の layout は小文字 pm)
	layout := "Jan 2 3:04pm"                                    // "3:10am"
	if !strings.Contains(clock, ":") {
		layout = "Jan 2 3pm" // 正時 "8am"
	}
	t, err := time.ParseInLocation(layout, date+" "+clock, time.Local)
	if err != nil {
		return time.Time{}, err
	}
	res := time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
	// リセットは常に未来。1 時間以上過去なら年境界とみなし翌年へ (誤差吸収に 1h の緩衝)。
	if res.Before(now.Add(-time.Hour)) {
		res = res.AddDate(1, 0, 0)
	}
	return res, nil
}
