package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	image = "voicevox/voicevox_engine:cpu-latest"
	// ユーザーが自分で立てた同名のコンテナを up / down で巻き込まないよう、この skill 専用の名前にする
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
	ctx, cancel := context.WithTimeout(appCtx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(engine, "/")+"/version", nil)
	if err != nil {
		return ""
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode/100 != 2 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(string(b)), `"`)
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
	ctx, cancel := context.WithTimeout(appCtx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// timeout で kill した後、孫プロセス (Chrome の子など) が出力のパイプを持ったままでも待ち続けない
	cmd.WaitDelay = 5 * time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if appCtx.Err() != nil {
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
	if rt == "container" {
		rc, out := runQuiet(60*time.Second, "container", "system", "status")
		if rc != 0 {
			return false, "container system start   (初回はカーネルを入れるか聞かれる。非対話なら --enable-kernel-install を付ける)"
		}
		first, _, _ := strings.Cut(out, "\n")
		return true, first
	}
	rc, _ := runQuiet(60*time.Second, "docker", "info")
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
	if rt := detectRuntime(); rt != "" {
		running, hint := runtimeService(rt)
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
	} else {
		row(false, "VOICEVOX エンジン", env.Engine+" で応答なし → up で起動する")
	}
	ok = ok && ver != ""
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

func cmdUp(env *Env) error {
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
		return fail("%s のサービスが動いていない → %s", rt, hint)
	}
	args := []string{"run", "--rm", "-d", "-p", "127.0.0.1:" + port + ":50021", "--name", containerName, image}
	fmt.Fprintf(env.Stderr, "up: %s %s   (初回はイメージの取得に数分かかる)\n", rt, strings.Join(args, " "))
	cmd := exec.CommandContext(appCtx, rt, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr // 取得の進捗を見せるため出力は端末へ流す
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		if e := interruptedErr(); e != nil {
			return e
		}
		return fail("%s run が失敗 (%v)。同名のコンテナが残っているなら down してから再実行する", rt, err)
	}
	for range 90 { // 起動直後は十数秒応答しない
		if ver := engineVersion(env.Engine); ver != "" {
			fmt.Fprintf(env.Stderr, "up: 起動した (%s, 版 %s)。終わったら down で止める\n", rt, ver)
			return nil
		}
		sleepCtx(2 * time.Second)
		if e := interruptedErr(); e != nil {
			return e
		}
	}
	return fail("180 秒待っても %s が応答しない。%s logs %s で原因を見て、やり直す前に down で止める (コンテナは起動したまま残っている)",
		env.Engine, rt, containerName)
}

var notFoundRe = regexp.MustCompile(`(?i)not ?found|no such container`)

func cmdDown(env *Env) error {
	// up 以降に container を入れた・消した場合でも取り残さないよう、優先順位ではなく両方のランタイムを見る
	var rts []string
	for _, rt := range []string{"container", "docker"} {
		if _, err := exec.LookPath(rt); err == nil {
			rts = append(rts, rt)
		}
	}
	if len(rts) == 0 {
		return fail("container も docker も無い (デスクトップアプリならアプリを終了する)")
	}
	var stopped []string
	for _, rt := range rts {
		if running, _ := runtimeService(rt); !running {
			continue // サービスが止まっていれば、そのランタイムのコンテナも動いていない
		}
		rc, out := runQuiet(120*time.Second, rt, "stop", containerName)
		if rc == 0 {
			stopped = append(stopped, rt)
		} else if !notFoundRe.MatchString(out) {
			return fail("%s stop %s が失敗 (rc=%d): %s", rt, containerName, rc, firstRunes(out, 300))
		}
	}
	if len(stopped) > 0 {
		fmt.Fprintf(env.Stderr, "down: %s を止めた (%s)\n", containerName, strings.Join(stopped, ", "))
	} else {
		fmt.Fprintf(env.Stderr, "down: %s は動いていない\n", containerName)
	}
	if ver := engineVersion(env.Engine); ver != "" {
		fmt.Fprintf(env.Stderr, "down: %s はまだ応答している (版 %s)。この skill 以外 (デスクトップアプリ等) のエンジン\n", env.Engine, ver)
	}
	return nil
}
