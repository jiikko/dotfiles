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

// Body.Progress の計数 (parse_test の TestLoadMetaReadsTitleFrontMatterAndCheckboxes から移設。
// 対象が Issue から Body へ移ったため)。期待値はフィクスチャを手で書いてリテラルで置く
// (production と同じ regexp から期待値を作る自己言及にしない)。
func TestBodyProgressCountsCheckboxes(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"チェックボックス無し", "# t\n\n本文だけ\n", ""},
		{"1/3", "# t\n\n- [x] 済み\n- [ ] 未\n- [ ] 未\n", "1/3"},
		{"全部済み", "- [x] a\n- [X] b\n", "2/2"},
		{"大文字 X も済み扱い", "- [X] a\n- [ ] b\n", "1/2"},
		{"* と + も箇条書き", "* [x] a\n+ [ ] b\n", "1/2"},
		{"字下げ付き", "  - [x] a\n\t- [ ] b\n", "1/2"},
		{"箇条書きでない [x] は数えない", "見出し [x] ではない\n- [x] a\n", "1/1"},
		{"末尾に改行が無い", "- [x] a", "1/1"},
		{"CRLF", "- [x] a\r\n- [ ] b\r\n", "1/2"},
		{"空文字列", "", ""},
		// ⚠️ 現行はコードフェンス非対応 (例示のチェックボックスも数える)。Issue から移しただけで
		// 挙動を変えていないことを明示的に固定する (フェンス対応は別 issue)
		{"フェンス内も数える", "# t\n\n```\n- [ ] 例示\n```\n- [x] 実物\n", "1/2"},
		// ⚠️ 無害化 (termsafe.PlainLine) を通していることを固定する。これが無いと
		// 行頭に ANSI や NUL がある行を数え落とす (旧 LoadMeta も無害化後に数えていた)。
		// R1 レビューで「関門を外しても全テスト green」だったため追加
		{"行頭に ANSI がある", "\x1b[31m- [x] a\n- [ ] b\n", "1/2"},
		{"行頭に NUL がある", "\x00- [x] a\n", "1/1"},
		// ⚠️ front matter 内も数える (旧 Issue.Progress は数えなかった)。Body.Progress の
		// doc で受け入れた差なので、意図した挙動として固定する
		{"front matter 内も数える", "---\nstatus: open\n- [x] a\n---\n# T\n- [ ] b\n", "1/2"},
	}
	for _, c := range cases {
		if got := NewBody(c.src).Progress(); got != c.want {
			t.Errorf("%s: Progress() = %q; want %q", c.name, got, c.want)
		}
	}
}

// 2 回目はキャッシュを返す (追加の走査をしない)。"" もキャッシュ対象であること。
func TestBodyProgressCaches(t *testing.T) {
	b := NewBody("- [x] a\n")
	if got := b.Progress(); got != "1/1" {
		t.Fatalf("1 回目: %q", got)
	}
	b.src = "- [ ] a\n- [ ] b\n" // キャッシュが効いていれば結果は変わらない
	if got := b.Progress(); got != "1/1" {
		t.Errorf("2 回目でキャッシュが効いていない: %q", got)
	}
	// "" もキャッシュされること (毎回数え直さない)
	e := NewBody("本文だけ\n")
	if got := e.Progress(); got != "" {
		t.Fatalf("空の 1 回目: %q", got)
	}
	e.src = "- [x] a\n"
	if got := e.Progress(); got != "" {
		t.Errorf(`"" がキャッシュされていない: %q`, got)
	}
}
