package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// 台本の校正のうち、文字列と台本の構造だけで決まるもの (zundamon-kaisetsu lint。issue 675)。
// 合成の前に回し、手順 3-3 の確認役には意味の確認 (事実・図解と資料の整合・書き起こしの筋) を任せる。
//
// 検出しない形 (確認役が見る): 口調の語尾の取り違え・推論の断定・図解とセリフの対応・図を指す言い方のうち下の一覧に無いもの
// (「これ」「ここ」「左の」「上の」は図以外を指すことが多く、単独では拾わない)・全角の数字 (１２個)・めたんの「僕」「ボク」
// (「ボクシング」「公僕」で止まるので、ひらがなの「ぼく」だけを見る)。
// 漢数字 (目安) が取りこぼす形: 数の途中にかかる慣用語 (三十分・十一人で → 「十分」「一人で」として除かれる)・
// 続けて並ぶ数の 2 つ目 (三人四人)。字句の近似の手直しを 2 周続けて破られたので、これ以上は直さない (issue 675 の敵対的レビュー)。
// 警告は誤りでない行もありうる (引用で「ぼく」と言う等)。漢数字は「目安」(rc に効かない): 慣用語 (一段と・一人で・三日月 …) と
// 数の表記の境目が字句では決まらず、一覧で塞ぐと取りこぼしと誤検出の繰り返しになる (issue 675 の敵対的レビュー)

type lintIssue struct {
	Line int
	Rule string
	Msg  string
	Hint bool // 目安 (rc に効かない)。完成した台本でも普通に出る決まり (めたんの連続発話) はこちら
}

const lintCaptionMax = 60 // 字幕 2 行 (SKILL.md の「60 字程度」を機械の線にしたもの)

var (
	// 半角の算用数字にすべき漢数字 (目安): 数の後に助数詞が続く形だけを見る (「一緒」「一般」「統一」は助数詞が続かないので当たらない)。
	// 前の「数」「何」は概数 (数十個・何百人) で、算用数字にできないので除く
	kanjiNumberRe = regexp.MustCompile(`(?:^|[^数何])([一二三四五六七八九十百千万億〇]+(?:つ|人|名|個|本|回|件|年|月|日|時間|行|秒|分|割|倍|種類|章|段|位|台|枚|社|点|歳|週|か月|ヶ月|円|ページ|文字|代))`)
	// 数を表さない慣用的な語。数える前に行から取り除く (行のどこかにあるかで判定すると、同じ行の本物の数まで見逃す)
	kanjiNumberIdioms = []string{"一人ひとり", "一つひとつ", "一日中", "一年中", "一段と", "一人で", "一人前", "同一人物", "十人十色", "二人三脚", "一回り", "一本化", "一枚岩", "一点張り", "三日月", "十分"}
	// 文末の「のだ」(後ろが句読点か行末)。「のだろう」「のだから」は数えない
	nodaRe = regexp.MustCompile(`のだ(?:[。！？!?、…」』）)っ〜ー♪\s]|$)`)
	// 図解を画面の位置や指示語で指す言い方 (図を名指しする形だけ。「左側の」は「左側の車線」のように図以外にも使うので外した)
	// 「図」の直後が助詞・句読点・行末のときだけ当たる (「その図書館」「以上の図表」で止めない)
	pointingRe = regexp.MustCompile(`(?:この|その|あの|上の|下の|左の|右の)図(?:[をはがでにのもと、。！？!?]|$)|図を見て|これを見て|ここを見て`)
)

// lintScript は台本の行を規則で見て、警告を行の順に返す。
func lintScript(s *Script) []lintIssue {
	var out []lintIssue
	add := func(i int, rule, format string, a ...any) {
		out = append(out, lintIssue{Line: i, Rule: rule, Msg: fmt.Sprintf(format, a...)})
	}
	showActive := false // 図解が出ている間 (show が付いた行から、null で消すまで。"next" は段を進めるだけ)
	metanRun, metanRunStart := 0, 0
	for i, line := range s.Lines {
		who := pyStr(line["who"])
		text := pyStr(line["text"])
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
		for _, m := range kanjiNumberRe.FindAllStringSubmatch(withoutIdioms(text), -1) {
			out = append(out, lintIssue{Line: i, Rule: "kanji-number", Msg: fmt.Sprintf("「%s」は半角の算用数字で書く (SKILL.md の「数は半角の算用数字」。慣用語なら無視してよい)", m[1]), Hint: true})
		}
		if m := pointingRe.FindString(text); m != "" {
			add(i, "pointing", "「%s」で図解を指している (ものは名前で呼ぶ。音だけで聞く人に通じない)", strings.TrimRight(m, "をはがでにのもと、。！？!?"))
		}
		show, hasShow := line["show"]
		// チャプターは build と同じく、中身のある chapter だけ数える (pyTruthy)。"next" は図解を変えないので、消したことにならない
		if pyTruthy(line["chapter"]) && i > 0 && showActive && (!hasShow || show == "next") {
			add(i, "show-across-chapter", "チャプターが変わっても前の図解が出たまま (この行に \"show\": null を書く)")
		}
		if hasShow {
			switch show.(type) {
			case nil:
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
	sort.SliceStable(out, func(a, b int) bool { return out[a].Line < out[b].Line })
	return out
}

// withoutIdioms は、行から数を表さない慣用語を取り除いた文 (取り除いた所は「・」にして、前後がつながらないようにする)。
func withoutIdioms(text string) string {
	for _, idiom := range kanjiNumberIdioms {
		text = strings.ReplaceAll(text, idiom, "・")
	}
	return text
}

// cmdLint は台本を規則で見て、警告を出す。警告が 1 つでもあれば rc=1。
func cmdLint(env *Env, scriptArg string) error {
	s, err := loadScript(resolvePath(scriptArg), env)
	if err != nil {
		return err
	}
	warned := 0
	for _, is := range lintScript(s) {
		kind := "警告"
		if is.Hint {
			kind = "目安"
		} else {
			warned++
		}
		fmt.Fprintf(env.Stdout, "%d\t%s\t%s\t%s\n", is.Line, kind, is.Rule, is.Msg)
	}
	if warned > 0 {
		return fail("台本の校正の警告が %d 件ある (上の一覧。行番号は lines の添字。目安は rc に効かない)", warned)
	}
	fmt.Fprintln(env.Stdout, "(警告なし)")
	return nil
}
