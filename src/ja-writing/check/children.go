package check

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"subproc"

	"ja-writing/cmdenv"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// 子プロセス (lint と diff) の実行 (本家の _Children と _wait_seconds・_parse_report・_check_entry)。

const (
	waitLimit   = 1000000 // _WAIT_LIMIT (これを超える秒数は上限なしで待つ)
	stderrLimit = 2000    // _STDERR_LIMIT (記録に残す子の標準エラーの字数)
	stopWait    = 5 * time.Second
)

// Runtime は run が外の世界から取る値。Main は DefaultRuntime を使い、テストは子の実行ファイル・時刻・経過の時計・乱数を差し替える。
//
// 子を関数の呼び出しにしない: lint.Main・diff.Main と regexp2 の照合は ctx を見ないので、止まらない照合を止める手段が kill だけになる。
type Runtime struct {
	// Executable は子として起こす ja-writing (lint・diff の subcommand を持つ実行ファイル)。
	Executable func() (string, error)
	// ChildEnv は子の環境変数 (本家は親の環境に PYTHONIOENCODING を足したもの)。本番は run の Env の環境変数の全部。
	ChildEnv func(env cmdenv.Env) []string
	// Now は今の時刻 (UTC・秒まで。本家の _utc_now)。
	Now func() time.Time
	// Monotonic は経過を測る時計の秒 (本家の time.monotonic)。
	Monotonic func() float64
	// RandomHex は n バイトの乱数の 16 進 (本家の os.urandom(n).hex())。
	RandomHex func(n int) (string, error)
	// TempDir は一時フォルダを作る親 (本家の tempfile.gettempdir())。本番は gettempdir。
	TempDir func(env cmdenv.Env) (string, error)
}

// DefaultRuntime は本番の Runtime。
func DefaultRuntime() Runtime {
	started := time.Now()
	return Runtime{
		Executable: os.Executable,
		ChildEnv:   cmdenv.Env.Environment,
		Now:        func() time.Time { return time.Now().UTC().Truncate(time.Second) },
		Monotonic:  func() float64 { return time.Since(started).Seconds() },
		RandomHex: func(n int) (string, error) {
			b := make([]byte, n)
			if _, err := rand.Read(b); err != nil {
				return "", err
			}
			return hex.EncodeToString(b), nil
		},
		TempDir: gettempdir,
	}
}

// waitSeconds は _wait_seconds (子を待つ秒数。上限を超える値は上限なし = 負の値)。
func waitSeconds(remaining float64) float64 {
	if remaining > waitLimit {
		return -1
	}
	return remaining
}

// childResult は子 1 つの結果 (本家の ChildResult の組: 状態・終了コード・標準出力・標準エラー・ミリ秒・誤り)。
type childResult struct {
	status  string // ran・timeout・error
	code    pyjson.Value
	stdout  []byte // ran のときだけ
	stderr  string
	elapsed int64
	err     pyjson.Value // 誤りの文か nil
}

// children は _Children (lint と diff の子を、同じ締め切りで並べて動かす)。
type children struct {
	deadline  float64
	monotonic func() float64
	env       []string
	cwd       string

	mu        sync.Mutex
	processes []*exec.Cmd
	stopped   bool
	wg        sync.WaitGroup
	results   map[string]childResult
}

func newChildren(rt Runtime, timeout float64, cwd string, environ []string) *children {
	return &children{
		deadline: rt.Monotonic() + timeout, monotonic: rt.Monotonic, env: environ, cwd: cwd,
		results: map[string]childResult{},
	}
}

func (c *children) start(name string, argv []string) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.runOne(name, argv)
	}()
}

func (c *children) set(name string, r childResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.results[name]; !ok {
		c.results[name] = r
	}
}

// cannotRun は「子プロセスを動かせません（型: 文）」。起動の失敗 (fork/exec の errno) は、本家の Popen が出す OSError と同じ型の名前と
// 「[Errno 番号] 理由: 'パス'」の文にする。ほかの誤り (本家では起きない) は Go の誤りの型と文。
func cannotRun(err error) string {
	if e, ok := osException(err); ok {
		return "子プロセスを動かせません（" + e.Error() + "）"
	}
	return fmt.Sprintf("子プロセスを動かせません（%T: %v）", err, err)
}

// osException はパスつきの errno の誤りを、本家の OSError (errno で決まる型の名前と「[Errno 番号] 理由: 'パス'」の文) にする。
// パスつきの errno でなければ (nil, false)。
func osException(err error) (*pyops.Exception, bool) {
	var pathErr *fs.PathError
	var errno syscall.Errno
	if !errors.As(err, &pathErr) || !errors.As(err, &errno) {
		return nil, false
	}
	return &pyops.Exception{Type: visibility.OSErrorType(errno), Msg: fmt.Sprintf("[Errno %d] %s: %s", int(errno), visibility.OSErrorReason("", errno), pystr.Repr(pathErr.Path))}, true
}

// popenArgs は本家の Popen が bytes にした argv・環境変数・作業フォルダ (子にこのバイト列で渡す)。
type popenArgs struct {
	argv, env []string
	cwd       string
}

// popenEncode は、本家の Popen が子を起こす前に argv・実行ファイル・環境変数・作業フォルダを bytes にする所を写す。
// 字は os.fsencode と同じく U+DC80..U+DCFF のサロゲートを 1 バイトに戻す (記録の JSON の "\udcff" は 0xff で渡る)。ほかのサロゲートは
// UnicodeEncodeError、NUL は ValueError("embedded null byte") で、子を起こさずに断る。Go の exec のまま起動すると、サロゲートは WTF-8 の
// 3 バイトで渡り、NUL は EINVAL の PathError (実行ファイルのパスつき) になる。
// 断りの順は本家と同じ: 環境変数を前から (名前、値の順に bytes にする)、argv を前から 1 つずつ (字を bytes にしてから NUL を見る)、
// 環境変数の NUL、作業フォルダ (bytes にしてから NUL を見る)。
// OS から受けた値 (os.Environ・os.Getwd) は、UTF-8 として読めるバイトと読めないバイトはそのまま戻る (本家の str は os.fsdecode で
// 読んだ値なので、os.fsencode で同じバイト列に戻る)。WTF-8 のサロゲートの形 (ED A0..BF xx) の生のバイトだけは字として読むので、
// 本家と答えが違う (EncodeSurrogateEscape の doc comment)。
// 本家の「illegal environment variable name」(名前に = のある環境変数) は写さない: Go の環境変数は「名前=値」の 1 つの文字列で、
// 最初の = で切った名前は = を持てない。
func popenEncode(argv, env []string, cwd string) (popenArgs, *pyops.Exception) {
	nullByte := &pyops.Exception{Type: "ValueError", Msg: "embedded null byte"}
	encode := func(s string) (string, *pyops.Exception) {
		encoded, err := pyunicode.EncodeSurrogateEscape(s)
		if err != nil {
			return "", &pyops.Exception{Type: "UnicodeEncodeError", Msg: err.Error()}
		}
		return encoded, nil
	}
	out := popenArgs{argv: make([]string, len(argv))}
	if env != nil { // nil は exec で今のプロセスの環境を引き継ぐ形なので、空の並びにしない
		out.env = make([]string, len(env))
	}
	for i, entry := range env {
		name, value, hasValue := strings.Cut(entry, "=")
		encoded, exc := encode(name)
		if exc != nil {
			return popenArgs{}, exc
		}
		if hasValue {
			v, exc := encode(value)
			if exc != nil {
				return popenArgs{}, exc
			}
			encoded += "=" + v
		}
		out.env[i] = encoded
	}
	for i, s := range argv {
		encoded, exc := encode(s)
		if exc != nil {
			return popenArgs{}, exc
		}
		if strings.IndexByte(encoded, 0) >= 0 {
			return popenArgs{}, nullByte
		}
		out.argv[i] = encoded
	}
	for _, s := range out.env {
		if strings.IndexByte(s, 0) >= 0 {
			return popenArgs{}, nullByte
		}
	}
	encoded, exc := encode(cwd)
	if exc != nil {
		return popenArgs{}, exc
	}
	if strings.IndexByte(encoded, 0) >= 0 {
		return popenArgs{}, nullByte
	}
	out.cwd = encoded
	return out, nil
}

// pathAsGiven は起動の誤りのパスを、bytes にする前の字に戻す。本家の OSError の filename は str の作業フォルダか実行ファイルで、
// bytes にしたパスのままだと、U+DC80..U+DCFF のサロゲートが 1 バイトの生のバイトになり、repr が本家と違う。
func (p popenArgs) pathAsGiven(err error, executable, cwd string) error {
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		return err
	}
	given := *pathErr
	switch {
	case pathErr.Op == "chdir" && pathErr.Path == p.cwd:
		given.Path = cwd
	case pathErr.Path == p.argv[0]:
		given.Path = executable
	default:
		return err
	}
	return &given
}

// runOne は _run_one。子を 1 つ動かし、どの道でも結果を残す。
func (c *children) runOne(name string, argv []string) {
	started := c.monotonic()
	elapsed := func() int64 { return int64((c.monotonic() - started) * 1000) }
	remaining := c.deadline - started
	if remaining <= 0 {
		c.set(name, childResult{status: "timeout", err: "時間切れ（--timeout の秒数を使い切った）"})
		return
	}
	var stdout, stderr lockedBuffer
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		c.set(name, childResult{status: "error", err: "子プロセスを動かす前に止められました"})
		return
	}
	popen, refused := popenEncode(argv, c.env, c.cwd)
	if refused != nil {
		c.mu.Unlock()
		c.set(name, childResult{status: "error", elapsed: elapsed(), err: "子プロセスを動かせません（" + refused.Error() + "）"})
		return
	}
	cmd := subproc.Command(popen.argv[0], popen.argv[1:]...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = popen.cwd, popen.env, &stdout, &stderr
	if err := cmd.Start(); err != nil {
		c.mu.Unlock()
		c.set(name, childResult{status: "error", stderr: "", elapsed: elapsed(), err: cannotRun(popen.pathAsGiven(err, argv[0], c.cwd))})
		return
	}
	c.processes = append(c.processes, cmd)
	c.mu.Unlock()
	var waitErr error
	done := make(chan struct{})
	go func() {
		waitErr = cmd.Wait()
		close(done)
	}()
	var expired <-chan time.Time
	if wait := waitSeconds(remaining); wait >= 0 {
		timer := time.NewTimer(time.Duration(wait * float64(time.Second)))
		defer timer.Stop()
		expired = timer.C
	}
	select {
	case <-done:
	case <-expired:
		// subprocess.run と同じ止め方 (ここまでの標準エラーを残す)
		_ = cmd.Process.Kill()
		<-done
		c.set(name, childResult{status: "timeout", stderr: clipStderr(stderr.Bytes()), elapsed: elapsed(),
			err: "時間切れ（--timeout の秒数を超えた）"})
		return
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		// 待てない (出力の写しが終わらないなど)。子は終わっている
		c.set(name, childResult{status: "error", elapsed: elapsed(), err: cannotRun(waitErr)})
		return
	}
	text := clipStderr(stderr.Bytes())
	code := cmd.ProcessState.ExitCode()
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		// シグナルで終わった子 (Popen の returncode は -番号)
		code = -int(ws.Signal())
	}
	if code != 0 {
		c.set(name, childResult{status: "error", code: int64(code), stderr: text, elapsed: elapsed(),
			err: fmt.Sprintf("終了コード%d", code)})
		return
	}
	c.set(name, childResult{status: "ran", code: int64(0), stdout: stdout.Bytes(), stderr: text, elapsed: elapsed()})
}

// clipStderr は decode("utf-8", "replace")[:_STDERR_LIMIT] (字で数える)。
func clipStderr(data []byte) string {
	runes := []rune(visibility.DecodeReplace(data))
	if len(runes) > stderrLimit {
		runes = runes[:stderrLimit]
	}
	return string(runes)
}

// childCommand は子 1 つの名前と argv。
type childCommand struct {
	name string
	argv []string
}

// run は commands の子を起こし、親の中の work (資料との照合) を動かしてから、全部の子を待つ。
// work が panic したら、動いている子を止めてから panic を流し直す (本家の _run_checks の except BaseException: children.stop(); raise)。
// 止めずに流すと、子は親を失ったまま締め切りまで動き続ける。
func (c *children) run(ctx context.Context, commands []childCommand, work func()) (map[string]childResult, error) {
	for _, cmd := range commands {
		c.start(cmd.name, cmd.argv)
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				c.stop()
				panic(r)
			}
		}()
		work()
	}()
	return c.wait(ctx)
}

// wait は全部の子を待つ。ctx が取り消されたら、動いている子を止めてから ctx の誤りを返す。
func (c *children) wait(ctx context.Context) (map[string]childResult, error) {
	finished := c.finished()
	select {
	case <-finished:
	case <-ctx.Done():
		c.stop()
		<-finished
		return nil, context.Cause(ctx)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.results, nil
}

// finished は全部の子の結果がそろうと閉じる。
func (c *children) finished() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	return done
}

// markStopped は、これから起動する子を起動させない (動いている子はそのまま)。
func (c *children) markStopped() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopped = true
}

// stop は動いている子を止める (これから起動する子も起動しない)。
func (c *children) stop() {
	c.mu.Lock()
	c.stopped = true
	processes := append([]*exec.Cmd(nil), c.processes...)
	c.mu.Unlock()
	// SIGTERM にしない: 本家と同じ kill (SIGKILL)。今の lint・diff は SIGTERM でも終わるのでテストでは見分けられないが、子がシグナルを
	// 受ける作りに変わっても (entry.Run の subcommands の stops) 止められるようにする。
	for _, p := range processes {
		_ = p.Process.Kill()
	}
	select {
	case <-c.finished():
	case <-time.After(stopWait * time.Duration(max(1, len(processes)))):
	}
}

// lockedBuffer は子の出力を受ける (exec の写しの goroutine が書き、時間切れのときは親が途中で読む)。
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

// reportKeys は _REPORT_KEYS (子の JSON で check が読むキーと型)。
var reportKeys = map[string][][2]string{
	"lint": {{"candidate_count", "int"}, {"counts", "dict"}, {"findings", "list"}, {"context", "dict"}},
	"diff": {{"status", "str"}, {"candidate_count", "int"}, {"meaning_risk_count", "int"},
		{"fail_on_meaning_risk_count", "int"}, {"meaning_risks", "list"}, {"full_diff", "dict"}, {"new_words", "list"},
		{"context", "dict"}},
}

// parseReport は _parse_report。子の標準出力 (lint か diff の --json) を読み、形式の名前とバージョン、check が読むキーと型を確かめる。
// (値, "") か (nil, 理由)。
func parseReport(stdout []byte, name string, version int) (*pyjson.Object, string) {
	expected := "yomiyasu-" + name
	value, err := pyjson.LoadsBytes(stdout)
	if err != nil {
		return nil, "子の標準出力を JSON として読めません"
	}
	o, ok := value.(*pyjson.Object)
	if !ok || !pyops.Equal(pyops.Get(o, "format"), expected) {
		return nil, "子の標準出力が " + expected + " の JSON ではありません"
	}
	if fv := pyops.Get(o, "format_version"); pyops.TypeName(fv) != "int" || !pyops.Equal(fv, int64(version)) {
		return nil, fmt.Sprintf("子の JSON の形式のバージョンが %d ではありません（yomiyasu_check.py と同じバージョンの yomiyasu_%s.py を置いてください）", version, name)
	}
	var wrong []string
	for _, kv := range reportKeys[name] {
		if pyops.TypeName(pyops.Get(o, kv[0])) != kv[1] {
			wrong = append(wrong, kv[0])
		}
	}
	if len(wrong) > 0 {
		return nil, "子の JSON に " + strings.Join(wrong, "、") + " がないか、型が違います"
	}
	return o, ""
}

// checkEntry は _check_entry (記録の checks の 1 つの初めの形)。
func checkEntry(name string, target *pyjson.Object) *pyjson.Object {
	o := pyjson.NewObject()
	for _, kv := range []struct {
		k string
		v pyjson.Value
	}{
		{"name", name}, {"status", "error"}, {"exit_code", nil}, {"duration_ms", int64(0)}, {"target", target},
		{"report", nil}, {"report_sha256", nil}, {"text", nil}, {"text_sha256", nil}, {"summary", nil}, {"stderr", ""},
		{"error", nil},
	} {
		o.Set(kv.k, kv.v)
	}
	return o
}
