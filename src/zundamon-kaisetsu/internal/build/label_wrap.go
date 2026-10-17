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
	"unicode/utf8"

	"zundamon-kaisetsu/internal/app"
	"zundamon-kaisetsu/internal/extcmd"
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
// "box" は図解のカード (Clip は中身がカードの外へ出た px。overflow: hidden で隠れる分)。
type LabelMeasure struct {
	Key    string   `json:"key"`
	Kind   string   `json:"kind"`
	Text   string   `json:"text"`
	Lines  []string `json:"lines"`
	Breaks []bool   `json:"breaks"`
	Clip   []int    `json:"clip"`
}

// ExpectedLabelKeys は計測モードが測るはずのラベルの鍵 (図解は箱の鍵だけ)。player.html の #measure の条件と揃える。
// 測った結果にこの鍵が欠けていたら、検査が空振りしたとして止める (測れなかったものを「問題なし」と数えない)。
func ExpectedLabelKeys(d *PlayerData) []string {
	var keys []string
	if slices.Contains(d.Overlays, "title") && d.Title != "" {
		keys = append(keys, "title")
	}
	for i, ch := range d.Chapters {
		if slices.Contains(d.Overlays, "topic") && len(d.Chapters) >= 2 && ch.Title != "" {
			keys = append(keys, fmt.Sprintf("topic:%d", i))
		}
		if len(d.Cards) > 0 && strings.TrimSpace(ch.Title) != "" {
			keys = append(keys, fmt.Sprintf("card:%d", i))
		}
	}
	for n, sd := range d.Shows {
		if sd.Type != "image" {
			keys = append(keys, fmt.Sprintf("show:%d", n))
		}
	}
	return keys
}

// LabelProblems は測った結果を判定し、問題のあるラベルを 1 行ずつ返す。期待した鍵の欠けも問題として返す。
func LabelProblems(d *PlayerData, ms []LabelMeasure) []string {
	got := map[string]bool{}
	var out []string
	for _, m := range ms {
		got[m.Key] = true
		where := labelWhere(d, m.Key)
		switch m.Kind {
		case "line":
			if len(m.Lines) > 1 {
				out = append(out, fmt.Sprintf("%s: 1 行に収まらず %d 行になる (%s)", where, len(m.Lines), strings.Join(m.Lines, " / ")))
			}
		case "wrap":
			var why []string
			if slices.Contains(m.Breaks, false) {
				why = append(why, "語の途中で改行する")
			}
			if len(m.Lines) > 1 && slices.ContainsFunc(m.Lines, func(l string) bool { return utf8.RuneCountInString(strings.TrimSpace(l)) <= 1 }) {
				why = append(why, "1 字だけの行ができる")
			}
			if len(why) > 0 {
				out = append(out, fmt.Sprintf("%s: %s (%s)", where, strings.Join(why, "・"), strings.Join(m.Lines, " / ")))
			}
		case "box":
			if len(m.Clip) == 2 && (m.Clip[0] > 0 || m.Clip[1] > 0) {
				out = append(out, fmt.Sprintf("%s: 中身がカードからはみ出して隠れる (横 %d px / 縦 %d px)", where, m.Clip[0], m.Clip[1]))
			}
		default:
			out = append(out, fmt.Sprintf("%s: 計測モードが知らない種類 %q を返した (player.html と label_wrap.go が食い違っている)", where, m.Kind))
		}
	}
	for _, k := range ExpectedLabelKeys(d) {
		if !got[k] {
			out = append(out, fmt.Sprintf("%s: 測れなかった (player.html の計測モードが返さない)", labelWhere(d, k)))
		}
	}
	return out
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
	rc, msg := extcmd.RunQuiet(120*time.Second, chrome, "--headless=new", "--disable-gpu", "--hide-scrollbars", "--force-device-scale-factor=1",
		fmt.Sprintf("--window-size=%d,%d", VideoSize[0], VideoSize[1]), "--virtual-time-budget=3000", "--dump-dom", uri)
	if rc != 0 {
		return nil, app.Fail("ラベルの改行を測る Chrome が失敗 (rc=%d): %s", rc, textx.LastRunes(msg, 300))
	}
	m := measureOut.FindStringSubmatch(msg)
	if m == nil {
		return nil, app.Fail("ラベルの改行を測れない (Chrome の出力に計測の結果が無い。templates/player.html の #measure が効いていない): %s", textx.LastRunes(msg, 300))
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

// CheckLabelLayout は build が書き出す前にラベルの改行を確かめる。問題があれば一覧を出して止める (allow なら出すだけで続ける)。
// Chrome が無いとき (HTML だけを書く環境) は、確かめていないことを出して続ける (mp4 は撮影に Chrome が要るので、そこで止まる)。
func CheckLabelLayout(env *app.Env, data *PlayerData, td string, allow bool) error {
	if len(ExpectedLabelKeys(data)) == 0 {
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
	if err != nil {
		return err
	}
	problems := LabelProblems(data, ms)
	if len(problems) == 0 {
		return nil
	}
	fmt.Fprintf(env.Stderr, "build: 意図しない改行・はみ出しのあるラベルが %d 個 (mp4 と同じ 1280x720 の舞台で測った):\n", len(problems))
	for _, p := range problems {
		fmt.Fprintf(env.Stderr, "  %s\n", p)
	}
	if allow {
		return nil
	}
	return app.Fail("ラベルの改行に問題が %d 個あるので、書き出す前に止めた (語を短くするか、文節が 1 行に収まる言い方にする。このまま書き出すなら --allow-label-wraps)", len(problems))
}
