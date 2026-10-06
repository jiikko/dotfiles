package main

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeEngine は起動・停止を切り替えられる偽のエンジン。止まっている間はポートで誰も待ち受けない (接続を拒否される)。
type fakeEngine struct {
	t      *testing.T
	addr   string
	mu     sync.Mutex
	srv    *http.Server
	alive  atomic.Bool
	ups    atomic.Int32
	downs  atomic.Int32
	upErr  error
	upGate chan struct{} // 閉じられるまで起動を終えない (nil なら待たない)
	spawns atomic.Int32
	downRT atomic.Value
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
	env.EngineDown = func(rt string) error {
		fe.downs.Add(1)
		fe.downRT.Store(rt)
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
	env.EngineDown = func(string) error { return errors.New("service down") }
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
	t.Setenv("PATH", shims+":/bin:/usr/bin") // 本物のランタイムを見つけさせない
	cancel := withAppCtx(t)
	cancel()
	if err := newEnv().EngineDown("container"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "container stop "+containerName) {
		t.Errorf("中断の後に container のコンテナを止めにいっていない。偽のランタイムへの呼び出し:\n%s", b)
	}
	if strings.Contains(string(b), "docker") {
		t.Errorf("印に無いランタイム (docker) に触った:\n%s", b)
	}
}

// up は印を起動より先に消す。起動が失敗 (時間切れ・中断・ランタイムが無い) しても、残った印で見張りが up のコンテナを止めない。
func TestCmdUpClearsMarkerEvenOnFailure(t *testing.T) {
	env, _ := newFakeEngineEnv(t, false)
	writeMarker(t, env, "docker", env.Engine, 0)
	t.Setenv("PATH", "/usr/bin:/bin") // ランタイムが見つからず起動は失敗する (本物のランタイムには触れない)
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
	t.Setenv("PATH", "/usr/bin:/bin")
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
	t.Setenv("PATH", "/usr/bin:/bin") // 起動は失敗する (本物のランタイムには触れない)
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
	if err := os.WriteFile(filepath.Join(shims, "container"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shims+":/usr/bin:/bin") // 偽の container だけを見せる (本物のランタイムには触れない)
	if err := cmdDown(env); err != nil {
		t.Fatal(err)
	}
	if markerExists(env) {
		t.Error("down で止めた後に、別のポートの印が残っている")
	}
}
