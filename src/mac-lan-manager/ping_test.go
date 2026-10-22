package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"mac-lan-manager/reach"
)

// fakeCheck は疎通の確認の偽物。dead に挙げた名前は届かない IP に引ける (名前は引けるが届かない)。それ以外は届く。
type fakeCheck struct {
	mu    sync.Mutex
	asked [][]string
	dead  map[string]bool
}

func (f *fakeCheck) check(_ context.Context, names []string) []reach.Name {
	f.mu.Lock()
	f.asked = append(f.asked, names)
	f.mu.Unlock()
	out := make([]reach.Name, len(names))
	for i, n := range names {
		a := reach.Addr{IP: "192.168.100.12", Ping: reach.Ping{Sent: 3, Recv: 3, AvgMs: 0.5}}
		if f.dead[n] {
			a = reach.Addr{IP: "192.168.100.12", Ping: reach.Ping{Sent: 3}, SSH: errors.New("i/o timeout")}
		}
		out[i] = reach.Name{Name: n, Addrs: []reach.Addr{a}}
	}
	return out
}

// 管理対象の相手の名前ごとに確かめ (内部 IP が固定の相手は有線の名前も)、ssh のユーザー名が分からない相手も外さない。
// 全部届けば rc=0、1 つでも届かなければ rc=1。
func TestPingChecksNamesOfManagedPeers(t *testing.T) {
	f := &fakeCheck{}
	var out, errb bytes.Buffer
	if rc := runPing([]string{"--json"}, &out, &errb, time.Now, nil, speedModel(nil), f.check); rc != 0 {
		t.Fatalf("rc=%d, want 0 stderr=%s", rc, errb.String())
	}
	want := [][]string{{"m2macmini.local", "m2macmini.wired.local"}, {"air.local"}, {"gone.local"}}
	slices.SortFunc(f.asked, func(a, b []string) int { return strings.Compare(a[0], b[0]) })
	slices.SortFunc(want, func(a, b []string) int { return strings.Compare(a[0], b[0]) })
	if !slices.EqualFunc(f.asked, want, slices.Equal) {
		t.Errorf("確かめた名前 = %v, want %v", f.asked, want)
	}
	var reps []pingReport
	if err := json.Unmarshal(out.Bytes(), &reps); err != nil || len(reps) != 3 {
		t.Fatalf("JSON = %s (%v)", out.String(), err)
	}

	f = &fakeCheck{dead: map[string]bool{"m2macmini.wired.local": true}}
	out.Reset()
	if rc := runPing([]string{"mini"}, &out, &errb, time.Now, nil, speedModel(nil), f.check); rc != 1 {
		t.Fatalf("届かない名前があるのに rc=%d", rc)
	}
	if len(f.asked) != 1 || !strings.Contains(out.String(), "✗ m2macmini.wired.local → 192.168.100.12 ping 届かない (0/3)  ssh 閉") {
		t.Errorf("別名で選んだ相手だけを確かめ、届かない名前に ✗ を付ける: asked=%v\n%s", f.asked, out.String())
	}
}

// 有線の名前が内部 IP と違う IP に引けたら lan_mismatch を立てる。
func TestPingMarksWiredNameElsewhere(t *testing.T) {
	check := func(_ context.Context, names []string) []reach.Name {
		out := make([]reach.Name, len(names))
		for i, n := range names {
			out[i] = reach.Name{Name: n, Addrs: []reach.Addr{{IP: "192.168.100.99", Ping: reach.Ping{Sent: 3, Recv: 3}}}}
		}
		return out
	}
	var out, errb bytes.Buffer
	rc := runPing([]string{"--json", "m2macmini.local"}, &out, &errb, time.Now, nil, speedModel(nil), check)
	// 今の契約 (README・usage): rc は「確かめた名前が全部届くか」だけで決め、lan_mismatch は知らせるだけ (rc=0)。
	// 画面の疎通の列は同じ状態を W✗ にする。揃えるかは 847 の判断で、揃えるならこの期待値を 1 に変える
	if rc != 0 {
		t.Errorf("rc = %d, want 0 (届いているので lan_mismatch だけでは 1 にしない)", rc)
	}
	var reps []pingReport
	if err := json.Unmarshal(out.Bytes(), &reps); err != nil || len(reps) != 1 || len(reps[0].Names) != 2 {
		t.Fatalf("JSON = %s (%v)", out.String(), err)
	}
	if n := reps[0].Names; n[0].Elsewhere || !n[1].Elsewhere {
		t.Errorf("lan_mismatch は有線の名前だけに立つ: %+v", n)
	}
}

// 止める合図が来たら、確認の結果 (端末の Ctrl-C は子の ping にも届き、0 received の統計を出す) より先に 130 を返す。
func TestPingStopWins(t *testing.T) {
	stop := make(chan struct{})
	check := func(ctx context.Context, names []string) []reach.Name {
		close(stop) // 合図を出してすぐ返す (ping が SIGINT で統計を出して終わった形。ctx が切れるのを待たない)
		return []reach.Name{{Name: names[0], Addrs: []reach.Addr{{IP: "192.168.100.12", Ping: reach.Ping{Sent: 1}, SSH: errors.New("i/o timeout")}}}}
	}
	var out, errb bytes.Buffer
	if rc := runPing(nil, &out, &errb, time.Now, stop, speedModel(nil), check); rc != 130 {
		t.Fatalf("rc=%d, want 130", rc)
	}
}

// 管理対象に無い名前は rc=2。
func TestPingUnknownTarget(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := runPing([]string{"nope"}, &out, &errb, time.Now, nil, speedModel(nil), (&fakeCheck{}).check); rc != 2 {
		t.Fatalf("rc=%d, want 2", rc)
	}
}
