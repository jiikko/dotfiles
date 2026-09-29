package metrics_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pro-con/metrics"
)

// resp は transcript の応答の 1 行 (2.1.282 の形。cache_creation の内訳つき)。
func resp(id, model string, in, write1h, read, out int64) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":"2026-09-26T12:09:39.525Z","message":{"id":%q,"model":%q,"usage":{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,"cache_creation":{"ephemeral_1h_input_tokens":%d,"ephemeral_5m_input_tokens":0}}}}`,
		id, model, in, write1h, read, out, write1h)
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 再開した session の transcript が写して持つ前の応答 (同じ message.id)・1 つの応答の複数の行・<synthetic> は数えない。
// サブエージェントの transcript は足す。
func TestReadUsageDedupsCopiedResponses(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "p", "s1.jsonl")
	second := filepath.Join(dir, "p", "s2.jsonl")
	writeLines(t, first, resp("m1", "claude-opus-5-5", 2, 1000, 0, 100), resp("m1", "claude-opus-5-5", 2, 1000, 0, 100), `{"type":"user","message":{"content":"x"}}`)
	writeLines(t, second, resp("m1", "claude-opus-5-5", 2, 1000, 0, 100), resp("m2", "claude-opus-5-5", 3, 0, 5000, 50),
		resp("m3", "<synthetic>", 0, 0, 0, 0), `{"type":"assistant","message":{"id":"m4","model":"claude-opus-5-5","usage":`) // 書きかけの末尾
	writeLines(t, filepath.Join(dir, "p", "s2", "subagents", "agent-1.jsonl"), resp("m5", "claude-haiku-4-5-20251001", 10, 0, 0, 10))
	files := append(metrics.TranscriptFiles(first), metrics.TranscriptFiles(second)...)
	u, err := metrics.ReadUsage(files)
	if err != nil {
		t.Fatal(err)
	}
	if u.Responses != 3 || u.Input != 15 || u.CacheWrite != 1000 || u.CacheRead != 5000 || u.Output != 160 {
		t.Fatalf("足した値 = %+v", u)
	}
	// opus: (5*4 + 1000*4*2 + 5000*0.2 + 150*20)/1e6 = 0.01202、haiku: (10*1 + 10*5)/1e6 = 0.00006 → $0.01
	if u.USD == nil || *u.USD != 0.01 || u.FiveHourPct == nil || *u.FiveHourPct != 0 || len(u.Unpriced) != 0 {
		t.Fatalf("料金換算 = %v %v %v", u.USD, u.FiveHourPct, u.Unpriced)
	}
}

// 料金換算は 1 時間の書き込みを入力の 2 倍、5 分を 1.25 倍で数え、読みは model ごとの単価 (Opus 5.5 は入力の 0.05 倍) で数える。
// 5 時間枠の % は、読みを 449 が合わせたときの重み (入力の 0.1 倍) で数えた額を FiveHourUSDPerPct で割る (料金換算を割らない)。
func TestReadUsagePrices(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	five := `{"type":"assistant","message":{"id":"m2","model":"claude-opus-5-5","usage":{"input_tokens":0,"cache_creation_input_tokens":1000000,"cache_read_input_tokens":0,"output_tokens":0,"cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":1000000}}}}`
	writeLines(t, p, resp("m1", "claude-opus-5-5", 1_000_000, 1_000_000, 1_000_000, 1_000_000), five)
	u, err := metrics.ReadUsage([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	// 料金: 入力 4 + 1 時間の書き込み 8 + 読み 0.2 + 出力 20 + 5 分の書き込み 5 = $37.2
	// 5 時間枠: 読みを 0.4 と数えて $37.4 → 10.69%
	if u.USD == nil || *u.USD != 37.2 || u.FiveHourPct == nil || *u.FiveHourPct != 10.69 {
		t.Fatalf("料金換算 = %v、5 時間枠 = %v", deref(u.USD), deref(u.FiveHourPct))
	}
}

// キャッシュの読み 100 万トークンの料金は model ごとに違う (Opus 5.5 $0.20 = 入力の 0.05 倍、Sonnet 5 / 5.5 $0.20、Haiku 4.5 $0.10 = 0.1 倍)。
// 5 時間枠の % はどの model も入力の 0.1 倍の重みで数える。
func TestReadUsageCacheReadPricePerModel(t *testing.T) {
	for _, tc := range []struct {
		model    string
		usd, pct float64
	}{
		{"claude-opus-5-5", 0.2, 0.11},           // 5 時間枠: $0.40 / 3.5
		{"claude-sonnet-5", 0.2, 0.06},           // $0.20 / 3.5
		{"claude-sonnet-5-5", 0.2, 0.06},         // claude-sonnet-5 の頭で当たる (単価は同じ)
		{"claude-haiku-4-5-20251001", 0.1, 0.03}, // $0.10 / 3.5
	} {
		p := filepath.Join(t.TempDir(), "s.jsonl")
		writeLines(t, p, resp("m1", tc.model, 0, 0, 1_000_000, 0))
		u, err := metrics.ReadUsage([]string{p})
		if err != nil {
			t.Fatal(err)
		}
		if u.USD == nil || *u.USD != tc.usd || u.FiveHourPct == nil || *u.FiveHourPct != tc.pct {
			t.Errorf("%s の読み 100 万: 料金換算 = %v (want %v)、5 時間枠 = %v (want %v)", tc.model, deref(u.USD), tc.usd, deref(u.FiveHourPct), tc.pct)
		}
	}
}

func deref(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

// 単価を知らない model が混じったら、料金換算は 0 ではなく無し (どの model かを残す)。transcript が読めなければエラー。
func TestReadUsageUnknownModelAndMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, p, resp("m1", "claude-opus-5-5", 1, 0, 0, 1), resp("m2", "claude-mystery-9", 1, 0, 0, 1))
	u, err := metrics.ReadUsage([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if u.USD != nil || u.FiveHourPct != nil || len(u.Unpriced) != 1 || u.Unpriced[0] != "claude-mystery-9" || u.Input != 2 {
		t.Fatalf("単価を知らない model の扱い = %+v", u)
	}
	if _, err := metrics.ReadUsage([]string{p, p + ".missing"}); err == nil {
		t.Fatal("読めない transcript があるのに、一部だけ足した値を返した")
	}
}

// 1 つの応答が複数の行に分かれていれば後の行を正とする (途中の値を先に書いた行で少なく数えない)。
func TestReadUsageLaterLineOfSameResponseWins(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, p, resp("m1", "claude-opus-5-5", 2, 0, 0, 10), resp("m1", "claude-opus-5-5", 2, 0, 0, 150))
	u, err := metrics.ReadUsage([]string{p})
	if err != nil || u.Responses != 1 || u.Output != 150 {
		t.Fatalf("足した値 = %+v %v", u, err)
	}
}
