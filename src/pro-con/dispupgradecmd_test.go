package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/dispatcher"
	"pro-con/eventlog"
	"pro-con/store"
)

// issue 505: 入れ替え (exec) で起きた dispatcher は前のプロセス像の続き。入れ替えの隙に --stop が置いた止める印を捨てず (頼んだ --stop が
// 時間切れまで待たない)、人が止めた印があっても回らずに抜けない・印を外さない (起動のときの扱いをしない)。
func TestUpgradedDispatcherKeepsStopMarks(t *testing.T) {
	root, dir := heldRoot(t)
	if err := store.Hold(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, dispatcher.StopRequestFile), []byte("stop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(upgradedFromEnv, "abc1234 09-26 08:02")
	runDispatcherT(t, "--"+fromScreenFlag, "--once", "--e2e", root)
	if b, _ := os.ReadFile(filepath.Join(dir, dispatcher.StopResultFile)); strings.TrimSpace(string(b)) != "ok" {
		t.Fatalf("入れ替えの隙に置かれた止める印を捨てた (止めた結果が無い): %q", b)
	}
	if !store.Held(dir) {
		t.Fatal("入れ替えで起きた dispatcher が人の止めた印を外した")
	}
	evs, err := eventlog.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	var said bool
	for _, e := range evs {
		said = said || (e.Kind == eventlog.KindUpgrade && strings.Contains(e.Reason, "abc1234 09-26 08:02 →"))
	}
	if !said {
		t.Fatalf("切り替わったことを出来事にしない: %v", evs)
	}
	if _, ok := os.LookupEnv(upgradedFromEnv); ok {
		t.Fatal("入れ替えの印を環境変数に残した (子へ漏れる)")
	}

	t.Setenv(upgradedFromEnv, "abc1234 09-26 08:02") // 手で起動した dispatcher の入れ替えでも、人の止めた印は外さない
	runDispatcherT(t, "--once", "--e2e", root)
	if !store.Held(dir) {
		t.Fatal("入れ替えで起きた (手で起動した) dispatcher が人の止めた印を外した")
	}
}

// --preflight は記録を読めるかだけを確かめて、版の名前を出す (lock を取らない: 動いている dispatcher が新版に走らせる)。
func TestDispatcherPreflight(t *testing.T) {
	root, dir := heldRoot(t)
	unlock, err := dispatcher.Lock(dir) // 動いている dispatcher の形
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	var out, errOut bytes.Buffer
	if rc := runDispatcher([]string{"--e2e", root, "--" + preflightFlag}, "", "", nil, pmConfig{}, &out, &errOut); rc != 0 || !strings.HasPrefix(out.String(), preflightOK+" ") {
		t.Fatalf("rc=%d out=%q err=%q", rc, out.String(), errOut.String())
	}
	if err := os.WriteFile(filepath.Join(dir, store.StateFile), []byte("{壊れた"), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if rc := runDispatcher([]string{"--e2e", root, "--" + preflightFlag}, "", "", nil, pmConfig{}, &out, &errOut); rc == 0 || strings.HasPrefix(out.String(), preflightOK) {
		t.Fatalf("読めない記録で確かめが通った: rc=%d out=%q", rc, out.String())
	}
}
