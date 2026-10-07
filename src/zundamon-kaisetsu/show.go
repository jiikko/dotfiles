package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// showData は舞台の中央に出す図解 1 つ (台本の行の show)。中身は PlayerData.Shows に 1 回だけ置き、行は番号で指す
// (行ごとに中身を持たせると、同じ図解が続く行の数だけデータが複製される。画像を足すと HTML が膨らむ。issue 645)。
type showData struct {
	Type  string       `json:"type"`
	Text  string       `json:"text,omitempty"`
	Sub   string       `json:"sub,omitempty"`
	Left  *compareSide `json:"left,omitempty"`
	Right *compareSide `json:"right,omitempty"`
}

// compareSide は比較カードの片側 (見出しと箇条書き)。
type compareSide struct {
	Title string   `json:"title"`
	Items []string `json:"items"`
}

// 図解に書ける長さ (文字数) と数。カードは収まらない分を隠すので、超えると末尾が黙って切れる。上限は 720p の舞台で撮って
// 収まった長さ (issue 645 の実測)。player.html の .show-* の文字の大きさや .stage-show の箱を変えたら測り直す。
//   - 重要語: 語は全角 8 字・補足は全角 20 字ほどで折り返し、語 2 行と補足 2 行までがカードに収まった
//   - 比較: 1 列は全角 8 字ほどで折り返す。見出しは 1 行 (8 字)、項目は 2 行 (16 字) を 3 個までがカードに収まった
//     (W を 16 字並べた項目も 2 行に収まる)
//
// 数えるのは文字数で表示幅ではないので、守れるのは普通の全角・半角の文字まで。1 字で何字分も幅を取る文字 (U+FDFD・U+2E3B 等) や
// 結合文字を重ねた語は、上限内でも切れる (2 周目の反証レビューの実測)。台本に出ない文字なので幅の検査は足していない。
const (
	keywordTextMax  = 16
	keywordSubMax   = 40
	compareTitleMax = 8
	compareItemMax  = 16
	compareItemsMax = 3
)

// showParsers は図解の種類ごとの検査。m は show のオブジェクト、at は「lines[i].show」までの位置。
var showParsers = map[string]func(path, at string, m map[string]any) (*showData, error){
	"keyword": parseKeyword,
	"compare": parseCompare,
}

func showTypes() []string {
	types := make([]string, 0, len(showParsers))
	for t := range showParsers {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// parseShow は行の show を検査して図解にする。null は「図解を消す」で nil を返す。
// 行の未知のキー (show の綴り違いを含む) は loadScript が検査しないので、ここに届かない。
func parseShow(path string, i int, v any) (*showData, error) {
	if v == nil {
		return nil, nil
	}
	at := fmt.Sprintf("lines[%d].show", i)
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fail("%s: %s はオブジェクトか null で書く (実際: %s)", path, at, pyRepr(v))
	}
	typ, _ := m["type"].(string)
	parse, ok := showParsers[typ]
	if !ok {
		return nil, fail("%s: %s.type は %s のどれか (実際: %s)", path, at, strings.Join(showTypes(), "/"), pyRepr(m["type"]))
	}
	return parse(path, at, m)
}

func parseKeyword(path, at string, m map[string]any) (*showData, error) {
	if err := onlyKeys(path, at+" (keyword)", m, "type", "text", "sub"); err != nil {
		return nil, err
	}
	text, err := showField(path, at, m, "text", true, keywordTextMax)
	if err != nil {
		return nil, err
	}
	sub, err := showField(path, at, m, "sub", false, keywordSubMax)
	if err != nil {
		return nil, err
	}
	return &showData{Type: "keyword", Text: text, Sub: sub}, nil
}

func parseCompare(path, at string, m map[string]any) (*showData, error) {
	if err := onlyKeys(path, at+" (compare)", m, "type", "left", "right"); err != nil {
		return nil, err
	}
	sd := &showData{Type: "compare"}
	for _, side := range []struct {
		key string
		dst **compareSide
	}{{"left", &sd.Left}, {"right", &sd.Right}} {
		sat := at + "." + side.key
		sm, ok := m[side.key].(map[string]any)
		if !ok {
			return nil, fail("%s: %s は {\"title\": …, \"items\": […]} で書く (実際: %s)", path, sat, pyRepr(m[side.key]))
		}
		if err := onlyKeys(path, sat, sm, "title", "items"); err != nil {
			return nil, err
		}
		title, err := showField(path, sat, sm, "title", true, compareTitleMax)
		if err != nil {
			return nil, err
		}
		raw, ok := sm["items"].([]any)
		if !ok || len(raw) == 0 || len(raw) > compareItemsMax {
			return nil, fail("%s: %s.items は 1〜%d 個の文字列のリストで書く (実際: %s)", path, sat, compareItemsMax, pyRepr(sm["items"]))
		}
		items := make([]string, len(raw))
		for j, it := range raw {
			if items[j], err = showString(path, fmt.Sprintf("%s.items[%d]", sat, j), it, true, true, compareItemMax); err != nil {
				return nil, err
			}
		}
		*side.dst = &compareSide{Title: title, Items: items}
	}
	return sd, nil
}

// onlyKeys は図解のオブジェクトに書けないキーがあれば止める。
func onlyKeys(path, at string, m map[string]any, allowed ...string) error {
	var extra []string
	for k := range m {
		if !slices.Contains(allowed, k) {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fail("%s: %s に書けるのは %s だけ (実際: %s)", path, at, strings.Join(allowed, "/"), strings.Join(extra, "/"))
	}
	return nil
}

// showField は図解のオブジェクト m の欄 key を showString で検査する。
func showField(path, at string, m map[string]any, key string, required bool, max int) (string, error) {
	v, present := m[key]
	return showString(path, at+"."+key, v, present, required, max)
}

// showString は図解の文字列の値を検査する。required なら空を止め、max を超える長さ (文字数) を止める。
func showString(path, at string, v any, present, required bool, max int) (string, error) {
	s, isStr := v.(string)
	switch {
	case required && (!isStr || pyStrip(s) == ""):
		return "", fail("%s: %s は空でない文字列で書く (実際: %s)", path, at, pyRepr(v))
	case present && !isStr:
		return "", fail("%s: %s は文字列で書く (実際: %s)", path, at, pyRepr(v))
	}
	if n := utf8.RuneCountInString(s); n > max {
		return "", fail("%s: %s は %d 字まで (実際: %d 字。長いと画面で切れる)", path, at, max, n)
	}
	return s, nil
}

// lineShows は各行に出す図解の番号 (無ければ nil) と、図解の表を返す。show を持つ行から、次に show を持つ行
// (null なら消す) の手前まで同じ図解を出し続ける。同じ中身の図解は表に 1 回だけ入れる (中身は JSON にして比べる)。
func lineShows(path string, lines []map[string]any) ([]*int, []showData, error) {
	idx := make([]*int, len(lines))
	var table []showData
	seen := map[string]int{}
	var cur *int
	for i, line := range lines {
		if v, ok := line["show"]; ok {
			sd, err := parseShow(path, i, v)
			if err != nil {
				return nil, nil, err
			}
			cur = nil
			if sd != nil {
				b, err := json.Marshal(sd)
				if err != nil {
					return nil, nil, err
				}
				n, ok := seen[string(b)]
				if !ok {
					n = len(table)
					seen[string(b)] = n
					table = append(table, *sd)
				}
				cur = &n
			}
		}
		idx[i] = cur
	}
	return idx, table, nil
}
