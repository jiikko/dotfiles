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
	// USD は API の料金に換算した額。FiveHourPct は 5 時間枠の % の粗い値で、USD を割った値ではない (fiveHourReadRate)
	USD         *float64 `json:"usd,omitempty"`
	FiveHourPct *float64 `json:"fiveHourPct,omitempty"`
	Unpriced    []string `json:"unpriced,omitempty"`
}

// FiveHourUSDPerPct は 5 時間枠の 1% に当たる額 (449 の「利用枠への換算」の粗い値。書き込み・読み・出力の重みは分けられていない)。
const FiveHourUSDPerPct = 3.5

// fiveHourReadRate は 5 時間枠の % を数えるときの読みの重み (入力の倍率)。449 は読みを入力の 0.1 倍と置いて FiveHourUSDPerPct を
// 合わせたので、% はこの重みのまま数える。🚨 料金の price.readRate に替えない: Opus 5.5 の読みの料金は 0.05 倍で、それで割ると
// 同じ使い方の % が 449 の合わせ方より小さく出る (読みが料金換算の 6〜7 割を占めた 449 の区間で 3 割ほど)。合わせ直すなら 449 の区間で測り直す
const fiveHourReadRate = 0.1

// price は 100 万トークンあたりの入力・出力の単価 ($) と、キャッシュの読みの入力に対する倍率。書き込みはどの model も入力の
// 5 分で 1.25 倍・1 時間で 2 倍 (claude-api skill の表。この表の model では、読みは Opus 5.5 が 0.05 倍 (= $0.20)・Fable 5.1 が 0.025 倍 (= $0.25) で他は 0.1 倍)。
type price struct{ in, out, readRate float64 }

// prices は model の名前の頭 → 単価 (日付の付いた名前も頭で当てる。claude-sonnet-5 は claude-sonnet-5-5 にも当たる。単価は同じ)。
var prices = []struct {
	prefix string
	p      price
}{
	{"claude-opus-5-5", price{in: 4, out: 20, readRate: 0.05}},
	{"claude-fable-5-1", price{in: 10, out: 50, readRate: 0.025}},
	{"claude-sonnet-5", price{in: 2, out: 10, readRate: 0.1}},
	{"claude-haiku-4-5", price{in: 1, out: 5, readRate: 0.1}},
}

// Priced は model の単価を持っているか (設定で選べるモデル store.Models が全部ここにあるかを store の検査が見る)。
func Priced(model string) bool {
	_, ok := priceOf(model)
	return ok
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

// cost は t を p で数えた額 ($)。readRate は読みの入力に対する倍率 (料金なら p.readRate、5 時間枠の % なら fiveHourReadRate)。
func (t tally) cost(p price, readRate float64) float64 {
	return (float64(t.in)*p.in + float64(t.write5m)*p.in*1.25 + float64(t.write1h)*p.in*2 + float64(t.read)*p.in*readRate + float64(t.out)*p.out) / 1e6
}

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
	usd, quota := 0.0, 0.0
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
		usd += t.cost(p, p.readRate)
		quota += t.cost(p, fiveHourReadRate)
	}
	if len(out.Unpriced) == 0 {
		usd = math.Round(usd*100) / 100
		pct := math.Round(math.Round(quota*100)/100/FiveHourUSDPerPct*100) / 100 // 記録済みの % と比べられるよう、セントに丸めてから割る
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
