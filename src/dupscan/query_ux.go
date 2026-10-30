package main

import (
	"dupscan/internal/jsonout"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"dupscan/internal/langs"
	"dupscan/internal/repo"
	"dupscan/internal/tree"
)

type queryUX struct {
	Approximation string
	ParseErrors   int
	// IgnoredParseErrors は、その言語のツールが読まない置き場 (Go の testdata) の読めないファイルの数 (ParseErrors に数えない)。
	IgnoredParseErrors int
	Unsupported        bool
	Truncated          bool
}

// newQueryUX は読めないファイルを、解析不完全に数えるものと testdata の下のものに分けて持つ queryUX。
func newQueryUX(approximation string, s scanned) queryUX {
	attention, ignored := s.splitErrFiles()
	return queryUX{Approximation: approximation, ParseErrors: len(attention), IgnoredParseErrors: len(ignored)}
}

func (s queryUX) suffix() string {
	omitted, unsupported := "なし", "なし"
	if s.Truncated {
		omitted = "あり"
	}
	if s.Unsupported {
		unsupported = "あり"
	}
	ignored := ""
	if s.IgnoredParseErrors > 0 {
		ignored = fmt.Sprintf(" (ほかに読めない testdata %d)", s.IgnoredParseErrors)
	}
	return fmt.Sprintf(" / 近似: %s / 省略: %s / 構文解析失敗: %d ファイル%s / 未対応言語: %s", s.Approximation, omitted, s.ParseErrors, ignored, unsupported)
}

type nextAction struct {
	Kind   string   `json:"kind"`
	Reason string   `json:"reason"`
	Argv   []string `json:"argv"`
}

func queryNextAction(sc repo.Scope, command, query, reason string, flags ...string) nextAction {
	args := append([]string{}, flags...)
	if command == "index" {
		args = append(args, query, "--")
	} else {
		args = append(args, "--", query)
	}
	return scopedNextAction(sc, command, reason, args...)
}

func scopedNextAction(sc repo.Scope, command, reason string, args ...string) nextAction {
	argv := append([]string{"dupscan", command}, args...)
	if sc.Whole() {
		argv = append(argv, sc.Root)
	} else {
		for _, prefix := range sc.Prefixes {
			argv = append(argv, filepath.Join(sc.Root, prefix))
		}
	}
	return nextAction{Kind: command, Reason: reason, Argv: argv}
}

func writeNextAction(w io.Writer, action nextAction) error {
	encoded, err := jsonout.Line(action)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "next_action: %s\n", encoded)
	return err
}

// callApproximation describes the syntactic matching used by index and callers.
// Swift の既定引数の注意は、呼び出しを数える相手に Swift の単位があるときだけ要る。results (結果の単位のファイル) を渡すと
// それで決め、渡さない (結果が 0 件を含む) ときは走査した全ファイルで決める。Go のファイルだけの結果に Swift の注意を出さない。
func (s scanned) callApproximation(labels bool, results ...string) string {
	match := "名前のみ (引数ラベル・個数で絞らない)"
	omitted := ""
	if labels {
		match = "名前と引数ラベル/個数"
		files := results
		if len(files) == 0 {
			for file := range s.contents {
				files = append(files, file)
			}
		}
		for _, file := range files {
			if filepath.Ext(file) == ".swift" {
				omitted = " Swiftの既定引数を省略した呼び出しは取りこぼす。"
				break
			}
		}
	}
	return "構文近似: " + match + "で照合。型未解決で同名の別定義が混ざりうる。" + omitted + " 0件でも未使用の証拠ではない。"
}

// sourceGlobs は走査したファイルの拡張子の glob (`*.go` など。重複なし・昇順)。打ち切ったときの rg の提示に使う。
func (s scanned) sourceGlobs() []string {
	seen := map[string]bool{}
	var globs []string
	for file := range s.contents {
		if ext := filepath.Ext(file); ext != "" && !seen[ext] {
			seen[ext] = true
			globs = append(globs, "*"+ext)
		}
	}
	sort.Strings(globs)
	return globs
}

// splitErrFiles は読めないファイルを、解析不完全に数えるものと、その言語のツールが読まない置き場 (Go の testdata) のものに分ける
// (gate・report の 1 行目と同じ分け方。langs.ToolIgnores)。壊れていて当然の fixture で、毎回の問い合わせに解析不完全の全文を出さない。
// named は名指しされたファイル (index -file の対象)。testdata の下でも、名指しされたら解析不完全に数える。
func (s scanned) splitErrFiles(named ...string) (attention, ignored []string) {
	for _, f := range s.errFiles {
		if langs.ToolIgnores(f) && !slices.Contains(named, f) {
			ignored = append(ignored, f)
		} else {
			attention = append(attention, f)
		}
	}
	return attention, ignored
}

// nameless は、names で読めないファイルを絞れない問い合わせか (名前が空・init の部分文字列。relevantAmong が全部を返す)。
func nameless(names []string) bool {
	for _, name := range names {
		needle := strings.ToLower(tree.MatchName(name))
		if needle == "" || strings.Contains(initUnitName, needle) {
			return true
		}
	}
	return false
}

// relevantAmong は files (読めなかったファイル) のうち、names (問い合わせの名前。index -file の outline は並べた単位の名前) の
// どれかを本文に含むもの。名前の照合は単位名・呼び出し名の小文字化した部分一致で、その名前は本文の部分文字列なので、
// 名前を含まないファイルは結果に影響しえない。名前が空のとき (他の条件だけの問い合わせ) と、
// 本文を持っていないファイルは、影響しうるものとして残す。
// 例外: 初期化子 (`init`) の「呼び出し ~N」は単位名でなく型名で他のファイルの呼び出しを数える (report.callCounts)。
// 名前が `init` の部分文字列になりうるときは、名前を含まないファイルも影響しうるので全部残す。
func (s scanned) relevantAmong(files, names []string) []string {
	// 照合は MatchName を通して行うので、警告の関連性も同じ形で判定する (囲み付きの query で警告が落ちない)。
	// 本文の中の綴りは囲み付きのこともあるので、囲みを外した語の部分文字列で探す (`repeat` は `repeat` にも `repeat` を含む)
	if nameless(names) {
		return files
	}
	needles := make([]string, 0, len(names))
	for _, name := range names {
		needles = append(needles, strings.ToLower(tree.MatchName(name)))
	}
	var relevant []string
	for _, file := range files {
		body, ok := s.contents[file]
		if !ok {
			relevant = append(relevant, file)
			continue
		}
		lower := strings.ToLower(string(body))
		for _, needle := range needles {
			if strings.Contains(lower, needle) {
				relevant = append(relevant, file)
				break
			}
		}
	}
	return relevant
}

// warnParseErrors は解析不完全を stderr に出す。読めなかったファイルのどれかが問い合わせの名前を含むときは全文
// (従来どおり。hit・0 件・抽出単位 0 のどれでも出す)。どれも含まないときは、結果に影響しないことを 1 行で示す。
const initUnitName = "init"

func (s scanned) warnParseErrors(w io.Writer, command, name string) error {
	return s.warnParseErrorsAny(w, command, []string{name}, "問い合わせの名前")
}

// warnParseErrorsAny は warnParseErrors の names 版 (index -file の outline: 並べた単位の名前のどれかを含むファイルだけを影響しうると数える)。
// what は 1 行の文言の主語 (名前を指定していない outline では「並べた単位の名前」)。
// testdata の下の読めないファイルは解析不完全に数えず、名前を含むときだけそこが欠けうることを 1 行で言う (含まなければ何も言わない)。
// named は名指しされたファイル (splitErrFiles)。
func (s scanned) warnParseErrorsAny(w io.Writer, command string, names []string, what string, named ...string) error {
	attention, ignored := s.splitErrFiles(named...)
	if hit := s.relevantAmong(ignored, names); len(hit) > 0 {
		// 名前で絞れない問い合わせ (名前が無い・init の部分文字列) では全部が「影響しうる」になるので、名前があるとは言わない
		where := "に" + what + "がある"
		if nameless(names) {
			where = ""
		}
		if _, err := fmt.Fprintf(w, "dupscan %s: 読めない testdata %d 個 (go が読まない置き場)%s。そこの定義・呼び出しは欠けうる: %s\n", command, len(hit), where, headList(hit, 3)); err != nil {
			return err
		}
	}
	if len(attention) == 0 {
		return nil
	}
	if len(s.relevantAmong(attention, names)) == 0 {
		_, err := fmt.Fprintf(w, "dupscan %s: 読めないファイル %d 個は%sを含まない\n", command, len(attention), what)
		return err
	}
	return writeParseWarning(w, command, attention)
}

// writeParseWarning keeps legacy stdout and no-match exit codes usable while exposing incomplete scans.
func writeParseWarning(w io.Writer, command string, files []string) error {
	if len(files) == 0 {
		return nil
	}
	quoted := make([]string, len(files))
	for i := range quoted {
		quoted[i] = strconv.Quote(files[i])
	}
	detail := headList(quoted, 3)
	_, err := fmt.Fprintf(w, "dupscan %s: 解析不完全: 読めない構文を含むファイル %d 個: %s。抽出単位が0件でも定義・参照・呼び出しは欠けうる。0件は不存在の証明ではない\n", command, len(files), detail)
	return err
}
