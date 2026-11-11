// Package lint は lint: 台本の校正のうち文字列と構造だけで決まるものと、資料との突き合わせの候補 (--source)。
package lint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"zundamon-kaisetsu/internal/app"
	"zundamon-kaisetsu/internal/paths"
	"zundamon-kaisetsu/internal/pyjson"
	"zundamon-kaisetsu/internal/script"
	"zundamon-kaisetsu/internal/show"
)

// 台本の校正のうち、文字列と台本の構造だけで決まるもの (zundamon-kaisetsu lint)。
// 合成の前に回し、手順 3-3 の確認役には意味の確認 (事実・図解と資料の整合・書き起こしの筋) を任せる。
//
// 検出しない形 (確認役が見る): 口調の語尾の取り違え・推論の断定・図解とセリフの対応・図を指す言い方のうち下の一覧に無いもの
// (「これ」「ここ」「左の」「上の」は図以外を指すことが多く、単独では拾わない)・全角の数字 (１２個)・めたんの「僕」「ボク」
// (「ボクシング」「公僕」で止まるので、ひらがなの「ぼく」だけを見る)。
// 目安の誤検出として受ける形: ずんだもんの感嘆の「うわ！」「こわ」(口調)、図解の「！」「？」で終わる文 (show-sentence は「。」だけを見る)。
// 漢数字 (目安) が取りこぼす形: 数の途中にかかる慣用語 (三十分・十一人で → 「十分」「一人で」として除かれる)・
// 続けて並ぶ数の 2 つ目 (三人四人)。字句の近似の手直しを 2 周続けて破られたので、これ以上は直さない。
// 警告は誤りでない行もありうる (引用で「ぼく」と言う等)。漢数字は「目安」(rc に効かない): 慣用語 (一段と・一人で・三日月 …) と
// 数の表記の境目が字句では決まらず、一覧で塞ぐと取りこぼしと誤検出の繰り返しになる

type lintIssue struct {
	Line int
	Rule string
	Msg  string
	Hint bool // 目安 (rc に効かない)。完成した台本でも普通に出る決まり (めたんの連続発話) はこちら
}

const lintCaptionMax = 60 // 字幕 2 行 (SKILL.md の「60 字程度」を機械の線にしたもの)

var (
	// 半角の算用数字にすべき漢数字 (目安): 数の後に助数詞が続く形だけを見る (「一緒」「一般」「統一」は助数詞が続かないので当たらない)。
	// 前の「数」「何」は概数 (数十個・何百人) で、算用数字にできないので除く。算用数字に万・億だけを添えた形 (15万件) は digitsWithMan が除く。
	// 1 組目は助数詞まで、2 組目は漢数字の部分
	kanjiNumberRe = regexp.MustCompile(`(?:^|[^数何])(([一二三四五六七八九十百千万億〇]+)(?:つ|人|名|個|本|回|件|年|月|日|時間|行|秒|分|割|倍|種類|章|段|位|台|枚|社|点|歳|週|か月|ヶ月|円|ページ|文字|代))`)
	// 数を表さない慣用的な語。数える前に行から取り除く (行のどこかにあるかで判定すると、同じ行の本物の数まで見逃す)
	kanjiNumberIdioms = []string{"一人ひとり", "一つひとつ", "一日中", "一年中", "一段と", "一人で", "一人前", "同一人物", "十人十色", "二人三脚", "一回り", "一本化", "一枚岩", "一点張り", "三日月", "十分"}
	// 文末の「のだ」(後ろが句読点か行末)。「のだろう」「のだから」は数えない
	nodaRe = regexp.MustCompile(`のだ(?:[。！？!?、…」』）)っ〜ー♪\s]|$)`)
	// 図解を画面の位置や指示語で指す言い方 (図を名指しする形だけ。「左側の」は「左側の車線」のように図以外にも使うので外した)
	// 「図」の直後が助詞・句読点・行末のときだけ当たる (「その図書館」「以上の図表」で止めない)
	pointingRe = regexp.MustCompile(`(?:この|その|あの|上の|下の|左の|右の)図(?:[をはがでにのもと、。！？!?]|$)|図を見て|これを見て|ここを見て`)
	// ずんだもんの文末の「〜わ」「〜のよ」(めたんの語尾)
	zundamonFeminineEndRe = regexp.MustCompile(`(?:わ|のよ)[。！？!?]*$`)
	sentenceSplitRe       = regexp.MustCompile(`[。！？!?]+`)
	questionEndRe         = regexp.MustCompile(`[？?][」』）)]*$`)
)

// チャプターの数の目安 (SKILL.md の「チャプターは 4〜6 個。尺が長い (8 分を超える程度) なら増やしてよい」)。
// 行数で尺を見積もる (1 行あたり約 5.5〜6 秒。87 行で約 8〜8.7 分。係数の実測は SKILL.md の手順 0)
const (
	// lintShowLinger は、図解が出たままになっている行数の目安 (issue 1055)。統合確認が「話題が移っても残っている」と拾った区間は
	// 4〜6 行だった (2026-11-10 の動画の 4 区間)。わざと長く出す図解にどれだけ当たるかは測れていない (残っていた台本が無い)。
	// 動画ごとに当たり外れを improving.md に書き、外れが多ければ行数を上げるか目安ごと外す
	lintShowLinger   = 4
	lintChaptersMin  = 4
	lintChaptersMax  = 6
	lintLongScript   = 87
	lintShowsPerChap = 2
	lintStyleLongMax = 20 // 声のスタイル (ノーマル以外) は 1 行の反応や一言にだけ使う。これより長い行は説明の行とみなす
)

// digitsWithMan は、t[start:end] の漢数字が万・億だけで、すぐ前が算用数字か (15万件・3億円)。SKILL.md の「数は半角の算用数字」が
// 許す書き方なので漢数字の目安にしない (出すと「150000件」のような読みにくい字幕に書き換えさせる)。「3千円」「1千万円」は数える
func digitsWithMan(t string, start, end int) bool {
	prev, _ := utf8.DecodeLastRuneInString(t[:start])
	return unicode.IsDigit(prev) && strings.Trim(t[start:end], "万億") == ""
}

// LintScript は台本の行を規則で見て、警告を行の順に返す。dict は skill の読み辞書 (readings.json。合流漏れを見る。nil なら見ない)。
func LintScript(s *script.Script, dict map[string]string) []lintIssue {
	var out []lintIssue
	add := func(i int, rule, format string, a ...any) {
		out = append(out, lintIssue{Line: i, Rule: rule, Msg: fmt.Sprintf(format, a...)})
	}
	hint := func(i int, rule, format string, a ...any) {
		out = append(out, lintIssue{Line: i, Rule: rule, Msg: fmt.Sprintf(format, a...), Hint: true})
	}
	scriptReadings, _ := s.Raw["readings"].(map[string]any)
	showActive := false // 図解が出ている間 (show が付いた行から、null で消すまで。"next" は段を進めるだけ)
	metanRun, metanRunStart := 0, 0
	var chapterStarts []int
	for i, line := range s.Lines {
		who := pyjson.Str(line["who"])
		text := pyjson.Str(line["text"])
		// 同じ話者の同じ字幕 (別の話者の「うん。」「うん。」は普通の会話)
		if i > 0 && text == pyjson.Str(s.Lines[i-1]["text"]) && who == pyjson.Str(s.Lines[i-1]["who"]) {
			add(i, "duplicate", "前の行と同じ話者・同じ字幕 (行番号で直したときの取り違えのことがある)")
		}
		// 目安: 辞書は部分一致で置き換えるので、合流すると別の語の中まで書き換わることがある (STORAGE の中の RAG)。止めずに知らせるだけにし、
		// 英字の語は語全体が一致したときだけ、記号だけの語 (〜) は見ない。英字の 1 文字読みで困る分は kana --check が止める
		if _, hasRead := line["read"]; !hasRead {
			for _, w := range pyjson.SortedKeys(dict) {
				if _, ok := scriptReadings[w]; !ok && dictWordIn(text, w) {
					hint(i, "readings-missing", "読み辞書の語「%s」が台本の readings に無い (手順 3-2 の辞書の合流を忘れていないか)", w)
				}
			}
		}
		if n := len(nonEmpty(sentenceSplitRe.Split(text, -1))); n >= 3 {
			hint(i, "sentences", "1 セリフが %d 文 (1〜2 文まで。短い相づちも 1 文と数える)", n)
		}
		if who == "metan" && nodaRe.MatchString(text) {
			hint(i, "tone", "めたんの文末に「のだ」(ずんだもんの語尾。引用なら無視してよい)")
		}
		if who == "zundamon" && zundamonFeminineEndRe.MatchString(text) {
			hint(i, "tone", "ずんだもんの文末が「〜わ」「〜のよ」(めたんの語尾)")
		}
		if _, ok := line["style_id"]; ok && utf8.RuneCountInString(text) > lintStyleLongMax {
			hint(i, "style-long", "声のスタイルを %d 字の行に付けている (ノーマル以外は 1 行の反応や一言にだけ使う)", utf8.RuneCountInString(text))
		}
		if sh, ok := line["show"].(map[string]any); ok {
			for _, f := range showTexts(sh) {
				if strings.HasSuffix(f, "。") {
					add(i, "show-sentence", "図解の「%s」が文になっている (図解は語・短い句だけ。文はセリフで言う)", f)
				}
			}
		}
		if pyjson.Truthy(line["chapter"]) {
			chapterStarts = append(chapterStarts, i)
		}
		if n := utf8.RuneCountInString(text); n > lintCaptionMax {
			add(i, "long", "字幕が %d 字 (%d 字まで。字幕 2 行に収まらない)", n, lintCaptionMax)
		}
		if n := len(nodaRe.FindAllString(text, -1)); who == "zundamon" && n >= 2 {
			add(i, "noda", "ずんだもんの「のだ」が 1 行に %d 回 (1 回まで)", n)
		}
		if who == "zundamon" && strings.Contains(text, "わたくし") {
			add(i, "pronoun", "ずんだもんの一人称が「わたくし」(ずんだもんは「ぼく」)")
		}
		if who == "metan" && strings.Contains(text, "ぼく") {
			add(i, "pronoun", "めたんの行に「ぼく」(めたんは「わたくし」。ずんだもんの言葉の引用なら無視してよい)")
		}
		t := withoutIdioms(text)
		for _, m := range kanjiNumberRe.FindAllStringSubmatchIndex(t, -1) {
			if digitsWithMan(t, m[4], m[5]) {
				continue
			}
			out = append(out, lintIssue{Line: i, Rule: "kanji-number", Msg: fmt.Sprintf("「%s」は半角の算用数字で書く (SKILL.md の「数は半角の算用数字」。慣用語なら無視してよい)", t[m[2]:m[3]]), Hint: true})
		}
		if m := pointingRe.FindString(text); m != "" {
			add(i, "pointing", "「%s」で図解を指している (ものは名前で呼ぶ。音だけで聞く人に通じない)", strings.TrimRight(m, "をはがでにのもと、。！？!?"))
		}
		show, hasShow := line["show"]
		// チャプターは build と同じく、中身のある chapter だけ数える (Truthy)。"next" は図解を変えないので、消したことにならない
		if pyjson.Truthy(line["chapter"]) && i > 0 && showActive && (!hasShow || show == "next") {
			add(i, "show-across-chapter", "チャプターが変わっても前の図解が出たまま (この行に \"show\": null を書く)")
		}
		if hasShow {
			switch show.(type) {
			case nil:
				// 目安: 何も消さない null は害が無いが、行を消す・まとめる編集で「消す」指定を別の行へ引き継いだ名残のことがある
				if !showActive {
					hint(i, "show-null-idle", "図解が出ていないのに \"show\": null (消す図解が無い。行を消した・まとめたときに、消す指定を引き継いだ名残ではないか)")
				}
				showActive = false
			case string: // "next" は図解を変えない
			default:
				showActive = true
			}
		}
		// めたんの連続発話: "next" で段を進める行は、図解の説明の続きなので数えない
		stepsShow := hasShow && show == "next"
		if who == "metan" && !stepsShow {
			if metanRun == 0 {
				metanRunStart = i
			}
			metanRun++
			if metanRun == 3 {
				// 目安: 校正済みの 2 本 (2026-10-08) でも 8 か所あり、図解の説明が続く場面などで許容していた。止めると毎回止まる
				out = append(out, lintIssue{Line: metanRunStart, Rule: "metan-run", Msg: "めたんが 3 行以上続く (間にずんだもんの質問か相づちを挟めないか見る)", Hint: true})
			}
		} else if who != "metan" {
			metanRun = 0
		}
	}
	// チャプター単位の目安
	if n := len(chapterStarts); n < lintChaptersMin || (n > lintChaptersMax && len(s.Lines) < lintLongScript) {
		at := 0
		if n > 0 {
			at = chapterStarts[0]
		}
		hint(at, "chapters", "チャプターが %d 個 (%d〜%d 個。%d 行を超える長い台本なら増やしてよい)", n, lintChaptersMin, lintChaptersMax, lintLongScript)
	}
	for k, c := range chapterStarts {
		end := len(s.Lines)
		if k+1 < len(chapterStarts) {
			end = chapterStarts[k+1]
		}
		shows := 0
		for _, l := range s.Lines[c:end] {
			if _, ok := l["show"].(map[string]any); ok {
				shows++
			}
		}
		if shows > lintShowsPerChap {
			hint(c, "shows-per-chapter", "このチャプターの図解が %d 回 (1〜2 回まで)", shows)
		}
		// 最初のチャプター (導入) は、めたんが話題を言って始める決まりなので見ない
		if k > 0 && (pyjson.Str(s.Lines[c]["who"]) != "zundamon" || !questionEndRe.MatchString(pyjson.Str(s.Lines[c]["text"]))) {
			hint(c, "chapter-question", "チャプターの最初の行がずんだもんの質問でない (質問で始めると区切りが伝わる)")
		}
	}
	out = append(out, showSpanHints(s)...)
	out = append(out, sourceLeaks(s)...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].Line < out[b].Line })
	return out
}

var (
	// 独自調査のモードで、元の資料の存在が分かる語。作者のうっかりを見つけるためのもので、言い換え (「元のまとめ」等) は検出しない。
	// 🚨 全部を目安 (rc に効かない) にしている: 公開された文献を名前で引く行 (「経産省の DX レポートでは」「総務省が公開している資料では」)
	// は SKILL.md が求める言い方で、字面では元の資料の漏れと区別できない (実測: 公開文献を引いた台本で、警告にした「レポート」2 件が
	// どちらも正当な引用だった)。漏れかどうかは手順 3-3 の確認役がこの一覧を見て決める。行ごとに抑止できる口ができたら警告へ戻せる
	sourceLeakRe = regexp.MustCompile(`資料|レポート|回答|文書|記事|ドキュメント|しりょう|れぽーと|[AＡ][IＩ][\s　]*の?(分析|回答)`)
	// 普通に使う語は先に取り除いてから見る (「アンケートの回答者」は資料の存在を言っていない)
	sourceLeakAllowed = []string{"回答者"}
)

// sourceLeaks は、source_mode が "public" (独自調査) の台本で、画面・概要欄・音声に出る文字に元の資料の存在が分かる語があれば目安を出す。
// 台本全体の欄 (タイトル・概要・クレジット) は行番号 -1 で出す。画像の中身・alt (画面に出ない)・mermaid のノード ID は見ない。
func sourceLeaks(s *script.Script) []lintIssue {
	if s.Raw["source_mode"] != "public" {
		return nil
	}
	var out []lintIssue
	find := func(text string) []string {
		for _, a := range sourceLeakAllowed {
			text = strings.ReplaceAll(text, a, "・")
		}
		return uniq(sourceLeakRe.FindAllString(text, -1))
	}
	report := func(i int, where string, ms []string) {
		if len(ms) > 0 {
			out = append(out, lintIssue{Line: i, Rule: "source-leak", Hint: true,
				Msg: where + "に「" + strings.Join(ms, "」「") + "」(独自調査のモードでは元の資料の存在を出さない。公開された文献を名前で引いているならそのままでよい。SKILL.md の「資料の語り方」)"})
		}
	}
	report(-1, "タイトル", find(script.DisplayTitle(s)))
	report(-1, "description", find(pyjson.Str(script.LineGet(s.Raw, "description", ""))))
	if c, ok := s.Raw["credits"].(string); ok { // build は 1 文字ずつのクレジットにするが、画面には続けて並ぶので全体で見る
		report(-1, "credits", find(c))
	} else if c, ok := s.Raw["credits"]; ok {
		for _, v := range pyjson.Iter(c) { // build と同じ回し方 (リストは要素、map はキー)
			report(-1, "credits", find(pyjson.Str(v)))
		}
	}
	for i, line := range s.Lines {
		text := pyjson.Str(script.LineGet(line, "text", ""))
		inText := find(text)
		report(i, "字幕", inText)
		// 読み上げは、字幕で出した語を除いて出す (同じ語を 2 回出さない)
		var spoken []string
		for _, m := range find(script.SpokenText(s, line)) {
			if !slices.Contains(inText, m) {
				spoken = append(spoken, m)
			}
		}
		report(i, "読み上げ", spoken)
		if c, ok := line["chapter"]; ok {
			report(i, "チャプター名", find(pyjson.Str(c)))
		}
		if sh, ok := line["show"].(map[string]any); ok {
			for _, f := range ShowVisibleStrings(sh, "show", sh["type"] == "mermaid") {
				report(i, "図解の "+f.key, find(f.text))
			}
		}
	}
	return out
}

type keyedText struct{ key, text string }

// ShowVisibleStrings は図解のうち画面に出る文字を、欄の名前 (show.left.title 等) つきで返す。画像のパスと alt は画面に出ないので除く。
// mermaid の code はラベルとして描かれるが、%% のコメント行は描かれないので除く (コードの図解の %% の行は画面に出るので見る)。
// 種類ごとの欄を列挙せずに全部の文字列を辿るのは、図解の種類や欄が増えても見落とさないため
// (lint の showTexts (文になりうる欄だけ) と source_check の showAllTexts (資料と突き合わせる語) とは見る範囲が違う)。
func ShowVisibleStrings(v any, key string, mermaid bool) []keyedText {
	var out []keyedText
	switch x := v.(type) {
	case string:
		for _, l := range strings.Split(x, "\n") { // mermaid の code は 1 つの要素に改行を含められる (行ごとに見る)
			if !mermaid || !strings.HasPrefix(strings.TrimSpace(l), "%%") {
				out = append(out, keyedText{key, l})
			}
		}
	case []any:
		for _, e := range x {
			out = append(out, ShowVisibleStrings(e, key, mermaid)...)
		}
	case map[string]any:
		for _, k := range pyjson.SortedKeys(x) {
			if k == "type" || k == "src" || k == "alt" {
				continue
			}
			out = append(out, ShowVisibleStrings(x[k], key+"."+k, mermaid)...)
		}
	}
	return out
}

func uniq(xs []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

var (
	asciiWordRe  = regexp.MustCompile(`^[A-Za-z0-9.+_-]+$`)
	symbolOnlyRe = regexp.MustCompile(`^[\p{P}\p{S}]+$`)
)

// dictWordIn は辞書の語 w が字幕 text に現れるか。英字の語は前後が英数字でないときだけ (語の一部に当てない)、記号だけの語は見ない。
func dictWordIn(text, w string) bool {
	if symbolOnlyRe.MatchString(w) {
		return false
	}
	if !asciiWordRe.MatchString(w) {
		return strings.Contains(text, w)
	}
	return regexp.MustCompile(`(?:^|[^A-Za-z0-9])` + regexp.QuoteMeta(w) + `(?:[^A-Za-z0-9]|$)`).MatchString(text)
}

func nonEmpty(xs []string) []string {
	var out []string
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			out = append(out, x)
		}
	}
	return out
}

// showTexts は図解の中の文字 (重要語の補足・比較の項目・箇条書きの項目) を返す (文になっていないかを見る用)。
func showTexts(sh map[string]any) []string {
	var out []string
	if items, ok := sh["items"].([]any); ok && pyjson.Str(sh["type"]) == "list" {
		for _, it := range items {
			if v, ok := it.(string); ok {
				out = append(out, v)
			}
		}
	}
	if v, ok := sh["sub"].(string); ok {
		out = append(out, v)
	}
	for _, side := range []string{"left", "right"} {
		if m, ok := sh[side].(map[string]any); ok {
			if items, ok := m["items"].([]any); ok {
				for _, it := range items {
					if v, ok := it.(string); ok {
						out = append(out, v)
					}
				}
			}
		}
	}
	return out
}

// LoadReadingsDict は skill の読み辞書 (readings.json)。読めなければ止める (黙って合流漏れを見なくなるのを防ぐ)。
func LoadReadingsDict(env *app.Env) (map[string]string, error) {
	p := filepath.Join(env.SkillDir, "readings.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, app.Fail("%s: 読めない (%v)", p, err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, app.Fail("%s: {\"語\": \"読み\"} の形で書く (%v)", p, err)
	}
	return m, nil
}

// withoutIdioms は、行から数を表さない慣用語を取り除いた文 (取り除いた所は「・」にして、前後がつながらないようにする)。
func withoutIdioms(text string) string {
	for _, idiom := range kanjiNumberIdioms {
		text = strings.ReplaceAll(text, idiom, "・")
	}
	return text
}

// CmdLint は台本を規則で見て、警告を出す。警告が 1 つでもあれば rc=1。sourceArg (lint --source) があれば資料との突き合わせの候補も出す (目安)。
func CmdLint(env *app.Env, scriptArg, sourceArg string) error {
	s, err := script.LoadScript(paths.ResolvePath(scriptArg), env)
	if err != nil {
		return err
	}
	dict, err := LoadReadingsDict(env)
	if err != nil {
		return err
	}
	issues := LintScript(s, dict)
	if sourceArg != "" {
		src, err := ReadSource(sourceArg)
		if err != nil {
			return err
		}
		issues = append(issues, SourceCandidates(s, src)...)
		sort.SliceStable(issues, func(a, b int) bool { return issues[a].Line < issues[b].Line })
	}
	warned := 0
	for _, is := range issues {
		kind := "警告"
		if is.Hint {
			kind = "目安"
		} else {
			warned++
		}
		fmt.Fprintf(env.Stdout, "%d\t%s\t%s\t%s\n", is.Line, kind, is.Rule, is.Msg)
	}
	if warned > 0 {
		return app.Fail("台本の校正の警告が %d 件ある (上の一覧。行番号は lines の添字。目安は rc に効かない)", warned)
	}
	fmt.Fprintln(env.Stdout, "(警告なし)")
	return nil
}

// showSpan は 1 つの図解が出ている区間。lines[start] が図解を出した行、end は消える手前の行 (チャプターの境の手前で切る)。
// from は残りを数え始める行: 段の無い図解は start、段のある図解 (比較・箇条書きの build、コードの steps) は最後に段が変わった行
// ("next" の行と、同じ図解を書き直して段が最初に戻った行)。
type showSpan struct {
	start, from, end int
	n, step          int            // 図解の表の番号と、今の段 (段の無い図解は -1)
	show             map[string]any // 図解を書いた行の show (画面に出る文字を引く)
}

// showSpans は台本の図解の区間を出した順に返す。どの行に何が出ているかは build と同じ show.LineShows で決める
// (lint で別に組み直すと、同じ中身の書き直し・書き方だけ違う同じ図解・書き直しで段が戻る形で build とずれる)。
// null・別の図解・チャプターの境・台本の終わりで区間を閉じる。チャプターをまたいで残る図解は show-across-chapter が警告するので、
// 境の手前で切り、境の後は同じ図解のあいだ数えない。
func showSpans(s *script.Script) []showSpan {
	idx, steps, _, err := show.LineShows(s.Path, s.Lines)
	if err != nil {
		return nil // script.LoadScript が読み込みで同じ検査を通している (ここには来ない)
	}
	var out []showSpan
	var cur *showSpan
	skip := -1 // チャプターの境で切った図解の番号 (別の図解になるまで数えない)
	end := func(i int) {
		if cur != nil {
			cur.end = i - 1
			out = append(out, *cur)
			cur = nil
		}
	}
	var last map[string]any
	for i, line := range s.Lines {
		if m, ok := line["show"].(map[string]any); ok {
			last = m
		}
		if i > 0 && pyjson.Truthy(line["chapter"]) && cur != nil {
			skip = cur.n
			end(i)
		}
		if idx[i] == nil {
			end(i)
			skip = -1
			continue
		}
		n, step := *idx[i], -1
		if steps[i] != nil {
			step = *steps[i]
		}
		switch {
		case cur != nil && cur.n == n:
			if step != cur.step {
				cur.from, cur.step = i, step
			}
		case cur == nil && n == skip:
		default:
			end(i)
			skip = -1
			cur = &showSpan{start: i, from: i, n: n, step: step, show: last}
		}
	}
	end(len(s.Lines))
	return out
}

// showDigitRe は図解と字幕の中の数 (3 桁区切りの「,」は showDigitSep で先に外す)。整数の部分が 2 桁以上の数だけを取る:
// 1 桁の数は「1か所」「2つ」のような語の一部が多く、区間の字幕のどこかに別の 1 桁の数があるかどうかで、照合が偶然で決まる
// (review-bench の 2・9・14 行目の「1か所」)。受けたこと: 2026-11-10 の動画の「前の年の約2倍」のような 1 桁の数は見ない。
var (
	showDigitRe  = regexp.MustCompile(`[0-9]{2,}(?:\.[0-9]+)?|[0-9]\.[0-9]+`)
	showDigitSep = regexp.MustCompile(`([0-9]),([0-9]{3})`)
)

func digitRuns(t string) []string {
	for showDigitSep.MatchString(t) {
		t = showDigitSep.ReplaceAllString(t, "$1$2")
	}
	return showDigitRe.FindAllString(t, -1)
}

// showSpanHints は図解の区間から出す目安 (issue 1055。どちらも rc に効かない):
//   - show-linger: 図解が lintShowLinger 行以上出たまま。話題が移っても残っている図解を見つけるためで、意図して長く出しているならそのままでよい
//   - show-number-unspoken: 図解の中の数が、出ている区間のどの字幕にも無い (画面の数字を誰も言わない)。数を丸ごとで比べ (「109」は「10920」を
//     言ったことにしない)、コードの図解は見ない (字幕でコードを読み上げない決まり)。mermaid も見ない (ノード ID・style の数字は画面に出ない。
//     ラベルの数字も見落とすが、記法とラベルを分けるには mermaid を読む必要がある)。言い換え (「約1万」と「10920」・「80%」と「8割」) は当たる側に倒れる。
//     全角の数字 (２０２６) は見ない (字幕も図解も半角で書く決まりで、kanji-number とは別に止めてはいない)
//
// 照合は字幕 (text) で行う: 字幕は算用数字で書く決まりで、読み (read・readings) は仮名に開いていることがある。
func showSpanHints(s *script.Script) []lintIssue {
	var out []lintIssue
	for _, sp := range showSpans(s) {
		if n := sp.end - sp.from + 1; n >= lintShowLinger {
			out = append(out, lintIssue{Line: sp.from, Rule: "show-linger", Hint: true,
				Msg: fmt.Sprintf("図解が %d 行出たまま (%d〜%d 行目)。話題が移ったら \"show\": null で消す (わざと長く出しているならそのままでよい)", n, sp.from, sp.end)})
		}
		if t := sp.show["type"]; t == "code" || t == "mermaid" {
			continue
		}
		spoken := map[string]bool{}
		for _, l := range s.Lines[sp.start : sp.end+1] {
			for _, d := range digitRuns(pyjson.Str(l["text"])) {
				spoken[d] = true
			}
		}
		var missing []string
		for _, f := range ShowVisibleStrings(sp.show, "show", false) {
			for _, d := range digitRuns(f.text) {
				if !spoken[d] && !slices.Contains(missing, d) {
					missing = append(missing, d)
					out = append(out, lintIssue{Line: sp.start, Rule: "show-number-unspoken", Hint: true,
						Msg: fmt.Sprintf("図解の %s の「%s」が、図解の出ている行の字幕で言われていない (画面の数はセリフでも言う)", f.key, d)})
				}
			}
		}
	}
	return out
}
