package issues

import (
	"strings"
	"testing"
)

func TestBodyCachesPerWidth(t *testing.T) {
	b := NewBody(sample)
	first := b.Lines(60, false)
	if b.renders != 1 {
		t.Fatalf("初回で整形されていない: renders=%d", b.renders)
	}
	if got := b.Lines(60, false); b.renders != 1 || len(got) != len(first) {
		t.Fatalf("同じ幅でキャッシュが効いていない: renders=%d", b.renders)
	}
	if b.Lines(40, false); b.renders != 2 {
		t.Fatalf("幅が変わったのに再整形されていない: renders=%d", b.renders)
	}
	if b.Lines(40, true); b.renders != 3 {
		t.Fatalf("colored が変わったのに再整形されていない: renders=%d", b.renders)
	}
	if b.Len() == 0 {
		t.Fatal("Len が整形後も 0")
	}
}

// URLs は本文の http(s) URL を出現順・重複なしで返す。終端の記号を URL に含めない。
func TestBodyURLs(t *testing.T) {
	src := "参考:\n" +
		"- markdown リンク: [公式](https://example.com/docs?a=1&b=2#frag)\n" +
		"- 括弧書き (https://example.com/paren)\n" +
		"- 文末の句点 https://example.com/ja。\n" +
		"- 英文の終止符 https://example.com/en.\n" +
		"- backtick 内 `https://example.com/tick`\n" +
		"- 重複 https://example.com/paren\n" +
		"- http も https://example.com/x と http://example.com/plain\n" +
		"URL でない: ftp://example.com/no  example.com/no-scheme\n"
	got := NewBody(src).URLs()
	want := []string{
		"https://example.com/docs?a=1&b=2#frag",
		"https://example.com/paren",
		"https://example.com/ja",
		"https://example.com/en",
		"https://example.com/tick",
		"https://example.com/x",
		"http://example.com/plain",
	}
	if len(got) != len(want) {
		t.Fatalf("件数が違う: got %d want %d\n%q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d 番目: got %q want %q", i, got[i], want[i])
		}
	}
}

// 整形 (Lines) の折り返しで URL が行をまたいでも、生ソースから拾うので割れない。
func TestBodyURLsIndependentOfWrapping(t *testing.T) {
	long := "https://example.com/" + strings.Repeat("segment/", 12)
	b := NewBody("本文 " + long + " の続き\n")
	b.Lines(40, false) // 狭い幅で折り返させる
	got := b.URLs()
	if len(got) != 1 || got[0] != long {
		t.Errorf("折り返しで URL が壊れた: %q", got)
	}
}

func TestBodyURLsEmpty(t *testing.T) {
	if got := NewBody("URL の無い本文\n").URLs(); len(got) != 0 {
		t.Errorf("URL が無いのに %q を返した", got)
	}
}
