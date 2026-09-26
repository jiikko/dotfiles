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
	// opus: (5*4 + 1000*4*2 + 5000*4*0.1 + 150*20)/1e6 = 0.01302、haiku: (10*1 + 10*5)/1e6 = 0.00006 → $0.01
	if u.USD == nil || *u.USD != 0.01 || u.FiveHourPct == nil || *u.FiveHourPct != 0 || len(u.Unpriced) != 0 {
		t.Fatalf("料金換算 = %v %v %v", u.USD, u.FiveHourPct, u.Unpriced)
	}
}

// 料金換算は 1 時間の書き込みを入力の 2 倍、5 分を 1.25 倍、読みを 0.1 倍で数え、5 時間枠の % は FiveHourUSDPerPct で割る。
func TestReadUsagePrices(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	five := `{"type":"assistant","message":{"id":"m2","model":"claude-opus-5-5","usage":{"input_tokens":0,"cache_creation_input_tokens":1000000,"cache_read_input_tokens":0,"output_tokens":0,"cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":1000000}}}}`
	writeLines(t, p, resp("m1", "claude-opus-5-5", 1_000_000, 1_000_000, 1_000_000, 1_000_000), five)
	u, err := metrics.ReadUsage([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	// 入力 4 + 1 時間の書き込み 8 + 読み 0.4 + 出力 20 + 5 分の書き込み 5 = $37.4 → 5 時間枠 10.69%
	if u.USD == nil || *u.USD != 37.4 || *u.FiveHourPct != 10.69 {
		t.Fatalf("料金換算 = %v %v", *u.USD, *u.FiveHourPct)
	}
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
