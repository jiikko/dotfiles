package live

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"mac-lan-manager/setup"
	"mac-lan-manager/speedtest"
)

// stepClock は Now を呼ぶたびに 1 秒進む時計 (速さの値は見ず、本物のパイプで量が数えられることだけを見る)。
func stepClock() func() time.Time {
	var mu sync.Mutex
	t := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		t = t.Add(time.Second)
		return t
	}
}

// 本物のパイプを通して測る: 偽物の ssh が相手の側を演じる (wc -c で数えて返す・head -c で流す)。上りは stdin を閉じて相手に
// 終わりを知らせ、下りは相手が流し終えても値を出す。
func TestSpeedStartMeasuresThroughPipes(t *testing.T) {
	ssh := fakeSSH(t, `for a; do last=$a; done
case "$last" in
true) exit 0 ;;
"exec wc -c") exec wc -c ;;
*) exec head -c 3000000 /dev/zero ;;
esac`)
	start := func(ctx context.Context, argv []string, withStdin bool) (speedtest.Proc, error) {
		argv = append([]string{ssh}, argv[1:]...) // PATH の ssh を引かない
		return speedStart(ctx, argv, withStdin)
	}
	d := setup.Dest{User: "koji", IP: "192.168.100.12", HostKeyAlias: "m2macmini.local"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := speedtest.Measure(ctx, d, start, speedtest.Clock{Now: stepClock(), After: time.After}, 50*time.Millisecond)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.UpBps <= 0 || r.DownBps <= 0 {
		t.Errorf("上り %v / 下り %v (相手が数えた量・届いた量が 0)", r.UpBps, r.DownBps)
	}
}

// 鍵で入れない ssh (stderr に Permission denied) は ErrNoKey になる (Wait の失敗に stderr の文を持たせている)。
func TestSpeedStartKeyDenied(t *testing.T) {
	ssh := fakeSSH(t, `echo 'koji@192.168.100.12: Permission denied (publickey).' >&2; exit 255`)
	start := func(ctx context.Context, argv []string, withStdin bool) (speedtest.Proc, error) {
		return speedStart(ctx, append([]string{ssh}, argv[1:]...), withStdin)
	}
	d := setup.Dest{User: "koji", IP: "192.168.100.12", HostKeyAlias: "m2macmini.local"}
	r := speedtest.Measure(context.Background(), d, start, speedtest.Clock{Now: stepClock(), After: time.After}, time.Second)
	if !errors.Is(r.Err, speedtest.ErrNoKey) {
		t.Errorf("Err = %v, want ErrNoKey", r.Err)
	}
}
