package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func side(title string, items ...string) map[string]any {
	list := make([]any, len(items))
	for i, it := range items {
		list[i] = it
	}
	return map[string]any{"title": title, "items": list}
}

func cmp(left, right any) map[string]any {
	m := map[string]any{"type": "compare", "left": left}
	if right != nil {
		m["right"] = right
	}
	return m
}

func withKey(m map[string]any, k string, v any) map[string]any {
	m[k] = v
	return m
}

// code は比較カードの cmp と同じく、コードの図解を組む。lines を省くと lines キーの無い図解になる。
func code(lang string, lines ...any) map[string]any {
	m := map[string]any{"type": "code", "lang": lang}
	if len(lines) > 0 {
		m["lines"] = lines[0]
	}
	return m
}

func TestRejectBadShow(t *testing.T) {
	for _, tc := range []struct {
		show any
		want string
	}{
		{"排他ロック", "オブジェクトか null"},
		{map[string]any{"type": "chart", "text": "x"}, "show.type は code/compare/image/keyword/list/mermaid のどれか"},
		{map[string]any{"text": "x"}, "show.type は code/compare/image/keyword/list/mermaid のどれか"},
		{map[string]any{"type": "keyword", "text": "x", "color": "red"}, "に書けるのは type/text/sub だけ"},
		{map[string]any{"type": "keyword", "text": " "}, "show.text は空でない文字列"},
		{map[string]any{"type": "keyword"}, "show.text は空でない文字列"},
		{map[string]any{"type": "keyword", "text": "x", "sub": 1}, "show.sub は文字列"},
		{map[string]any{"type": "keyword", "text": strings.Repeat("語", 17)}, "show.text は 16 字まで"},
		{map[string]any{"type": "keyword", "text": "x", "sub": strings.Repeat("補", 41)}, "show.sub は 40 字まで"},
		{cmp(side("前", "a"), nil), "show.right は {"},
		{cmp("前", side("後", "b")), "show.left は {"},
		{withKey(cmp(side("前", "a"), side("後", "b")), "title", "x"), "show (compare) に書けるのは type/left/right/build だけ"},
		{cmp(withKey(side("前", "a"), "note", "x"), side("後", "b")), "show.left に書けるのは title/items だけ"},
		{cmp(side("", "a"), side("後", "b")), "show.left.title は空でない文字列"},
		{cmp(map[string]any{"items": []any{"a"}}, side("後", "b")), "show.left.title は空でない文字列"},
		{cmp(side("前", "a", ""), side("後", "b")), "show.left.items[1] は空でない文字列"},
		{cmp(side("前", "a"), side("後", " ")), "show.right.items[0] は空でない文字列"},
		{cmp(side(strings.Repeat("見", 9), "a"), side("後", "b")), "show.left.title は 8 字まで"},
		{cmp(side("前"), side("後", "b")), "show.left.items は 1〜3 個"},
		{cmp(side("前", "a", "b", "c", "d"), side("後", "b")), "show.left.items は 1〜3 個"},
		{cmp(side("前", "a"), map[string]any{"title": "後", "items": "b"}), "show.right.items は 1〜3 個"},
		{cmp(side("前", "a"), map[string]any{"title": "後", "items": []any{"b", 1}}), "show.right.items[1] は空でない文字列"},
		{cmp(side("前", "a", strings.Repeat("項", 17)), side("後", "b")), "show.left.items[1] は 16 字まで"},
		{code("a"), "show.lines は 1〜11 個"},
		{code("a", []any{}), "show.lines は 1〜11 個"},
		{code("a", "x"), "show.lines は 1〜11 個"},
		{code("a", make([]any, 12)), "show.lines は 1〜11 個"},
		{code("a", []any{"x", 1}), "show.lines[1] は文字列"},
		{code("a", []any{"\tx"}), "show.lines[0] にタブ・改行を入れない"},
		{code("a", []any{"x\ny"}), "show.lines[0] にタブ・改行を入れない"},
		{code("a", []any{strings.Repeat("x", 45)}), "show.lines[0] は幅 44 まで"},
		{code("a", []any{"x", strings.Repeat("漢", 23)}), "show.lines[1] は幅 44 まで"},
		{code("a", []any{"", "  "}), "show.lines が空行だけ"},
		{withKey(code("a", []any{"x"}), "highlight", json.Number("1")), "show.highlight は行番号 (1 始まり) のリスト"},
		{withKey(code("a", []any{"x"}), "highlight", []any{json.Number("0")}), "show.highlight は 1〜1 の行番号"},
		{withKey(code("a", []any{"x"}), "highlight", []any{json.Number("2")}), "show.highlight は 1〜1 の行番号"},
		{withKey(code("a", []any{"x"}), "highlight", []any{json.Number("1.5")}), "show.highlight は 1〜1 の行番号"},
		{withKey(code("a", []any{"x"}), "highlight", []any{"1"}), "show.highlight は 1〜1 の行番号"},
		{code(strings.Repeat("l", 13), []any{"x"}), "show.lang は幅 12 まで"},
		{code(strings.Repeat("言", 7), []any{"x"}), "show.lang は幅 12 まで"},
		{code("a", []any{"x\ry"}), "show.lines[0] にタブ・改行を入れない"},
		{withKey(code("a", []any{"x"}), "lang", 1), "show.lang は文字列"},
		{withKey(code("a", []any{"x"}), "caption", "x"), "show (code) に書けるのは type/lang/lines/highlight/steps だけ"},
		// 箇条書き (issue 680)
		{map[string]any{"type": "list", "items": []any{"a"}}, "show.items は 2〜5 個"},
		{map[string]any{"type": "list", "items": []any{"a", "b", "c", "d", "e", "f"}}, "show.items は 2〜5 個"},
		{map[string]any{"type": "list"}, "show.items は 2〜5 個"},
		{map[string]any{"type": "list", "items": []any{"a", " "}}, "show.items[1] は空でない文字列"},
		{map[string]any{"type": "list", "items": []any{strings.Repeat("項", 15), "b"}}, "show.items[0] は 14 字まで"},
		{map[string]any{"type": "list", "title": strings.Repeat("見", 17), "items": []any{"a", "b"}}, "show.title は 16 字まで"},
		{map[string]any{"type": "list", "items": []any{"a", "b"}, "build": "yes"}, "show.build は true か false"},
		{map[string]any{"type": "list", "title": " ", "items": []any{"a", "b"}}, "show.title は空白だけにしない"},
		{map[string]any{"type": "list", "title": "", "items": []any{"a", "b"}}, "show.title は空白だけにしない"},
		{map[string]any{"type": "list", "items": []any{"a", "b"}, "sub": "x"}, "show (list) に書けるのは type/title/items/build だけ"},
	} {
		path := writeScript(t, map[string]any{"lines": []any{
			map[string]any{"who": "metan", "text": "a"},
			map[string]any{"who": "metan", "text": "b", "show": tc.show},
		}})
		_, err := loadScript(path, testEnv(t))
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "lines[1].show") {
			t.Errorf("%v: %q を含む拒否にならなかった (%v)", tc.show, tc.want, err)
		}
	}
}

// fillCodeLines はコードの行を、先に置いた n 行と合わせて上限 (codeLinesMax) まで埋める残りの行。
func fillCodeLines(n int) []any {
	var rest []any
	for i := n; i < codeLinesMax; i++ {
		rest = append(rest, "x")
	}
	return rest
}

// TestAcceptShowAtLimit は、上限ちょうどの長さの日本語が通ることを確かめる (長さは文字数で数える。バイト数で数えると、
// 全角 1 字が 3 バイトなので、画面に収まる語まで止めてしまう)。
func TestAcceptShowAtLimit(t *testing.T) {
	path := writeScript(t, map[string]any{"lines": []any{
		map[string]any{"who": "metan", "text": "a", "show": map[string]any{
			"type": "keyword", "text": strings.Repeat("語", keywordTextMax), "sub": strings.Repeat("補", keywordSubMax),
		}},
		map[string]any{"who": "metan", "text": "b", "show": cmp(
			side(strings.Repeat("見", compareTitleMax), strings.Repeat("項", compareItemMax), "x", "y"),
			side("後", "z"),
		)},
		map[string]any{"who": "metan", "text": "c", "show": withKey(code(strings.Repeat("言", codeLangMax/2),
			append([]any{strings.Repeat("x", codeColsMax), strings.Repeat("漢", codeColsMax/2), ""}, fillCodeLines(3)...)),
			"highlight", []any{json.Number("1"), json.Number(fmt.Sprint(codeLinesMax))})},
		// 言語名と強調は省略でき、先頭・末尾の空行も書ける (空行だけのコードは止まる)
		map[string]any{"who": "metan", "text": "d", "show": map[string]any{"type": "code", "lines": []any{"", "x", ""}}},
		map[string]any{"who": "metan", "text": "e", "show": withKey(code("go", []any{"x"}), "highlight", []any{})},
	}})
	if _, err := loadScript(path, testEnv(t)); err != nil {
		t.Errorf("上限ちょうどの長さを拒否した: %v", err)
	}
}

func TestLineShowsPersistClearAndDedupe(t *testing.T) {
	kw := func(text string) map[string]any { return map[string]any{"type": "keyword", "text": text} }
	lines := []map[string]any{
		{"who": "metan", "text": "0"},                  // 図解の前
		{"who": "metan", "text": "1", "show": kw("A")}, // A を出す
		{"who": "metan", "text": "2"},                  // 出し続ける
		{"who": "metan", "text": "3", "show": kw("B")}, // B に替える
		{"who": "metan", "text": "4", "show": nil},     // 消す
		{"who": "metan", "text": "5"},                  // 消えたまま
		{"who": "metan", "text": "6", "show": kw("A")}, // 同じ中身は表の同じ番号
		{"who": "metan", "text": "7", "show": cmp(side("前", "a"), side("後", "b"))},
		{"who": "metan", "text": "8", "show": cmp(side("前", "a"), side("後", "c"))},   // 箇条書きが違えば別の図解
		{"who": "metan", "text": "9", "show": cmp(side("前", "a"), side("後", "b"))},   // リストを持つ図解も同じ中身なら同じ番号
		{"who": "metan", "text": "10", "show": cmp(side("前", "x"), side("後", "b"))},  // 左の項目だけが違う
		{"who": "metan", "text": "11", "show": cmp(side("前X", "a"), side("後", "b"))}, // 左の見出しだけが違う
		{"who": "metan", "text": "12", "show": cmp(side("前", "a"), side("後X", "b"))}, // 右の見出しだけが違う
	}
	idx, _, table, err := lineShows("s.json", lines)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]int, len(idx))
	for i, p := range idx {
		got[i] = -1
		if p != nil {
			got[i] = *p
		}
	}
	if want := []int{-1, 0, 0, 1, -1, -1, 0, 2, 3, 2, 4, 5, 6}; !reflect.DeepEqual(got, want) {
		t.Errorf("行ごとの図解の番号: got %v want %v", got, want)
	}
	if len(table) != 7 || !reflect.DeepEqual(table[:2], []showData{{Type: "keyword", Text: "A"}, {Type: "keyword", Text: "B"}}) ||
		table[3].Right.Items[0] != "c" {
		t.Errorf("図解の表: got %+v (want 重要語 A, B と比較 5 つ。4 つ目の右の項目は c)", table)
	}
}

// buildWithShows は testdata/build の台本を一時ディレクトリに写し、edit で行を書き換えてから assemble する。
// edit は写した先のディレクトリも受け取る (図の画像を台本の隣に置くため)。
func buildWithShows(t *testing.T, edit func(dir string, lines []any)) (*PlayerData, error) {
	t.Helper()
	env := testEnv(t)
	dir := t.TempDir()
	copyTree(t, filepath.Join("testdata", "build"), dir)
	path := filepath.Join(dir, "script.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := decodeJSON(b, &raw); err != nil {
		t.Fatal(err)
	}
	edit(dir, raw["lines"].([]any))
	if b, err = json.Marshal(raw); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := loadScript(resolvePath(path), env)
	if err != nil {
		t.Fatal(err)
	}
	data, _, err := assemble(s, env)
	return data, err
}

// TestShowDoesNotChangeAudioOrFrames は、図解を足しても各行の時刻と撮影の状態の列が変わらないことを確かめる。
// 図解は行の関数なので、mp4 が撮る状態の種類は増えない。キャッシュの鍵に show が入ると、testdata の合成済み wav が
// 引けずに assemble が落ちる。
func TestShowDoesNotChangeAudioOrFrames(t *testing.T) {
	build := func(edit func(lines []any)) *PlayerData {
		data, err := buildWithShows(t, func(_ string, lines []any) { edit(lines) })
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	plain := build(func([]any) {})
	shown := build(func(lines []any) {
		lines[1].(map[string]any)["show"] = map[string]any{"type": "keyword", "text": "排他ロック"}
		lines[3].(map[string]any)["show"] = nil
	})
	if !reflect.DeepEqual(plain.Frames, shown.Frames) {
		t.Errorf("図解で撮影の状態の列が変わった")
	}
	if len(shown.Shows) != 1 {
		t.Fatalf("図解の表: got %v", shown.Shows)
	}
	for i, l := range shown.Lines {
		on := l.Show != nil && *l.Show == 0
		if want := i == 1 || i == 2; on != want {
			t.Errorf("lines[%d] の図解: got %v want %v", i, l.Show, want)
		}
		if l.Start != plain.Lines[i].Start || l.End != plain.Lines[i].End {
			t.Errorf("lines[%d] の時刻が変わった", i)
		}
	}
}

// TestCodeHighlightNormalized は、強調する行番号を重複なしの昇順にそろえ、強調・行・言語名のどれか 1 つだけが違うコードを
// 別の図解にすることを確かめる。
func TestCodeHighlightNormalized(t *testing.T) {
	hl := func(ns ...string) map[string]any {
		list := make([]any, len(ns))
		for i, n := range ns {
			list[i] = json.Number(n)
		}
		return withKey(code("go", []any{"a", "b", "c"}), "highlight", list)
	}
	idx, _, table, err := lineShows("s.json", []map[string]any{
		{"who": "metan", "text": "0", "show": hl("3", "1", "3")},
		{"who": "metan", "text": "1", "show": hl("1", "3")}, // そろえた後は同じ中身
		{"who": "metan", "text": "2", "show": hl("2")},      // 強調だけが違う
		{"who": "metan", "text": "3", "show": withKey(code("go", []any{"a", "b", "X"}), "highlight", []any{json.Number("1"), json.Number("3")})},  // 行だけが違う
		{"who": "metan", "text": "4", "show": withKey(code("sql", []any{"a", "b", "c"}), "highlight", []any{json.Number("1"), json.Number("3")})}, // 言語名だけが違う
	})
	if err != nil {
		t.Fatal(err)
	}
	if *idx[0] != 0 || *idx[1] != 0 || *idx[2] != 1 || *idx[3] != 2 || *idx[4] != 3 {
		t.Errorf("図解の番号: got %d %d %d %d %d want 0 0 1 2 3", *idx[0], *idx[1], *idx[2], *idx[3], *idx[4])
	}
	if !reflect.DeepEqual(table[0].Marks, []int{1, 3}) {
		t.Errorf("強調する行: got %v want [1 3]", table[0].Marks)
	}
}

// TestShowKeysReadByPlayer は、build が出す図解の JSON のキーを player.html が同じ名前で読んでいることを確かめる。
// キーの名前は Go の構造体タグと player.html の 2 か所に書くので、片方だけを変えるとテストは緑のまま画面から図解の一部が消える。
func TestShowKeysReadByPlayer(t *testing.T) {
	tpl := playerTemplate(t)
	samples := []showData{
		{Type: "keyword", Text: "a", Sub: "b"},
		{Type: "compare", Left: &compareSide{Title: "a", Items: []string{"b"}}, Right: &compareSide{Title: "c", Items: []string{"d"}}, Build: true},
		// highlight と steps は台本では同時に書けないが、どちらのキーも読まれていることを 1 つのサンプルで見る
		{Type: "code", Lang: "go", Lines: []string{"a"}, Marks: []int{1}, Steps: [][]int{{1}}},
		{Type: "image", Src: "data:image/png;base64,", Alt: "b"},
		{Type: "list", Title: "a", Items: []string{"b", "c"}, Build: true},
	}
	// サンプルは build が出しうる全種類を覆う (mermaid は build で image に置き換わるので player には届かない)
	have := map[string]bool{}
	for _, sd := range samples {
		have[sd.Type] = true
	}
	for _, typ := range showTypes() {
		if typ != "mermaid" && !have[typ] {
			t.Errorf("図解の種類 %s のサンプルが無い (showParsers に足したらここにも足す)", typ)
		}
	}
	// 行の段 (timelineLine の step) を、図解の番号 (show) と一緒に showAt へ渡している (字句の検査。渡し方の誤りは実 Chrome で見る)
	if !regexp.MustCompile(`showAt\([^;]*line\.show[^;]*line\.step`).MatchString(tpl) {
		t.Error("player.html が行の step を showAt へ渡していない (段のある図解が最初の段のまま動かない)")
	}
	if !strings.Contains(tpl, "未知の図解") {
		t.Error("player.html の showAt に、未知の種類を見えるようにする分岐が無い")
	}
	// branch は種類 typ の分岐 (図解の関数 showAt / drawShow の中の `sd.type === 'typ'` から次の `} else` まで) をつないだもの。
	// 描画は drawShow、段の切り替えは showAt にあり、同じ種類の分岐が両方にある (どちらかで読まれていればよい)。
	// キーはその種類の分岐の中で読まれている必要がある。
	// 検出しない形 (字句の検査の限界。脅威モデルはうっかりした編集): 分岐の中の入れ子の `} else` (誤った red になる) /
	// 2 つの関数の外の helper に読む処理を移す (分岐から読まれなくなるので red になる)
	// 2 つの関数の本体を別々に切り出す (分岐の終わりを探すときに、関数の境を越えて次の関数の分岐まで取らないため)
	var bodies []string
	for _, fn := range []string{"function showAt(", "function drawShow("} {
		i := strings.Index(tpl, fn)
		if i < 0 {
			t.Fatalf("player.html に %s が無い", fn)
		}
		part := tpl[i:]
		if j := strings.Index(part[1:], "\n  function "); j >= 0 {
			part = part[:j+1]
		}
		bodies = append(bodies, part)
	}
	branch := func(typ string) string {
		var out string
		for _, rest := range bodies {
			for {
				i := strings.Index(rest, "sd.type === '"+typ+"'")
				if i < 0 {
					break
				}
				seg := rest[i:]
				if j := strings.Index(seg, "} else"); j >= 0 {
					seg = seg[:j]
				}
				out += seg
				rest = rest[i+1:]
			}
		}
		if out == "" {
			t.Errorf("図解の関数の中に種類 %s の分岐が無い", typ)
		}
		return out
	}
	// 逆向き: プレイヤーが読む sd.<キー> は、どれかのサンプルの JSON に出ている (Go 側で JSON に出なくなった欄を見つける)。
	// 検出しない形: 2 つの関数の中のコメントに書いた sd.<語> も拾う (誤った red になる。コメントでは sd. と書かない) /
	// 全種類のキーの和集合と比べるので、ある種類の分岐が別の種類のキー (code の分岐が sd.text を読む等) を読んでも通る
	written := map[string]bool{}
	for _, sd := range samples {
		b, err := json.Marshal(sd)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		for k := range m {
			written[k] = true
		}
	}
	for _, body := range bodies {
		for _, m := range regexp.MustCompile(`\bsd\.(\w+)`).FindAllStringSubmatch(body, -1) {
			if !written[m[1]] {
				t.Errorf("player.html が sd.%s を読むが、build の図解の JSON に %s が出ない", m[1], m[1])
			}
		}
	}
	// 段: 見せていない項目を隠す CSS と付ける JS、強調の CSS と付ける JS、項目に付ける印 (data-row) と選ぶ側の印がそれぞれ揃っている
	for _, pin := range []struct{ re, what string }{
		{`\.show-card \.pending \{[^}]*visibility: hidden`, "段で隠す項目の CSS (.show-card .pending)"},
		{`classList\.toggle\('pending',`, "段で隠す項目に pending を付ける JS"},
		{`\.show-code \.ln\.hl \{`, "強調する行の CSS (.show-code .ln.hl)"},
		{`classList\.toggle\('hl',`, "強調する行に hl を付ける JS"},
	} {
		if !regexp.MustCompile(pin.re).MatchString(tpl) {
			t.Errorf("player.html に %s が無い", pin.what)
		}
	}
	if !strings.Contains(tpl, "li.dataset.row = j") || !strings.Contains(tpl, "li[data-row]") {
		t.Error("比較の項目に付ける印 (dataset.row) と、段で選ぶ側 (li[data-row]) が揃っていない")
	}
	var sdType string
	reads := func(prefix, key string) bool {
		return regexp.MustCompile(`\b` + regexp.QuoteMeta(prefix+key) + `\b`).MatchString(branch(sdType))
	}
	for _, sd := range samples {
		sdType = sd.Type
		b, err := json.Marshal(sd)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(tpl, "sd.type === '"+sd.Type+"'") {
			t.Errorf("player.html に図解の種類 %s の描画が無い", sd.Type)
		}
		for k, v := range m {
			if !reads("sd.", k) {
				t.Errorf("%s の %s を player.html が読んでいない (sd.%s が無い)", sd.Type, k, k)
			}
			if side, ok := v.(map[string]any); ok {
				for sk := range side {
					if !reads("side.", sk) {
						t.Errorf("%s.%s の %s を player.html が読んでいない (side.%s が無い)", sd.Type, k, sk, sk)
					}
				}
			}
		}
	}
}

// TestPlayerPlacesCharsForShowsFromStart は、図解を使う台本では最初から立ち絵を縮めた位置に置くことを確かめる (issue 645)。
// 図解を出すときだけ縮めると、mp4 は場面ごとの静止画をつなぐので立ち絵が瞬間移動する。印はまとめ撮りの分岐より前に付ける
// (後ろだと、mp4 の絵では図解が出るまで立ち絵が大きいまま)。
func TestPlayerPlacesCharsForShowsFromStart(t *testing.T) {
	tb, err := os.ReadFile(testEnv(t).Template())
	if err != nil {
		t.Fatal(err)
	}
	tpl := string(tb)
	if !regexp.MustCompile(`(?m)^\.stage\.has-shows \{ --char-w: 26; --char-h: 72; \}`).MatchString(tpl) {
		t.Error("図解を使う台本で立ち絵を縮める CSS (.stage.has-shows .char) が無い")
	}
	toggle := regexp.MustCompile(`\$\('stage'\)\.classList\.toggle\('has-shows', !!\(D\.shows && D\.shows\.length\)\)`).FindStringIndex(tpl)
	sheet := strings.Index(tpl, "location.hash.match(/^#sheet=")
	switch {
	case toggle == nil:
		t.Error("図解の有無で舞台に has-shows を付けていない")
	case sheet < 0:
		t.Error("まとめ撮りの分岐が見つからない (テストの前提が変わった)")
	case toggle[0] > sheet:
		t.Error("has-shows を付けるのがまとめ撮りの分岐より後ろにある (mp4 では図解が出るまで立ち絵が大きい)")
	}
}

// TestSheetFragmentMatchesPlayer は、mp4 のまとめ撮りで Go が作る #sheet= の断片を、player.html の正規表現が受け取ることを確かめる。
// 合わないとプレイヤーは通常の画面のまま撮られ、動画が黙って壊れる (口の段階を増やす・状態に要素を足す、で起きる。issue 650)
func TestSheetFragmentMatchesPlayer(t *testing.T) {
	re := playerSheetRegexp(t)
	tpl := playerTemplate(t)
	// 欄の意味の対応 (行, 話し中, 口, まばたき, 区切りのカード) も Go と同じ順に分解している (正規表現が合っても、順が違うと話し中と口が入れ替わる)。
	// 検出しない形: 字面を残したまま意味だけ変える書き換え (map の中で並べ替える・paint の引数の順を変える)。
	// 確実に見るには JS を node で評価する形が要る (issue 650 の敵対的レビュー 2 周目 P3-a。今はうっかりした編集だけを止める)
	// 区切りのカード (issue 681): データの cards を読み、カードの間は図解とトピック名を隠す配線
	for _, want := range []string{"for (const [li, sp, lv, bl, cd] of states)", "paint(li, sp, lv, bl, cd, false)",
		"runAt(D.cards || [], k)", "setCard(card, line);", ".stage.carding .stage-topic, .stage.carding .stage-show { visibility: hidden; }"} {
		if !strings.Contains(tpl, want) {
			t.Errorf("player.html のまとめ撮りの分解が Go の順 (行, 話し中, 口, まばたき, カード) でない (%s が無い)", want)
		}
	}
	allBlinks := 1<<len(castOrder) - 1 // 全員が目を閉じている (ビットの最大値)
	var group []visualState
	for _, li := range []int{-1, 0, 12} {
		for sp := range 2 {
			for lv := range mouthLevels {
				for _, bl := range []int{0, allBlinks} {
					for cd := range 2 {
						group = append(group, visualState{li, sp, lv, bl, cd})
					}
				}
			}
		}
	}
	for _, g := range [][]visualState{group, group[:1]} {
		if f := sheetFragment(g); !re.MatchString(f) {
			t.Errorf("player.html の正規表現 %s が Go の断片を受け取らない: %s", re, f)
		}
	}
	for v, lv := range vowelMouth {
		if lv < 0 || lv >= mouthLevels {
			t.Errorf("母音 %s の口の開き %d が段階の数 %d の外", v, lv, mouthLevels)
		}
	}
}

// playerTemplate は player.html を、JS のコメント (// から行末・/* */) を除いて返す
// (コメントに残った旧名や説明文を「読んでいる」と数えないため。文字列の中の // は player.html には無い前提)
func playerTemplate(t *testing.T) string {
	t.Helper()
	tb, err := os.ReadFile(testEnv(t).Template())
	must(t, err)
	s := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(string(tb), "")
	return regexp.MustCompile(`(?m)(^|[^:'"])//[^\n]*`).ReplaceAllString(s, "$1")
}

// playerSheetRegexp は player.html のまとめ撮りの正規表現 (location.hash.match(/^#sheet=…/)) を抜き出して RE2 にする。
// この正規表現は JS と RE2 で同じ意味に読める書き方 (^ $ \d 文字クラス) に限っている
func playerSheetRegexp(t *testing.T) *regexp.Regexp {
	t.Helper()
	ms := regexp.MustCompile(`location\.hash\.match\(/(\^#sheet=.+?\$)/\)`).FindAllStringSubmatch(playerTemplate(t), -1)
	if len(ms) != 1 {
		t.Fatalf("player.html のまとめ撮りの正規表現 (location.hash.match(/^#sheet=…$/)) が 1 つでない (%d 個)", len(ms))
	}
	re, err := regexp.Compile(ms[0][1])
	must(t, err)
	return re
}

// 箇条書きの段 (issue 680): build なら項目の数だけ "next" で強調を進められ、build でなければ "next" は止まる
func TestListShowSteps(t *testing.T) {
	list := func(build bool) map[string]any {
		return map[string]any{"type": "list", "title": "理由", "items": []any{"a", "b", "c"}, "build": build}
	}
	lines := func(show any, nexts int) []any {
		out := []any{map[string]any{"who": "metan", "text": "a", "show": show}}
		for range nexts {
			out = append(out, map[string]any{"who": "metan", "text": "b", "show": "next"})
		}
		return out
	}
	s, err := loadScript(writeScript(t, map[string]any{"lines": lines(list(true), 2)}), testEnv(t))
	must(t, err)
	_, steps, shows, err := lineShows(s.Path, s.Lines)
	must(t, err)
	if len(shows) != 1 || shows[0].stepCount() != 3 {
		t.Fatalf("箇条書きの段の数が違う: %+v", shows)
	}
	var got []int
	for _, st := range steps {
		got = append(got, *st)
	}
	if !slices.Equal(got, []int{0, 1, 2}) {
		t.Errorf("行の段が違う: %v (want [0 1 2])", got)
	}
	for _, tc := range []struct {
		show  map[string]any
		nexts int
	}{{list(true), 3}, {list(false), 1}} {
		if _, err := loadScript(writeScript(t, map[string]any{"lines": lines(tc.show, tc.nexts)}), testEnv(t)); err == nil || (tc.nexts == 1 && !strings.Contains(err.Error(), "箇条書きの build")) {
			t.Errorf("build=%v で next を %d 回書けた (段を超える / 段の無い図解)", tc.show["build"], tc.nexts)
		}
	}
}
