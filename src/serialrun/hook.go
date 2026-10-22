package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// 差し込む前置き。PATH 上の名前で書く (許可ルールを `Bash(serial-run -- make test:*)` の形で書けるように)
const wrapPrefix = "serial-run -- "

// runHook は Claude Code の PreToolUse(Bash) hook として、重いコマンドの直前に `serial-run -- ` を差し込む
// (hookSpecificOutput.updatedInput)。差し込まないときは何も出さない。
// 🚨 コマンド全体を `serial-run -- zsh -c '…'` で包まない。Claude Code の許可ルールは書き換えた後のコマンドで判定される
// (2.1.308 で実測: 元のコマンドへの deny は素通りし、書き換えた後への deny は効いた)。丸ごと包むと
// `make test && git push --force` の `git push` への deny が効かなくなる。重い 1 つの単純コマンドの前にだけ差し込めば、
// 他の部分は元の字面のまま判定される。
// 🚨 どんな失敗でも tool を止めない (何も出さずに rc=0)。直列化は便宜であって、止める理由にならない
func runHook(stdin io.Reader, stdout io.Writer) int {
	var in struct {
		ToolName  string         `json:"tool_name"`
		ToolInput map[string]any `json:"tool_input"`
	}
	if err := json.NewDecoder(stdin).Decode(&in); err != nil || in.ToolName != "Bash" || in.ToolInput == nil {
		return 0
	}
	cmd, ok := in.ToolInput["command"].(string)
	if !ok {
		return 0
	}
	wrapped, changed := wrapCommand(cmd)
	if !changed {
		return 0
	}
	// updatedInput は tool の入力を丸ごと置き換えるので、command 以外 (timeout・description・run_in_background) を写す
	out := map[string]any{}
	for k, v := range in.ToolInput {
		out[k] = v
	}
	out["command"] = wrapped
	hso := map[string]any{
		"hookEventName": "PreToolUse",
		"updatedInput":  out,
	}
	// 前に誰かいるときだけ、待つことを LLM に先に知らせる (待っている間は Bash の出力が返らないので、固まったと読ませない)。
	// additionalContext は updatedInput と一緒に LLM へ届く (Claude Code 2.1.308 で実測)
	if note := queueNote(); note != "" {
		hso["additionalContext"] = note
	}
	_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": hso})
	return 0
}

// queueNote は既定の lock の待ち行列に誰かいれば、LLM 向けの 1 段落を返す。いなければ空 (普段は何も足さない)。
// 置き場が無ければ作らずに空を返す (hook は待ち行列を書き換えない。死んだ札の回収だけは predecessors が行う)
func queueNote() string {
	base, err := stateBase()
	if err != nil {
		return ""
	}
	q := queue{dir: filepath.Join(base, "default")}
	if _, err := os.Stat(q.qdir()); err != nil {
		return ""
	}
	preds, err := q.predecessors(math.MaxInt64)
	if err != nil || len(preds) == 0 {
		return ""
	}
	head := ""
	if info, ok := q.readInfo(preds[0].seq); ok {
		head = fmt.Sprintf("先頭は %s の %s (%s 前から)。", info.Cwd, info.Cmd, fmtDur(time.Since(info.Start)))
	}
	return fmt.Sprintf("serial-run: このコマンドの重い部分は serial-run の順番待ちに入る (マシン全体で 1 本ずつ流す仕組み)。"+
		"前に %d 本ある。%s待っている間は Bash の出力が返らない (固まったのではない)。"+
		"長く待ちそうなら run_in_background で起こすか timeout を延ばし、状況は別の呼び出しで `serial-run status` を打って見る。"+
		"待ちの上限は既定 1 時間で、超えたら実行せずに rc=125 で抜ける (stderr に「待ちの上限」と出る)", len(preds), head)
}

var (
	assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	// 次の語もコマンドの位置のままにする予約語 (`if make test; then …` の make を見る)
	keywords = map[string]bool{"if": true, "then": true, "else": true, "elif": true, "do": true, "while": true, "until": true, "!": true, "{": true, "time": true}
	// make の、次の引数を値として取るオプション
	makeValueOpts = map[string]bool{"-C": true, "-f": true, "-I": true, "-o": true, "-W": true, "--directory": true, "--file": true}
	// make の、数値の値を省略できるオプション (`make -j test` の test は target)
	makeOptionalNumOpts = map[string]bool{"-j": true, "-l": true, "--jobs": true}
	// xcodebuild の、ビルドせずに情報だけを出す呼び方 (待たせない)
	xcodeInfoFlags = map[string]bool{"-version": true, "-list": true, "-showBuildSettings": true, "-showsdks": true,
		"-showdestinations": true, "-help": true, "-usage": true, "-license": true, "-checkFirstLaunchStatus": true}
	numRe = regexp.MustCompile(`^[0-9]+$`)
)

// unwrapPrefix は前に付いた包みのコマンド (env / nice / nohup / command / noglob / runtimeout) を外した残りの語を返す。
// 差し込む位置は包みの前のまま (`serial-run -- runtimeout 600 make test`。包みの中に入れると待ちが runtimeout の上限を食う)
func unwrapPrefix(words []string) []string {
	for len(words) > 0 {
		i := 1
		switch filepath.Base(words[0]) {
		case "nohup", "command", "noglob":
		case "env":
			for i < len(words) && (strings.HasPrefix(words[i], "-") || strings.Contains(words[i], "=")) {
				if words[i] == "-u" || words[i] == "-S" || words[i] == "-P" {
					i++
				}
				i++
			}
		case "nice":
			if i < len(words) && words[i] == "-n" {
				i += 2
			} else if i < len(words) && strings.HasPrefix(words[i], "-") {
				i++
			}
		case "runtimeout":
			for i < len(words) && strings.HasPrefix(words[i], "-") {
				if words[i] == "-k" {
					i++
				}
				i++
			}
			i++ // 秒
		default:
			return words
		}
		if i > len(words) {
			return nil
		}
		words = words[i:]
	}
	return words
}

// isHeavy は 1 つの単純コマンドの語 (引用を外したもの) が直列化の対象かを返す。対象の一覧の正本はここ
func isHeavy(words []string) bool {
	words = unwrapPrefix(words)
	if len(words) == 0 {
		return false
	}
	switch filepath.Base(words[0]) {
	case "mutate-verify", "mutate-verify-list":
		return true
	case "xcodebuild":
		for _, a := range words[1:] {
			if xcodeInfoFlags[a] {
				return false
			}
		}
		return true
	case "go":
		return len(words) > 1 && words[1] == "test"
	case "swift":
		return len(words) > 1 && (words[1] == "build" || words[1] == "test")
	case "make", "gmake":
		for i := 1; i < len(words); i++ {
			a := words[i]
			switch {
			case makeValueOpts[a]:
				i++
			case makeOptionalNumOpts[a]:
				if i+1 < len(words) && numRe.MatchString(words[i+1]) {
					i++
				}
			case strings.HasPrefix(a, "-") || strings.Contains(a, "="):
			case a == "test" || a == "lint" || strings.HasPrefix(a, "test-") || strings.HasPrefix(a, "lint-"):
				return true
			}
		}
	}
	return false
}

// wrapCommand は cmd の中の、コマンドの位置にある重い単純コマンドの直前に wrapPrefix を差し込む。
// 字句は保守的に読む: 読み切れない形 (heredoc・case・算術・閉じていない引用) は丸ごと差し込まない。
// 引用符・コマンド置換の中は書き換えない (commit message の本文などの文字列を変えない)。
func wrapCommand(cmd string) (string, bool) {
	// PATH を書き換えるコマンドは差し込まない (前置きの PATH= / export PATH の後では serial-run が見つからず rc=127 になる)
	if strings.Contains(cmd, "serial-run") || strings.Contains(cmd, "<<") || strings.Contains(cmd, "((") || strings.Contains(cmd, "PATH=") {
		return cmd, false
	}
	s := scanner{src: cmd}
	inserts, ok := s.scan()
	if !ok || len(inserts) == 0 {
		return cmd, false
	}
	var b strings.Builder
	last := 0
	for _, at := range inserts {
		b.WriteString(cmd[last:at])
		b.WriteString(wrapPrefix)
		last = at
	}
	b.WriteString(cmd[last:])
	return b.String(), true
}

type scanner struct {
	src     string
	i       int
	inserts []int

	// 今読んでいる単純コマンド
	words    []string
	cmdAt    int  // コマンド語の位置 (-1 = まだ無い)
	inCmdPos bool // 次の語がコマンドの位置か
}

func (s *scanner) endSimple() {
	if s.cmdAt >= 0 && isHeavy(s.words) {
		s.inserts = append(s.inserts, s.cmdAt)
	}
	s.words, s.cmdAt, s.inCmdPos = nil, -1, true
}

func (s *scanner) scan() ([]int, bool) {
	s.cmdAt, s.inCmdPos = -1, true
	depth := 0 // サブシェル ( … ) の深さ
	src := s.src
	for s.i < len(src) {
		c := src[s.i]
		switch {
		case c == ' ' || c == '\t':
			s.i++
		case c == '&' && s.i+1 < len(src) && src[s.i+1] == '>':
			// `&>` / `&>>` はリダイレクト (区切りの & ではない)
			s.i++
		case c == '\n' || c == ';' || c == '&' || c == '|':
			s.endSimple()
			s.i++
		case c == '(':
			if !s.inCmdPos {
				return nil, false
			}
			depth++
			s.endSimple()
			s.i++
		case c == ')':
			if depth == 0 {
				return nil, false
			}
			depth--
			s.endSimple()
			s.i++
		case c == '>' || c == '<':
			// リダイレクト: 演算子と続く対象の語は単純コマンドの引数に数えない (`2>&1` の & を区切りと読まない)
			s.i++
			for s.i < len(src) && (src[s.i] == '>' || src[s.i] == '&' || src[s.i] == '|') {
				s.i++
			}
			for s.i < len(src) && (src[s.i] == ' ' || src[s.i] == '\t') {
				s.i++
			}
			if _, ok := s.word(); !ok {
				return nil, false
			}
		case c == '#' && s.atWordStart():
			for s.i < len(src) && src[s.i] != '\n' {
				s.i++
			}
		default:
			start := s.i
			w, ok := s.word()
			if !ok {
				return nil, false
			}
			// 語の直後の ( (`arr=(go test)` の配列・`f()` の関数定義) はサブシェルではない。読み切れないので書き換えない
			if s.i < len(src) && src[s.i] == '(' {
				return nil, false
			}
			if s.cmdAt < 0 {
				if s.inCmdPos && w == "case" {
					return nil, false
				}
				if s.inCmdPos && (keywords[w] || w == "}") {
					continue
				}
				if s.inCmdPos && assignRe.MatchString(src[start:s.i]) {
					continue
				}
				if !s.inCmdPos {
					continue
				}
				s.cmdAt = start
				s.inCmdPos = false
			}
			s.words = append(s.words, w)
		}
	}
	if depth != 0 {
		return nil, false
	}
	s.endSimple()
	return s.inserts, true
}

func (s *scanner) atWordStart() bool {
	if s.i == 0 {
		return true
	}
	p := s.src[s.i-1]
	return p == ' ' || p == '\t' || p == '\n' || p == ';' || p == '&' || p == '|' || p == '('
}

// word は 1 語を読み、引用を外した値を返す。コマンド置換の中身は値に含めない (置換の結果は読めないので、
// その語は対象の名前に一致しない)。閉じていない引用・置換は ok=false
func (s *scanner) word() (string, bool) {
	src := s.src
	var b strings.Builder
	for s.i < len(src) {
		c := src[s.i]
		switch c {
		case ' ', '\t', '\n', ';', '&', '|', '(', ')', '<', '>':
			return b.String(), true
		case '\\':
			if s.i+1 >= len(src) {
				return "", false
			}
			b.WriteByte(src[s.i+1])
			s.i += 2
		case '\'':
			j := strings.IndexByte(src[s.i+1:], '\'')
			if j < 0 {
				return "", false
			}
			b.WriteString(src[s.i+1 : s.i+1+j])
			s.i += j + 2
		case '"':
			s.i++
			for {
				if s.i >= len(src) {
					return "", false
				}
				d := src[s.i]
				if d == '"' {
					s.i++
					break
				}
				if d == '\\' && s.i+1 < len(src) {
					b.WriteByte(src[s.i+1])
					s.i += 2
					continue
				}
				if (d == '$' && s.i+1 < len(src) && (src[s.i+1] == '(' || src[s.i+1] == '{')) || d == '`' {
					if !s.skipSubst() {
						return "", false
					}
					b.WriteString("\x00")
					continue
				}
				b.WriteByte(d)
				s.i++
			}
		case '$', '`':
			if c == '`' || (s.i+1 < len(src) && (src[s.i+1] == '(' || src[s.i+1] == '{')) {
				if !s.skipSubst() {
					return "", false
				}
				b.WriteString("\x00")
				continue
			}
			b.WriteByte(c)
			s.i++
		default:
			b.WriteByte(c)
			s.i++
		}
	}
	return b.String(), true
}

// skipSubst は $( … ) / ${ … } / ` … ` を、引用と入れ子を数えて読み飛ばす (中身は書き換えない)
func (s *scanner) skipSubst() bool {
	src := s.src
	if src[s.i] == '`' {
		for j := s.i + 1; j < len(src); j++ {
			if src[j] == '\\' {
				j++
				continue
			}
			if src[j] == '`' {
				s.i = j + 1
				return true
			}
		}
		return false
	}
	open, closeCh := src[s.i+1], byte(')')
	if open == '{' {
		closeCh = '}'
	}
	depth := 0
	for j := s.i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case '\'':
			k := strings.IndexByte(src[j+1:], '\'')
			if k < 0 {
				return false
			}
			j += k + 1
		case '"':
			for j++; j < len(src) && src[j] != '"'; j++ {
				if src[j] == '\\' {
					j++
				}
			}
			if j >= len(src) {
				return false
			}
		case open:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				s.i = j + 1
				return true
			}
		}
	}
	return false
}
