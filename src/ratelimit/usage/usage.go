// Package usage は Claude Code の `/usage` 出力と codex の rateLimits を取得・整形する。
//
// glogx / bubbletea には一切依存しない自己完結パッケージ (ユーザー要望 2026-07-21: 「切り離しやすく設計」)。
// 消費者は glogx (利用枠のオーバーレイ・ダッシュボード) と、この module の main (bin/ratelimit)。
// codex 側のデータ源と経路選定の理由は codex.go 冒頭を参照。
//
// データ源の注意: `/usage` の % は「このマシンのローカルセッションに基づく近似」で、
// 他デバイス・claude.ai の消費を含まない (出力自身がそう明記している)。リセット時刻は
// サーバのウィンドウ境界由来。`claude -p "/usage"` は LLM を呼ばない (num_turns=0・
// ゼロコスト) ため高速で、確認のために利用枠を減らさない。
// ただしサーバの `/api/oauth/usage` は 1 回ずつ叩くので、頻度には上限がある (shared.go)。
package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"subproc"

	"github.com/jiikko/dotfiles/src/termsafe"
)

// Window は 1 つの利用枠 (5h セッション / weekly) の残量とリセット時刻。
type Window struct {
	Label string // 表示用ラベル ("5h" / "7d" / "7d(Fable)" / "cx7d")
	// 🚨 `/usage` の**元ラベル**を持つ `Raw` フィールドは消した (issue 316 / 317)。
	// production の読み手が 0 の write-only で、しかも `usage_cache.go` の termsafe は
	// `Label` しか通さないため、**サニタイズ対象外の untrusted 文字列が永続キャッシュに
	// 載り続ける**形だった。既存キャッシュに `"Raw"` が残っていても、Go の JSON は
	// 未知フィールドを無視するので読み込みは壊れない。
	Percent int       // 使用率 0-100
	ResetAt time.Time // 枠がリセットされる時刻 (ローカルタイム)
	// Source は枠の出所 (SourceCodex = codex、空文字 = Claude Code)。omitempty により
	// codex 対応前のディスクキャッシュ (フィールドなし) は Claude 枠として読める。
	Source string `json:",omitempty"`
	// WindowMins は枠の長さ (分)。codex は API の windowDurationMins、Claude は元ラベル
	// ("Current session" 等) から確定する。0 = 不明 (窓幅を返さない codex 枠 / この項目が
	// 無い頃のディスクキャッシュ)。全画面ダッシュボードの盤はこれが無いと描けないので、
	// 不明のときはテキストカードへ落ちる (dial.go)。
	WindowMins int64 `json:",omitempty"`
	// Unused は「枠は存在するが、まだ消費が始まっていない」。この状態の枠はリセット時刻を
	// 持たない (codex の rateLimits は resetsAt を null で返す。窓は最初の消費で開くため、
	// 開いていない窓に締め切りは無い)。ResetAt はゼロ値で、経過率・残り時間は計算しない。
	// 🚨 以前はこの枠を「表示情報が欠けている」として読み飛ばしていたが、5h を使っていない
	// 時間帯にダッシュボードの 5h カードが黙って消え、壊れたように見えた (ユーザー報告 2026-09-03)。
	// 「無い」と「まだ使っていない」は別の事実なので、型で区別して描画側に伝える。
	Unused bool `json:",omitempty"`
	// Pending は「この枠はまだ取得中」の場所取り (PendingWindows が作る。値は持たない)。取得を待つ間も
	// 表と盤のレイアウトを、届いた後と同じにするため (issue 626)。表示だけのもので、キャッシュへは書かない。
	Pending bool `json:"-"`
	// Spinner は場所取りの語の前に添えるスピナーのコマ (呼び出し側が毎フレーム入れ替える。空なら添えない)。
	Spinner string `json:"-"`
}

// pendingWord は場所取りの枠に出す語。
const pendingWord = "取得中..."

// pendingText は場所取りの枠に出す文字列 (スピナーのコマ + 語)。
func pendingText(w Window) string {
	if w.Spinner == "" {
		return pendingWord
	}
	return w.Spinner + " " + pendingWord
}

// PendingWindows は出所 src の場所取りの枠を返す。Claude は描く枠が defaultOrder で決まっているので
// 届いた後と同じ枠になる。codex は枠の構成がプランで変わるので、like (前回取れた枠。古くてよい) にある codex の枠の
// 形を写し、無ければよくある形 (weekly 1 枠) を置く (外れたら届いたときに行の数が変わる)。
//
// 値 (Percent / ResetAt) は表示しない目安で、盤のカードの見出しと下の段の形 (中央の大きな数字が入るか・下の段を
// 何行に畳むか。dial.go の renderCard / cardHead) を届いた後と揃えるために持つ。like の同じ枠の値 (前回) を写し、
// 無ければ 2 桁の使用率だけを置く (外れると盤のカードの中の形が変わりうる)。
func PendingWindows(src string, like []Window) []Window {
	const guess = 50
	// Unused も目安として残す (盤の下の段の形が変わるため)。場所取りのカードは語も値も描かない (dial.go の cardFrame / textCardBody)
	pending := func(w Window) Window {
		w.Pending = true
		return w
	}
	if src == SourceCodex {
		var ws []Window
		for _, w := range like {
			if w.Source == SourceCodex {
				ws = append(ws, pending(w))
			}
		}
		if len(ws) > 0 {
			return ws
		}
		week := int64((7 * 24 * time.Hour) / time.Minute)
		return []Window{{Label: codexLabel(&week), Source: SourceCodex, WindowMins: week, Percent: guess, Pending: true}}
	}
	prev := &Snapshot{Windows: like}
	// 窓幅は /usage の元ラベルから決める経路 (windowMinsFor) を通す (Claude 枠の窓幅の出典を 1 つに保つ)
	mins := map[string]int64{"5h": windowMinsFor("Current session"), "7d": windowMinsFor("Current week")}
	ws := make([]Window, 0, len(defaultOrder))
	for _, l := range defaultOrder {
		if w, ok := prev.Find(l); ok && w.Source == "" {
			if w.WindowMins == 0 {
				w.WindowMins = mins[l] // 窓幅を持たない頃のキャッシュ (Window.WindowMins の doc)
			}
			ws = append(ws, pending(w))
			continue
		}
		ws = append(ws, Window{Label: l, WindowMins: mins[l], Percent: guess, Pending: true})
	}
	return ws
}

// Span は枠の長さ。不明なら 0 (呼び出し側が「窓のどこにいるか」を出さない判断に使う)。
func (w Window) Span() time.Duration { return time.Duration(w.WindowMins) * time.Minute }

// Snapshot は `/usage` 一回分のパース結果。
type Snapshot struct {
	Windows []Window
	Version string // Claude Code の CLI バージョン ("2.1.216" 等)。取得失敗時は空
	// CodexVersion は codex CLI のバージョン ("0.144.6" 等)。未導入・取得失敗時は空
	// (omitempty により codex バージョン対応前のディスクキャッシュも欠損として読める)。
	CodexVersion string `json:",omitempty"`
	// ClaudeErr は Claude 側の最後の取得が失敗したときの理由 (Snapshot.With が載せ、Claude が取れたら消す)。
	// codex だけ取れた回も表示は成立するので、これが無いと呼び出し側は「Claude が取れない」ことも理由も
	// 知る手段が無い (前回の Claude 枠が残り続け、古い値が黙って表示される)。
	ClaudeErr string `json:",omitempty"`
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

// claudeResult は `claude ... --output-format stream-json` の行の必要フィールドだけ。
type claudeResult struct {
	Type    string  `json:"type"`
	Result  *string `json:"result"`
	IsError bool    `json:"is_error"`
	// UsageReport は assistant 行 (local command の /usage) にだけ載る。rate_limits が null =
	// CLI がサーバから枠を受け取れなかった。キーごと無い (古い版) とは区別する。
	UsageReport *struct {
		RateLimits json.RawMessage `json:"rate_limits"`
	} `json:"usage_report"`
}

// streamResult は stream-json の行を集めた結果。
type streamResult struct {
	Result   string
	IsError  bool
	noLimits bool
}

// ParseStream は `claude -p /usage --output-format stream-json --verbose` の出力を Snapshot にする。
// サーバから枠を受け取れなかった (usage_report.rate_limits が null で、枠の行が無い) ときは errNoLimits を包んで返す。
// `claude -p /usage` を自分の引数で起こす呼び出し元 (pro-con の dispatcher) も、これと FetchShared を使う。
func ParseStream(out []byte, now time.Time) (*Snapshot, error) {
	res, err := parseStream(out)
	if err != nil {
		return nil, err
	}
	if res.IsError {
		return nil, errors.New("/usage がエラーを返した")
	}
	snap, err := Parse(res.Result, now)
	switch {
	case err == nil:
		return snap, nil
	case res.noLimits:
		return nil, errNoLimits
	}
	// 実際に返った文の 1 行目を添える (未認証・書式変更などの原因を取り違えて見せない)
	if line := firstNonEmptyLine(res.Result); line != "" {
		return nil, fmt.Errorf("%w: %q", err, clipRunes(termsafe.PlainLine(line), 80))
	}
	return nil, err
}

// clipRunes は s を n 文字までに切る (切ったら … を足す)。誤りに添える引用が画面と状態のファイルを広げないため。
func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// parseStream は stream-json の出力 (1 行 1 JSON) から result 行と usage_report を拾う。
// result 行は `result` キーを持ち、`type` が無いか "result" の行 (json 形式の 1 行も同じ形で読める)。
func parseStream(out []byte) (streamResult, error) {
	var res streamResult
	found := false
	for line := range strings.Lines(string(out)) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r claudeResult
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return streamResult{}, fmt.Errorf("/usage 出力の JSON パース失敗: %w", err)
		}
		if r.UsageReport != nil && string(r.UsageReport.RateLimits) == "null" {
			res.noLimits = true
		}
		if r.Result != nil && (r.Type == "" || r.Type == "result") {
			res.Result, res.IsError, found = *r.Result, r.IsError, true
		}
	}
	if !found {
		return streamResult{}, errors.New("/usage 出力に result が無い")
	}
	return res, nil
}

// fetchClaude は `claude -p "/usage"` を実行して結果をパースする。呼び出し側は Fetch (shared.go) で、
// 全プロセス共有のゲートを通してからここへ来る。
//
// --output-format stream-json で読むのは、サーバが枠を返さなかったことを区別するため: CLI はそのとき
// 枠の行を黙って省くだけで、result の文面では「書式が変わって読めない」と見分けられない。assistant 行の
// usage_report.rate_limits が null なら errNoLimits にする (2.1.287 で実測。usage_report が無い版では
// 区別できず、従来どおりパースの失敗になる)。
//
// --model haiku を明示する: /usage はローカルコマンド処理で LLM を呼ばない
// (num_turns=0・total_cost_usd=0・duration_api_ms=0 を実測) ためモデル指定は結果に影響
// しないが、将来 /usage が推論を伴う実装に変わった場合に備え最小モデルへ固定しておく (保険)。
func fetchClaude(ctx context.Context) (*Snapshot, error) {
	// バージョンは /usage と独立なので並列取得して起動 fork の直列化を避ける。取得失敗は
	// 致命ではない (バージョン表示が消えるだけ) ため error は握りつぶし空文字にする。
	verCh := make(chan string, 1)
	go func() { verCh <- FetchVersion(ctx) }()

	cmd := subproc.CommandContext(ctx, "claude", "-p", "/usage", "--model", "haiku", "--output-format", "stream-json", "--verbose")
	// claude -p は cwd の project にセッション記録 (~/.claude/projects/<cwd>/*.jsonl) を作る (実測 2026-09-26)。
	// 呼び出し元の cwd (glogx / hook ならユーザーの作業 repo) のままだと、その repo の --resume 一覧に
	// /usage だけのセッションが取得のたびに溜まるので、一時ディレクトリで起こして 1 か所に寄せる。
	// 一時ディレクトリが無い環境 (TMPDIR の指す先が消えた等) では起動自体が失敗するので、そのときは
	// 呼び出し元の cwd のまま起こす (記録の寄せ先より取得の成功を優先する)。
	if fi, err := os.Stat(os.TempDir()); err == nil && fi.IsDir() {
		cmd.Dir = os.TempDir()
	}
	// exit status だけでは原因 (PATH 上の別の claude が壊れている等) が分からないので、
	// stderr の最初の行を失敗の理由に添える。
	stderr := &headWriter{max: stderrKeep}
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if err != nil {
		// stderr は端末へ出る (ratelimit の -refresh / glogx) ので、ここで制御列を落とす
		if line := termsafe.PlainLine(firstNonEmptyLine(stderr.buf.String())); line != "" {
			return nil, fmt.Errorf("claude /usage 実行失敗: %w: %s", err, line)
		}
		return nil, fmt.Errorf("claude /usage 実行失敗: %w", err)
	}
	snap, err := ParseStream(out, time.Now())
	if err != nil {
		return nil, err
	}
	snap.Version = <-verCh
	return snap, nil
}

// FetchVersion は `claude --version` から CLI バージョン番号だけを取り出す。
// 出力例: "2.1.216 (Claude Code)" → "2.1.216"。取得・パース失敗はすべて空文字を返す
// (バージョン表示は付加情報であり、欠けても呼び出し側の主処理は成立させる)。
func FetchVersion(ctx context.Context) string {
	cmd := subproc.CommandContext(ctx, "claude", "--version")
	out, err := cmd.Output()
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
			Label:      labelFor(raw),
			Percent:    pct,
			ResetAt:    reset,
			WindowMins: windowMinsFor(raw),
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

// windowMinsFor は `/usage` の元ラベルから枠の長さ (分) を決める。Claude 側は窓幅を出力
// しないので、ラベルの語が唯一の出典になる ("Current session" = 5 時間、"Current week" 系 =
// 7 日)。未知のラベルは 0 (不明) にして、推測で盤を描かせない。
func windowMinsFor(raw string) int64 {
	switch {
	case strings.HasPrefix(raw, "Current session"):
		return int64((5 * time.Hour) / time.Minute)
	case strings.HasPrefix(raw, "Current week"):
		return int64((7 * 24 * time.Hour) / time.Minute)
	}
	return 0
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

// firstNonEmptyLine は s の空白以外を含む最初の行を前後の空白を落として返す (無ければ空)。
func firstNonEmptyLine(s string) string {
	for line := range strings.Lines(s) {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// stderrKeep は失敗の理由のために残す stderr の先頭のバイト数 (最初の行しか使わない)。
const stderrKeep = 4096

// headWriter は先頭 max バイトだけを残して残りを捨てる io.Writer。子の stderr を上限なしに
// メモリへ溜めない (Output が Stderr=nil のときに使う内部バッファも上限つき)。
type headWriter struct {
	buf bytes.Buffer
	max int
}

func (w *headWriter) Write(p []byte) (int, error) {
	if room := w.max - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil // 捨てた分も書けたことにする (子を EPIPE で止めない)
}
