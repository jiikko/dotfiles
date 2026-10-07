package main

import (
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// showData は舞台の中央に出す図解 1 つ (台本の行の show)。中身は PlayerData.Shows に 1 回だけ置き、行は番号で指す
// (行ごとに中身を持たせると、同じ図解が続く行の数だけデータが複製される。画像を足すと HTML が膨らむ。issue 645)。
type showData struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Sub  string `json:"sub,omitempty"`
}

// keywordTextMax / keywordSubMax は重要語の図解に書ける長さ (文字数)。カードは収まらない分を隠すので、超えると末尾が黙って
// 切れる。720p の舞台で、語は全角 8 字・補足は全角 20 字ほどで折り返し、語 2 行と補足 2 行までがカードに収まった
// (issue 645 の実測)。player.html の .show-keyword の文字の大きさや .stage-show の箱を変えたら測り直す。
// 数えるのは文字数で表示幅ではないので、守れるのは普通の全角・半角の文字まで。1 字で何字分も幅を取る文字 (U+FDFD・U+2E3B 等) や
// 結合文字を重ねた語は、上限内でも切れる (2 周目の反証レビューの実測)。台本に出ない文字なので幅の検査は足していない。
const (
	keywordTextMax = 16
	keywordSubMax  = 40
)

// showKeys は図解の種類ごとに書けるキー。必須のものは parseShow が見る。
var showKeys = map[string][]string{
	"keyword": {"type", "text", "sub"},
}

func showTypes() []string {
	types := make([]string, 0, len(showKeys))
	for t := range showKeys {
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
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fail("%s: lines[%d].show はオブジェクトか null で書く (実際: %s)", path, i, pyRepr(v))
	}
	typ, _ := m["type"].(string)
	allowed, ok := showKeys[typ]
	if !ok {
		return nil, fail("%s: lines[%d].show.type は %s のどれか (実際: %s)", path, i, strings.Join(showTypes(), "/"), pyRepr(m["type"]))
	}
	var extra []string
	for k := range m {
		if !slices.Contains(allowed, k) {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return nil, fail("%s: lines[%d].show (%s) に書けるのは %s だけ (実際: %s)", path, i, typ, strings.Join(allowed, "/"), strings.Join(extra, "/"))
	}
	text, ok := m["text"].(string)
	if !ok || pyStrip(text) == "" {
		return nil, fail("%s: lines[%d].show.text は空でない文字列で書く (実際: %s)", path, i, pyRepr(m["text"]))
	}
	if n := utf8.RuneCountInString(text); n > keywordTextMax {
		return nil, fail("%s: lines[%d].show.text は %d 字まで (実際: %d 字。長いと画面で切れる)", path, i, keywordTextMax, n)
	}
	sd := &showData{Type: typ, Text: text}
	if sv, ok := m["sub"]; ok {
		sub, ok := sv.(string)
		if !ok {
			return nil, fail("%s: lines[%d].show.sub は文字列で書く (実際: %s)", path, i, pyRepr(sv))
		}
		if n := utf8.RuneCountInString(sub); n > keywordSubMax {
			return nil, fail("%s: lines[%d].show.sub は %d 字まで (実際: %d 字。長いと画面で切れる)", path, i, keywordSubMax, n)
		}
		sd.Sub = sub
	}
	return sd, nil
}

// lineShows は各行に出す図解の番号 (無ければ nil) と、図解の表を返す。show を持つ行から、次に show を持つ行
// (null なら消す) の手前まで同じ図解を出し続ける。同じ中身の図解は表に 1 回だけ入れる。
func lineShows(path string, lines []map[string]any) ([]*int, []showData, error) {
	idx := make([]*int, len(lines))
	var table []showData
	var cur *int
	for i, line := range lines {
		if v, ok := line["show"]; ok {
			sd, err := parseShow(path, i, v)
			if err != nil {
				return nil, nil, err
			}
			cur = nil
			if sd != nil {
				n := slices.Index(table, *sd)
				if n < 0 {
					n = len(table)
					table = append(table, *sd)
				}
				cur = &n
			}
		}
		idx[i] = cur
	}
	return idx, table, nil
}
