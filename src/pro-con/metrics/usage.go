package metrics

// PG の枠 (issue 516。数え方は 449 の「測り方」): PG の transcript の各応答の message.usage を、同じ message.id の重複を除いて足す。
// 再開した session の transcript は前の会話の応答を同じ message.id のまま写して持つ (2026-09-26 に C-070 の 2 本で実測) ので、
// カードの transcript をまとめて id で重複を除けば、写しを二重に数えない。1 つの応答が複数の行に分かれていれば後の行を正とする
// (同じ値で書かれるのを 2.1.282 で見たが、途中の値を先に書く版があっても少なく数えない)。止めたときの <synthetic> の記録は数えない。
// サブエージェントの transcript (<session>/subagents/*.jsonl) も PG の枠なので足す。

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Usage はカード 1 枚の PG の枠。USD / FiveHourPct は単価を知らない model が混じったら nil (Unpriced にその model)。
type Usage struct {
	Input      int64 `json:"input"`
	CacheWrite int64 `json:"cacheWrite"`
	CacheRead  int64 `json:"cacheRead"`
	Output     int64 `json:"output"`
	// Responses は数えた応答の数
	Responses int `json:"responses"`
	// USD は API の料金に換算した額、FiveHourPct はそれを 5 時間枠の % に直した粗い値 (FiveHourUSDPerPct)
	USD         *float64 `json:"usd,omitempty"`
	FiveHourPct *float64 `json:"fiveHourPct,omitempty"`
	Unpriced    []string `json:"unpriced,omitempty"`
}

// FiveHourUSDPerPct は 5 時間枠の 1% に当たる API 料金換算の額 (449 の「利用枠への換算」の粗い値。書き込み・読み・出力の重みは分けられていない)。
const FiveHourUSDPerPct = 3.5

// price は 100 万トークンあたりの入力・出力の単価 ($)。キャッシュの読みは入力の 0.1 倍、書き込みは 5 分で 1.25 倍・1 時間で 2 倍
// (449 が claude-api skill の表から引いた値)。
type price struct{ in, out float64 }

// prices は model の名前の頭 → 単価 (日付の付いた名前も頭で当てる)。
var prices = []struct {
	prefix string
	p      price
}{
	{"claude-opus-5-5", price{4, 20}},
	{"claude-sonnet-5", price{2, 10}},
	{"claude-haiku-4-5", price{1, 5}},
}

func priceOf(model string) (price, bool) {
	for _, p := range prices {
		if strings.HasPrefix(model, p.prefix) {
			return p.p, true
		}
	}
	return price{}, false
}

// tally は model ごとに足したトークン (書き込みは 5 分と 1 時間を分けて持つ: 単価が違う)。
type tally struct{ in, write5m, write1h, read, out int64 }

// tallyOf は応答 1 つのトークン。
func tallyOf(l usageLine) tally {
	u := l.Message.Usage
	t := tally{in: u.Input, read: u.CacheRead, out: u.Output}
	if u.Split != nil && u.Split.M5+u.Split.H1 == u.CacheCreation {
		t.write5m, t.write1h = u.Split.M5, u.Split.H1
	} else { // 内訳が無い・合わない: pro-con の session は 1 時間のキャッシュを使う (449 の実測) ので 1 時間で数える
		t.write1h = u.CacheCreation
	}
	return t
}

// response は message.id ごとの応答 (後の行で上書きする)。
type response struct {
	model string
	t     tally
}

type usageLine struct {
	Type    string `json:"type"`
	Message *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input         int64 `json:"input_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
			Output        int64 `json:"output_tokens"`
			Split         *struct {
				M5 int64 `json:"ephemeral_5m_input_tokens"`
				H1 int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
}

// TranscriptFiles は session の transcript と、そのサブエージェントの transcript。
func TranscriptFiles(transcript string) []string {
	subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents", "*.jsonl"))
	return append([]string{transcript}, subs...)
}

// ReadUsage は files の transcript の応答を message.id の重複を除いて足す。1 つでも読めなければエラー (一部だけ足した値を出さない)。
func ReadUsage(files []string) (Usage, error) {
	byID := map[string]response{}
	for _, f := range files {
		if err := eachUsage(f, func(l usageLine) {
			if m := l.Message; m.ID != "" && m.Model != "<synthetic>" {
				byID[m.ID] = response{model: m.Model, t: tallyOf(l)}
			}
		}); err != nil {
			return Usage{}, err
		}
	}
	byModel := map[string]*tally{}
	for _, r := range byID {
		t := byModel[r.model]
		if t == nil {
			t = &tally{}
			byModel[r.model] = t
		}
		t.in, t.write5m, t.write1h, t.read, t.out = t.in+r.t.in, t.write5m+r.t.write5m, t.write1h+r.t.write1h, t.read+r.t.read, t.out+r.t.out
	}
	out := Usage{Responses: len(byID)}
	usd := 0.0
	models := make([]string, 0, len(byModel))
	for m := range byModel {
		models = append(models, m)
	}
	sort.Strings(models)
	for _, m := range models {
		t := byModel[m]
		out.Input += t.in
		out.CacheWrite += t.write5m + t.write1h
		out.CacheRead += t.read
		out.Output += t.out
		p, ok := priceOf(m)
		if !ok {
			out.Unpriced = append(out.Unpriced, m)
			continue
		}
		usd += (float64(t.in)*p.in + float64(t.write5m)*p.in*1.25 + float64(t.write1h)*p.in*2 + float64(t.read)*p.in*0.1 + float64(t.out)*p.out) / 1e6
	}
	if len(out.Unpriced) == 0 {
		usd = math.Round(usd*100) / 100
		pct := math.Round(usd/FiveHourUSDPerPct*100) / 100
		out.USD, out.FiveHourPct = &usd, &pct
	}
	return out, nil
}

// eachUsage は transcript の usage を持つ assistant の行ごとに f を呼ぶ (読めない行は飛ばす: 書きかけの末尾の行など)。
func eachUsage(path string, f func(usageLine)) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = fh.Close() }() // 読むだけ
	r := bufio.NewReaderSize(fh, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if bytes.Contains(line, []byte(`"usage"`)) { // 大半の行 (道具の結果など) は解かない
			var l usageLine
			if json.Unmarshal(line, &l) == nil && l.Type == "assistant" && l.Message != nil && l.Message.Usage != nil {
				f(l)
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
