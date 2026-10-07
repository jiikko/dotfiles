package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	image = "voicevox/voicevox_engine:cpu-latest"
	// ユーザーが自分で立てた同名のコンテナを up / down で巻き込まないよう、この skill 専用の名前にする。
	// 実際の名前はポートを後ろに付けたもの (containerNameFor)。見張りが印のポートのコンテナだけを止められるように
	// (名前が 1 つだと、別のポートに up したコンテナまで名前で止めてしまう。敵対的レビュー)
	containerName = "zundamon-kaisetsu-voicevox"
	startHint     = "  起動: zundamon-kaisetsu up   (container か docker でエンジンを立てる)"
)

// engineRequest は VOICEVOX エンジンへの 1 回の要求。body が nil なら GET、それ以外は JSON の POST。
func engineRequest(engine, path string, params url.Values, body []byte) ([]byte, error) {
	u := strings.TrimRight(engine, "/") + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	method := http.MethodGet
	var rd io.Reader
	if body != nil {
		method = http.MethodPost
		rd = bytes.NewReader(body)
	}
	ctx, cancel := context.WithTimeout(appCtx, 120*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, fail("VOICEVOX エンジン (%s) と通信できない (%s): %v\n%s", engine, path, err, startHint)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if h := engineUseHook.Load(); h != nil {
		(*h)() // 最後に使った時刻 (見張りはこれで止める時期を決める)
	}
	res, err := http.DefaultClient.Do(req)
	if e := interruptedErr(); e != nil {
		return nil, e // 中断を「エンジンと通信できない」と言わない
	}
	if err != nil {
		// 接続拒否・起動途中の切断・timeout
		return nil, fail("VOICEVOX エンジン (%s) と通信できない (%s): %v\n%s", engine, path, err, startHint)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if e := interruptedErr(); e != nil {
		return nil, e // 本文を読む途中の中断も「エンジンと通信できない」と言わない
	}
	if err != nil {
		return nil, fail("VOICEVOX エンジン (%s) と通信できない (%s): %v\n%s", engine, path, err, startHint)
	}
	if res.StatusCode/100 != 2 {
		return nil, fail("%s が HTTP %d を返した: %s", path, res.StatusCode, firstRunes(string(b), 300))
	}
	return b, nil
}

func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// engineVersion は応答すれば版を、しなければ "" を返す。
func engineVersion(engine string) string {
	ver, _ := probeEngine(engine)
	return ver
}

// probeEngine は engineVersion に加えて、接続を拒否されたか (そのポートで誰も待ち受けていない) を返す。
// 自動起動はこれが真のときだけ行う。待ち受けているのに答えない相手は、別のサーバか起動途中のエンジンなので触らない。
func probeEngine(engine string) (ver string, refused bool) {
	ctx, cancel := context.WithTimeout(appCtx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(engine, "/")+"/version", nil)
	if err != nil {
		return "", false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Is(err, syscall.ECONNREFUSED)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode/100 != 2 {
		return "", false
	}
	return strings.Trim(strings.TrimSpace(string(b)), `"`), false
}

// --- runtime (container / docker) ---

// detectRuntime は Apple の container CLI を優先し、無ければ docker。どちらも無ければ "" (デスクトップアプリで代用する)。
func detectRuntime() string {
	for _, name := range []string{"container", "docker"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return ""
}

// runQuiet はコマンドを stdin なしで走らせ、rc と stdout+stderr を返す (timeout は rc=124、起動できなければ 127)。
func runQuiet(timeout time.Duration, name string, args ...string) (int, string) {
	return runQuietCtx(appCtx, timeout, name, args...)
}

// runQuietCtx は runQuiet の親 context を選べる版。中断の後の後始末 (エンジンの停止) は appCtx 以外から呼ぶ。
func runQuietCtx(parent context.Context, timeout time.Duration, name string, args ...string) (int, string) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// timeout で kill した後、孫プロセス (Chrome の子など) が出力のパイプを持ったままでも待ち続けない
	cmd.WaitDelay = 5 * time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if errors.Is(parent.Err(), context.Canceled) {
		return 130, "中断した"
	}
	if ctx.Err() != nil {
		return 124, fmt.Sprintf("%.0f 秒で応答しない", timeout.Seconds())
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, strings.TrimSpace(out.String())
	case errors.As(err, &ee):
		return ee.ExitCode(), strings.TrimSpace(out.String())
	default:
		return 127, err.Error()
	}
}

// runtimeService はランタイムのサービスが動いているか。動いていなければ起こし方を返す。
func runtimeService(rt string) (bool, string) {
	return runtimeServiceCtx(appCtx, rt)
}

func runtimeServiceCtx(ctx context.Context, rt string) (bool, string) {
	if rt == "container" {
		rc, out := runQuietCtx(ctx, 60*time.Second, "container", "system", "status")
		if rc != 0 {
			return false, "container system start   (初回はカーネルを入れるか聞かれる。非対話なら --enable-kernel-install を付ける)"
		}
		first, _, _ := strings.Cut(out, "\n")
		return true, first
	}
	rc, _ := runQuietCtx(ctx, 60*time.Second, "docker", "info")
	if rc != 0 {
		return false, "Docker Desktop (か colima 等) を起動する"
	}
	return true, ""
}

func localPort(engine string) (string, error) {
	u, err := url.Parse(engine)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		return "", fail("up / down は手元のエンジンにだけ使える (%s は別ホスト)", engine)
	}
	if p := u.Port(); p != "" {
		return p, nil
	}
	return "80", nil
}

func cmdCheck(env *Env) error {
	ok := true
	row := func(good bool, label, detail string) {
		mark := "NG"
		if good {
			mark = "OK"
		}
		fmt.Fprintf(env.Stdout, "%s  %s: %s\n", mark, label, detail)
	}
	enc := ""
	for _, n := range []string{"ffmpeg", "afconvert"} {
		if _, err := exec.LookPath(n); err == nil {
			enc = n
			break
		}
	}
	row(enc != "", "音声の圧縮", orStr(enc, "ffmpeg も afconvert も無い (brew install ffmpeg)"))
	ok = ok && enc != ""
	uv, _ := exec.LookPath("uv")
	row(uv != "", "立ち絵の書き出し: uv", orStr(uv, "無い (psd_faces.py で PSD から表情を書き出すときだけ要る。brew install uv)"))
	chrome := findChrome()
	row(chrome != "", "mp4 用: Chrome", orStr(chrome, "無い (mp4 を作るときだけ要る。環境変数 CHROME で指定できる)"))
	h264 := h264Encoder() != nil
	row(h264, "mp4 用: H.264", map[bool]string{true: "ffmpeg で書ける", false: "ffmpeg が無いか H.264 を書けない (mp4 を作るときだけ要る)"}[h264])

	ver := engineVersion(env.Engine)
	autoStart := false // 止まっていても、synth / kana / speakers が使うときに起動できる
	if rt := detectRuntime(); rt != "" {
		running, hint := runtimeService(rt)
		_, perr := localPort(env.Engine)
		autoStart = running && perr == nil
		note := " (container が無いので docker)"
		if rt == "container" {
			note = " (container を優先)"
		}
		row(true, "コンテナ", rt+note)
		state := "起動中"
		if !running {
			state = "停止中 → " + hint
		}
		row(running || ver != "", rt+" のサービス", state)
	} else {
		row(ver != "", "コンテナ", "container も docker も無い → VOICEVOX のデスクトップアプリを起動すれば同じエンジンが使える")
	}
	if ver != "" {
		row(true, "VOICEVOX エンジン", fmt.Sprintf("%s で応答 (版 %s)", env.Engine, ver))
	} else if autoStart {
		row(true, "VOICEVOX エンジン", env.Engine+" は停止中 (synth / kana / speakers が使うときに起動し、終わったら止める)")
	} else {
		row(false, "VOICEVOX エンジン", env.Engine+" で応答なし → 上の行を直すか、デスクトップアプリを起動する")
	}
	ok = ok && (ver != "" || autoStart)
	if !ok {
		return &errExit{} // 表に理由を出したので、追加の文言なしで rc=1
	}
	return nil
}

func orStr(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

// cmdUp は明示的な起動。自動起動の印を消すので、synth などが終わっても止めない (止めるのは down)。
func cmdUp(env *Env) error {
	return withStartLock(env, func() error {
		// 印は起動より先に消す。up が時間切れや中断で失敗しても、残った印で見張りが up のコンテナを止めないように
		clearAutoMarkerForUp(env)
		return startEngine(env)
	})
}

// startEngine は手元のエンジンを container (無ければ docker) で起動し、応答するまで待つ。既に応答していれば何もしない。
// 出力はすべて stderr へ出す (kana の stdout は読みの一覧なので、起動の進捗を混ぜない)。
func startEngine(env *Env) error {
	port, err := localPort(env.Engine)
	if err != nil {
		return err
	}
	if ver := engineVersion(env.Engine); ver != "" {
		fmt.Fprintf(env.Stderr, "up: %s は既に応答している (版 %s)。起動はしない\n", env.Engine, ver)
		return nil
	}
	rt := detectRuntime()
	if rt == "" {
		return fail("container も docker も無い。VOICEVOX のデスクトップアプリを起動するか、どちらかを入れる")
	}
	if running, hint := runtimeService(rt); !running {
		if e := interruptedErr(); e != nil {
			return e // 中断で確認が失敗したのを「サービスが動いていない」と言わない (issue 652)
		}
		return fail("%s のサービスが動いていない → %s", rt, hint)
	}
	// イメージは手元に無いときだけ取得する (image pull は手元にあってもレジストリに問い合わせるので、オフラインや取得の回数制限で
	// 起動できなくなり、上流のタグが更新されるたびに普段の synth で約 3.7GB を取り直すことになる)
	if rc, _ := runQuiet(60*time.Second, rt, "image", "inspect", image); rc != 0 {
		if e := interruptedErr(); e != nil {
			return e
		}
		fmt.Fprintf(env.Stderr, "up: %s image pull %s   (初回は約 3.7GB の取得に数分かかる)\n", rt, image)
		pull := exec.CommandContext(appCtx, rt, "image", "pull", image)
		pull.Stdout, pull.Stderr = env.Stderr, env.Stderr
		pull.WaitDelay = 5 * time.Second
		if err := pull.Run(); err != nil {
			if e := interruptedErr(); e != nil {
				return e
			}
			return fail("%s image pull %s が失敗 (%v)。回線と %s のサービスを確かめて再実行する", rt, image, err, rt)
		}
	}
	name := containerNameFor(env.Engine)
	args := []string{"run", "--rm", "-d", "-p", "127.0.0.1:" + port + ":50021", "--name", name, image}
	fmt.Fprintf(env.Stderr, "up: %s %s\n", rt, strings.Join(args, " "))
	ctx, cancel := context.WithTimeout(appCtx, startRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, rt, args...)
	cmd.Stdout, cmd.Stderr = env.Stderr, env.Stderr // 取得の進捗を見せるため出力は端末 (stderr) へ流す
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		if e := interruptedErr(); e != nil {
			return e
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fail("%s run が %s たっても終わらない (取得は済んでいるので、ランタイムが止まっている)。%s のサービスを確かめて、down してから再実行する",
				rt, startRunTimeout, rt)
		}
		return fail("%s run が失敗 (%v)。同名のコンテナが残っているなら down してから再実行する", rt, err)
	}
	for range 90 { // 起動直後は十数秒応答しない
		if ver := engineVersion(env.Engine); ver != "" {
			fmt.Fprintf(env.Stderr, "up: 起動した (%s, 版 %s)\n", rt, ver)
			return nil
		}
		sleepCtx(2 * time.Second)
		if e := interruptedErr(); e != nil {
			return e
		}
	}
	return fail("180 秒待っても %s が応答しない。%s logs %s で原因を見て、やり直す前に down で止める (コンテナは起動したまま残っている)",
		env.Engine, rt, name)
}

// containerNameFor は手元のエンジンの URL に対応するコンテナの名前。
func containerNameFor(engine string) string {
	port, err := localPort(engine)
	if err != nil {
		return containerName
	}
	return containerName + "-" + port
}

// startRunTimeout は container run の上限。起動用のロックの中で走るので、返らないと synth / down / 見張りが全部待ち続ける。
// イメージの取得は (手元に無ければ) run の前に image pull で済ませ、こちらには含めない (約 3.7GB の取得は回線しだいで何十分もかかり、上限で
// 打ち切ると毎回同じ所で切られて二度と起動できなくなる)。取得は上限なしで、進捗を出し、Ctrl-C で止められる。
// テストで縮めるので変数にしている
var startRunTimeout = 5 * time.Minute

// notFoundRe は「そのコンテナは無い」を停止の成功とみなす文言。2026-10-07 に本物で測った stderr (どちらも rc=1):
//   - Apple container 1.5.0: Error: internalError: "failed to stop container" (cause: "notFound: "container with ID <名前> not found"")
//   - docker 29.8.0: Error response from daemon: No such container: <名前>
//
// docker のデーモンが動いていないときの「接続できない」は合わない (失敗として扱う)。版を上げて文言が変わったら測り直す (issue 646 の 6)
var notFoundRe = regexp.MustCompile(`(?i)not ?found|no such container`)

func cmdDown(env *Env) error {
	var stopped []string
	err := withStartLock(env, func() error {
		var err error
		// 旧版が起こした固定名のコンテナも止める (名前にポートを付ける前の版。残っていると同じポートで起動できない)。
		// 印が別のポートの自動エンジンを指していれば、それも止める (止めずに印だけ消すと、見張りが終わってコンテナが残り続ける)
		names := []string{containerNameFor(env.Engine), containerName}
		if _, url, ok := readAutoMarker(env); ok && url != "" && !samePort(url, env.Engine) {
			names = append(names, containerNameFor(url))
		}
		stopped, err = stopContainers(appCtx, "", names...)
		if err == nil {
			// 印が指すコンテナも止めたので、印が守るものは残っていない。消さないと、後で同じポートに応答したものを見て、
			// 見張りが別に up したコンテナを止めうる
			removeAutoMarker(env)
		}
		return err
	})
	if err != nil {
		return err
	}
	if len(stopped) > 0 {
		fmt.Fprintf(env.Stderr, "down: %s を止めた (%s)\n", containerNameFor(env.Engine), strings.Join(stopped, ", "))
	} else {
		fmt.Fprintf(env.Stderr, "down: %s は動いていない\n", containerNameFor(env.Engine))
	}
	if ver := engineVersion(env.Engine); ver != "" {
		fmt.Fprintf(env.Stderr, "down: %s はまだ応答している (版 %s)。この skill 以外 (デスクトップアプリ等) のエンジン\n", env.Engine, ver)
	}
	return nil
}

// ctxInterrupted は ctx が取り消された (中断された) なら errInterrupted を返す。🚨 全体の appCtx ではなく渡された ctx で見る:
// 見張りは appCtx と別の ctx (cleanupContext) で stopContainers を呼ぶので、appCtx で判定すると、見張りが SIGTERM を受けている
// 間の成功 (not found) や本物の失敗まで「中断した」になる (issue 652 の敵対的レビュー)。時間切れ (DeadlineExceeded) は中断ではない
func ctxInterrupted(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return errInterrupted
	}
	return nil
}

// stopContainers はこの skill 専用の名前のコンテナだけを止め、止めたランタイムの名前を返す。
// only を指定すると、そのランタイムだけを止める (自動で起動したものを止めるとき。別のランタイムで up したエンジンに触らない)。
// only の指定があるときは、サービスの応答が無いのを「動いていない」と読まず失敗にする (止められていないのに止めたことにしない)。
// names は止めるコンテナの名前 (ポートごとの名前。down は旧版の固定名も渡す)。
func stopContainers(ctx context.Context, only string, names ...string) ([]string, error) {
	// up 以降に container を入れた・消した場合でも取り残さないよう、優先順位ではなく両方のランタイムを見る
	var rts []string
	for _, rt := range []string{"container", "docker"} {
		if only != "" && rt != only {
			continue
		}
		if _, err := exec.LookPath(rt); err == nil {
			rts = append(rts, rt)
		}
	}
	if len(rts) == 0 {
		return nil, fail("container も docker も無い (デスクトップアプリならアプリを終了する)")
	}
	var stopped []string
	for _, rt := range rts {
		if running, _ := runtimeServiceCtx(ctx, rt); !running {
			if e := ctxInterrupted(ctx); e != nil {
				return stopped, e // 中断で確認が 130 で返ったのを「動いていない」と読まない (読むと down が rc=0 で印を消す)
			}
			if only != "" {
				return stopped, fail("%s のサービスが応答しない (コンテナを止められたか分からない)", rt)
			}
			continue // サービスが止まっていれば、そのランタイムのコンテナも動いていない
		}
		for _, name := range names {
			rc, out := runQuietCtx(ctx, 120*time.Second, rt, "stop", name)
			if rc == 0 {
				stopped = append(stopped, rt+" "+name)
			} else if e := ctxInterrupted(ctx); e != nil {
				return stopped, e // 中断で stop が 130 で返ったのを「失敗」と言わない (issue 652)
			} else if !notFoundRe.MatchString(out) {
				return stopped, fail("%s stop %s が失敗 (rc=%d): %s", rt, name, rc, firstRunes(out, 300))
			}
		}
	}
	return stopped, nil
}
