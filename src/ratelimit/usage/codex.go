// codex の残量取得。データ源は `codex app-server` (stdio JSON-RPC) の
// `account/rateLimits/read`。
//
// なぜこの経路か (調査 2026-07-31): codex にはスラッシュコマンドを CLI 側で処理する機構が
// なく、`codex exec "/status"` は文字列がそのままプロンプトとして LLM に渡り実トークンを
// 消費する (実測 input 24.8K)。しかも exec --json のイベントに使用率は含まれない。LLM を
// 呼ばずに使用率を返すのは app-server API だけで、codex の desktop app / IDE 拡張が使用率
// 表示に使うのと同じ経路 (ゼロトークン・実測 0.6〜1.2s)。
//
// app-server は [experimental] 表記でプロトコルが変わりうる。読むフィールドを usedPercent /
// windowDurationMins / resetsAt の 3 つに絞り、壊れたときの影響は「codex の枠が出ない」に
// 閉じる (Snapshot.With: codex の失敗は付加情報の欠けとして黙る)。スキーマの一次情報は `codex app-server generate-json-schema` の
// GetAccountRateLimitsResponse。
// (パッケージ doc は usage.go 側。このブロックはファイルコメント)

package usage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"subproc"
)

// SourceCodex は codex 由来の Window を示す Source 値 (空文字 = Claude Code)。
const SourceCodex = "codex"

// codexRequests は app-server へ流す JSON-RPC 3 行。initialize の応答を待たずに全行書いて
// よい (server は行を順に処理する。実測で成立)。id=2 が rateLimits 要求。
const codexRequests = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"glogx","title":"glogx","version":"0"}}}
{"jsonrpc":"2.0","method":"initialized"}
{"jsonrpc":"2.0","id":2,"method":"account/rateLimits/read","params":{}}
`

// codexRateLimitsID は codexRequests 中の rateLimits 要求の JSON-RPC id。
const codexRateLimitsID = 2

// FetchCodex は `codex app-server` を起動して codex の利用枠を取得する。ctx はタイムアウト
// 付きで渡す契約 (Fetch と同じ)。無期限 ctx だと server 無応答時に stdout 待ちで返らない。
//
// 🚨 応答を受信する前に stdin を閉じないこと: app-server は stdin の EOF で shutdown し、
// 処理中の要求への応答を捨てる (実測 2026-07-31。パイプで 3 行流して即 EOF にすると
// initialize 応答すら返らない)。stdin を開いたまま stdout を待ち、応答受信後に ctx cancel で
// プロセスを終了させる。
//
// 未確認リスク: ctx が timeout したとき kill されるのは app-server 本体だけで、その子孫は残りうる
// (subproc.CommandContext はプロセスグループを作らない)。通常の経路では app-server は自分の子を
// 片付けて終わる (実測 2026-09-26: 取得を 4 回回して孤児・残存とも 0)。timeout 経路で子孫が残る
// 報告が出たら、subproc に「グループごと止める」起動口を足すことを検討する (git の対話入力のように
// 端末の前景グループに居る必要があるコマンドには使えないので、全体の既定にはしない)。
func FetchCodex(ctx context.Context) ([]Window, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := subproc.CommandContext(ctx, "codex", "app-server")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex app-server stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex app-server 起動失敗: %w", err)
	}
	// cancel でプロセスを止めてから回収する。stdin は開いたままでよい (kill 後の Wait は
	// WaitDelay がパイプの残留を強制解決する)。
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()
	// ctx が終わったら読み口を閉じて Scan を返させる。WaitDelay がパイプを強制的に閉じるのは Wait の中
	// だけで、Scan は Wait より前にいる。app-server の子孫が stdout の書き込み端を握ったまま応答しないと、
	// kill しても Scan はその子孫が終わるまで返らなかった (実測: timeout 2 秒で 20 秒)。
	stop := context.AfterFunc(ctx, func() { _ = stdout.Close() })
	defer stop()
	if _, err := io.WriteString(stdin, codexRequests); err != nil {
		return nil, fmt.Errorf("codex app-server への書き込み失敗: %w", err)
	}
	sc := bufio.NewScanner(stdout)
	// 既定の 64KB 上限だと将来フィールドが太った応答行で黙って取りこぼすため余裕を持たせる。
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		ws, found, err := parseCodexRPCLine(sc.Bytes())
		if found {
			return ws, err
		}
	}
	// 読み口を閉じた (= ctx が終わった) ときの読み取りエラーは timeout として返す
	if err := ctx.Err(); err != nil {
		// 包む err が deadline (timeout) か canceled (呼び出し側の中断) かを区別する
		return nil, fmt.Errorf("codex app-server の応答を待つあいだに打ち切られた: %w", err)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("codex app-server 出力の読み取り失敗: %w", err)
	}
	return nil, errors.New("codex app-server が rateLimits 応答を返さず終了した")
}

// FetchCodexVersion は `codex --version` から CLI バージョン番号だけを取り出す
// (Claude 側 FetchVersion の codex 版)。取得・パース失敗はすべて空文字を返す
// (バージョン表示は付加情報であり、欠けても呼び出し側の主処理は成立させる)。
func FetchCodexVersion(ctx context.Context) string {
	cmd := subproc.CommandContext(ctx, "codex", "--version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseCodexVersion(string(out))
}

// parseCodexVersion は `codex --version` の出力末尾トークンを返す純関数。
// "codex-cli 0.144.6" → "0.144.6" (Claude 側 parseVersion が先頭トークンなのは
// "2.1.216 (Claude Code)" と並びが逆のため)。空・空白のみは空文字。
func parseCodexVersion(out string) string {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

// codexRPCLine は app-server が返す JSON-RPC 行の必要フィールドだけ。id は応答・server 発の
// 要求の両方に付くため、Method が空 (= 応答) かどうかで区別する。
type codexRPCLine struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *codexRPCError  `json:"error"`
}

type codexRPCError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

// parseCodexRPCLine は JSON-RPC の 1 行を解釈する。found は「rateLimits 要求への応答行
// だった」こと (通知・他 id の行は false で読み飛ばす)。応答行のときだけ ws/err が意味を持つ。
func parseCodexRPCLine(line []byte) (ws []Window, found bool, err error) {
	var msg codexRPCLine
	if json.Unmarshal(line, &msg) != nil {
		return nil, false, nil // JSON でない行 (将来の診断出力等) は読み飛ばす
	}
	if msg.ID == nil || *msg.ID != codexRateLimitsID || msg.Method != "" {
		return nil, false, nil
	}
	if msg.Error != nil {
		return nil, true, fmt.Errorf("codex rateLimits がエラーを返した: %s (code=%d)", msg.Error.Message, msg.Error.Code)
	}
	ws, err = parseCodexRateLimits(msg.Result)
	return ws, true, err
}

// codexRateLimitsResult は GetAccountRateLimitsResponse の必要部分。secondary は現行の
// plus プランでは null だが、枠構成はプラン依存なので両方拾う。
type codexRateLimitsResult struct {
	RateLimits struct {
		Primary   *codexRateWindow `json:"primary"`
		Secondary *codexRateWindow `json:"secondary"`
	} `json:"rateLimits"`
}

// codexRateWindow の usedPercent はスキーマ上 int32 だが、rollout ログの同系イベントでは
// 8.0 のような float で観測されているため float64 で受けて丸める (プロトコル揺れへの耐性)。
type codexRateWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins *int64  `json:"windowDurationMins"`
	ResetsAt           *int64  `json:"resetsAt"` // Unix 秒
}

// parseCodexRateLimits は rateLimits 応答の result から利用枠を抽出する。resetsAt が null の
// 枠は「まだ消費が始まっていない」枠として Unused で返す (窓は最初の消費で開くので、開く前の
// 窓にはリセット時刻が無い。実測: 5h を使っていない時間帯の primary が
// `usedPercent: 0, windowDurationMins: 300, resetsAt: null`)。1 枠も取れなければエラー。
func parseCodexRateLimits(result []byte) ([]Window, error) {
	var res codexRateLimitsResult
	if err := json.Unmarshal(result, &res); err != nil {
		return nil, fmt.Errorf("codex rateLimits 応答のパース失敗: %w", err)
	}
	ws := make([]Window, 0, 2)
	for _, w := range []*codexRateWindow{res.RateLimits.Primary, res.RateLimits.Secondary} {
		if w == nil {
			continue
		}
		win := Window{
			Label:      codexLabel(w.WindowDurationMins),
			Source:     SourceCodex,
			Percent:    int(math.Round(w.UsedPercent)),
			WindowMins: codexWindowMins(w.WindowDurationMins),
		}
		if w.ResetsAt == nil {
			win.Unused = true
		} else {
			win.ResetAt = time.Unix(*w.ResetsAt, 0)
		}
		ws = append(ws, win)
	}
	if len(ws) == 0 {
		return nil, errors.New("codex rateLimits 応答に利用枠がない")
	}
	return ws, nil
}

// codexWindowMins は API の windowDurationMins をそのまま枠の長さ (分) にする。null は 0
// (不明)。負値も 0 に倒す — 窓幅は経過割合の分母なので、負のまま通すと盤が破綻する。
func codexWindowMins(mins *int64) int64 {
	// 🚨 上限も見る。Span() は分を time.Duration へ掛けるので、巨大値はオーバーフローして
	// **負や 0** になる (実測: 200000000 分 → -1790762h、MaxInt64 → -1m)。結果は
	// 「窓幅不明」へ落ちるので事故にはならないが、負を弾く宣言だけして通していた。
	if mins == nil || *mins <= 0 || *mins > maxWindowMins {
		return 0
	}
	return *mins
}

// maxWindowMins は枠の長さとして受け付ける上限 (366 日)。これを超える窓は実在せず、
// Span() のオーバーフローを構造的に避ける。
const maxWindowMins = 366 * 24 * 60

// codexLabel は枠の窓幅 (分) を "cx7d" / "cx5h" のような短ラベルへ写像する。cx 接頭辞で
// Claude の枠 ("5h"/"7d") と識別する。窓幅不明 (null) は素の "cx"。
func codexLabel(mins *int64) string {
	if mins == nil {
		return "cx"
	}
	m := *mins
	switch {
	case m > 0 && m%(24*60) == 0:
		return fmt.Sprintf("cx%dd", m/(24*60))
	case m > 0 && m%60 == 0:
		return fmt.Sprintf("cx%dh", m/60)
	default:
		return fmt.Sprintf("cx%dm", m)
	}
}

// HasClaude は Snapshot が Claude Code 由来の枠を含むか。usage キャッシュは Claude 枠を
// 必須、codex 枠を best-effort とするため、保存・読み込み時の完全性判定に使う。
func (s *Snapshot) HasClaude() bool {
	for _, w := range s.Windows {
		if w.Source == "" {
			return true
		}
	}
	return false
}

// HasCodex は Snapshot が codex 由来の枠を含むか (タイトル表記の出し分け用)。
func (s *Snapshot) HasCodex() bool {
	for _, w := range s.Windows {
		if w.Source == SourceCodex {
			return true
		}
	}
	return false
}

// Part は 1 つの出所 (Claude Code / codex) の 1 回分の取得結果。Snapshot.With で表示中の Snapshot へ入れる。
// 出所ごとに分けて返すのは、速い方を遅い方に待たせずに表示するため (glogx。issue 626。以前は両方を
// 待ってから併合する FetchAll だった)。
type Part struct {
	Source  string   // 空文字 = Claude Code / SourceCodex = codex (Window.Source と同じ語彙)
	Windows []Window // Err が nil なら 1 枠以上 (Fetch / FetchCodex は 0 枠をエラーにする)
	Version string   // CLI のバージョン。取得失敗時は空 (枠の取得とは独立に取る)
	Err     error
}

// FetchClaudePart は Claude Code の残量を Part で返す。
func FetchClaudePart(ctx context.Context) Part {
	snap, err := Fetch(ctx)
	if err != nil {
		return Part{Err: err}
	}
	return Part{Windows: snap.Windows, Version: snap.Version}
}

// FetchCodexPart は codex の残量を Part で返す。バージョンは rateLimits と独立なので並列に取る
// (Claude 側 Fetch と同じ理由)。バージョンの取得失敗は空文字 = 表示が消えるだけで、枠の取得には影響しない。
func FetchCodexPart(ctx context.Context) Part {
	verCh := make(chan string, 1)
	go func() { verCh <- FetchCodexVersion(ctx) }()
	ws, err := FetchCodex(ctx)
	return Part{Source: SourceCodex, Windows: ws, Version: <-verCh, Err: err}
}

// With は s (nil = まだ何も無い) に p を入れた新しい Snapshot を返す (s は書き換えない。glogx は
// Snapshot のポインタを描画キャッシュの鍵にしているので、取得のたびに別の値にする)。
//
// 不変条件: 枠は、その出所の一時失敗では失わない (last-good を出所ごとに持つ)。
//   - p が成功: その出所の枠とバージョンを p で置き換える。Claude が成功したら ClaudeErr を消す
//   - p が失敗: その出所の枠は s のまま残す。Claude の失敗は理由を ClaudeErr に載せる (残した枠が古いことを
//     表示側が伝えるため)。codex の失敗は付加情報の欠けとして黙る
//
// 枠の並びは Claude が先・codex が後に保つ (届いた順に依らない)。
func (s *Snapshot) With(p Part) *Snapshot {
	out := &Snapshot{}
	if s != nil {
		*out = *s // 枠の slice は下で作り直すので共有のままでよい (s の要素は書き換えない)
	}
	if p.Version != "" {
		if p.Source == SourceCodex {
			out.CodexVersion = p.Version
		} else {
			out.Version = p.Version
		}
	}
	if p.Err != nil {
		if p.Source != SourceCodex {
			out.ClaudeErr = p.Err.Error()
		}
		return out
	}
	if p.Source != SourceCodex {
		out.ClaudeErr = ""
	}
	var claude, codex []Window
	for _, w := range out.Windows {
		if w.Source == p.Source {
			continue
		}
		if w.Source == SourceCodex {
			codex = append(codex, w)
		} else {
			claude = append(claude, w)
		}
	}
	if p.Source == SourceCodex {
		codex = p.Windows
	} else {
		claude = p.Windows
	}
	out.Windows = append(append([]Window(nil), claude...), codex...)
	return out
}
