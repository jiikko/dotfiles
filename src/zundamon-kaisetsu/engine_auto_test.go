package main

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// fakeEngine は起動・停止を切り替えられる偽のエンジン。止まっている間はポートで誰も待ち受けない (接続を拒否される)。
type fakeEngine struct {
	t       *testing.T
	addr    string
	mu      sync.Mutex
	srv     *http.Server
	alive   atomic.Bool
	ups     atomic.Int32
	downs   atomic.Int32
	upErr   error
	upGate  chan struct{} // 閉じられるまで起動を終えない (nil なら待たない)
	spawns  atomic.Int32
	downRT  atomic.Value
	downURL atomic.Value
}

func (fe *fakeEngine) start() {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	ln, err := net.Listen("tcp", fe.addr)
	if err != nil {
		fe.t.Errorf("偽のエンジンを %s で待ち受けられない: %v", fe.addr, err)
		return
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`"0.0.0-fake"`))
	}), ReadHeaderTimeout: 5 * time.Second}
	fe.srv = srv
	go func() { _ = srv.Serve(ln) }()
	fe.alive.Store(true)
}

func (fe *fakeEngine) stop() {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	if fe.srv != nil {
		_ = fe.srv.Close()
		fe.srv = nil
	}
	fe.alive.Store(false)
}

func newFakeEngineEnv(t *testing.T, alive bool) (*Env, *fakeEngine) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0") // 空いているポートを決めるためだけに開いて閉じる
	if err != nil {
		t.Fatal(err)
	}
	fe := &fakeEngine{t: t, addr: ln.Addr().String()}
	_ = ln.Close()
	if alive {
		fe.start()
	}
	t.Cleanup(fe.stop)
	// 自動起動はランタイム (container / docker) の有無を PATH で見る。手元と CI (ランタイムが無い) で結果が変わらないよう、
	// 実行されても何もせず失敗するだけの偽の container を置く (本物のランタイムには触れない。起動・停止は上の fake が担う)
	shims := t.TempDir()
	if err := os.WriteFile(filepath.Join(shims, "container"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shims)
	env := testEnv(t)
	env.Engine = "http://" + fe.addr
	env.StateDir = t.TempDir()
	env.Now = time.Now
	env.EngineUp = func() error {
		fe.ups.Add(1)
		if fe.upGate != nil {
			<-fe.upGate
		}
		if fe.upErr != nil {
			return fe.upErr
		}
		fe.start()
		return nil
	}
	env.EngineDown = func(rt, engine string) error {
		fe.downs.Add(1)
		fe.downRT.Store(rt)
		fe.downURL.Store(engine)
		fe.stop()
		return nil
	}
	env.SpawnReaper = func() error { fe.spawns.Add(1); return nil }
	return env, fe
}

func markerExists(env *Env) bool { return isFile(autoMarkerPath(env)) }

func writeMarker(t *testing.T, env *Env, rt, url string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(autoMarkerPath(env), []byte(rt+"\n"+url+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	if err := os.Chtimes(autoMarkerPath(env), at, at); err != nil {
		t.Fatal(err)
	}
}

// 止まっているエンジンは使うときに起動し、印 (ランタイムとエンジンの URL) を残して見張りを起こす。コマンド自身は止めない。
func TestWithEngineStartsAndSpawnsReaper(t *testing.T) {
	env, fe := newFakeEngineEnv(t, false)
	ran := false
	if err := withEngine(env, func() error { ran = fe.alive.Load(); return nil }); err != nil {
		t.Fatal(err)
	}
	if !ran || fe.ups.Load() != 1 || fe.spawns.Load() != 1 || fe.downs.Load() != 0 {
		t.Errorf("本体の実行中に応答=%v / 起動 %d / 見張り %d / 停止 %d (want true / 1 / 1 / 0)", ran, fe.ups.Load(), fe.spawns.Load(), fe.downs.Load())
	}
	if _, url, ok := readAutoMarker(env); !ok || url != env.Engine {
		t.Errorf("印にエンジンの URL が無い: ok=%v url=%q", ok, url)
	}
}

// 既に応答しているエンジン (up で起動した・デスクトップアプリ) は起動もせず、見張りも起こさない。
func TestWithEngineLeavesForeignEngine(t *testing.T) {
	env, fe := newFakeEngineEnv(t, true)
	if err := withEngine(env, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if fe.ups.Load() != 0 || fe.spawns.Load() != 0 || markerExists(env) {
		t.Errorf("自分で起動していないエンジンを管理しようとした: 起動 %d / 見張り %d / 印=%v", fe.ups.Load(), fe.spawns.Load(), markerExists(env))
	}
}

// 起動に失敗しても (時間切れの後もコンテナは残りうる) 印は消さず、見張りは起動より先に起きている。
func TestWithEngineUpFailureKeepsMarkerForReaper(t *testing.T) {
	env, fe := newFakeEngineEnv(t, false)
	fe.upErr = errors.New("180 秒待っても応答しない")
	ran := false
	err := withEngine(env, func() error { ran = true; return nil })
	if err == nil || ran {
		t.Fatalf("起動の失敗で本体を走らせた / エラーを返さない: ran=%v err=%v", ran, err)
	}
	if !markerExists(env) || fe.spawns.Load() != 1 || fe.downs.Load() != 0 {
		t.Errorf("印=%v / 見張り %d / 停止 %d (want true / 1 / 0)", markerExists(env), fe.spawns.Load(), fe.downs.Load())
	}
}

// 2 つのコマンドが同時に来ても、起動は 1 回だけ (確かめると起動は同じロックの中)。
func TestWithEngineConcurrentStartOnce(t *testing.T) {
	env, fe := newFakeEngineEnv(t, false)
	fe.upGate = make(chan struct{})
	doneA, doneB := make(chan error, 1), make(chan error, 1)
	go func() { doneA <- withEngine(env, func() error { return nil }) }()
	waitUntil(t, "A の起動開始", func() bool { return fe.ups.Load() == 1 })
	go func() { doneB <- withEngine(env, func() error { return nil }) }() // A の起動中に来る: まだ接続を拒否される
	// B が確かめる (手元のポートなら 1ms 未満) のに十分な窓を A の起動中に作る。ロックが無ければ B はここで「止まっている」を見て
	// 二重に起動する。ロックがあれば B は待つので、窓の長さは結果を変えない
	// sleep-ok: window: 待ちではなく、競合の窓を作る入力
	time.Sleep(100 * time.Millisecond)
	close(fe.upGate)
	if err := <-doneA; err != nil {
		t.Fatal(err)
	}
	if err := <-doneB; err != nil {
		t.Fatal(err)
	}
	if fe.ups.Load() != 1 {
		t.Errorf("二重に起動した (起動 %d 回)", fe.ups.Load())
	}
}

// 別のポートで自動起動したエンジンが動いていれば、起動せずに理由を返す (印も消さない)。止まっている古い印なら上書きして起動する。
func TestWithEngineOtherPortAutoEngine(t *testing.T) {
	env, fe := newFakeEngineEnv(t, false)
	_, other := newFakeEngineEnv(t, true)
	otherURL := "http://" + other.addr
	writeMarker(t, env, "container", otherURL, 0)
	err := withEngine(env, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "別のポート") {
		t.Fatalf("別のポートの自動エンジンがあるのに起動しようとした: %v", err)
	}
	if _, url, _ := readAutoMarker(env); url != otherURL || fe.ups.Load() != 0 {
		t.Errorf("動いている自動エンジンの印を書き換えた / 起動した: url=%q 起動 %d", url, fe.ups.Load())
	}

	other.stop()
	if err := withEngine(env, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, url, _ := readAutoMarker(env); url != env.Engine || fe.ups.Load() != 1 {
		t.Errorf("止まっている古い印を上書きして起動しない: url=%q 起動 %d", url, fe.ups.Load())
	}
}

// 見張りが死んでいたら、次にエンジンを使うときに起こし直す。
func TestWithEngineRespawnsDeadReaper(t *testing.T) {
	env, fe := newFakeEngineEnv(t, true)
	writeMarker(t, env, "container", env.Engine, 0)
	if err := withEngine(env, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if fe.spawns.Load() != 1 {
		t.Errorf("自動で起動したエンジンに見張りがいないのに起こさなかった (見張り %d)", fe.spawns.Load())
	}
}

// エンジンへの要求のたびに、最後に使った時刻を更新する (長い合成の途中で見張りに止められない)。
func TestWithEngineTouchesLastUseOnEachRequest(t *testing.T) {
	env, _ := newFakeEngineEnv(t, true)
	old := time.Now().Add(-time.Hour)
	err := withEngine(env, func() error {
		_ = os.Chtimes(lastUsePath(env), old, old)
		_, _ = engineRequest(env.Engine, "/version", nil, nil)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(lastUsePath(env)); err != nil || time.Since(st.ModTime()) > time.Minute {
		t.Errorf("要求の後に最後に使った時刻が更新されていない (%v)", err)
	}
}

// 待ち受けているのに答えない相手 (別のサーバ・起動途中のエンジン) は、起動しない。
func TestWithEngineIgnoresNonEngineListener(t *testing.T) {
	env, fe := newFakeEngineEnv(t, false)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(other.Close)
	env.Engine = other.URL
	ran := false
	if err := withEngine(env, func() error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("本体を走らせない: ran=%v err=%v", ran, err)
	}
	if fe.ups.Load() != 0 || markerExists(env) {
		t.Errorf("答えない相手のポートで起動しようとした: 起動 %d / 印=%v", fe.ups.Load(), markerExists(env))
	}
}

// 別ホストのエンジンは起動できないので、自動で管理しない。
func TestWithEngineRemoteIsUntouched(t *testing.T) {
	env, fe := newFakeEngineEnv(t, false)
	env.Engine = "http://192.0.2.1:50021"
	ran := false
	if err := withEngine(env, func() error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("別ホストで本体を走らせない: ran=%v err=%v", ran, err)
	}
	if fe.ups.Load() != 0 || fe.spawns.Load() != 0 {
		t.Errorf("別ホストのエンジンを管理しようとした: 起動 %d / 見張り %d", fe.ups.Load(), fe.spawns.Load())
	}
}

// 見張り: 印に書かれたエンジンを見て、最後に使ってから engineIdleStop たつまでは止めず、たったら印のランタイムで止めて終わる。
func TestReaperStopsAfterIdle(t *testing.T) {
	env, fe := newFakeEngineEnv(t, true)
	writeMarker(t, env, "docker", env.Engine, 0)
	env.Engine = "http://127.0.0.1:1" // 見張りの起動引数ではなく、印の URL を見る
	touchLastUse(env)
	now := time.Now()
	r := &reaper{}
	env.Now = func() time.Time { return now.Add(engineIdleStop - time.Second) }
	if r.once(env) || fe.downs.Load() != 0 {
		t.Fatalf("まだ使われてから %s なのに止めた / 見張りを終えた", engineIdleStop-time.Second)
	}
	env.Now = func() time.Time { return now.Add(engineIdleStop + time.Second) }
	if !r.once(env) || fe.downs.Load() != 1 || markerExists(env) {
		t.Fatalf("使われなくなったのに止めない: 停止 %d / 印=%v", fe.downs.Load(), markerExists(env))
	}
	if rt, _ := fe.downRT.Load().(string); rt != "docker" {
		t.Errorf("印のランタイムで止めていない: %q (want docker)", rt)
	}
	if u, _ := fe.downURL.Load().(string); !samePort(u, "http://"+fe.addr) {
		t.Errorf("印のエンジン (ポート) のコンテナを止めていない: %q", u)
	}
}

// 見張り: 印が無い (up / down) なら止めずに終わる。応答しないなら、印を書いてから startGrace の間は起動途中とみなして待ち、
// その後は印を消して終わる。止めるのに失敗したら続け、reaperMaxFailed 回続けて失敗したら印を残して終わる。
func TestReaperExitConditions(t *testing.T) {
	env, fe := newFakeEngineEnv(t, false)
	if !(&reaper{}).once(env) || fe.downs.Load() != 0 {
		t.Errorf("印が無いのに止めた / 終えない (停止 %d)", fe.downs.Load())
	}

	writeMarker(t, env, "container", env.Engine, time.Minute)
	if (&reaper{}).once(env) || !markerExists(env) {
		t.Errorf("起動途中かもしれない間に見張りを終えた / 印を消した")
	}
	writeMarker(t, env, "container", env.Engine, startGrace+time.Minute)
	if !(&reaper{}).once(env) || markerExists(env) {
		t.Errorf("応答しないまま startGrace たったのに見張りを終えない / 印を残した")
	}

	fe.start()
	writeMarker(t, env, "container", env.Engine, 0)
	env.Now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	env.EngineDown = func(string, string) error { return errors.New("service down") }
	r := &reaper{}
	for i := 1; i < reaperMaxFailed; i++ {
		if r.once(env) {
			t.Fatalf("%d 回目の失敗で見張りを終えた", i)
		}
	}
	if !r.once(env) || !markerExists(env) {
		t.Errorf("%d 回続けて失敗しても見張りを終えない / 印を消した", reaperMaxFailed)
	}
}

// 本番の停止 (newEnv の EngineDown) は、印のランタイムのコンテナだけを止め、中断 (appCtx の取り消し) の後でも止めにいく。
// PATH を偽の container / docker だけにして、本物のランタイムには触れない。
func TestProductionEngineDownStopsOnlyMarkedRuntime(t *testing.T) {
	shims := t.TempDir()
	log := filepath.Join(t.TempDir(), "calls")
	for _, rt := range []string{"container", "docker"} {
		shim := "#!/bin/sh\necho \"" + rt + " $*\" >> " + log + "\nexit 0\n"
		if err := os.WriteFile(filepath.Join(shims, rt), []byte(shim), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", shims) // 本物のランタイムを見つけさせない
	cancel := withAppCtx(t)
	cancel()
	if err := newEnv().EngineDown("container", "http://127.0.0.1:50021"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "container stop "+containerName+"-50021\n") {
		t.Errorf("中断の後に、印のポートのコンテナを止めにいっていない。偽のランタイムへの呼び出し:\n%s", b)
	}
	// 名前は印のポートのものだけ: 別のポートに up したコンテナ・旧版の固定名には届かない (見張りが up のコンテナを止めた P1)
	if strings.Count(string(b), " stop ") != 1 {
		t.Errorf("印のポート以外のコンテナも止めにいった:\n%s", b)
	}
	if strings.Contains(string(b), "docker") {
		t.Errorf("印に無いランタイム (docker) に触った:\n%s", b)
	}
}

// up は印を起動より先に消す。起動が失敗 (時間切れ・中断・ランタイムが無い) しても、残った印で見張りが up のコンテナを止めない。
func TestCmdUpClearsMarkerEvenOnFailure(t *testing.T) {
	env, _ := newFakeEngineEnv(t, false)
	writeMarker(t, env, "docker", env.Engine, 0)
	t.Setenv("PATH", noRuntimePath(t)) // ランタイムが見つからず起動は失敗する (本物のランタイムには触れない)
	if err := cmdUp(env); err == nil {
		t.Fatal("ランタイムが無いのに up が成功した")
	}
	if markerExists(env) {
		t.Error("up が失敗した後に印が残っている (見張りが up のコンテナを止めうる)")
	}
}

// URL の無い印 (書きかけ・壊れた印) は持ち主が分からないので、上書きして起動する (「別のポートで動いている」と誤って断らない)。
func TestWithEngineOldOneLineMarker(t *testing.T) {
	env, fe := newFakeEngineEnv(t, false)
	if err := os.WriteFile(autoMarkerPath(env), []byte("docker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := withEngine(env, func() error { return nil }); err != nil {
		t.Fatalf("古い印で起動できない: %v", err)
	}
	if _, url, _ := readAutoMarker(env); fe.ups.Load() != 1 || url != env.Engine {
		t.Errorf("起動 %d / 印の URL %q (want 1 / %s)", fe.ups.Load(), url, env.Engine)
	}
}

// ランタイムが無ければ印を書かない (何も起動しないので、見張りに管理させるものが無い)。
func TestWithEngineNoRuntimeWritesNoMarker(t *testing.T) {
	env, fe := newFakeEngineEnv(t, false)
	fe.upErr = errors.New("container も docker も無い")
	t.Setenv("PATH", noRuntimePath(t))
	if err := withEngine(env, func() error { return nil }); err == nil {
		t.Fatal("起動できないのにエラーを返さない")
	}
	if markerExists(env) || fe.spawns.Load() != 0 {
		t.Errorf("ランタイムが無いのに印=%v / 見張り %d", markerExists(env), fe.spawns.Load())
	}
}

// 見張り: どのランタイムで起動したか分からない印では止めない (空のランタイムで止めると全ランタイムの同名コンテナに届く)。
func TestReaperUnknownRuntimeDoesNotStop(t *testing.T) {
	env, fe := newFakeEngineEnv(t, true)
	writeMarker(t, env, "", env.Engine, 0)
	env.Now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	if !(&reaper{}).once(env) || fe.downs.Load() != 0 || markerExists(env) {
		t.Errorf("ランタイムの分からない印で止めた / 見張りを終えない / 印を残した: 停止 %d", fe.downs.Load())
	}
}

// 別のポートのエンジン (デスクトップアプリ等) を使っても、自動で起動したエンジンの「最後に使った時刻」は更新しない。
func TestWithEngineOtherEngineDoesNotExtendAutoEngine(t *testing.T) {
	env, _ := newFakeEngineEnv(t, true)
	_, auto := newFakeEngineEnv(t, true)
	writeMarker(t, env, "container", "http://"+auto.addr, 0)
	old := time.Now().Add(-time.Hour)
	if err := os.WriteFile(lastUsePath(env), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(lastUsePath(env), old, old)
	err := withEngine(env, func() error {
		_, _ = engineRequest(env.Engine, "/version", nil, nil)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(lastUsePath(env)); time.Since(st.ModTime()) < 30*time.Minute {
		t.Error("別のエンジンを使っただけで、自動のエンジンの最後に使った時刻を更新した")
	}
}

// 同じエンジンを別の書き方 (localhost・末尾の /) で指しても、自動のエンジンとして最後に使った時刻を更新する (使用中に止めない)。
func TestWithEngineSamePortDifferentSpelling(t *testing.T) {
	env, fe := newFakeEngineEnv(t, false)
	if err := withEngine(env, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	_, port, _ := strings.Cut(fe.addr, ":")
	for _, spelling := range []string{"http://localhost:" + port, env.Engine + "/"} {
		old := time.Now().Add(-time.Hour)
		_ = os.Chtimes(lastUsePath(env), old, old)
		other := *env
		other.Engine = spelling
		err := withEngine(&other, func() error {
			_, _ = engineRequest(other.Engine, "/version", nil, nil)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if st, _ := os.Stat(lastUsePath(env)); time.Since(st.ModTime()) > time.Minute {
			t.Errorf("%s で使ったのに最後に使った時刻を更新しない (見張りが使用中に止める)", spelling)
		}
	}
	if fe.ups.Load() != 1 {
		t.Errorf("同じエンジンを別の書き方で指しただけで起動し直した (起動 %d 回)", fe.ups.Load())
	}
}

// 別のポートへの up は、自動のエンジンの印を消さない (失敗したときに、その自動エンジンが見張りに止められないまま残らないように)。
func TestCmdUpKeepsOtherPortMarker(t *testing.T) {
	env, _ := newFakeEngineEnv(t, false)
	_, auto := newFakeEngineEnv(t, true)
	writeMarker(t, env, "container", "http://"+auto.addr, 0)
	t.Setenv("PATH", noRuntimePath(t)) // 起動は失敗する (本物のランタイムには触れない)
	if err := cmdUp(env); err == nil {
		t.Fatal("ランタイムが無いのに up が成功した")
	}
	if !markerExists(env) {
		t.Error("別のポートへの up で、自動のエンジンの印を消した")
	}
}

// 見張り: どのポートに起動したか分からない印 (URL の無い・壊れた印) では止めない。
func TestReaperMarkerWithoutURLDoesNotStop(t *testing.T) {
	env, fe := newFakeEngineEnv(t, true)
	if err := os.WriteFile(autoMarkerPath(env), []byte("container\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.Now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	if !(&reaper{}).once(env) || fe.downs.Load() != 0 || markerExists(env) {
		t.Errorf("URL の無い印で止めた / 見張りを終えない / 印を残した: 停止 %d", fe.downs.Load())
	}
}

// down は名前で全ランタイムのコンテナを止めるので、止めたら印をポートに関係なく消す (残すと、見張りが後で up したコンテナを止めうる)。
func TestCmdDownClearsAnyMarkerAfterStop(t *testing.T) {
	env, _ := newFakeEngineEnv(t, false)
	_, auto := newFakeEngineEnv(t, false)
	writeMarker(t, env, "container", "http://"+auto.addr, 0)
	shims := t.TempDir()
	log := filepath.Join(t.TempDir(), "calls")
	if err := os.WriteFile(filepath.Join(shims, "container"), []byte("#!/bin/sh\necho \"$*\" >> "+log+"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shims) // 偽の container だけを見せる (本物のランタイムには触れない)
	if err := cmdDown(env); err != nil {
		t.Fatal(err)
	}
	if markerExists(env) {
		t.Error("down で止めた後に、別のポートの印が残っている")
	}
	// down は自分のポートの名前と、旧版の固定名を止める
	b, _ := os.ReadFile(log)
	for _, name := range []string{containerNameFor(env.Engine), containerName, containerNameFor("http://" + auto.addr)} {
		if !strings.Contains(string(b), "stop "+name+"\n") {
			t.Errorf("down が %s を止めにいっていない:\n%s", name, b)
		}
	}
}

// up はポートごとの名前でコンテナを起こし、container run が返らなければ startRunTimeout で打ち切る
// (起動用のロックの中で走るので、返らないと synth / down / 見張りが全部待ち続ける)。
func TestStartEngineRunTimeoutAndPortName(t *testing.T) {
	// sleep の場所は PATH を差し替える (newFakeEngineEnv) 前に決める (差し替えた後の PATH にはランタイムも sleep も無い)
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	env, _ := newFakeEngineEnv(t, false)
	shims := t.TempDir()
	log := filepath.Join(t.TempDir(), "calls")
	// 手元にイメージが無い (image inspect が失敗) ので取得する。
	// sleep-ok: dummy: 時間のかかる取得 (startRunTimeout より長い 1 秒。上限に含まれていれば打ち切られる) と、返らない container run を演じる
	// (run は startRunTimeout で kill される。kill されなくても 30 秒で終わる)
	shim := "#!/bin/sh\necho \"$*\" >> " + log + "\ncase \"$1 $2\" in \"image inspect\") exit 1 ;; \"image pull\") " + sleepBin +
		" 1 ;; run*) exec " + sleepBin + " 30 ;; esac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(shims, "container"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shims)
	old := startRunTimeout
	startRunTimeout = 500 * time.Millisecond
	t.Cleanup(func() { startRunTimeout = old })
	begin := time.Now()
	err = startEngine(env)
	if err == nil || !strings.Contains(err.Error(), "終わらない") {
		t.Fatalf("返らない run を打ち切っていない: %v", err)
	}
	if time.Since(begin) > 20*time.Second {
		t.Errorf("打ち切りまで %s かかった (上限が効いていない)", time.Since(begin))
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "--name "+containerNameFor(env.Engine)+" ") {
		t.Errorf("ポートごとの名前で起動していない:\n%s", b)
	}
	// 取得は run の前に、上限の外で済ませる (取得が上限より長くても run まで進む)
	if pi, ri := strings.Index(string(b), "image pull "+image), strings.Index(string(b), "run "); pi < 0 || ri < 0 || pi > ri {
		t.Errorf("取得を run の前に上限の外で済ませていない:\n%s", b)
	}
}

// 起動用のロックを待つときは、持っているプロセスを出す (詰まったときに何を止めればよいか分かるように)。
func TestWithStartLockShowsHolder(t *testing.T) {
	env, _ := newFakeEngineEnv(t, true)
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- withStartLock(env, func() error { close(held); <-release; return nil })
	}()
	<-held
	var mu sync.Mutex
	var errb bytes.Buffer
	waiter := *env
	waiter.Stderr = writerFunc(func(p []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return errb.Write(p) })
	waitDone := make(chan error, 1)
	go func() { waitDone <- withStartLock(&waiter, func() error { return nil }) }()
	out := func() string { mu.Lock(); defer mu.Unlock(); return errb.String() }
	waitUntil(t, "待つ側の表示", func() bool { return strings.Contains(out(), "持っているのは") })
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-waitDone; err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("pid %d", os.Getpid()); !strings.Contains(out(), want) {
		t.Errorf("持っているプロセスを出していない (want %q): %s", want, out())
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// 本番の配線: synth / kana / speakers は dispatch から自動起動を通る (withEngine の包みが外れたら red)。
func TestDispatchWrapsEngineCommands(t *testing.T) {
	script := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(script, []byte(`{"lines":[{"who":"metan","text":"a"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"speakers"}, {"kana", "あ"}, {"synth", script}} {
		t.Run(args[0], func(t *testing.T) {
			env, fe := newFakeEngineEnv(t, false)
			_ = dispatch(args, env) // 偽のエンジンは本物の応答を返さないので、本体の成否は見ない
			if fe.ups.Load() != 1 {
				t.Errorf("%s が止まっているエンジンを起動しなかった (起動 %d 回): 自動起動を通っていない", args[0], fe.ups.Load())
			}
		})
	}
}

// 本番の配線: newEnv は自動起動・停止・見張りの手段と状態の置き場を入れる。起動の手段は本物の startEngine につながっている。
func TestNewEnvWiresEngineAuto(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Linux では状態の置き場に XDG_CACHE_HOME が HOME より優先される。差し替えないと本物のキャッシュを指す (issue 646 の 5)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("PATH", noRuntimePath(t)) // 本物のランタイムを見つけさせない (起動は「無い」で失敗する)
	t.Setenv("VOICEVOX_URL", "http://127.0.0.1:1")
	e := newEnv()
	if e.EngineUp == nil || e.EngineDown == nil || e.SpawnReaper == nil {
		t.Fatal("自動起動・停止・見張りの手段が入っていない")
	}
	if !strings.HasPrefix(e.StateDir, home) {
		t.Errorf("状態の置き場がユーザーのキャッシュ (HOME の下) でない: %q", e.StateDir)
	}
	if err := e.EngineUp(); err == nil || !strings.Contains(err.Error(), "container も docker も無い") {
		t.Errorf("EngineUp が startEngine につながっていない: %v", err)
	}
}

// 本番の見張り: build したバイナリを __reap で起こすと、別のセッションに切り離され、見張りのロックを持って回る。
// 印が古く (startGrace を過ぎ)、エンジンが応答しなければ、印を消して自分で終わる。本物のランタイムには触れない (PATH から外す)。
func TestReaperProcessLifecycle(t *testing.T) {
	bin := buildBinary(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir()) // Linux で本物の印と見張りに届かないように (issue 646 の 5)
	t.Setenv("PATH", noRuntimePath(t))
	t.Setenv("VOICEVOX_URL", "http://127.0.0.1:1") // 誰も待ち受けていない
	old := reaperExe
	reaperExe = func() (string, error) { return bin, nil }
	t.Cleanup(func() { reaperExe = old })
	env := newEnv()
	if err := os.MkdirAll(env.StateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 起動途中 (印が新しい) なら見張りは待ち続ける: ロックを持ち、自分の pid を書き、別のセッションにいる
	// 見張りは途中で失敗しても必ず kill する (起動途中の印なら最大 30 分残るので)
	lockFile := filepath.Join(env.StateDir, "reaper.lock")
	t.Cleanup(func() {
		b, _ := os.ReadFile(lockFile)
		if p, _ := strconv.Atoi(strings.TrimSpace(string(b))); p > 0 && reaperAlive(env) {
			_ = syscall.Kill(p, syscall.SIGKILL)
		}
	})
	writeMarker(t, env, "container", env.Engine, 0)
	if err := env.SpawnReaper(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "見張りがロックを取る", func() bool { return reaperAlive(env) })
	var pid int
	waitUntil(t, "見張りの pid", func() bool {
		b, _ := os.ReadFile(filepath.Join(env.StateDir, "reaper.lock"))
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return pid > 0
	})
	if sid, err := syscall.Getsid(pid); err != nil || sid != pid {
		t.Errorf("見張りが別のセッションに切り離されていない (sid=%d pid=%d err=%v)", sid, pid, err)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "kill した見張りのロックが外れる", func() bool { return !reaperAlive(env) })

	// 印が古く、エンジンが応答しなければ、見張りは印を消して終わる
	writeMarker(t, env, "container", env.Engine, startGrace+time.Minute)
	if err := env.SpawnReaper(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "見張りが印を消して終わる", func() bool { return !markerExists(env) && !reaperAlive(env) })
}

// 見張りが止めた後もポートが応答しているなら (デスクトップアプリ・旧版の固定名のコンテナ)、失敗には数えずログに残す。
func TestReaperWarnsWhenStillAnswering(t *testing.T) {
	env, fe := newFakeEngineEnv(t, true)
	writeMarker(t, env, "container", env.Engine, 0)
	env.EngineDown = func(string, string) error { fe.downs.Add(1); return nil } // 「無い」で成功扱いになった停止を演じる (止まらない)
	var errb bytes.Buffer
	env.Stderr = &errb
	env.Now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	if !(&reaper{}).once(env) || fe.downs.Load() != 1 {
		t.Fatalf("止めにいって見張りを終えていない (停止 %d)", fe.downs.Load())
	}
	if !strings.Contains(errb.String(), "まだ応答している") {
		t.Errorf("止めた後も応答しているのにログに残さない: %s", errb.String())
	}
}

// 手元にイメージがあれば取得しない (取得はレジストリに問い合わせるので、オフラインや回数制限で起動できなくなる)。
func TestStartEngineSkipsPullWhenImageIsLocal(t *testing.T) {
	env, _ := newFakeEngineEnv(t, false)
	shims := t.TempDir()
	log := filepath.Join(t.TempDir(), "calls")
	// image pull は失敗する (オフライン)。run は成功して返るが、偽のエンジンは立たないので応答待ちは時間切れになる
	shim := "#!/bin/sh\necho \"$*\" >> " + log + "\ncase \"$1 $2\" in \"image pull\") exit 1 ;; esac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(shims, "container"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shims)
	cancel := withAppCtx(t)
	done := make(chan error, 1)
	go func() { done <- startEngine(env) }()
	waitUntil(t, "run まで進む", func() bool { b, _ := os.ReadFile(log); return strings.Contains(string(b), "run ") })
	cancel() // 応答待ち (最大 180 秒) を打ち切る
	<-done
	b, _ := os.ReadFile(log)
	if strings.Contains(string(b), "image pull") {
		t.Errorf("手元にイメージがあるのに取得した (オフラインで起動できなくなる):\n%s", b)
	}
}

// 起動 (初回の取得を含む) が長くても、起動した直後に見張りが止めないよう、使った時刻を起動の後で取り直す。
func TestWithEngineTouchesLastUseAfterStart(t *testing.T) {
	env, fe := newFakeEngineEnv(t, false)
	up := env.EngineUp
	env.EngineUp = func() error {
		old := time.Now().Add(-time.Hour) // 起動に 1 時間かかった (長い取得) のを演じる
		_ = os.Chtimes(lastUsePath(env), old, old)
		return up()
	}
	if err := withEngine(env, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	env.Now = time.Now
	if (&reaper{}).once(env) || fe.downs.Load() != 0 {
		t.Errorf("起動した直後に見張りが止めた (停止 %d)", fe.downs.Load())
	}
}

// TestReaperCommandRunsFromRoot は、見張りを作業ディレクトリ "/" で、端末から切り離して起こすことを確かめる (issue 646 の 3)。
// 呼び出し側の cwd のまま起こすと、外付けディスクから実行したときに見張りが生きている間ディスクを取り出せない。
func TestReaperCommandRunsFromRoot(t *testing.T) {
	env := &Env{Engine: "http://127.0.0.1:50077"}
	cmd := reaperCommand("/path/to/zundamon-kaisetsu", env)
	if cmd.Dir != "/" {
		t.Errorf("見張りの作業ディレクトリ: got %q want /", cmd.Dir)
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		t.Error("見張りを新しいセッションで起こしていない (端末を閉じると一緒に止まる)")
	}
	if want := []string{"/path/to/zundamon-kaisetsu", "--engine", env.Engine, reapCmd}; !slices.Equal(cmd.Args, want) {
		t.Errorf("見張りの引数: got %v want %v", cmd.Args, want)
	}
	if cmd.Env != nil {
		t.Error("見張りの環境変数を絞っている (DOCKER_HOST などが届かず、別のコンテナを見る)")
	}
}

// noRuntimePath は「ランタイム (container / docker) が無い」PATH。空のディレクトリにする: /usr/bin:/bin にすると、docker が
// /usr/bin にある Linux では本物の docker に届く (issue 646 の 5)。テストの偽物は /bin/sh と絶対パスのコマンドだけを使う
func noRuntimePath(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// TestReaperKeepsEngineInUse は、エンジンを使っているコマンドがいる間は、最後に使った時刻から engineIdleStop を過ぎても
// 止めないことを確かめる (長い合成の途中でスリープし、復帰直後に見張りが先に動く形。issue 646 の 1)。
func TestReaperKeepsEngineInUse(t *testing.T) {
	env, fe := newFakeEngineEnv(t, true)
	writeMarker(t, env, "docker", env.Engine, 0)
	touchLastUse(env)
	release, err := holdEngineInUse(env)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	env.Now = func() time.Time { return now.Add(engineIdleStop + time.Hour) } // スリープから復帰した
	r := &reaper{}
	if r.once(env) || fe.downs.Load() != 0 {
		t.Fatalf("使っているコマンドがいるのに止めた (停止 %d)", fe.downs.Load())
	}
	release()
	if !r.once(env) || fe.downs.Load() != 1 {
		t.Fatalf("使い終わった後も止めない (停止 %d)", fe.downs.Load())
	}
}

// TestWithEngineHoldsInUse は、withEngine が本体を実行している間だけ使用中のロックを持つことを確かめる。
func TestWithEngineHoldsInUse(t *testing.T) {
	env, _ := newFakeEngineEnv(t, true)
	var during bool
	if err := withEngine(env, func() error { during = engineInUse(env); return nil }); err != nil {
		t.Fatal(err)
	}
	if !during {
		t.Error("本体の実行中に使用中のロックを持っていない")
	}
	if engineInUse(env) {
		t.Error("本体が終わった後も使用中のロックが残っている")
	}
}

// TestEngineInUseUnknownMeansInUse は、使用中のロックを確かめられないとき (ファイルを開けない) は「使っている」に倒す
// (止めない) ことを確かめる。
func TestEngineInUseUnknownMeansInUse(t *testing.T) {
	env := &Env{StateDir: t.TempDir()}
	if err := os.Mkdir(filepath.Join(env.StateDir, "engine.inuse"), 0o755); err != nil { // ディレクトリは O_RDWR で開けない
		t.Fatal(err)
	}
	if !engineInUse(env) {
		t.Error("確かめられないのに「使っていない」と判定した (使用中のエンジンを止めうる)")
	}
}

// TestWithEngineOtherPortReleasesInUse は、別のポートのエンジン (デスクトップアプリ等) を使うコマンドが、自動で起動した
// エンジンの停止を延ばさないことを確かめる (使用中のロックを手放す)。
func TestWithEngineOtherPortReleasesInUse(t *testing.T) {
	envA, feA := newFakeEngineEnv(t, true) // 自動で起動したエンジン
	writeMarker(t, envA, "docker", envA.Engine, 0)
	touchLastUse(envA)
	now := time.Now()
	envA.Now = func() time.Time { return now.Add(engineIdleStop + time.Hour) }
	other := &fakeEngine{t: t, addr: "127.0.0.1:0"}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	other.addr = ln.Addr().String()
	_ = ln.Close()
	other.start()
	t.Cleanup(other.stop)
	envB := *envA
	envB.Engine = "http://" + other.addr // 別のポートで応答しているエンジン
	var stopped bool
	if err := withEngine(&envB, func() error { stopped = (&reaper{}).once(envA); return nil }); err != nil {
		t.Fatal(err)
	}
	if !stopped || feA.downs.Load() != 1 {
		t.Errorf("別のポートのエンジンを使っている間、自動のエンジンを止めなかった (停止 %d)", feA.downs.Load())
	}
}

// TestEngineInUseIsShared は、使用中のロックを複数のコマンドが同時に持てることを確かめる (排他にすると、合成の途中で打った
// kana / speakers が合成の終わりまで待たされる)。
func TestEngineInUseIsShared(t *testing.T) {
	env := &Env{StateDir: t.TempDir()}
	r1, err := holdEngineInUse(env)
	if err != nil {
		t.Fatal(err)
	}
	defer r1()
	done := make(chan error, 1)
	go func() {
		r2, err := holdEngineInUse(env)
		if err == nil {
			r2()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second): // 待ちの上限 (共有なら即座に返る)
		t.Fatal("2 つ目のコマンドが使用中のロックを取れずに待っている (共有ロックになっていない)")
	}
}

// TestWithEngineHoldsInUseBeforeStart は、エンジンを起動する時点で、もう使用中のロックを持っていることを確かめる
// (起動用のロックより先に取る。後に取ると、起動を確かめてから使い始めるまでの間に見張りが止めうる)。
func TestWithEngineHoldsInUseBeforeStart(t *testing.T) {
	env, _ := newFakeEngineEnv(t, false) // 応答しないので起動する
	up := env.EngineUp
	var heldAtStart bool
	env.EngineUp = func() error { heldAtStart = engineInUse(env); return up() }
	if err := withEngine(env, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !heldAtStart {
		t.Error("起動する時点で使用中のロックを持っていない")
	}
}
