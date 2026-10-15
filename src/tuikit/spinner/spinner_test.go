package spinner

import (
	"os"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"github.com/jiikko/dotfiles/src/tuikit/widthenv"
)

func TestMain(m *testing.M) {
	widthenv.ExitIfUnsupported()
	os.Exit(m.Run())
}

// 各コマは 1 桁で、隣どうしが違う (回って見える)。
func TestFramesAreOneColumnAndDistinct(t *testing.T) {
	seen := map[string]bool{}
	for i := range Len {
		f := Frame(i)
		if w := termwidth.Of(f); w != 1 {
			t.Errorf("Frame(%d)=%q の幅 %d (1 桁でない)", i, f, w)
		}
		if seen[f] {
			t.Errorf("Frame(%d)=%q が重複", i, f)
		}
		seen[f] = true
	}
}

// 増やし続けた整数・負の整数も Len で回す (restartable は frame を引数で受けて負も来うる)。
func TestFrameWraps(t *testing.T) {
	for _, tc := range []struct{ i, want int }{{0, 0}, {Len, 0}, {Len + 3, 3}, {-1, Len - 1}, {-Len - 2, Len - 2}} {
		if got, want := Frame(tc.i), frames[tc.want]; got != want {
			t.Errorf("Frame(%d)=%q, want %q", tc.i, got, want)
		}
	}
}

// At は時刻だけで決まる: step の中では同じコマ、step を跨ぐと次のコマ、Len 個の step で一周する。
func TestAtSelectsByTime(t *testing.T) {
	step := 80 * time.Millisecond
	start := time.UnixMilli(1_700_000_000_000) // 80ms の倍数 (step の境目)
	f0 := At(start, step)
	if got := At(start.Add(step-time.Millisecond), step); got != f0 {
		t.Errorf("step の中でコマが変わった: %q → %q", f0, got)
	}
	if got := At(start.Add(step), step); got == f0 {
		t.Errorf("step を跨いでもコマが変わらない: %q", got)
	}
	if got := At(start.Add(time.Duration(Len)*step), step); got != f0 {
		t.Errorf("Len 個の step で一周しない: %q → %q", f0, got)
	}
	// 寄せる前の treefiler の式 (UnixMilli/80) と同じコマを選ぶ
	for d := time.Duration(0); d < 2*time.Second; d += 7 * time.Millisecond {
		now := start.Add(d)
		if got, want := At(now, step), frames[int(now.UnixMilli()/80)%Len]; got != want {
			t.Fatalf("At(%v)=%q, 旧式は %q", now, got, want)
		}
	}
}

func TestAtNonPositiveStep(t *testing.T) {
	if got := At(time.UnixMilli(1_700_000_000_123), 0); got != frames[0] {
		t.Errorf("step=0 で %q", got)
	}
}
