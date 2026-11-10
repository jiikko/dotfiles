// Package llm は単位の列を claude -p に渡し、話題への分類 (board.Response) を返させる。
//
// 送るのは期間内の発言と、その文脈として出す期間前の発言だけ。応答の形は --json-schema で固定し、
// 中身の正しさは信じない (検査は board.Assemble)。
package llm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jiikko/dotfiles/src/termsafe"
	"slack-bbs/board"
	"subproc"
)

// Runner は claude を引数 args・標準入力 stdin で 1 回起動し、stdout を返す。
type Runner interface {
	Run(ctx context.Context, args []string, stdin []byte) ([]byte, error)
}

// ExecRunner は実物の claude (Bin) を起動する。
type ExecRunner struct {
	Bin string
}

// Run は Bin を一時ディレクトリで起動する (cwd の repo の CLAUDE.md や設定を拾わない)。
func (r ExecRunner) Run(ctx context.Context, args []string, stdin []byte) ([]byte, error) {
	cmd := subproc.CommandContext(ctx, r.Bin, args...)
	cmd.Dir = os.TempDir()
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w: %s", r.Bin, err, termsafe.ClipRunes(termsafe.PlainLine(errb.String()), 400))
	}
	return out.Bytes(), nil
}

// Schema は claude -p --json-schema に渡す応答の形 (board.Response と同じ)。
const Schema = `{"type":"object","additionalProperties":false,"required":["topics"],"properties":{"topics":{"type":"array","items":{` +
	`"type":"object","additionalProperties":false,"required":["title","state","members","conclusion","background","next","labels"],"properties":{` +
	`"title":{"type":"string"},` +
	`"state":{"type":"string","enum":["decided","open","info"]},` +
	`"members":{"type":"array","items":{"type":"string"}},` +
	`"conclusion":` + point + `,` +
	`"background":{"type":"array","items":` + point + `},` +
	`"next":{"type":"array","items":` + point + `},` +
	`"labels":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["kind","reason","refs"],"properties":{"kind":{"type":"string","enum":["action","must_read"]},"reason":{"type":"string"},"refs":{"type":"array","items":{"type":"string"}}}}}` +
	`}}}}}`

const point = `{"type":"object","additionalProperties":false,"required":["text","refs"],"properties":{"text":{"type":"string"},"refs":{"type":"array","items":{"type":"string"}}}}`

// Classifier は分類のしかたの名前: モデルと、指示文・応答の形のハッシュの組。キャッシュの分類はこれが同じときだけ使い回す
// (指示文を変えたら、前の指示文で作った分類を使い回さない)。
func Classifier(model string) string { return classifierOf(model, instructions+Schema) }

func classifierOf(model, prompt string) string {
	h := sha256.Sum256([]byte(prompt))
	return model + "@" + hex.EncodeToString(h[:6])
}

// Args は claude -p の引数。
//
// --setting-sources "" で user / project の設定を読まない: hooks (Stop hook が最後の応答を差し替えることがある) と、
// 毎回載る CLAUDE.md・rules を外す (2026-11-09 の実測で、付けると入力が約 6,300 トークン、付けないと約 11 万トークン)。
// --bare は使わない: 認証が ANTHROPIC_API_KEY に限られ、OAuth (サブスクリプション) で動かない。
// --no-session-persistence: 付けないと、送った会話の本文がセッションとして ~/.claude に残る。
// --tools "" と --strict-mcp-config: 分類に道具は要らない (外へ書く・取りに行く経路を作らない)。
func Args(model string) []string {
	return []string{
		"-p", "--model", model,
		"--output-format", "json", "--json-schema", Schema,
		"--no-session-persistence", "--setting-sources", "", "--strict-mcp-config", "--tools", "",
	}
}

// Usage は 1 回の呼び出しの費用と量 (claude -p の json の出力から取る)。
type Usage struct {
	CostUSD      float64
	InputTokens  int
	OutputTokens int
}

type result struct {
	IsError          bool            `json:"is_error"`
	Subtype          string          `json:"subtype"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	TotalCostUSD     float64         `json:"total_cost_usd"`
	Usage            struct {
		InputTokens              int `json:"input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		OutputTokens             int `json:"output_tokens"`
	} `json:"usage"`
}

// Classify は単位を分類させる。
func Classify(ctx context.Context, r Runner, model, channel string, units []board.Unit) (board.Response, Usage, error) {
	prompt, err := Prompt(channel, units)
	if err != nil {
		return board.Response{}, Usage{}, err
	}
	out, err := r.Run(ctx, Args(model), prompt)
	if err != nil {
		return board.Response{}, Usage{}, err
	}
	return parse(out)
}

func parse(out []byte) (board.Response, Usage, error) {
	var res result
	if err := json.Unmarshal(out, &res); err != nil {
		return board.Response{}, Usage{}, fmt.Errorf("claude の出力を JSON として読めない: %w", err)
	}
	u := Usage{
		CostUSD:      res.TotalCostUSD,
		InputTokens:  res.Usage.InputTokens + res.Usage.CacheCreationInputTokens + res.Usage.CacheReadInputTokens,
		OutputTokens: res.Usage.OutputTokens,
	}
	if res.IsError {
		return board.Response{}, u, fmt.Errorf("claude が失敗を返した (%s): %s", res.Subtype, termsafe.ClipRunes(termsafe.PlainLine(res.Result), 400))
	}
	if len(res.StructuredOutput) == 0 || string(res.StructuredOutput) == "null" {
		return board.Response{}, u, errors.New("claude の出力に structured_output が無い (--json-schema が効いていない)")
	}
	var resp board.Response
	if err := json.Unmarshal(res.StructuredOutput, &resp); err != nil {
		return board.Response{}, u, fmt.Errorf("structured_output を読めない: %w", err)
	}
	return resp, u, nil
}

type inUnit struct {
	ID         string  `json:"id"`
	Thread     bool    `json:"thread,omitempty"`
	Continuing bool    `json:"continuing,omitempty"`
	Messages   []inMsg `json:"messages"`
}

type inMsg struct {
	Ts   string `json:"ts"`
	User string `json:"user"`
	Text string `json:"text"`
	Old  bool   `json:"old,omitempty"`
	Me   bool   `json:"me,omitempty"`
}

const instructions = `あなたは Slack のチャンネルの会話を、後から追いつく読み手のために話題ごとに分ける係です。
下の JSON の units は会話の単位です (スレッド 1 本、またはスレッド外の発言 1 件)。各単位をちょうど 1 つの話題に入れてください。

- members: 話題に入れる単位の id。全部の単位をどれか 1 つの話題に入れ、同じ id を 2 回使わない。入力に無い id を作らない
- 同じ件について話している単位は 1 つの話題にまとめる。雑談や単発の共有は小さな話題のままでよい
- title: 話題の見出し (日本語で短く。何の件かが分かる名詞句)
- state: decided (何かが決まった) / open (議論中・答えの出ていない質問がある) / info (共有だけで、決めることがない)
- 要約は 3 つの欄に分ける。どれも原文に書かれていることだけを書き、推測を足さない。各項目の text は 1 文で短く (60 字程度まで)。refs は根拠の発言の ts (その話題の中の発言)
  - conclusion: 今どうなっているか・何が決まったかを 1 項目 (決まっていなければ、何が論点かを書く)
  - background: そこに至った経緯・前提を 0〜3 項目 (古い順)
  - next: これから誰が何をするか・答えの出ていない問いを 0〜3 項目 (人の名前を主語にする)
- labels: 読み手 (me: true の発言を書いた人) にとっての印。付けるのは本当に当てはまるときだけで、迷ったら付けない
  - action: 読み手が返事・作業・判断をしないと進まない (名指しが無くても、読み手の担当だと原文から分かる場合)
  - must_read: 対応は要らないが、知らないと困る (決定・方針の変更・障害・締め切り)
  - reason に理由を 1 文、refs に根拠の発言の ts
- old: true の発言は、期間より前の文脈です。continuing: true の単位は、期間より前に始まり期間内に返信が付いたスレッドです
`

// PromptFile は 2 段の実行で、分類する側 (GUI のセッションのサブエージェント等) へ渡す文面。claude -p と違って
// --json-schema で形を縛れないので、応答の形と書き先を文面で渡す (形は show の側で検査する)。
func PromptFile(channel string, units []board.Unit, responsePath string) ([]byte, error) {
	p, err := Prompt(channel, units)
	if err != nil {
		return nil, err
	}
	tail := "\n応答は次の JSON Schema に合う JSON だけを、ファイル " + responsePath + " に書いてください (説明文やコードフェンスを付けない)。\n" + Schema + "\n"
	return append(p, tail...), nil
}

// Prompt は claude に渡す入力 (指示と単位の JSON)。
func Prompt(channel string, units []board.Unit) ([]byte, error) {
	in := make([]inUnit, 0, len(units))
	for _, u := range units {
		iu := inUnit{ID: u.ID, Thread: u.Thread, Continuing: u.Continuing}
		for _, m := range u.Msgs {
			iu.Messages = append(iu.Messages, inMsg{Ts: m.Ts, User: m.Name, Text: m.Text, Old: !m.New, Me: m.Mine})
		}
		in = append(in, iu)
	}
	b, err := json.Marshal(map[string]any{"channel": channel, "units": in})
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	sb.WriteString(instructions)
	sb.WriteString("\n")
	sb.Write(b)
	sb.WriteString("\n")
	return []byte(sb.String()), nil
}
