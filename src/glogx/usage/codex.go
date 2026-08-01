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
// 閉じる (FetchAll 参照)。スキーマの一次情報は `codex app-server generate-json-schema` の
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
	"os/exec"
	"time"
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
// ⚠️ 応答を受信する前に stdin を閉じないこと: app-server は stdin の EOF で shutdown し、
// 処理中の要求への応答を捨てる (実測 2026-07-31。パイプで 3 行流して即 EOF にすると
// initialize 応答すら返らない)。stdin を開いたまま stdout を待ち、応答受信後に ctx cancel で
// プロセスを終了させる。
func FetchCodex(ctx context.Context) ([]Window, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "app-server")
	cmd.WaitDelay = SubprocessWaitDelay
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
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("codex app-server 出力の読み取り失敗: %w", err)
	}
	return nil, errors.New("codex app-server が rateLimits 応答を返さず終了した")
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
// 枠は表示に必要な情報が欠けているため読み飛ばす (Claude 側 Parse がパース不能行を continue
// で捨てるのと同じ方針)。1 枠も取れなければエラー。
func parseCodexRateLimits(result []byte) ([]Window, error) {
	var res codexRateLimitsResult
	if err := json.Unmarshal(result, &res); err != nil {
		return nil, fmt.Errorf("codex rateLimits 応答のパース失敗: %w", err)
	}
	ws := make([]Window, 0, 2)
	for _, w := range []*codexRateWindow{res.RateLimits.Primary, res.RateLimits.Secondary} {
		if w == nil || w.ResetsAt == nil {
			continue
		}
		ws = append(ws, Window{
			Label:   codexLabel(w.WindowDurationMins),
			Raw:     "codex",
			Source:  SourceCodex,
			Percent: int(math.Round(w.UsedPercent)),
			ResetAt: time.Unix(*w.ResetsAt, 0),
		})
	}
	if len(ws) == 0 {
		return nil, errors.New("codex rateLimits 応答に利用枠がない")
	}
	return ws, nil
}

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

// HasCodex は Snapshot が codex 由来の枠を含むか (タイトル表記の出し分け用)。
func (s *Snapshot) HasCodex() bool {
	for _, w := range s.Windows {
		if w.Source == SourceCodex {
			return true
		}
	}
	return false
}

// FetchAll は Claude Code と codex の残量を並列取得して 1 つの Snapshot へ併合する。
// codex は付加情報の扱い: 失敗 (未インストール・未ログイン・プロトコル変更) しても Claude の
// 表示は成立させ、逆に Claude 側が失敗しても codex の枠だけの Snapshot を返す。両方失敗した
// ときだけエラー (呼び出し側の「取得失敗」表示は全滅時に限る)。
//
// ⚠️ 片側だけの失敗は err=nil で返るため、返った Snapshot は前回より枠が減っていることが
// ある。前回結果を持つ呼び出し側は MergeLastGood で欠けた出所を補完すること (これを怠ると
// 一時失敗のたびに取れていた枠が黙って消える。敵対的レビュー指摘 2026-07-31)。
func FetchAll(ctx context.Context) (*Snapshot, error) {
	type codexRes struct {
		ws  []Window
		err error
	}
	ch := make(chan codexRes, 1)
	go func() {
		ws, err := FetchCodex(ctx)
		ch <- codexRes{ws, err}
	}()
	snap, err := Fetch(ctx)
	cx := <-ch
	switch {
	case err == nil:
		snap.Windows = append(snap.Windows, cx.ws...) // codex 失敗時 ws は nil で no-op
		return snap, nil
	case cx.err == nil:
		return &Snapshot{Windows: cx.ws}, nil
	default:
		return nil, errors.Join(err, cx.err)
	}
}

// MergeLastGood は新しい取得結果 s に枠が 1 本も無い出所 (Claude / codex) の枠を、前回の
// 取得結果 prev から引き継ぐ。FetchAll は片側の一時失敗を err=nil で返すため、結果をそのまま
// 表示へ置き換えると「一度取れた表示は一時失敗で失わない」という last-good 不変条件
// (glogx 側 usageOverlay.handle の doc) が出所単位で破れる — claude の一時失敗で 5h/7d 行が
// 黙って消える。出所単位の補完で不変条件を「枠は、その出所の一時失敗では失わない」へ一般化
// する。Fetch / FetchCodex は成功時に必ず 1 枠以上返す (0 枠はエラー) ため、「出所の枠が 0 =
// その出所は今回失敗」と同値であり、失敗フラグの持ち回りは要らない。
func (s *Snapshot) MergeLastGood(prev *Snapshot) {
	if prev == nil {
		return
	}
	has := make(map[string]bool, len(s.Windows))
	for _, w := range s.Windows {
		has[w.Source] = true
	}
	for _, w := range prev.Windows {
		if !has[w.Source] {
			s.Windows = append(s.Windows, w)
		}
	}
	// バージョン表示も last-good: claude 失敗時の FetchAll (または --version だけの失敗) は
	// Version 空で返るため、前回値があれば保つ。
	if s.Version == "" {
		s.Version = prev.Version
	}
}
