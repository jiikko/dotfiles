package check

// 本家 yomiyasu_hook.py (v3.1.0) の写し。check の公開の口 (ArmedEntries・ArmedUpdate・StartRun・VerifyQuick・RunKnownTexts・LatestFinalPath・
// TextSHA256Of) を呼ぶ。入口は hook パッケージの Main で、ここの HookMainWith を呼ぶだけ。
//
// hook パッケージに置かない: 本家と同じ入力で動かした記録との突き合わせは、check のテストの interp (op の解釈・状態フォルダの木の比べ・
// 時刻と実行 ID を固定した Runtime) を使う。hook パッケージから check の内部のテストは使えず、check のテストから hook を import すると
// import が輪になる。

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"ja-writing/cmdenv"
	"ja-writing/internal/pyfs"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pyos"
	"ja-writing/internal/pyshlex"
	"ja-writing/internal/pystr"
	"ja-writing/visibility"
)

// HookVersion は本家の yomiyasu_hook.py の __version__。
const HookVersion = "3.1.0"

var hookEvents = []string{"UserPromptExpansion", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "SessionEnd"}

// hookBudgets は _BUDGETS (イベントごとの秒数の上限)。
var hookBudgets = map[string]float64{"UserPromptExpansion": 4, "UserPromptSubmit": 4, "PreToolUse": 7, "PostToolUse": 7, "Stop": 20, "SessionEnd": 0.8}

const (
	hookMaxInput = 4 * 1024 * 1024
	hookMaxText  = 2 * 1024 * 1024
)

// hookHelp は _HELP。
const hookHelp = `使い方: yomiyasu_hook.py --host claude-code|codex [--state-dir DIR] [--words フォルダ | --no-words]
       yomiyasu_hook.py --print-config claude-code|codex
       yomiyasu_hook.py --version

任意のフックです。利用者が Claude Code か Codex の設定ファイルに貼ったときだけ、ホストが決まった時点で呼びます。
登録した原稿（yomiyasu_check.py start）について、返答の終わりに最終稿が検査記録と合うかを確かめ、合わなければ作業を
続けさせます。登録した原稿に関係する投稿で、本文が検査記録と照合できないときは止めます。読みやすさの合格や、意味が
保たれたかは判定しません。登録していない原稿には何もしません。

引数:
  --host claude-code|codex  フックとして動きます。ホストのイベントの JSON を標準入力から読みます。
  --state-dir DIR           登録を探す状態フォルダ。省くと環境変数 YOMIYASU_STATE_DIR（空白だけの値と ${ を
                            含む値は使いません）、git の最上位（なければ作業ディレクトリ）の .yomiyasu/ の順に探します。
                            Claude Code では、環境変数 CLAUDE_PROJECT_DIR のプロジェクトの .yomiyasu/ も探します。
  --words フォルダ          語の一覧（allow.txt と flag.txt。flag-patterns.txt も置けます）のフォルダ。フックは lint を
                            動かさないので、続けさせるときや止めるときに案内する yomiyasu_check.py run のコマンドに
                            --words と絶対パスを付けます。
                            一覧を読めないときは付けず、check が作った状態フォルダの hook-errors.log に1行残します。
  --no-words                案内する yomiyasu_check.py run のコマンドに --no-words を付けます（--words と一緒には使えず、
                            両方あればどちらも付けません）。どちらも省くと、run が自分で .yomiyasu-words/ を探します。
  --print-config claude-code|codex
                            設定に貼る JSON の例（このスクリプトの絶対パス入り）を出します。設定ファイルには書き込みません。
  --version                 バージョンを表示して終わります。
  -h, --help                このヘルプを表示して終わります。

終了コード:
  0  フックとして呼ばれたとき（--host）は、いつも0です。入力や引数が違っても、登録がなくても、内部の誤りでも、
     ホストの作業を止めないよう何も出さずに終わります（知らない引数も無視します）。作業を続けさせるときと投稿を
     止めるときは、終了コードではなく標準出力の JSON で伝えます。内部の誤りは、check が作った状態フォルダが
     あれば、その hook-errors.log に1行残します。-h、--version、--print-config も0です。
  2  --print-config の値がないか、claude-code でも codex でもない（標準エラーに使える値を出します）。`

const hookPrintConfigError = "--print-config には claude-code か codex を渡してください。"

// hookOptions は _options の dict (鍵があるか・値)。
type hookOptions struct {
	version, help, noWords        bool
	host, stateDir, words, config *string
	hasConfig, hasWords           bool
}

// parseHookOptions は _options。知らない引数があれば ok が偽 (本家の None。何も出さずに 0 で終わる)。
func parseHookOptions(argv []string) (o hookOptions, ok bool) {
	for i := 0; i < len(argv); {
		key := argv[i]
		switch {
		case key == "--version":
			o.version = true
			i++
		case key == "-h" || key == "--help":
			o.help = true
			i++
		case (key == "--host" || key == "--state-dir" || key == "--print-config" || key == "--words") && i+1 < len(argv):
			value := argv[i+1]
			switch key {
			case "--host":
				o.host = &value
			case "--state-dir":
				o.stateDir = &value
			case "--print-config":
				o.config, o.hasConfig = &value, true
			case "--words":
				o.words, o.hasWords = &value, true
			}
			i += 2
		case key == "--no-words":
			o.noWords = true
			i++
		case key == "--print-config":
			o.config, o.hasConfig = nil, true
			i++
		default:
			return hookOptions{}, false
		}
	}
	return o, true
}

// HookMain は ja-writing hook の入口 (本家の main)。
func HookMain(ctx context.Context, env cmdenv.Env) int {
	return HookMainWith(ctx, env, DefaultRuntime())
}

// hookRun は main の try の中で持つ値 (誤りのときに hook-errors.log を書く先と、処理中の登録)。
type hookRun struct {
	ctx     context.Context
	env     cmdenv.Env
	rt      Runtime
	opts    hookOptions
	states  []string
	event   string
	host    string
	cwd     string
	data    *pyjson.Object
	started float64
	budget  float64

	mu         sync.Mutex
	processing *[2]string // (state, run_id)

	wordsOnce sync.Once
	wordsArgs string
}

func (h *hookRun) setProcessing(state, runID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.processing = &[2]string{state, runID}
}

func (h *hookRun) clearProcessing() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.processing = nil
}

// HookMainWith は HookMain の Runtime を差し替える口 (記録の突き合わせが時刻・実行 ID・経過の時計を固定する)。
func HookMainWith(ctx context.Context, env cmdenv.Env, rt Runtime) int {
	h := &hookRun{ctx: ctx, env: env, rt: rt, event: "unknown"}
	output, rc, err := h.main(env.Args)
	if err == nil && output != "" {
		if _, werr := io.WriteString(env.Stdout, pyfs.BackslashReplace(output+"\n")); werr != nil {
			err = &hookFault{err: werr}
		}
	}
	if err != nil {
		h.fail(err)
		return 0
	}
	return rc
}

// fail は main の except BaseException (締め切りなら処理中の登録の log に timeout を残し、hook-errors.log に 1 行)。
func (h *hookRun) fail(err error) {
	h.mu.Lock()
	processing := h.processing
	h.mu.Unlock()
	if isPyError(err, "TimeoutError") && processing != nil && h.rt.Monotonic() >= h.budget {
		deadline := h.rt.Monotonic() + 0.2
		_, _ = ArmedUpdate(context.Background(), h.rt, processing[0], processing[1], func(entry *pyjson.Object) (bool, error) {
			h.log(entry, "timeout", nil)
			return true, nil
		}, &deadline)
	}
	h.errorLog(h.states, h.event, err)
}

// main は main の try の中 (出す文と終了コード)。誤りは fail が hook-errors.log に残す。
func (h *hookRun) main(argv []string) (output string, rc int, err error) {
	defer pyops.Catch(&err)
	defer catchFailure(&err)
	opts, ok := parseHookOptions(argv)
	if !ok {
		return "", 0, nil
	}
	h.opts = opts
	switch {
	case opts.version:
		return "yomiyasu_hook.py " + HookVersion, 0, nil
	case opts.help:
		return hookHelp, 0, nil
	case opts.config != nil && (*opts.config == "claude-code" || *opts.config == "codex"):
		text, err := hookConfig(*opts.config)
		return text, 0, err
	case opts.hasConfig:
		_, _ = io.WriteString(h.env.Stderr, hookPrintConfigError+"\n")
		return "", 2, nil
	}
	if opts.host == nil || (*opts.host != "claude-code" && *opts.host != "codex") {
		return "", 0, nil
	}
	h.host = *opts.host
	raw, err := io.ReadAll(io.LimitReader(h.env.Stdin, hookMaxInput+1))
	if err != nil {
		return "", 0, err
	}
	if len(raw) > hookMaxInput || !utf8.Valid(raw) {
		return "", 0, nil
	}
	value, err := pyjson.LoadsBytes(raw)
	if err != nil {
		return "", 0, nil // 本家は json.loads の ValueError で何も出さずに終わる
	}
	data, isDict := value.(*pyjson.Object)
	if !isDict {
		return "", 0, nil
	}
	session, isStr := data.Field("session_id").(string)
	if !isStr || pystr.Strip(session) == "" {
		return "", 0, nil
	}
	h.data = data
	event, _ := data.Field("hook_event_name").(string)
	if !slices.Contains(hookEvents, event) || (h.host == "claude-code" && event == "PostToolUse") || (h.host == "codex" && event == "UserPromptExpansion") {
		return "", 0, nil
	}
	h.event = event
	cwd, isStr := data.Field("cwd").(string)
	if !isStr || !strings.HasPrefix(cwd, "/") || !hookIsDir(cwd) {
		return "", 0, nil
	}
	h.cwd = cwd
	h.states = h.stateCandidates()
	invocation := hookInvocation(data, h.host, event)
	if invocation == nil && !slices.ContainsFunc(h.states, func(state string) bool { return hookIsDir(visibility.PathJoin(state, "armed")) }) {
		return "", 0, nil
	}
	h.started = h.rt.Monotonic()
	h.budget = h.started + hookBudgets[event]
	return h.withDeadline(invocation)
}

// withDeadline は本家の SIGALRM (締め切りで TimeoutError('hook_budget')) を、処理を別の goroutine で動かして締め切りで見切ることで写す。
// 見切った後も goroutine は止まらない (ctx を取り消すのでロックの待ちは抜ける)。本家と違い、見切った時点でロックを持っていれば、
// fail の timeout の log はロックを取れずに書かれない。
func (h *hookRun) withDeadline(invocation *string) (string, int, error) {
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	h.ctx = ctx
	type result struct {
		output string
		err    error
	}
	// タイマーの秒数は処理を始める前に決める (本家の setitimer と同じ順に経過の時計を読む。記録は時計を読む回数ごとに進めて比べる)
	wait := h.budget - h.rt.Monotonic()
	if wait <= 0.001 {
		// 本家は 0.001 秒の SIGALRM を掛けてから check を読み込むので、処理に入る前に必ず鳴る
		return "", 0, &hookFault{err: timeoutException("hook_budget"), loc: hookLocDeadline}
	}
	done := make(chan result, 1)
	go func() {
		var r result
		func() {
			defer pyops.Catch(&r.err)
			defer catchFailure(&r.err)
			r.output, r.err = h.process(invocation)
		}()
		done <- r
	}()
	timer := time.NewTimer(time.Duration(wait * float64(time.Second)))
	defer timer.Stop()
	select {
	case r := <-done:
		return r.output, 0, r.err
	case <-timer.C:
		return "", 0, &hookFault{err: timeoutException("hook_budget"), loc: hookLocDeadline}
	}
}

// stateCandidates は _states。
func (h *hookRun) stateCandidates() []string {
	var candidates []string
	env := h.env.Getenv(stateDirEnv)
	var fromEnv *string
	if pystr.Strip(env) != "" && !strings.Contains(env, "${") {
		fromEnv = &env
	}
	for _, value := range []*string{h.opts.stateDir, fromEnv} {
		if value != nil && *value != "" {
			candidates = append(candidates, hookAbsolute(visibility.PathJoin(h.cwd, HomePath(*value, h.env.Lookup))))
		}
	}
	candidates = append(candidates, hookProjectState(h.cwd))
	if project, set := h.env.Lookup("CLAUDE_PROJECT_DIR"); h.host == "claude-code" && set && project != "" && !strings.Contains(project, "${") {
		candidates = append(candidates, hookProjectState(hookAbsolute(project)))
	}
	var result []string
	seen := map[string]bool{}
	for _, path := range candidates {
		key := hookRealpath(path)
		if !seen[key] {
			seen[key] = true
			result = append(result, path)
		}
	}
	return result
}

// stateGiven は _state_given (最初の状態フォルダを --state-dir か環境変数で決めたか)。
func (h *hookRun) stateGiven() bool {
	env := h.env.Getenv(stateDirEnv)
	return (h.opts.stateDir != nil && *h.opts.stateDir != "") || (pystr.Strip(env) != "" && !strings.Contains(env, "${"))
}

// hookAbsolute は os.path.abspath (作業ディレクトリを読めなければ本家と同じく FileNotFoundError)。
func hookAbsolute(path string) string {
	return must(visibility.AbsolutePath(path))
}

// hookRealpath は os.path.normcase(os.path.realpath(path)) (書けない字のパスは本家と同じく UnicodeEncodeError・ValueError)。
func hookRealpath(path string) string {
	if place := hookPlace(path); place != nil {
		return *place
	}
	if strings.ContainsRune(path, 0) {
		pyops.Raise("ValueError", "embedded null byte")
	}
	_, err := pyfs.Encode(path)
	pyops.Raise("UnicodeEncodeError", "%s", err.Error())
	return ""
}

// hookPlace は _place (比べる形のパス。この機械のファイルを指せない名前なら nil)。
func hookPlace(path string) *string {
	if strings.ContainsRune(path, 0) {
		return nil
	}
	if _, err := pyfs.Encode(path); err != nil {
		return nil
	}
	place := armedPlace(path)
	return &place
}

// hookExists・hookIsDir・hookIsSymlink・hookIsFile は Path(...).exists() などを _path_matches で包んだもの (読めない・書けない名前は偽)。
func hookExists(path string) bool { return visibility.PathExists(path) }

func hookStat(path string, follow bool) (fs.FileInfo, bool) {
	if strings.ContainsRune(path, 0) {
		return nil, false
	}
	if _, err := pyfs.Encode(path); err != nil {
		return nil, false
	}
	var info fs.FileInfo
	var err error
	if follow {
		info, err = pyos.Stat(path)
	} else {
		info, err = pyos.Lstat(path)
	}
	return info, err == nil
}

func hookIsDir(path string) bool {
	info, ok := hookStat(path, true)
	return ok && info.IsDir()
}

func hookIsFile(path string) bool {
	info, ok := hookStat(path, true)
	return ok && info.Mode().IsRegular()
}

func hookIsSymlink(path string) bool {
	info, ok := hookStat(path, false)
	return ok && info.Mode()&fs.ModeSymlink != 0
}

// hookProjectState は _project_state (cwd から上へ .git を探し、見つかった所の .yomiyasu。無ければ cwd の .yomiyasu)。
func hookProjectState(cwd string) string {
	return visibility.PathJoin(hookProjectRootOr(cwd, visibility.PathString(cwd)), ".yomiyasu")
}

// hookProjectRootOr は path から上へ .git を探し、見つかった所 (pathlib の字面)。無ければ otherwise。
func hookProjectRootOr(path, otherwise string) string {
	current := visibility.PathString(path)
	for {
		if hookExists(visibility.PathJoin(current, ".git")) {
			return current
		}
		parent := visibility.PathParent(current)
		if parent == current {
			return otherwise
		}
		current = parent
	}
}

// hookInvocation は _invocation (登録を頼む呼び出しなら、原稿を探す文。そうでなければ nil)。
func hookInvocation(data *pyjson.Object, host, event string) *string {
	if event == "UserPromptExpansion" && host == "claude-code" {
		name := data.Field("command_name")
		if pyops.Equal(name, "yomiyasu") || pyops.Equal(name, "yomiyasu:yomiyasu") {
			if args, ok := data.Field("command_args").(string); ok {
				return &args
			}
			return nil
		}
	}
	if event != "UserPromptSubmit" {
		return nil
	}
	prompt, ok := data.Field("prompt").(string)
	if !ok {
		return nil
	}
	prompt = pystr.LStrip(prompt)
	if host == "claude-code" {
		for _, command := range []string{"/yomiyasu:yomiyasu", "/yomiyasu"} {
			if rest, found := strings.CutPrefix(prompt, command); found && rest != "" {
				if r, _ := pyfs.DecodeRune(rest); isSpaceRune(r) {
					return &rest
				}
			}
		}
		return nil
	}
	for _, word := range splitWhitespace(prompt) {
		if word == "$yomiyasu" || word == "$yomiyasu:yomiyasu" {
			return &prompt
		}
	}
	return nil
}

func isSpaceRune(r rune) bool { return r != utf8.RuneError && pystr.IsSpace(string(r)) }

// splitWhitespace は str.split() (空白の並びで切り、空の語を出さない)。
func splitWhitespace(s string) []string {
	var words []string
	start := -1
	for i := 0; i < len(s); {
		r, n := pyfs.DecodeRune(s[i:])
		if isSpaceRune(r) {
			if start >= 0 {
				words = append(words, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
		i += n
	}
	if start >= 0 {
		words = append(words, s[start:])
	}
	return words
}

// asciiIdentifier は str.isidentifier() の ASCII の範囲 (例外の型の名前は ASCII)。
func asciiIdentifier(s string) bool {
	for i, c := range []byte(s) {
		letter := c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z')
		if !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return s != ""
}

// hookConfig は _config (設定に貼る JSON)。本家の sys.executable と -B とスクリプトのパスの代わりに、この実行ファイルと hook の subcommand を書く。
func hookConfig(host string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, ok := visibility.Realpath(pyos.Decode(exe)); ok {
		exe = real
	}
	events := []string{"UserPromptExpansion", "UserPromptSubmit", "PreToolUse", "Stop", "SessionEnd"}
	if host != "claude-code" {
		events = []string{"UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "SessionEnd"}
	}
	timeouts := map[string]int64{"UserPromptExpansion": 5, "UserPromptSubmit": 5, "PreToolUse": 10, "PostToolUse": 10, "Stop": 30, "SessionEnd": 2}
	if host == "claude-code" {
		timeouts["SessionEnd"] = 1
	}
	hooks := pyjson.NewObject()
	for _, event := range events {
		handler := pyjson.NewObject()
		handler.Set("type", "command")
		handler.Set("timeout", timeouts[event])
		if host == "claude-code" {
			handler.Set("command", exe)
			handler.Set("args", pyjson.Strings([]string{"hook", "--host", host}))
		} else {
			handler.Set("command", pyshlex.Join([]string{exe, "hook", "--host", host}))
		}
		row := pyjson.NewObject()
		row.Set("hooks", []pyjson.Value{handler})
		switch event {
		case "UserPromptExpansion":
			row.Set("matcher", "^(yomiyasu|yomiyasu:yomiyasu)$")
		case "PreToolUse":
			if host == "claude-code" {
				row.Set("matcher", "^(Bash|mcp__.*)$")
			} else {
				row.Set("matcher", "^Bash$|^mcp__.*")
			}
		case "PostToolUse":
			row.Set("matcher", "^Bash$")
		}
		hooks.Set(event, []pyjson.Value{row})
	}
	config := pyjson.NewObject()
	if host != "claude-code" {
		config.Set("description", "yomiyasu の任意のフック（登録した原稿だけを対象にする）")
	}
	config.Set("hooks", hooks)
	return pyjson.Dumps(config, pyjson.Options{Indented: true, Indent: 2})
}

// hookFault は本家の例外に、hook-errors.log に書く場所 (トレースバックの最後の枠の「ファイル名:行」) を添えたもの。
type hookFault struct {
	err error
	loc string
}

func (f *hookFault) Error() string { return f.err.Error() }
func (f *hookFault) Unwrap() error { return f.err }

// 本家のトレースバックの最後の枠 (pin した本家の行)。投げずに渡す例外は枠が無いので「yomiyasu_hook.py:0」。
const (
	hookLocNone     = "yomiyasu_hook.py:0"
	hookLocDeadline = "yomiyasu_hook.py:897" // deadline_reached
	hookLocBudget   = "yomiyasu_hook.py:760" // _process の remaining
)

// hookErrorType は type(error).__name__ (識別子でなければ Exception)。
func hookErrorType(err error) string {
	var e *pyops.Exception
	if errors.As(err, &e) {
		return e.Type
	}
	var v *visibility.Error
	if errors.As(err, &v) {
		return v.Type
	}
	if o, ok := osException(err); ok {
		return o.Type
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return visibility.OSErrorType(errno)
	}
	return "Exception"
}

// hookErrorLocation は本家のトレースバックの最後の枠。語の一覧の誤りは本家の markdown_visibility.py の行で、ほかの例外 (check の口と
// hook の中の誤り) は hook の 0 行にする (本家はその例外を投げた行を書くので、場所の字は本家と違う。README の「揃わない範囲」)。
func hookErrorLocation(err error) string {
	var f *hookFault
	if errors.As(err, &f) && f.loc != "" {
		return f.loc
	}
	var v *visibility.Error
	if errors.As(err, &v) && v.Type == visibility.TypeWordsError {
		for _, site := range wordsErrorSites {
			if strings.Contains(v.Message, site.text) {
				return "markdown_visibility.py:" + site.line
			}
		}
	}
	return hookLocNone
}

// wordsErrorSites は本家の markdown_visibility.py で WordsError を投げる行と、その文に固有の字 (上から順に照らす)。
var wordsErrorSites = []struct{ text, line string }{
	{"--words に空の値は使えません", "973"},
	{": 作業ディレクトリを読めないため、相対パスを決められません", "976"},
	{": --words の", "981"},
	{"行目の使う語の中にタブがあります", "1028"},
	{"行目の使う語は ", "1031"},
	{": UTF-8 として読めません（", "1045"},
	{": 行が多すぎます（", "1051"},
	{"行目に、名前と正規表現を分けるタブがありません", "1255"},
	{"行目の名前が空です", "1260"},
	{"行目の名前は ", "1262"},
	{": パターンが多すぎます（", "1264"},
	{"行目の正規表現を使えません（", "1269"},
	{"試す組み合わせの合計が多すぎます", "1272"},
	{": ファイルを開けません（", "1301"},
}

// errorLog は _error_log (check が作った状態フォルダの hook-errors.log に 1 行足す。64 KiB を超えたら前の半分を捨てる)。
func (h *hookRun) errorLog(states []string, event string, err error) {
	for _, state := range states {
		if !hookIsDir(state) {
			continue
		}
		made := false
		for _, name := range []string{".gitignore", "state.json"} {
			path := visibility.PathJoin(state, name)
			if hookExists(path) || hookIsSymlink(path) {
				made = true
				break
			}
		}
		if !made {
			return
		}
		if !slices.Contains(hookEvents, event) {
			event = "unknown"
		}
		kind := hookErrorType(err)
		if !asciiIdentifier(kind) {
			kind = "Exception"
		}
		line := pyfs.BackslashReplace(ISO(h.rt.Now()) + " " + event + " " + kind + " " + hookErrorLocation(err) + "\n")
		path := visibility.PathJoin(state, "hook-errors.log")
		if hookIsSymlink(path) {
			continue
		}
		if !appendErrorLine(path, []byte(line)) {
			continue
		}
		return
	}
}

// appendErrorLine は hook-errors.log に line を足す。通常のファイルでなければ偽 (本家は次の状態フォルダへ進む)。失敗は握る (本家の except BaseException: pass)。
func appendErrorLine(path string, line []byte) bool {
	fd, err := pyos.OpenFD(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return true
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return true
	}
	if !info.Mode().IsRegular() {
		return false
	}
	if err := pyos.Chmod(path, 0o600); err != nil {
		return true
	}
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return true
	}
	if len(data)+len(line) > 65536 {
		data = data[len(data)/2:]
		if _, after, found := strings.Cut(string(data), "\n"); found {
			data = []byte(after)
		} else {
			data = nil
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return true
	}
	if _, err := file.Write(append(data, line...)); err != nil {
		return true
	}
	_ = file.Truncate(int64(len(data) + len(line)))
	return true
}

// log は _log (登録の log に 1 件足す。stopActive が nil でなければ stop_hook_active を残す)。
func (h *hookRun) log(entry *pyjson.Object, result string, stopActive pyjson.Value, event ...string) {
	name := h.event
	if len(event) > 0 {
		name = event[0]
	}
	ms := int(math.Trunc((h.rt.Monotonic() - h.started) * 1000))
	item := armedLogItem(h.rt, name, result, ms)
	if stopActive != nil {
		item.Set("stop_hook_active", pyops.Truthy(stopActive))
	}
	appendArmedLog(entry, item)
}
