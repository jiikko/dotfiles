package main

import (
	"encoding/json"
	"fmt"
	"io"
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
		Cwd       string         `json:"cwd"`
	}
	if err := json.NewDecoder(stdin).Decode(&in); err != nil || in.ToolName != "Bash" || in.ToolInput == nil {
		return 0
	}
	cmd, ok := in.ToolInput["command"].(string)
	if !ok {
		return 0
	}
	wrapped, changed, skip := wrapCommand(cmd)
	logHookDecision(cmd, changed, skip)
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
	if note := queueNote(in.Cwd); note != "" {
		hso["additionalContext"] = note
	}
	_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": hso})
	return 0
}

// hookLogMax は hook.log の上限 (超えたら hook.log.1 へ回して作り直す)
const hookLogMax = 1 << 20

// logHookDecision は hook の判断を状態の置き場の hook.log に 1 行残す (効いたか・なぜ包まなかったかを後から確かめるため。
// hook の stderr は Claude Code に出ない)。残すのは、差し込んだときと、重いコマンドの名前を含むのに読み切れず見送ったときだけ。
// 置き場が無ければ作らない
func logHookDecision(cmd string, changed bool, skip string) {
	decision := "wrapped"
	if !changed {
		if skip == "" || !mentionsHeavy(cmd) {
			return
		}
		decision = "skipped:" + skip
	}
	base, err := stateBase()
	if err != nil {
		return
	}
	if _, err := os.Stat(base); err != nil {
		return
	}
	p := hookLogPath(base)
	if fi, err := os.Stat(p); err == nil && fi.Size() > hookLogMax {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s\t%s\t%s\n", time.Now().Format(time.RFC3339), decision, displayCmd([]string{cmd}))
}

// mentionsHeavy は、コマンドの文字列に重いコマンドの名前が出るか (見送りを記録するかの粗い判定)
func mentionsHeavy(cmd string) bool {
	for _, w := range []string{"make", "go test", "go -C", "swift", "xcodebuild", "mutate-verify"} {
		if strings.Contains(cmd, w) {
			return true
		}
	}
	return false
}

// queueNote は既定の lock の待ち行列に誰かいれば、LLM 向けの 1 段落を返す。いなければ空 (普段は何も足さない)。
// 置き場が無ければ作らずに空を返す (hook は待ち行列を書き換えず、死んだ札の回収もしない)
// 見る列は hook の入力の cwd の列 (コマンドが先頭で cd する形では、実際に並ぶ列と違うことがある)
func queueNote(cwd string) string {
	base, err := stateBase()
	if err != nil {
		return ""
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	lock, _ := lockFor(cwd) // 範囲の値の誤りは本体が警告してマシン全体の列に並ぶので、同じ列を見る
	q, err := openExistingQueue(filepath.Join(base, lock))
	if err != nil {
		return ""
	}
	preds, err := q.lineup()
	if err != nil || len(preds) == 0 {
		return ""
	}
	head := ""
	if info, ok := q.readInfo(preds[0].seq); ok {
		head = "先頭は " + holderSummary(info, true) + "。"
		if n := longHoldNote(info, true); n != "" {
			head += n + "。"
		}
	}
	return fmt.Sprintf("serial-run: このコマンドの重い部分は serial-run の順番待ちに入る (同じ repo の重いコマンドを 1 本ずつ流す仕組み。SERIAL_RUN_SCOPE=machine ならマシン全体で 1 本)。"+
		"前に %d 本ある。%s待っている間は Bash の出力が返らない (固まったのではない)。"+
		"foreground の Bash の timeout は最大 10 分なので、前が長いなら run_in_background で起こす。同じコマンドを打ち直すと列の最後尾に並び直す。"+
		"状況は別の呼び出しで `serial-run status` を打って見る。"+
		"待ちの上限は既定 %sで、超えたら実行せずに rc=%d で抜ける (stderr に「待ちの上限」と出る。テストの失敗ではない)",
		len(preds), head, fmtDur(defaultMaxWait), rcTool)
}

var (
	assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	// PATH を書き換える語 (前置きの代入・export / declare の引数)。GOPATH= などの別の変数は含めない
	pathAssignRe = regexp.MustCompile(`^(PATH|path)\+?=`)
	// 次の語もコマンドの位置のままにする予約語 (`if make test; then …` の make を見る)
	keywords = map[string]bool{"if": true, "then": true, "else": true, "elif": true, "do": true, "while": true, "until": true, "!": true, "{": true, "time": true}
	// make の、次の引数を値として取るオプション
	makeValueOpts = map[string]bool{"-C": true, "-f": true, "-I": true, "-o": true, "-W": true, "--directory": true, "--file": true}
	// make の、レシピを走らせない呼び方 (dry-run・問い合わせ・データベースの表示・usage)。待たせない
	makeNoRunOpts = map[string]bool{"-n": true, "--dry-run": true, "--just-print": true, "--recon": true, "-q": true, "--question": true,
		"-p": true, "--print-data-base": true, "-h": true, "--help": true, "-v": true, "--version": true, "-t": true, "--touch": true}
	// make の短いオプションの束 (`-nk` など) のうち、レシピを走らせない文字 / 値を取る文字 (そこから後ろは値: `-Cscripts`)
	makeNoRunShort = "nqpht"
	makeValueShort = "CfIoW"
	shortClusterRe = regexp.MustCompile(`^-[A-Za-z]+$`)
	// PATH を書き換えうる組み込みのコマンド (引数に PATH= を取る)
	pathSetters = map[string]bool{"export": true, "declare": true, "typeset": true, "local": true, "readonly": true}
	// 複合コマンドを始める語 / 閉じる語。パイプの段そのものが複合コマンドだと (`| while …` / `done |`)、段の区切りを読み切れない
	compoundOpeners = map[string]bool{"if": true, "while": true, "until": true, "for": true, "select": true, "{": true}
	compoundClosers = map[string]bool{"}": true, "done": true, "fi": true}
	// 使い方を出すだけの引数 (go / swift / mutate-verify)。待たせない
	helpFlags = map[string]bool{"-h": true, "-help": true, "--help": true}
	// xcodebuild の、ビルドせずに情報だけを出す呼び方 (待たせない)
	xcodeInfoFlags = map[string]bool{"-version": true, "-list": true, "-showBuildSettings": true, "-showsdks": true,
		"-showdestinations": true, "-help": true, "-usage": true, "-license": true, "-checkFirstLaunchStatus": true}
)

// unwrapPrefix は前に付いた包みのコマンド (env / nice / nohup / command / noglob / runtimeout) を外した残りの語を返す。
// 差し込む位置は包みの前のまま (`serial-run -- runtimeout 600 make test`。包みの中に入れると待ちが runtimeout の上限を食う)
func unwrapPrefix(words []string) []string {
	for len(words) > 0 {
		i := 1
		switch filepath.Base(words[0]) {
		// 🚨 noglob は含めない: zsh の前置きの修飾子で実行ファイルではないので、前に serial-run を差し込むと exec できず rc=127 になる
		case "nohup", "command":
		case "env":
			for i < len(words) && (strings.HasPrefix(words[i], "-") || strings.Contains(words[i], "=")) {
				if words[i] == "-u" || words[i] == "-S" || words[i] == "-P" || words[i] == "-C" {
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

// changesPath は単純コマンドが PATH を書き換えるか (`export PATH=…`・`builtin export PATH=…`・`unset PATH`・`eval …`)。
// 引数の中の PATH= (`grep PATH=`・`make test PATH=/x`・`env PATH=/x make`) は見ない
func changesPath(words []string) bool {
	for len(words) > 0 && (words[0] == "builtin" || words[0] == "command") {
		words = words[1:]
	}
	if len(words) == 0 {
		return false
	}
	switch {
	case words[0] == "eval":
		return true // 中身を読めない
	case words[0] == "unset":
		for _, a := range words[1:] {
			if a == "PATH" || a == "path" {
				return true
			}
		}
	case pathSetters[words[0]]:
		for _, a := range words[1:] {
			if pathAssignRe.MatchString(a) {
				return true
			}
		}
	}
	return false
}

func isSerialRun(words []string) bool {
	w := unwrapPrefix(words)
	return len(w) > 0 && filepath.Base(w[0]) == "serial-run"
}

func anyHelp(words []string) bool {
	for _, a := range words {
		if helpFlags[a] {
			return true
		}
	}
	return false
}

// isHeavy は 1 つの単純コマンドの語 (引用を外したもの) が直列化の対象かを返す。対象の一覧の正本はここ。
// 🚨 コマンド名を足したら、hook の入口 (_claude/hooks/serial-run-wrap.sh) の手がかりの語にも足す
// (tests/claude/test_serial_run_wrap.sh が、対象の例が入口を通り抜けることを確かめる)
func isHeavy(words []string) bool {
	words = unwrapPrefix(words)
	if len(words) == 0 {
		return false
	}
	switch filepath.Base(words[0]) {
	case "mutate-verify", "mutate-verify-list":
		return !anyHelp(words[1:])
	case "xcodebuild":
		for _, a := range words[1:] {
			if xcodeInfoFlags[a] {
				return false
			}
		}
		return true
	case "go":
		rest := words[1:]
		for len(rest) > 0 { // go のグローバルなオプション (`go -C dir test` / `go -C=dir test`)
			if rest[0] == "-C" && len(rest) >= 2 {
				rest = rest[2:]
			} else if strings.HasPrefix(rest[0], "-C=") {
				rest = rest[1:]
			} else {
				break
			}
		}
		if len(rest) == 0 || rest[0] != "test" {
			return false
		}
		// -args から後ろはテストのバイナリへ渡す引数 (help の判定に使わない)
		own := rest[1:]
		for i, a := range own {
			if a == "-args" || a == "--args" {
				own = own[:i]
				break
			}
		}
		return !anyHelp(own)
	case "swift":
		return len(words) > 1 && (words[1] == "build" || words[1] == "test") && !anyHelp(words[2:])
	case "make", "gmake":
		heavy := false
		for i := 1; i < len(words); i++ {
			a := words[i]
			switch {
			case makeValueOpts[a]:
				i++
			case makeNoRunOpts[a] || (shortClusterRe.MatchString(a) && shortClusterHasNoRun(a[1:])):
				return false
			case shortClusterRe.MatchString(a) && strings.ContainsRune(makeValueShort, rune(a[len(a)-1])):
				i++ // 束の最後が値を取る文字 (`-kC dir`): 次の語はその値
			case strings.HasPrefix(a, "-") || strings.Contains(a, "="):
			case a == "test" || a == "lint" || strings.HasPrefix(a, "test-") || strings.HasPrefix(a, "lint-"):
				heavy = true
			}
		}
		return heavy
	}
	return false
}

// shortClusterHasNoRun は make の短いオプションの束を左から読み、値を取る文字より前にレシピを走らせない文字があるか
func shortClusterHasNoRun(cluster string) bool {
	for _, c := range cluster {
		switch {
		case strings.ContainsRune(makeValueShort, c):
			return false // ここから後ろは値 (`-Cscripts` の p は dry-run ではない)
		case strings.ContainsRune(makeNoRunShort, c):
			return true
		}
	}
	return false
}

// wrapCommand は cmd の中の、コマンドの位置にある重い単純コマンドの直前に wrapPrefix を差し込む。
// 字句は保守的に読む: 読み切れない形は丸ごと差し込まず、理由 (skip) を返す。引用符・コマンド置換の中は書き換えない
// (commit message の本文などの文字列を変えない)。既に `serial-run -- …` の形の単純コマンドは、コマンド語が serial-run なので重いと読まない。
// 🚨 `<<` と `((` は字句解析の前に打ち切る。scanner は heredoc の本文を単純コマンドとして読み続けるので、
// `git commit -F - <<'EOF'` の本文の `make test` に差し込んでしまう。算術の `)` もサブシェルの閉じと読み違える
func wrapCommand(cmd string) (wrapped string, changed bool, skip string) {
	switch {
	case strings.Contains(cmd, "<<"):
		return cmd, false, "heredoc"
	case strings.Contains(cmd, "(("):
		return cmd, false, "arith"
	}
	s := scanner{src: cmd}
	inserts, ok := s.scan()
	switch {
	case !ok:
		return cmd, false, "parse"
	case s.pathChanged:
		// PATH を書き換えるコマンドは差し込まない (前置きの PATH= / export PATH の後では serial-run が見つからず rc=127 になる)
		return cmd, false, "path"
	case s.manual || (s.sawPipe && strings.Contains(cmd, "serial-run")):
		// 手で書いた serial-run があるコマンドには差し込まない。パイプがあれば、どこに書かれていても (xargs・sh -c・置換の中) 見送る:
		// 同じパイプラインに 2 本の serial-run が入ると互いの札を待って止まる
		return cmd, false, "manual"
	case s.sawPipe && len(inserts) > 1:
		// 🚨 構造の歯止め: パイプを含むコマンドでは差し込みを 1 か所までにする。字句で段の境目を読み違えても (複合コマンド・リダイレクト・
		// シェルの方言)、1 つのパイプラインに serial-run が 2 本入って互いの札を待つ形は起きない。取りこぼすのは、重いパイプラインが
		// 2 つ並ぶ形 (`make lint | tail; make test | tail`) だけ
		return cmd, false, "multi"
	case s.compoundPipe || s.pipeNewline:
		// パイプの 1 段がサブシェル・グループ・制御構文 (`(…) |`・`| while …`)、または | の後で改行している形は、段の境目を読み切れない
		// (片側を包むと、もう片側に差し込んで互いの札を待って止まる)
		return cmd, false, "parse"
	case len(inserts) == 0:
		return cmd, false, ""
	}
	var b strings.Builder
	last := 0
	for _, at := range inserts {
		b.WriteString(cmd[last:at])
		b.WriteString(wrapPrefix)
		last = at
	}
	b.WriteString(cmd[last:])
	return b.String(), true, ""
}

type scanner struct {
	src     string
	i       int
	inserts []int

	// 今読んでいる単純コマンド
	words []string
	cmdAt int // コマンド語の位置 (-1 = まだ無い。予約語・代入の後もコマンドの位置のまま)

	pipeHeavy    int  // 今のパイプラインで最初の重い段の位置 (-1 = まだ無い)
	pathChanged  bool // PATH を書き換える語があったか
	manual       bool // 手で書いた serial-run がコマンドの位置にあったか
	sawPipe      bool // パイプ (`|`) があったか
	compoundPipe bool // 複合コマンドの境目と | が隣り合ったか
	pipeNewline  bool // `|` の後で改行したか (コメントの後の改行を含む。bash は同じパイプラインとして続ける)
	closer       bool // 直前の語が複合コマンドを閉じた (`)` `}` `done` `fi`)
	afterPipe    bool // 直前が | だった
}

// endSimple は単純コマンドの終わり。pipe は、次がパイプの続き (`|`) か
func (s *scanner) endSimple(pipe bool) {
	// 1 つのパイプラインで包むのは最初の重い段 1 つだけ (両端を包むと、互いの札を待って待ちの上限まで止まる)
	if s.cmdAt >= 0 {
		switch {
		case isSerialRun(s.words):
			s.manual = true
		case s.pipeHeavy < 0 && isHeavy(s.words):
			s.pipeHeavy = s.cmdAt
		}
		if changesPath(s.words) {
			s.pathChanged = true
		}
	}
	// 1 つのパイプラインで包むのは最初の重い段 1 つだけ (位置はパイプラインの終わりで決める)
	if !pipe {
		if s.pipeHeavy >= 0 {
			s.inserts = append(s.inserts, s.pipeHeavy)
		}
		s.pipeHeavy = -1
	}
	s.words, s.cmdAt = nil, -1
}

func (s *scanner) scan() ([]int, bool) {
	s.cmdAt, s.pipeHeavy = -1, -1
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
		case c == '|' && s.i+1 < len(src) && src[s.i+1] == '|':
			s.endSimple(false) // `||`
			s.i += 2
		case c == '|':
			s.endSimple(true) // パイプ (`|` / `|&`)
			s.sawPipe = true
			if s.closer {
				s.compoundPipe = true
			}
			s.closer, s.afterPipe = false, true
			s.i++
			if s.i < len(src) && src[s.i] == '&' {
				s.i++
			}
			// | の後の空白・行の継ぎ・コメントを読み飛ばし、改行が来たら見送る (scanner は改行を区切りと読むが、bash はパイプを続ける)
			j := s.i
			for j < len(src) {
				switch {
				case src[j] == ' ' || src[j] == '\t':
					j++
					continue
				case src[j] == '\\' && j+1 < len(src) && src[j+1] == '\n':
					j += 2
					continue
				case src[j] == '#':
					for j < len(src) && src[j] != '\n' {
						j++
					}
					continue
				}
				break
			}
			if j < len(src) && src[j] == '\n' {
				s.pipeNewline = true
			}
		case c == '\\' && s.i+1 < len(src) && src[s.i+1] == '\n':
			s.i += 2 // 行の継ぎ (空白と同じ)
		case c == '\n' || c == ';' || c == '&':
			s.endSimple(false)
			s.closer, s.afterPipe = false, false
			s.i++
		case c == '(':
			if s.cmdAt >= 0 {
				return nil, false
			}
			if s.afterPipe {
				s.compoundPipe = true
			}
			s.afterPipe = false
			depth++
			s.endSimple(false)
			s.i++
		case c == ')':
			if depth == 0 {
				return nil, false
			}
			depth--
			s.endSimple(false)
			s.closer, s.afterPipe = true, false
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
		case c >= '0' && c <= '9' && s.atWordStart() && digitsBeforeRedirect(src, s.i):
			// 番号付きのリダイレクト (`2>&1`): 番号を語として読むと、閉じた複合コマンドの後の | を見落とし、コマンドの位置も取り違える
			for src[s.i] >= '0' && src[s.i] <= '9' {
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
				if w == "case" {
					return nil, false
				}
				if assignRe.MatchString(src[start:s.i]) {
					// 前置きの代入。PATH を変えると、その後ろの serial-run が見つからない
					if pathAssignRe.MatchString(src[start:s.i]) {
						s.pathChanged = true
					}
					continue
				}
				if s.afterPipe && compoundOpeners[w] {
					s.compoundPipe = true
				}
				if w == "time" || w == "!" {
					continue // | の直後のまま (zsh は `| time (…)` を受ける)
				}
				s.afterPipe = false
				if compoundClosers[w] {
					s.closer = true
					if w != "}" {
						s.cmdAt = start
						s.words = append(s.words, w)
						continue
					}
				} else {
					s.closer = false
				}
				if keywords[w] || w == "}" {
					continue
				}
				s.cmdAt = start
			}
			s.words = append(s.words, w)
		}
	}
	if depth != 0 {
		return nil, false
	}
	s.endSimple(false)
	return s.inserts, true
}

// digitsBeforeRedirect は src[i:] が「数字の並びの直後に < か >」か
func digitsBeforeRedirect(src string, i int) bool {
	j := i
	for j < len(src) && src[j] >= '0' && src[j] <= '9' {
		j++
	}
	return j > i && j < len(src) && (src[j] == '<' || src[j] == '>')
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
			if c == '$' && s.i+1 < len(src) && src[s.i+1] == '\'' {
				return "", false // $'…' (中で \' が閉じにならない) は読み切れない
			}
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
