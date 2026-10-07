package main

// main の signal の扱いの結合テスト (実際にバイナリを作って起動する)。

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "zundamon-kaisetsu")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// isolatedEnv は本物のバイナリを走らせるときの環境。エンジンの自動起動・自動停止が本物のコンテナ (container / docker) と
// ユーザーのキャッシュ (ロックと印) に届かないよう、PATH からランタイムを外し、HOME を一時ディレクトリにする。
func isolatedEnv(t *testing.T, extra ...string) []string {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PATH=") && !strings.HasPrefix(kv, "HOME=") && !strings.HasPrefix(kv, "XDG_CACHE_HOME=") {
			env = append(env, kv)
		}
	}
	// PATH は空のディレクトリ (/usr/bin にすると、docker がそこにある Linux では本物に届く)。状態の置き場は Linux では
	// XDG_CACHE_HOME が HOME より優先されるので、それも差し替える (issue 646 の 5)
	return append(append(env, "PATH="+t.TempDir(), "HOME="+t.TempDir(), "XDG_CACHE_HOME="+t.TempDir()), extra...)
}

// blockingEngine は /speakers を release が閉じられるまで返さない偽のエンジン。
func blockingEngine(t *testing.T) (url string, requested *atomic.Bool, release chan struct{}) {
	t.Helper()
	requested = &atomic.Bool{}
	release = make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested.Store(true)
		select {
		case <-release:
			_, _ = io.WriteString(w, `[{"name":"四国めたん","styles":[{"name":"ノーマル","id":2}]}]`)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		srv.Close()
	})
	return srv.URL, requested, release
}

// SIGINT を受けたら後始末をして、同じ signal で死ぬ (rc=130 で普通に終えると bash のループが止まらない)。
func TestMainDiesBySignal(t *testing.T) {
	bin := buildBinary(t)
	url, requested, _ := blockingEngine(t)
	cmd := exec.Command(bin, "speakers")
	cmd.Env = isolatedEnv(t, "VOICEVOX_URL="+url)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "エンジンへの問い合わせ", requested.Load)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("終了の仕方が違う: %v", err)
	}
	ws := ee.Sys().(syscall.WaitStatus)
	if !ws.Signaled() || ws.Signal() != syscall.SIGINT {
		t.Errorf("SIGINT で死んでいない (exit=%d signaled=%v)", ws.ExitStatus(), ws.Signaled())
	}
}

// 起動時に無視されていた SIGINT は無視したまま (nohup や背景のジョブ。Notify で無視を解かない)。
func TestMainKeepsIgnoredSignal(t *testing.T) {
	bin := buildBinary(t)
	url, requested, release := blockingEngine(t)
	cmd := exec.Command("/bin/sh", "-c", `trap "" INT; exec "$0" speakers`, bin)
	cmd.Env = isolatedEnv(t, "VOICEVOX_URL="+url)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "エンジンへの問い合わせ", requested.Load)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	close(release) // 無視していれば、応答を受けて普通に終わる
	if err := cmd.Wait(); err != nil {
		t.Errorf("無視されていた SIGINT で止まった: %v", err)
	}
}
