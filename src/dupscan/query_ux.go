package main

import (
	"dupscan/internal/jsonout"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dupscan/internal/repo"
	"dupscan/internal/tree"
)

type queryUX struct {
	Approximation string
	ParseErrors   int
	Unsupported   bool
	Truncated     bool
}

func (s queryUX) suffix() string {
	omitted, unsupported := "なし", "なし"
	if s.Truncated {
		omitted = "あり"
	}
	if s.Unsupported {
		unsupported = "あり"
	}
	return fmt.Sprintf(" / 近似: %s / 省略: %s / 構文解析失敗: %d ファイル / 未対応言語: %s", s.Approximation, omitted, s.ParseErrors, unsupported)
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
func (s scanned) callApproximation(labels bool) string {
	match := "名前のみ (引数ラベル・個数で絞らない)"
	omitted := ""
	if labels {
		match = "名前と引数ラベル/個数"
		for file := range s.contents {
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

// relevantParseErrors は読めなかったファイルのうち、問い合わせの名前を本文に含むもの。
// 名前の照合は単位名・呼び出し名の小文字化した部分一致で、その名前は本文の部分文字列なので、
// 名前を含まないファイルは結果に影響しえない。名前が空のとき (他の条件だけの問い合わせ) と、
// 本文を持っていないファイルは、影響しうるものとして残す。
// 例外: 初期化子 (`init`) の「呼び出し ~N」は単位名でなく型名で他のファイルの呼び出しを数える (report.callCounts)。
// 問い合わせの名前が `init` の部分文字列になりうるときは、名前を含まないファイルも影響しうるので全部残す。
func (s scanned) relevantParseErrors(name string) []string {
	// 照合は MatchName を通して行うので、警告の関連性も同じ形で判定する (囲み付きの query で警告が落ちない)。
	// 本文の中の綴りは囲み付きのこともあるので、囲みを外した語の部分文字列で探す (`repeat` は `repeat` にも `repeat` を含む)
	needle := strings.ToLower(tree.MatchName(name))
	if needle == "" || strings.Contains(initUnitName, needle) {
		return s.errFiles
	}
	var relevant []string
	for _, file := range s.errFiles {
		body, ok := s.contents[file]
		if !ok || strings.Contains(strings.ToLower(string(body)), needle) {
			relevant = append(relevant, file)
		}
	}
	return relevant
}

// warnParseErrors は解析不完全を stderr に出す。読めなかったファイルのどれかが問い合わせの名前を含むときは全文
// (従来どおり。hit・0 件・抽出単位 0 のどれでも出す)。どれも含まないときは、結果に影響しないことを 1 行で示す。
const initUnitName = "init"

func (s scanned) warnParseErrors(w io.Writer, command, name string) error {
	if len(s.errFiles) == 0 {
		return nil
	}
	if len(s.relevantParseErrors(name)) == 0 {
		_, err := fmt.Fprintf(w, "dupscan %s: 読めないファイル %d 個は問い合わせの名前を含まない\n", command, len(s.errFiles))
		return err
	}
	return writeParseWarning(w, command, s.errFiles)
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
