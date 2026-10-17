package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"zundamon-kaisetsu/internal/app"
	"zundamon-kaisetsu/internal/extcmd"
	"zundamon-kaisetsu/internal/show"
	"zundamon-kaisetsu/internal/textx"
)

// ラベルの改行の検査 (issue 812): 画面に出すラベル (左上のタイトル・トピック名・区切りのカード・図解の欄) が意図しない場所で
// 改行していないかを、mp4 の撮影と同じ Chrome の描画で測る。字数の上限 (show.KeywordTextMax 等) は「収まる長さ」の目安で、
// どこで折り返すか (文節が 1 行の幅を超えると語の途中で切れる・タイトルが 2 行になる) は描いてみないと分からない。
//
// 測るのは player.html の計測モード (#measure)、判定はこのファイル。検出しないもの:
//   - 語の境目 (Intl.Segmenter の word) では切れているが、文節の途中の改行 (Chrome の auto-phrase の文節は外から見えない)
//   - mp4 と違う窓の幅で HTML を開いたときの改行 (測るのは 1280x720 の舞台だけ。字幕も測らない)
//   - 箇条書きの項目・コードは折り返さない (white-space: nowrap / pre) ので、幅の超過はカードのはみ出しとしてだけ出る

// LabelMeasure は player.html の計測モードが返すラベル 1 つ。Kind が "line" は 1 行で出す欄、"wrap" は語の境目でなら折り返してよい欄、
// "box" は図解のカード。文字のラベル (line / wrap) の Lines は描かれた行、Glen は行ごとの書記素の数、Breaks は行の切れ目が語の境目か。
// Clip は描画範囲が箱 (と舞台) の外へ出た px [横, 縦]。overflow: hidden や舞台の端で隠れる分
type LabelMeasure struct {
	Key    string   `json:"key"`
	Kind   string   `json:"kind"`
	Text   string   `json:"text"`
	Lines  []string `json:"lines"`
	Glen   []int    `json:"glen"`
	Breaks []bool   `json:"breaks"`
	Clip   []int    `json:"clip"`
}

// ExpectedLabel は計測モードが測るはずのラベル。Text は台本から出した字 (図解の箱は空)
type ExpectedLabel struct {
	Key, Kind, Text string
}

// ExpectedLabels は計測モードが測るはずのラベル。player.html の #measure の条件と揃える。
// 測った結果にこの鍵が欠けていたり、字が違ったりしたら、検査が空振りしたとして止める (測れなかったものを「問題なし」と数えない)。
func ExpectedLabels(d *PlayerData) []ExpectedLabel {
	var out []ExpectedLabel
	add := func(key, kind, text string) { out = append(out, ExpectedLabel{key, kind, text}) }
	if slices.Contains(d.Overlays, "title") && d.Title != "" {
		add("title", "line", d.Title)
	}
	for i, ch := range d.Chapters {
		if slices.Contains(d.Overlays, "topic") && len(d.Chapters) >= 2 && ch.Title != "" {
			add(fmt.Sprintf("topic:%d", i), "line", ch.Title)
		}
		if len(d.Cards) > 0 && jsTrim(ch.Title) != "" {
			add(fmt.Sprintf("card:%d", i), "wrap", ch.Title)
		}
	}
	for n, sd := range d.Shows {
		k := fmt.Sprintf("show:%d", n)
		switch sd.Type {
		case "image":
			continue
		case "keyword":
			add(k+":text", "wrap", sd.Text)
			if sd.Sub != "" {
				add(k+":sub", "wrap", sd.Sub)
			}
		case "list":
			if sd.Title != "" {
				add(k+":title", "line", sd.Title)
			}
			for j, it := range sd.Items {
				add(fmt.Sprintf("%s:items[%d]", k, j), "line", it)
			}
		case "compare":
			for _, side := range []struct {
				name string
				c    *show.CompareSide
			}{{"left", sd.Left}, {"right", sd.Right}} {
				add(k+":"+side.name+".title", "line", side.c.Title)
				for j, it := range side.c.Items {
					add(fmt.Sprintf("%s:%s.items[%d]", k, side.name, j), "wrap", it)
				}
			}
		}
		add(k, "box", "")
	}
	return out
}

// jsTrim は JavaScript の String.prototype.trim (player.html がチャプター名の空白だけを判定する) と同じ文字を両端から除く。
// Go の strings.TrimSpace とは U+FEFF (除く) と U+0085 (除かない) が違う
func jsTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		switch {
		case r >= 0x09 && r <= 0x0d, r == 0x20, r == 0xa0, r == 0x1680, r >= 0x2000 && r <= 0x200a,
			r == 0x2028, r == 0x2029, r == 0x202f, r == 0x205f, r == 0x3000, r == 0xfeff:
			return true
		}
		return false
	})
}

// clipTolerance は描画範囲のはみ出しとして数えない px (Range の外接矩形と箱の境界の丸めの差)
const clipTolerance = 1

// LabelProblems は測った結果を判定する。layout は台本の字を直せば消える問題 (改行・はみ出し)、broken は計測の契約違反
// (期待したラベルの欠け・字や種類の食い違い・壊れた計測)。broken は --allow-label-wraps でも止める (測れていないものを通さない)
func LabelProblems(d *PlayerData, ms []LabelMeasure) (layout, broken []string) {
	want := map[string]ExpectedLabel{}
	for _, e := range ExpectedLabels(d) {
		want[e.Key] = e
	}
	got := map[string]bool{}
	report := func(dst *[]string, m LabelMeasure, why []string) {
		if len(why) == 0 {
			return
		}
		line := fmt.Sprintf("%s: %s", labelWhere(d, m.Key), strings.Join(why, "・"))
		if len(m.Lines) > 0 {
			line += fmt.Sprintf(" (%s)", strings.Join(m.Lines, " / "))
		}
		*dst = append(*dst, line)
	}
	for _, m := range ms {
		got[m.Key] = true
		var bad, why []string
		if e, ok := want[m.Key]; ok && (e.Kind != m.Kind || e.Text != m.Text) {
			bad = append(bad, fmt.Sprintf("計測モードが返した種類・字 (%q %q) が台本 (%q %q) と違う (player.html と label_wrap.go が食い違っている)", m.Kind, m.Text, e.Kind, e.Text))
		}
		if len(m.Clip) != 2 {
			bad = append(bad, fmt.Sprintf("はみ出しの量を読めない (clip %v)", m.Clip))
		} else if m.Clip[0] > clipTolerance || m.Clip[1] > clipTolerance {
			why = append(why, fmt.Sprintf("箱か舞台からはみ出して隠れる (横 %d px / 縦 %d px)", m.Clip[0], m.Clip[1]))
		}
		switch m.Kind {
		case "box":
		case "line", "wrap":
			if len(m.Lines) == 0 || len(m.Breaks) != len(m.Lines)-1 || len(m.Glen) != len(m.Lines) || strings.Join(m.Lines, "") != m.Text {
				bad = append(bad, fmt.Sprintf("行の測り方が壊れている (lines %q / breaks %v / glen %v)", m.Lines, m.Breaks, m.Glen))
				break
			}
			if m.Kind == "line" && len(m.Lines) > 1 {
				why = append(why, fmt.Sprintf("1 行に収まらず %d 行になる", len(m.Lines)))
				break
			}
			if slices.Contains(m.Breaks, false) {
				why = append(why, "語の途中で改行する")
			}
			if len(m.Lines) > 1 && slices.ContainsFunc(m.Glen, func(n int) bool { return n <= 1 }) {
				why = append(why, "1 字だけの行ができる")
			}
		default:
			bad = append(bad, fmt.Sprintf("計測モードが知らない種類 %q を返した (player.html と label_wrap.go が食い違っている)", m.Kind))
		}
		report(&broken, m, bad)
		report(&layout, m, why)
	}
	for _, e := range ExpectedLabels(d) {
		if !got[e.Key] {
			broken = append(broken, fmt.Sprintf("%s: 測れなかった (player.html の計測モードが返さない)", labelWhere(d, e.Key)))
		}
	}
	return layout, broken
}

// labelWhere はラベルの鍵を、台本のどこかが分かる言い方にする。
func labelWhere(d *PlayerData, key string) string {
	if key == "title" {
		return "左上のタイトル"
	}
	kind, rest, _ := strings.Cut(key, ":")
	idx, sub, _ := strings.Cut(rest, ":")
	n, err := strconv.Atoi(idx)
	switch {
	case err != nil:
	case kind == "topic" && n < len(d.Chapters):
		return fmt.Sprintf("トピック名「%s」", d.Chapters[n].Title)
	case kind == "card" && n < len(d.Chapters):
		return fmt.Sprintf("区切りのカード「%s」", d.Chapters[n].Title)
	case kind == "show" && n < len(d.Shows):
		where := fmt.Sprintf("図解 %d (%s。lines[%s] から)", n, d.Shows[n].Type, firstLineOfShow(d, n))
		if sub != "" {
			where += " の " + sub
		}
		return where
	}
	return key
}

// firstLineOfShow は図解 n を最初に出す行の番号 (台本の lines の添字)。
func firstLineOfShow(d *PlayerData, n int) string {
	for i, l := range d.Lines {
		if l.Show != nil && *l.Show == n {
			return fmt.Sprint(i)
		}
	}
	return "?"
}

var measureOut = regexp.MustCompile(`(?s)<pre id="measure-out">(.*?)</pre>`)

// MeasureLabelsWithChrome は計測モードの HTML を Chrome で開き、--dump-dom の出力から測った結果を取り出す。
func MeasureLabelsWithChrome(page string) ([]LabelMeasure, error) {
	chrome := extcmd.FindChrome()
	if chrome == "" {
		return nil, errNoChrome
	}
	uri := (&url.URL{Scheme: "file", Path: page}).String() + "#measure"
	// DOM は stdout だけから読む (Chrome は stderr にログを出し、混ぜると計測の結果の途中に割り込みうる)
	rc, dom, msg := extcmd.RunSplit(120*time.Second, chrome, "--headless=new", "--disable-gpu", "--hide-scrollbars", "--force-device-scale-factor=1",
		fmt.Sprintf("--window-size=%d,%d", VideoSize[0], VideoSize[1]), "--virtual-time-budget=3000", "--dump-dom", uri)
	if e := app.InterruptedErr(); e != nil {
		return nil, e
	}
	m := measureOut.FindStringSubmatch(dom)
	switch {
	case m == nil && rc != 0: // 終了の処理で落ちただけなら結果は出ているので、結果が無いときだけ Chrome の失敗として扱う
		return nil, &chromeFailed{fmt.Sprintf("rc=%d: %s", rc, textx.LastRunes(msg, 300))}
	case m == nil:
		return nil, app.Fail("ラベルの改行を測れない (Chrome の出力に計測の結果が無い。templates/player.html の #measure が効いていない): %s", textx.LastRunes(dom+msg, 300))
	}
	var res struct {
		Labels []LabelMeasure `json:"labels"`
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &res); err != nil {
		return nil, app.Fail("ラベルの改行の計測結果を読めない (%v)", err)
	}
	return res.Labels, nil
}

var errNoChrome = errors.New("chrome が無い")

// chromeFailed は計測の Chrome が落ちた・応答しない (負荷で watchdog に落とされることがある。mp4.go の waitForIdleCPU)。
// 台本の問題ではないので build は止めず、確かめていないことを出して続ける。player.html が結果を返さない
// (計測モードが効いていない) のは契約の食い違いなので止める
type chromeFailed struct{ msg string }

func (e *chromeFailed) Error() string { return e.msg }

// CheckLabelLayout は build が書き出す前にラベルの改行を確かめる。問題があれば一覧を出して止める (allow なら出すだけで続ける)。
// Chrome が無いとき (HTML だけを書く環境) は、確かめていないことを出して続ける (mp4 は撮影に Chrome が要るので、そこで止まる)。
func CheckLabelLayout(env *app.Env, data *PlayerData, td string, allow bool) error {
	if len(ExpectedLabels(data)) == 0 {
		return nil
	}
	h, err := RenderHTML(data, env)
	if err != nil {
		return err
	}
	page := filepath.Join(td, "measure.html")
	if err := os.WriteFile(page, []byte(h), 0o644); err != nil {
		return app.Fail("%s: 書けない (%v)", page, err)
	}
	measure := MeasureLabelsWithChrome
	if env.MeasureLabels != nil {
		measure = func(p string) ([]LabelMeasure, error) {
			b, err := env.MeasureLabels(p)
			if err != nil {
				return nil, err
			}
			var res struct {
				Labels []LabelMeasure `json:"labels"`
			}
			if err := json.Unmarshal(b, &res); err != nil {
				return nil, app.Fail("ラベルの改行の計測結果を読めない (%v)", err)
			}
			return res.Labels, nil
		}
	}
	ms, err := measure(page)
	if errors.Is(err, errNoChrome) {
		fmt.Fprintln(env.Stderr, "build: Chrome が無いので、ラベル (タイトル・図解など) の改行は確かめていない")
		return nil
	}
	var cf *chromeFailed
	if errors.As(err, &cf) {
		fmt.Fprintf(env.Stderr, "build: 🚨 ラベル (タイトル・図解など) の改行を測る Chrome が失敗したので、確かめずに続ける (%s)\n", cf.msg)
		return nil
	}
	if err != nil {
		return err
	}
	layout, broken := LabelProblems(data, ms)
	if len(broken) > 0 {
		fmt.Fprintf(env.Stderr, "build: ラベルの改行を正しく測れなかった (%d 個):\n", len(broken))
		for _, p := range broken {
			fmt.Fprintf(env.Stderr, "  %s\n", p)
		}
		return app.Fail("ラベルの改行の計測が壊れているので止めた (templates/player.html の #measure と label_wrap.go が食い違っている。--allow-label-wraps では通さない)")
	}
	if len(layout) == 0 {
		return nil
	}
	fmt.Fprintf(env.Stderr, "build: 意図しない改行・はみ出しのあるラベルが %d 個 (mp4 と同じ 1280x720 の舞台で測った):\n", len(layout))
	for _, p := range layout {
		fmt.Fprintf(env.Stderr, "  %s\n", p)
	}
	if allow {
		return nil
	}
	return app.Fail("ラベルの改行に問題が %d 個あるので、書き出す前に止めた (語を短くするか、文節が 1 行に収まる言い方にする。このまま書き出すなら --allow-label-wraps)", len(layout))
}
