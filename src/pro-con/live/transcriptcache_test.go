package live

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// countingCache は読んだ回数を path ごとに数える TranscriptCache。
func countingCache() (*TranscriptCache, map[string]int) {
	reads := map[string]int{}
	return &TranscriptCache{Read: func(p string) (Transcript, error) {
		reads[p]++
		return Transcript{Title: fmt.Sprintf("%s#%d", filepath.Base(p), reads[p])}, nil
	}}, reads
}

func writeFile(t *testing.T, p, s string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// 大きさも更新時刻も同じなら読み直さず前の結果を返す。どちらかが変われば読み直す。
func TestTranscriptCacheRereadsOnlyWhenChanged(t *testing.T) {
	c, reads := countingCache()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	t0 := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	writeFile(t, p, "a", t0)
	for range 3 {
		if got, err := c.Get(p); err != nil || got.Title != "s.jsonl#1" {
			t.Fatalf("変わっていないのに読み直した / 読めない: %q %v", got.Title, err)
		}
	}
	writeFile(t, p, "ab", t0) // 伸びた (更新時刻は同じ)
	if got, _ := c.Get(p); got.Title != "s.jsonl#2" {
		t.Fatalf("大きさが変わったのに前の結果: %q", got.Title)
	}
	writeFile(t, p, "ab", t0.Add(time.Second)) // 同じ大きさで書き直された
	if got, _ := c.Get(p); got.Title != "s.jsonl#3" {
		t.Fatalf("更新時刻が変わったのに前の結果: %q", got.Title)
	}
	if reads[p] != 3 {
		t.Fatalf("読んだ回数 %d (期待 3)", reads[p])
	}
}

// 読めなかった結果は覚えない (次は読み直す)。消えたファイルはエラーを返す。
func TestTranscriptCacheDoesNotKeepFailures(t *testing.T) {
	fail := true
	n := 0
	c := &TranscriptCache{Read: func(string) (Transcript, error) {
		n++
		if fail {
			return Transcript{}, errors.New("壊れた行")
		}
		return Transcript{Title: "ok"}, nil
	}}
	p := filepath.Join(t.TempDir(), "s.jsonl")
	writeFile(t, p, "a", time.Now())
	if _, err := c.Get(p); err == nil {
		t.Fatal("読めないのにエラーを返さない")
	}
	fail = false
	if got, err := c.Get(p); err != nil || got.Title != "ok" || n != 2 {
		t.Fatalf("失敗を覚えて読み直さない: %q %v (読んだ回数 %d)", got.Title, err, n)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(p); err == nil {
		t.Fatal("消えたファイルに前の結果を返した")
	}
}

// 覚えておく数には上限があり、超えたら一番長く使っていないものから捨てる (dispatcher は何日も動く)。
func TestTranscriptCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c, reads := countingCache()
	dir := t.TempDir()
	path := func(i int) string { return filepath.Join(dir, fmt.Sprintf("s%d.jsonl", i)) }
	t0 := time.Now()
	for i := range transcriptCacheMax + 1 {
		writeFile(t, path(i), "a", t0)
	}
	for i := range transcriptCacheMax {
		_, _ = c.Get(path(i))
	}
	_, _ = c.Get(path(0))                  // 0 番を使い直す (一番古いのは 1 番になる)
	_, _ = c.Get(path(transcriptCacheMax)) // 上限を超える
	if len(c.m) != transcriptCacheMax {
		t.Fatalf("覚えている数 %d (上限 %d)", len(c.m), transcriptCacheMax)
	}
	_, _ = c.Get(path(0))
	_, _ = c.Get(path(1))
	if reads[path(0)] != 1 || reads[path(1)] != 2 {
		t.Fatalf("捨てる順が違う: 0 番を %d 回・1 番を %d 回読んだ (期待 1 回・2 回)", reads[path(0)], reads[path(1)])
	}
}
