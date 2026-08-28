package issues

import (
	"strings"
	"testing"

	"glogx/termwidth"
)

// textSpans はテスト用に素のテキスト 1 本のスパン列を作る。
func textSpans(s string) []span { return []span{{Text: s, Style: styleText}} }

// joinSpans は折り返し結果を 1 本の文字列へ戻す (折り返し位置を | で示す)。
func joinSpans(lines [][]span) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		var b strings.Builder
		for _, sp := range l {
			b.WriteString(sp.Text)
		}
		out = append(out, b.String())
	}
	return strings.Join(out, "|")
}

func TestWrapSpansEnglishBreaksAtSpaces(t *testing.T) {
	got := joinSpans(wrapSpans(textSpans("the quick brown fox jumps"), 12))
	if got != "the quick|brown fox|jumps" {
		t.Fatalf("英文が単語境界で折り返されていない: %q", got)
	}
}

func TestWrapSpansJapaneseBreaksBetweenClusters(t *testing.T) {
	// 日本語は空白が無いのでクラスタ間で折り返す (できないと 1 行に収まらず強制改行になる)
	lines := wrapSpans(textSpans("日本語の文章は空白が無い"), 10)
	if len(lines) < 2 {
		t.Fatalf("日本語が折り返されていない: %q", joinSpans(lines))
	}
	for i, l := range lines {
		if w := spansWidth(l); w > 10 {
			t.Fatalf("行 %d が幅を超えた: w=%d %q", i, w, joinSpans(lines))
		}
	}
}

func TestWrapSpansKinsokuKeepsPunctuationOffLineHead(t *testing.T) {
	// 「。」が行頭に来る幅を選ぶ: 禁則が効いていれば前の字と一緒に前行へ残る
	for limit := 4; limit <= 20; limit += 2 {
		for _, l := range wrapSpans(textSpans("あいうえお。かきくけこ、さしすせそ"), limit) {
			var b strings.Builder
			for _, sp := range l {
				b.WriteString(sp.Text)
			}
			if head := b.String(); strings.HasPrefix(head, "。") || strings.HasPrefix(head, "、") {
				t.Fatalf("limit=%d で句読点が行頭に来た: %q", limit, head)
			}
		}
	}
}

func TestWrapSpansProgressesWhenClusterWiderThanLimit(t *testing.T) {
	// 幅 1 に幅 2 の字を流す: 1 行 1 字で必ず前進する (無限ループ・空行の量産を防ぐ)
	lines := wrapSpans(textSpans("日本語"), 1)
	if len(lines) != 3 {
		t.Fatalf("幅 1 での分割が想定と違う: %q", joinSpans(lines))
	}
}

func TestWrapSpansPreservesTextExceptBreakSpaces(t *testing.T) {
	const src = "alpha beta gamma delta epsilon zeta"
	got := strings.ReplaceAll(joinSpans(wrapSpans(textSpans(src), 11)), "|", " ")
	if got != src {
		t.Fatalf("折り返しで本文が変わった:\n want %q\n got  %q", src, got)
	}
}

func TestWrapSpansKeepsStyleBoundaries(t *testing.T) {
	spans := []span{
		{Text: "普通の文と", Style: styleText},
		{Text: "コード", Style: styleCodeSpan},
		{Text: "の混在", Style: styleText},
	}
	for _, l := range wrapSpans(spans, 8) {
		for _, sp := range l {
			if strings.Contains(sp.Text, "\x1b") {
				t.Fatalf("スパンに ANSI が混ざっている: %q", sp.Text)
			}
		}
	}
	// スタイルが保たれているか (コード部分が styleCodeSpan のまま残る)
	found := false
	for _, l := range wrapSpans(spans, 8) {
		for _, sp := range l {
			if sp.Style == styleCodeSpan && strings.Contains(sp.Text, "コ") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("折り返し後に styleCodeSpan が失われた")
	}
}

func TestWrapSpansNoLimit(t *testing.T) {
	if got := joinSpans(wrapSpans(textSpans("a b c"), 0)); got != "a b c" {
		t.Fatalf("limit<=0 で折り返してしまった: %q", got)
	}
}

func TestTruncSpans(t *testing.T) {
	got := truncSpans(textSpans("abcdefghij"), 5, "…")
	if w := spansWidth(got); w > 5 {
		t.Fatalf("切り詰め後が幅を超えた: w=%d", w)
	}
	var b strings.Builder
	for _, sp := range got {
		b.WriteString(sp.Text)
	}
	if !strings.HasSuffix(b.String(), "…") {
		t.Fatalf("切り詰めたのに省略記号が付いていない: %q", b.String())
	}
	// 収まる場合はそのまま
	if got := truncSpans(textSpans("abc"), 5, "…"); spansWidth(got) != 3 {
		t.Fatalf("収まる入力を切ってしまった: %q", got)
	}
}

func TestExpandTabs(t *testing.T) {
	if got := expandTabs("a\tb"); got != "a   b" {
		t.Fatalf("タブ展開が想定と違う: %q", got)
	}
	// タブは次のタブ位置 (4 桁刻み) まで詰める: "タブ" は幅 4 なので 4 桁進んで x は 8 桁目
	if got := expandTabs("タブ\tx"); got != "タブ    x" || termwidth.Of(got) != 9 {
		t.Fatalf("タブ位置がタブストップに揃っていない: %q (w=%d)", got, termwidth.Of(got))
	}
}

// flattenSpans の分割は幅と同じエンジンを使う (issue 124)。
//
// 折り返しは「セル幅の総和」で行の幅を決めるので、分割器が幅の出典と別だと
// 総和が行全体の幅と一致せず、**limit を超えた行**が出る (silent。ASCII / CJK / 絵文字では
// 一致するので手元の目視では絶対に出ない)。render.go の dropToColumn と同型の欠陥で、
// そちらだけ直すと同じバグがここに残る。
func TestFlattenSpansClusterWidthsSumToTextWidth(t *testing.T) {
	// ⚠️ Unicode 16 で追加され、uniseg v0.4.7 (15.0) が前の文字と結合しない rune を必ず含める。
	//   これが無いと、分割を uniseg へ戻す変更が green のまま通る
	for _, s := range []string{
		"axࢗz", "a᫏b", "aᄻ8b", "aṞeb", "ჯa x",
		"ಕಾ ಕಾ", "கா", "؀ arabic", "日本語のテキスト", "⚠️ 警告 🚀", "ASCII only",
	} {
		sum := 0
		for _, c := range flattenSpans(textSpans(s)) {
			sum += c.w
		}
		if want := termwidth.Of(s); sum != want {
			t.Errorf("セル幅の総和が行全体の幅と食い違う: %q → 総和 %d / termwidth.Of %d", s, sum, want)
		}
	}
}

// 上の食い違いが実際に「limit を超える行」として現れることを、折り返しの出力側で pin する。
func TestWrapSpansNeverExceedsLimitForDivergentClusters(t *testing.T) {
	const limit = 6
	for _, s := range []string{
		"axࢗz axࢗz axࢗz",
		"a᫏b a᫏b a᫏b",
		"aᄻ8b aᄻ8b",
		"ಕಾ ಕಾ ಕಾ ಕಾ",
	} {
		for i, line := range wrapSpans(textSpans(s), limit) {
			w := 0
			for _, sp := range line {
				w += termwidth.Of(sp.Text)
			}
			if w > limit {
				t.Errorf("行が limit を超えた: %q の %d 行目 幅 %d > %d", s, i+1, w, limit)
			}
		}
	}
}
