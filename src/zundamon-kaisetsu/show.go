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
	Lang  string       `json:"lang,omitempty"`
	Lines []string     `json:"lines,omitempty"`
	Marks []int        `json:"highlight,omitempty"`
	// 段: 後の行の "show": "next" で 1 段ずつ見せる。比較は Build で、左右の同じ番号の項目を 1 行ずつ出す。
	// コードは Steps で、段ごとに強調する行 (1 始まり) を変える (Marks とは同時に書けない)
	Build bool     `json:"build,omitempty"`
	Steps [][]int  `json:"steps,omitempty"`
	Src   string   `json:"src,omitempty"` // 画像の実際のパス。assemble (embedShowImages) が data URI に置き換える
	Alt   string   `json:"alt,omitempty"`
	Code  []string `json:"code,omitempty"` // mermaid の記法。assemble (embedMermaid) が PNG にして image に置き換えるので、player には届かない
	// srcWritten は台本に書いたままの src (エラーの表示用)。JSON に出さないので、重複除去の鍵にも入らない
	srcWritten string
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
//   - コード: 折り返さない。1 行に半角 49 桁ほどが収まり、幅は余白を取って 44 まで。1 行目の上に言語名の分の余白を取って
//     11 行まで (.show-code pre の padding)。言語名は幅 12 (全角 6 字) までなら 1 行目に重ならない。
//     全角は半角約 1.7 字分の幅で出るので、codeWidth が 2 と数えるのは上限の側に倒れる
//
// 重要語と比較は文字数で数え、コードと言語名は幅 (codeWidth) で数える。どちらも、1 字で何字分も幅を取る文字 (U+FDFD・U+2E3B 等) や
// 結合文字を重ねた語は上限内でも切れる (2 周目の反証レビューの実測)。台本に出ない文字なので、それ以上の幅の検査は足していない。
const (
	keywordTextMax  = 16
	keywordSubMax   = 40
	compareTitleMax = 8
	compareItemMax  = 16
	compareItemsMax = 3
	codeLangMax     = 12
	codeColsMax     = 44
	codeLinesMax    = 11
)

// showNext は行の show に書く「今の図解を 1 段進める」の印。
const showNext = "next"

// stepCount は図解の段の数 (段の無い図解は 0)。
func (sd *showData) stepCount() int {
	switch {
	case sd.Build:
		return len(sd.Left.Items) // 左右の数が同じことは parseCompare が止めている (対を 1 段ずつ出す)
	case len(sd.Steps) > 0:
		return len(sd.Steps)
	}
	return 0
}

// showParsers は図解の種類ごとの検査。m は show のオブジェクト、at は「lines[i].show」までの位置。
var showParsers = map[string]func(path, at string, m map[string]any) (*showData, error){
	"keyword": parseKeyword,
	"compare": parseCompare,
	"code":    parseCode,
	"image":   parseImage,
	"mermaid": parseMermaid,
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
		return nil, fail("%s: %s はオブジェクトか null か \"next\" (段を進める) で書く (実際: %s)", path, at, pyRepr(v))
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
	if err := onlyKeys(path, at+" (compare)", m, "type", "left", "right", "build"); err != nil {
		return nil, err
	}
	sd := &showData{Type: "compare"}
	if bv, ok := m["build"]; ok {
		b, isBool := bv.(bool)
		if !isBool {
			return nil, fail("%s: %s.build は true か false で書く (実際: %s)", path, at, pyRepr(bv))
		}
		sd.Build = b
	}
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
	if sd.Build {
		// 段は左右の同じ番号の項目を対にして 1 つずつ出すので、数が違うと片方だけが増える段ができる
		if len(sd.Left.Items) != len(sd.Right.Items) {
			return nil, fail("%s: %s.build は左右の項目の数を揃えて書く (同じ番号の項目を対にして 1 段ずつ出す。実際: %d 個と %d 個)", path, at, len(sd.Left.Items), len(sd.Right.Items))
		}
		if sd.stepCount() < 2 {
			return nil, fail("%s: %s.build は項目が 2 個以上あるときだけ書ける (1 段では見せていけない)", path, at)
		}
	}
	return sd, nil
}

func parseCode(path, at string, m map[string]any) (*showData, error) {
	if err := onlyKeys(path, at+" (code)", m, "type", "lang", "lines", "highlight", "steps"); err != nil {
		return nil, err
	}
	lang, err := showField(path, at, m, "lang", false, 1<<30)
	if err != nil {
		return nil, err
	}
	if w := codeWidth(lang); w > codeLangMax {
		return nil, fail("%s: %s.lang は幅 %d まで (半角 1・全角 2 で数える。実際: %d。長いと 1 行目に重なる)", path, at, codeLangMax, w)
	}
	raw, ok := m["lines"].([]any)
	if !ok || len(raw) == 0 || len(raw) > codeLinesMax {
		return nil, fail("%s: %s.lines は 1〜%d 個の文字列のリストで書く (実際: %s)", path, at, codeLinesMax, pyRepr(m["lines"]))
	}
	lines := make([]string, len(raw))
	blank := true
	for j, v := range raw {
		lat := fmt.Sprintf("%s.lines[%d]", at, j)
		line, ok := v.(string)
		switch {
		case !ok:
			return nil, fail("%s: %s は文字列で書く (実際: %s)", path, lat, pyRepr(v))
		case strings.ContainsAny(line, "\t\n\r"):
			return nil, fail("%s: %s にタブ・改行を入れない (タブはスペースで書き、改行は行を分ける)", path, lat)
		case codeWidth(line) > codeColsMax:
			return nil, fail("%s: %s は幅 %d まで (半角 1・全角 2 で数える。実際: %d。長いと画面で切れる)", path, lat, codeColsMax, codeWidth(line))
		}
		lines[j] = line
		blank = blank && pyStrip(line) == ""
	}
	if blank {
		return nil, fail("%s: %s.lines が空行だけ", path, at)
	}
	sd := &showData{Type: "code", Lang: lang, Lines: lines}
	hv, hasMarks := m["highlight"]
	sv, hasSteps := m["steps"]
	switch {
	case hasMarks && hasSteps:
		return nil, fail("%s: %s に highlight と steps を両方は書けない (段ごとに強調するなら steps だけにする)", path, at)
	case hasMarks:
		if sd.Marks, err = lineMarks(path, at+".highlight", hv, len(lines)); err != nil {
			return nil, err
		}
	case hasSteps:
		list, ok := sv.([]any)
		if !ok || len(list) < 2 {
			return nil, fail("%s: %s.steps は段ごとの行番号のリストを 2 段以上並べて書く (例: [[2], [4, 5]]。実際: %s)", path, at, pyRepr(sv))
		}
		for j, v := range list {
			marks, err := lineMarks(path, fmt.Sprintf("%s.steps[%d]", at, j), v, len(lines))
			if err != nil {
				return nil, err
			}
			if j > 0 && slices.Equal(marks, sd.Steps[j-1]) { // "next" を書いても画面が変わらない段になる
				return nil, fail("%s: %s.steps[%d] が 1 つ前の段と同じ強調 (段を進めても画面が変わらない)", path, at, j)
			}
			sd.Steps = append(sd.Steps, marks)
		}
	}
	return sd, nil
}

// lineMarks はコードの強調する行番号 (1 始まり) のリストを検査し、重複を除いて昇順に並べる。空のリストは強調しない。
func lineMarks(path, at string, v any, nLines int) ([]int, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fail("%s: %s は行番号 (1 始まり) のリストで書く (実際: %s)", path, at, pyRepr(v))
	}
	marks := []int{}
	for _, x := range list {
		n, ok := x.(json.Number)
		k, err := n.Int64()
		if !ok || err != nil || k < 1 || int(k) > nLines {
			return nil, fail("%s: %s は 1〜%d の行番号のリストで書く (実際: %s)", path, at, nLines, pyRepr(v))
		}
		if !slices.Contains(marks, int(k)) {
			marks = append(marks, int(k))
		}
	}
	sort.Ints(marks)
	return marks, nil
}

// codeWidth はコードの 1 行の表示の幅 (半角 1・それ以外 2)。コードは折り返さずに出すので、文字数ではなく幅で上限を見る。
// 全角・半角の判定は ASCII かどうかだけの近似 (半角カナ・幅 0 の結合文字・全角も 2 と数えるので、上限の側に倒れる)。
func codeWidth(s string) int {
	w := 0
	for _, r := range s {
		if r < 0x80 {
			w++
		} else {
			w += 2
		}
	}
	return w
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

// lineShows は各行に出す図解の番号 (無ければ nil)・段 (段の無い図解なら nil) と、図解の表を返す。show を持つ行から、
// 次に show を持つ行 (null なら消す) の手前まで同じ図解を出し続ける。"show": "next" の行は図解を変えずに段を 1 つ進める。
// 同じ中身の図解は表に 1 回だけ入れる (中身は JSON にして比べる)。
func lineShows(path string, lines []map[string]any) ([]*int, []*int, []showData, error) {
	idx := make([]*int, len(lines))
	steps := make([]*int, len(lines))
	var table []showData
	seen := map[string]int{}
	var cur, step *int
	for i, line := range lines {
		if v, ok := line["show"]; ok {
			if v == showNext {
				at := fmt.Sprintf("lines[%d].show", i)
				switch {
				case cur == nil:
					return nil, nil, nil, fail("%s: %s の \"next\" は、図解を出している間にだけ書ける (前の行に図解が無いか、null で消している)", path, at)
				case step == nil:
					return nil, nil, nil, fail("%s: %s の \"next\" は、段のある図解 (比較の build・コードの steps) にだけ書ける", path, at)
				case *step+1 >= table[*cur].stepCount():
					return nil, nil, nil, fail("%s: %s の \"next\" が多い (この図解は %d 段で、もう最後の段を出している)", path, at, table[*cur].stepCount())
				}
				next := *step + 1
				step = &next
			} else {
				sd, err := parseShow(path, i, v)
				if err != nil {
					return nil, nil, nil, err
				}
				cur, step = nil, nil
				if sd != nil {
					b, err := json.Marshal(sd)
					if err != nil {
						return nil, nil, nil, err
					}
					n, ok := seen[string(b)]
					if !ok {
						n = len(table)
						seen[string(b)] = n
						table = append(table, *sd)
					}
					cur = &n
					if sd.stepCount() > 0 {
						step = new(int)
					}
				}
			}
		}
		idx[i], steps[i] = cur, step
	}
	return idx, steps, table, nil
}
