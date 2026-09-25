package main

// pro-con e2e — Claude が e2e モードの画面を操作する口 (2026-09-25 にユーザーが依頼)。画面は隔離した tmux サーバ
// (`-L pro-con-e2e-<置き場のハッシュ>`、`-f /dev/null` でユーザーの設定を読まない) の中で動かし、キーを送って画面の文字を読む。
// PG は台本どおりに動く偽物 (dispatcher/e2e.go) なので claude は起動しない (利用枠を使わない)。
//
//	pro-con e2e start <置き場>             画面を起動する (dispatcher は画面が起こす)
//	pro-con e2e keys <置き場> <キー>...     tmux のキー名で送る (Enter / Escape / Q / r / Down 等)
//	pro-con e2e text <置き場> <文>          文をそのまま打つ
//	pro-con e2e screen <置き場>             画面の文字を出す
//	pro-con e2e wait <置き場> <文> [秒]     画面にその文が出るまで待つ (既定 30 秒。出なければ rc=1 で画面を出す)
//	pro-con e2e stop <置き場>               Q → quit で閉じる (dispatcher と偽の PG も止まる)。閉じなければ隔離サーバを止める
//	pro-con e2e scenario <置き場>           依頼 → 質問 → 回答 → テストの係 → レビュー → 終了を通しで確かめる

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const e2eUsage = `usage: pro-con e2e <start|keys|text|screen|wait|stop|scenario> <置き場> ...`

// e2eSocket は置き場ごとの隔離した tmux サーバの名前 (本番の tmux サーバに触らない)。
func e2eSocket(root string) string {
	return fmt.Sprintf("pro-con-e2e-%x", sha1.Sum([]byte(root)))[:len("pro-con-e2e-")+10]
}

// e2eTmux は隔離サーバへの tmux コマンド。TMUX / TMUX_PANE を落とす ($TMUX が生きていると -L より先に本番へ繋がる形を避ける)。
func e2eTmux(root string, args ...string) *exec.Cmd {
	cmd := exec.Command("tmux", append([]string{"-L", e2eSocket(root)}, args...)...)
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "TMUX=") && !strings.HasPrefix(e, "TMUX_PANE=") {
			env = append(env, e)
		}
	}
	cmd.Env = env
	return cmd
}

func e2eScreen(root string) (string, error) {
	out, err := e2eTmux(root, "capture-pane", "-p", "-t", "0").Output()
	return string(out), err
}

// e2eWait は画面に want が出るまで待つ (上限 timeout)。出なければ最後の画面つきのエラー。
func e2eWait(root, want string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		s, err := e2eScreen(root)
		if err == nil && strings.Contains(s, want) {
			return nil
		}
		last = s
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("%v 待っても %q が画面に出ない。最後の画面:\n%s", timeout, want, last)
}

func runE2E(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		_, _ = fmt.Fprintln(stderr, e2eUsage)
		return 2
	}
	op, root := args[0], args[1]
	root, err := filepath.Abs(root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con e2e:", err)
		return 2
	}
	rest := args[2:]
	fail := func(err error) int { _, _ = fmt.Fprintln(stderr, "pro-con e2e:", err); return 1 }
	switch op {
	case "start":
		if err := e2eStart(root); err != nil {
			return fail(err)
		}
	case "keys":
		if err := e2eTmux(root, append([]string{"send-keys", "-t", "0"}, rest...)...).Run(); err != nil {
			return fail(err)
		}
	case "text":
		if len(rest) != 1 {
			_, _ = fmt.Fprintln(stderr, e2eUsage)
			return 2
		}
		if err := e2eTmux(root, "send-keys", "-t", "0", "-l", rest[0]).Run(); err != nil {
			return fail(err)
		}
	case "screen":
		s, err := e2eScreen(root)
		if err != nil {
			return fail(err)
		}
		_, _ = fmt.Fprint(stdout, s)
	case "wait":
		if len(rest) < 1 {
			_, _ = fmt.Fprintln(stderr, e2eUsage)
			return 2
		}
		timeout := 30 * time.Second
		if len(rest) > 1 {
			n, err := strconv.Atoi(rest[1])
			if err != nil {
				_, _ = fmt.Fprintln(stderr, e2eUsage)
				return 2
			}
			timeout = time.Duration(n) * time.Second
		}
		if err := e2eWait(root, rest[0], timeout); err != nil {
			return fail(err)
		}
	case "stop":
		if err := e2eStop(root); err != nil {
			return fail(err)
		}
	case "scenario":
		if err := e2eScenario(root, stdout); err != nil {
			return fail(err)
		}
	default:
		_, _ = fmt.Fprintln(stderr, e2eUsage)
		return 2
	}
	return 0
}

func e2eStart(root string) error {
	if err := e2eTmux(root, "has-session").Run(); err == nil {
		return errors.New("この置き場の e2e の画面は既に動いている (pro-con e2e stop で閉じる)")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := e2eTmux(root, "-f", "/dev/null", "new-session", "-d", "-x", "200", "-y", "50", exe+" --e2e "+shellQuote(root)).Run(); err != nil {
		return fmt.Errorf("隔離した tmux で画面を起動できない: %w", err)
	}
	// socket の実際のパスを控える (tmux はサーバが終わっても socket を消さない。閉じた後に控えたパスだけを消す)
	if sock, err := e2eTmux(root, "display-message", "-p", "#{socket_path}").Output(); err == nil {
		_ = os.WriteFile(filepath.Join(root, "e2e-tmux-socket"), sock, 0o600)
	}
	return e2eWait(root, "producer-consumer", 20*time.Second)
}

// e2eStop は Q → quit で閉じ、画面が抜けるまで待つ (dispatcher と偽の PG を止め終えるまで)。抜けなければ隔離サーバを止める。
func e2eStop(root string) (err error) {
	// どの出口でも、最後に dispatcher と偽の PG を止め、控えた socket を消す (画面が先に落ちていた / 隔離サーバを止めた場合も。
	// dispatcher は画面と別のプロセスグループなので、画面や隔離サーバと一緒には終わらない)
	defer func() {
		if exe, xerr := os.Executable(); xerr == nil {
			if out, serr := stopCmd(exe, []string{"--e2e", root}).CombinedOutput(); serr != nil && err == nil {
				err = fmt.Errorf("dispatcher を止められない: %w: %s", serr, strings.TrimSpace(string(out)))
			}
		}
		removeE2ESocket(root)
	}()
	if err := e2eTmux(root, "has-session").Run(); err != nil {
		return nil // 画面は動いていない (dispatcher は上の defer が止める)
	}
	_ = e2eTmux(root, "send-keys", "-t", "0", "Q").Run()
	_ = e2eTmux(root, "send-keys", "-t", "0", "-l", "quit").Run()
	_ = e2eTmux(root, "send-keys", "-t", "0", "Enter").Run()
	for range 300 { // 60 秒
		if err := e2eTmux(root, "has-session").Run(); err != nil {
			return nil // 画面が抜けて、session と一緒に隔離サーバも終わった (socket は defer が消す)
		}
		time.Sleep(200 * time.Millisecond)
	}
	// 撃つのは -L <この置き場の隔離名> のサーバだけ (e2eTmux が必ず -L を付け、TMUX を落としている)
	_ = e2eTmux(root, "kill-server").Run()
	return errors.New("60 秒待っても画面が閉じないので、隔離サーバを止めた")
}

// e2eScenario は台本どおりの通し: 依頼 → (偽の PG が質問) → 回答 → (テストの係が echo e2e-ok を実行) → レビュー → 終了。
func e2eScenario(root string, stdout io.Writer) error {
	step := func(s string) { _, _ = fmt.Fprintln(stdout, time.Now().Format("15:04:05"), s) }
	err := e2eStart(root)
	defer func() { _ = e2eStop(root) }() // 起動の待ちが失敗しても後始末する (new-session には成功しているかもしれない)
	if err != nil {
		return err
	}
	send := func(keys ...string) error {
		return e2eTmux(root, append([]string{"send-keys", "-t", "0"}, keys...)...).Run()
	}
	text := func(s string) error { return e2eTmux(root, "send-keys", "-t", "0", "-l", s).Run() }
	step("依頼を出す")
	if err := errors.Join(send("n"), text("e2e の通しの確認"), send("Enter")); err != nil {
		return err
	}
	step("PG の質問を待つ")
	if err := e2eWait(root, "質問待ち (1)", 30*time.Second); err != nil {
		return err
	}
	step("回答する (選択はカードについて質問待ちの列へ移っている)")
	if err := errors.Join(send("r"), text("続けてください"), send("Enter")); err != nil {
		return err
	}
	step("テストの係の実行とレビューを待つ")
	if err := e2eWait(root, "レビュー (1)", 60*time.Second); err != nil {
		return err
	}
	step("閉じる (Q → quit)")
	if err := e2eStop(root); err != nil {
		return err
	}
	step("通った")
	return nil
}

// removeE2ESocket は起動のときに控えた socket を消す (控えたパスが、この置き場の隔離サーバの名前で終わるときだけ)。
func removeE2ESocket(root string) {
	rec := filepath.Join(root, "e2e-tmux-socket")
	data, err := os.ReadFile(rec)
	if err != nil {
		return
	}
	if p := strings.TrimSpace(string(data)); strings.HasSuffix(p, "/"+e2eSocket(root)) {
		_ = os.Remove(p)
	}
	_ = os.Remove(rec)
}

// shellQuote は s を POSIX シェルの 1 語にする。引用の要らない語はそのまま返す (記録・画面に `make test` と出す)。
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellJoin は argv を、シェルが eval すると同じ argv に戻る 1 行にする (空白で繋ぐだけだと引用が外れて別のコマンドになる = 463)。
func shellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shellQuote(a)
	}
	return strings.Join(q, " ")
}
