package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// --- エンジンの自動起動と、使われなくなったら止める見張り ---
//
// synth / kana / speakers は、手元のエンジンのポートで誰も待ち受けていなければ自分で起動し、見張り (reaper) を 1 つ起こす。
// 見張りは、最後にエンジンを使ってから engineIdleStop たったら、自動で起動したコンテナを止めて終わる。
//
// 各コマンドは止めない。止める責任を、コマンドとは別の 1 つのプロセスに置くことで、コマンドが正常に終わらない経路
// (kill -9・出力先のパイプが閉じた SIGPIPE・2 回目の Ctrl-C) でもエンジンが残らない
// (コマンドごとに止める方式は、それらの経路と並行の調停で敵対的レビューに 2 本の P1 を出され、作り直した)。
//
// 状態は StateDir に置く:
//   - engine.auto: 自動で起動した印。1 行目は起動に使ったランタイム (container / docker)、2 行目はエンジンの URL。
//     コンテナ名は 1 つなので、自動で起動したエンジンも同時に 1 つだけ。見張りは印に書かれたエンジンを見て、印のランタイムの
//     コンテナだけを止める。up / down を明示的に打つと消え、見張りは何もせずに終わる (up で起動したエンジンは自動では止めない)
//   - engine.last: 最後にエンジンを使った時刻 (mtime)。エンジンへの要求のたびに更新するので、長い合成の途中では止まらない
//   - engine.start.lock: コマンドの「確かめる・起動する・使った時刻を更新する」、見張りの「時刻を見直して止める」、up / down を
//     1 つずつにする。見張りが止めると決めてから止めるまでの間に来たコマンドが、止まるエンジンを使い始めないように
//   - reaper.lock: 見張りが生きている間ずっと持つロック。取れれば見張りはいない (pid の再利用を気にしなくてよい)
//
// 印は消して取り消さない (起動に失敗しても、時間切れでも残す)。「何も起動していない」は起動側からは確かめられない
// (時間切れの後もコンテナは起動したまま残りうる) ので、印を消すかは見張りが、エンジンが応答しないまま startGrace たったかで決める。

const (
	engineIdleStop = 10 * time.Minute
	// startGrace は印を書いてから、応答しなくても「起動途中」とみなす間。初回のイメージの取得 (約 3.7GB) が収まる長さにする。
	// 長くても、起動しなかったときに見張りが寝て待つだけで負担はほぼ無い
	startGrace      = 30 * time.Minute
	reaperInterval  = 30 * time.Second
	reaperMaxFailed = 20 // 止めるのに続けて失敗したら見張りを終える回数 (印は残し、次にエンジンを使うコマンドが見張りを起こし直す)
	reapCmd         = "__reap"
)

func autoMarkerPath(env *Env) string { return filepath.Join(env.StateDir, "engine.auto") }
func lastUsePath(env *Env) string    { return filepath.Join(env.StateDir, "engine.last") }

// engineUseHook はエンジンへの要求のたびに呼ばれる (最後に使った時刻を更新する)。withEngine の間だけ設定される。
var engineUseHook atomic.Pointer[func()]

// withEngine は、エンジンを使うコマンドの本体 fn を、エンジンの自動起動で包む。
// 自動で管理しない (別ホストのエンジン、起動の手段が注入されていない) なら fn をそのまま呼ぶ。
func withEngine(env *Env, fn func() error) error {
	if env.EngineUp == nil || env.SpawnReaper == nil || env.StateDir == "" {
		return fn()
	}
	if _, err := localPort(env.Engine); err != nil {
		return fn() // 別ホストのエンジンは起動できない
	}
	if err := withStartLock(env, func() error { return ensureEngine(env) }); err != nil {
		return err
	}
	if usesAutoEngine(env) { // 別のポートのエンジン (デスクトップアプリ等) を使っても、自動のエンジンの停止を延ばさない
		hook := func() { touchLastUse(env) }
		engineUseHook.Store(&hook)
		defer engineUseHook.Store(nil)
	}
	return fn()
}

// withStartLock は engine.start.lock を持って fn を呼ぶ。
func withStartLock(env *Env, fn func() error) error {
	if env.StateDir == "" {
		return fn()
	}
	if err := os.MkdirAll(env.StateDir, 0o755); err != nil {
		return fail("%s を作れない (%v)", env.StateDir, err)
	}
	lockPath := filepath.Join(env.StateDir, "engine.start.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fail("%s を開けない (%v)", lockPath, err)
	}
	defer func() { _ = f.Close() }() // ロックは close (とプロセスの終了) で外れる
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		// 初回のイメージの取得は数分かかるので、黙って待たない。持っているプロセスを出す (詰まったら何を止めればよいか分かるように)
		holder, _ := os.ReadFile(lockPath)
		fmt.Fprintf(env.Stderr, "engine: 他のコマンドがエンジンを起動・停止している。終わるのを待つ (持っているのは %s)\n",
			orStr(strings.TrimSpace(string(holder)), "不明 ("+lockPath+")"))
		if err := flockWait(int(f.Fd()), syscall.LOCK_EX); err != nil {
			return err
		}
	}
	// 持ち主を書く (表示用。ロックの判定には使わない)
	if err := f.Truncate(0); err == nil {
		_, _ = f.WriteAt([]byte(fmt.Sprintf("pid %d (%s)\n", os.Getpid(), strings.Join(os.Args, " "))), 0)
	}
	return fn()
}

// ensureEngine は (起動用のロックの中で) 誰も待ち受けていなければエンジンを起動し、使った時刻を更新し、
// 自動で起動したエンジンに見張りがいることを保証する。
func ensureEngine(env *Env) error {
	ver, refused := probeEngine(env.Engine)
	if ver == "" && refused {
		return startAutoEngine(env) // 見張りは起動より先に起こしている
	}
	if usesAutoEngine(env) {
		touchLastUse(env)
	}
	ensureReaper(env) // 見張りが死んでいても (kill・再起動)、次にエンジンを使うときに起こし直す
	return nil
}

func startAutoEngine(env *Env) error {
	if e := interruptedErr(); e != nil {
		return e // 中断の後の「応答しない」は判定に使わない
	}
	if _, url, ok := readAutoMarker(env); ok && url != "" && !samePort(url, env.Engine) {
		if v, r := probeEngine(url); v != "" || !r {
			return fail("別のポート (%s) で自動起動したエンジンが動いている。コンテナは同時に 1 つしか動かせないので、"+
				"そのポートを使うか、zundamon-kaisetsu down で止めてから使う", url)
		}
	}
	rt := detectRuntime()
	if rt == "" {
		return env.EngineUp() // 起動の手段が無い (理由は EngineUp が返す)。何も起動しないので印も書かない
	}
	marker := autoMarkerPath(env)
	// 印と見張りは起動より先に置く。起動の途中で死んでも (kill -9・中断)、見張りが止めるか、起動していなければ印を消す
	if err := os.WriteFile(marker, []byte(rt+"\n"+env.Engine+"\n"), 0o644); err != nil {
		return fail("%s を書けない (%v)", marker, err)
	}
	touchLastUse(env)
	ensureReaper(env)
	fmt.Fprintf(env.Stderr, "engine: %s が応答しないので起動する (最後に使ってから %.0f 分で自動で止まる)\n",
		env.Engine, engineIdleStop.Minutes())
	if err := env.EngineUp(); err != nil {
		return err
	}
	// 起動 (初回の取得を含む) に 10 分以上かかっても、起動した直後に見張りが止めないように、使った時刻を起動の後で取り直す
	touchLastUse(env)
	return nil
}

// readAutoMarker は印を読む。url が空の印 (書きかけ・壊れた印) は持ち主が分からないので、起動側は上書きし、見張りは止めずに捨てる。
func readAutoMarker(env *Env) (rt, url string, ok bool) {
	b, err := os.ReadFile(autoMarkerPath(env))
	if err != nil {
		return "", "", false
	}
	lines := strings.Split(string(b), "\n") // 行ごとに分けてから空白を落とす (全体を先に落とすと空の 1 行目が消えて行がずれる)
	rt = strings.TrimSpace(lines[0])
	if len(lines) > 1 {
		url = strings.TrimSpace(lines[1])
	}
	return rt, url, true
}

// usesAutoEngine はこのコマンドが使うエンジンが、自動で起動したものか (印が無ければ、これから起動しうるので真)。
func usesAutoEngine(env *Env) bool {
	_, url, ok := readAutoMarker(env)
	return !ok || url == "" || samePort(url, env.Engine)
}

// samePort は 2 つの手元のエンジンの URL が同じポートを指すか。コンテナの同一性はポートで決まる (名前は 1 つ・待ち受けは
// 127.0.0.1)。URL の文字列で比べると、localhost と 127.0.0.1・末尾の / の違いで同じエンジンを別物と読む。
// ポートは文字列のまま比べる (先頭に 0 を付けた "050021" は別物と読む)。手で打たない限り出ない書き方なので正規化していない
// (敵対的レビュー 5 周目で記録のみ)。そう書く使い方が出てきたら、数に直して比べる。
func samePort(a, b string) bool {
	pa, ea := localPort(a)
	pb, eb := localPort(b)
	if ea != nil || eb != nil {
		return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
	}
	return pa == pb
}

func ensureReaper(env *Env) {
	if !isFile(autoMarkerPath(env)) || reaperAlive(env) {
		return
	}
	if err := env.SpawnReaper(); err != nil {
		fmt.Fprintf(env.Stderr, "engine: 見張りを起こせなかった (%v)。使い終わったら zundamon-kaisetsu down で止める\n", err)
	}
}

func touchLastUse(env *Env) {
	p := lastUsePath(env)
	now := time.Now()
	if err := os.Chtimes(p, now, now); err != nil {
		_ = os.WriteFile(p, nil, 0o644)
	}
}

// reaperAlive は見張りのロックが他のプロセスに持たれているか。
func reaperAlive(env *Env) bool {
	f, err := os.OpenFile(filepath.Join(env.StateDir, "reaper.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// cmdReap は見張りの本体。見張りのロックを持ったまま、止めるか止める必要が無くなるまで回る。
func cmdReap(env *Env) error {
	f, err := os.OpenFile(filepath.Join(env.StateDir, "reaper.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return nil // 別の見張りが既にいる
	}
	if err := f.Truncate(0); err == nil { // どのプロセスが見張りかを残す (表示用。生きているかの判定はロックで行う)
		_, _ = f.WriteAt([]byte(fmt.Sprintf("%d\n", os.Getpid())), 0)
	}
	r := &reaper{}
	for {
		done := false
		if err := withStartLock(env, func() error { done = r.once(env); return nil }); err != nil {
			return err
		}
		if done {
			return nil
		}
		env.Sleep(reaperInterval)
		if e := interruptedErr(); e != nil {
			return e
		}
	}
}

type reaper struct{ failed int }

// once は (起動用のロックの中で) 1 回見て、見張りを終えてよければ true を返す。
func (r *reaper) once(env *Env) bool {
	rt, url, ok := readAutoMarker(env)
	if !ok {
		return true // 印が無い: up / down を明示的に打った (か、止め終わった)
	}
	if (rt != "container" && rt != "docker") || url == "" {
		// どのランタイムで・どのポートに起動したか分からない印は止めない (空のランタイムで止めると全ランタイムの同名コンテナに届く)
		_ = os.Remove(autoMarkerPath(env))
		fmt.Fprintf(env.Stderr, "reaper: 印 (%q, %q) の持ち主が分からないので止めずに終える\n", rt, url)
		return true
	}
	if ver, refused := probeEngine(url); ver == "" && refused {
		if st, err := os.Stat(autoMarkerPath(env)); err == nil && env.Now().Sub(st.ModTime()) < startGrace {
			return false // 起動途中かもしれない
		}
		_ = os.Remove(autoMarkerPath(env)) // 誰も待ち受けていない: 起動しなかったか、既に止まっている
		return true
	}
	last := lastUseTime(env)
	if env.Now().Sub(last) < engineIdleStop {
		r.failed = 0 // 使われている間の失敗は数えない (続けて失敗した回数だけを数える)
		return false
	}
	if err := env.EngineDown(rt, url); err != nil {
		r.failed++
		if r.failed >= reaperMaxFailed {
			fmt.Fprintf(env.Stderr, "reaper: %d 回続けて止められなかった (%v)。見張りを終える (zundamon-kaisetsu down で止める)\n", r.failed, err)
			return true
		}
		fmt.Fprintf(env.Stderr, "reaper: 止められなかった (%v)。%s 後にもう一度試す\n", err, reaperInterval)
		return false
	}
	_ = os.Remove(autoMarkerPath(env))
	fmt.Fprintf(env.Stderr, "reaper: %s 使われなかったので %s を止めた (%s)\n", env.Now().Sub(last).Round(time.Second), containerNameFor(url), rt)
	// 「そのコンテナは無い」も停止の成功に数えるので、止めた後もポートが応答していることがある。デスクトップアプリなど
	// この skill 以外のエンジンなら正しい結果なので、失敗には数えず (数えると止められないものを止めにいき続ける)、ログに残すだけにする。
	// 旧版が固定名で起こしたコンテナが残っていると、これに当たる (issue 646)
	if ver, _ := probeEngine(url); ver != "" {
		fmt.Fprintf(env.Stderr, "reaper: 止めた後も %s はまだ応答している (この skill 以外のエンジンか、旧版が起こしたコンテナ。後者なら zundamon-kaisetsu down で止める)\n", url)
	}
	return true
}

// lastUseTime は最後にエンジンを使った時刻。記録が無ければ印を書いた時刻にする。
func lastUseTime(env *Env) time.Time {
	for _, p := range []string{lastUsePath(env), autoMarkerPath(env)} {
		if st, err := os.Stat(p); err == nil {
			return st.ModTime()
		}
	}
	return env.Now()
}

// reaperExe は見張りとして起こすバイナリ。テストでは go test のバイナリではなく、build した zundamon-kaisetsu に差し替える
var reaperExe = os.Executable

// spawnReaper は見張りを、端末とプロセスグループから切り離して起こす (起こしたコマンドが終わっても、Ctrl-C を受けても残る)。
func spawnReaper(env *Env) error {
	exe, err := reaperExe()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(env.StateDir, "reaper.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = logf.Close() }()
	cmd := exec.Command(exe, "--engine", env.Engine, reapCmd)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// flockWait はロックが取れるまで待つ。中断 (Ctrl-C) されたら待つのをやめる。
func flockWait(fd, how int) error {
	for {
		err := syscall.Flock(fd, how|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return fail("ロックを取れない (%v)", err)
		}
		sleepCtx(200 * time.Millisecond)
		if e := interruptedErr(); e != nil {
			return e
		}
	}
}

// clearAutoMarkerForUp は up を明示的に打ったときに (起動用のロックの中で) 印を消す。up で起動したものは自動では止めない
// (見張りは印が無いと何もせず終わる)。別のポートの印は消さない: その自動エンジンが動いていれば up は同名のコンテナで失敗し、
// 印を消すと、その自動エンジンが見張りに止めてもらえないまま残る。
func clearAutoMarkerForUp(env *Env) {
	if env.StateDir == "" {
		return
	}
	if _, url, ok := readAutoMarker(env); ok && url != "" && !samePort(url, env.Engine) {
		return
	}
	removeAutoMarker(env)
}

func removeAutoMarker(env *Env) {
	if env.StateDir != "" {
		_ = os.Remove(autoMarkerPath(env))
	}
}

// defaultStateDir は印・時刻・ロックの置き場。
func defaultStateDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "zundamon-kaisetsu")
}

// cleanupContext は停止の上限。中断 (Ctrl-C) の後でも止めにいけるよう appCtx には結び付けない。
func cleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}
