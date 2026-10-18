package filer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hugeFolderModel は 1 つのフォルダに n 件のファイル (と dotfile 1 件) を置き、そのフォルダを開いてカーソルを中へ入れた Model。
// dotfile を混ぜるのは、隠す設定 (既定) で kids が絞り込みのスライスを作る経路を通すため (実際のフォルダには .DS_Store 等がある)。
func hugeFolderModel(b *testing.B, n int) *Model {
	b.Helper()
	b.Setenv("TREEFILER_CONFIG_DIR", b.TempDir())
	dir := b.TempDir()
	big := filepath.Join(dir, "big")
	if err := os.MkdirAll(big, 0o755); err != nil {
		b.Fatal(err)
	}
	for i := range n {
		if err := os.WriteFile(filepath.Join(big, fmt.Sprintf("f%06d.txt", i)), nil, 0o644); err != nil {
			b.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(big, ".DS_Store"), nil, 0o644); err != nil {
		b.Fatal(err)
	}
	for _, d := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			b.Fatal(err)
		}
	}
	m, err := New(dir, Options{Now: func() time.Time { return fixedNow }})
	if err != nil {
		b.Fatal(err)
	}
	m.Resize(200, 50)
	cur := m.loadPath(filepath.Join(big, fmt.Sprintf("f%06d.txt", n/2))) // big の真ん中のファイルへ (big を開く)
	if cur == nil {
		b.Fatal("big の中のファイルが読めない")
	}
	for p := cur.parent; p != nil; p = p.parent {
		p.expanded = true
	}
	m.setCur(cur)
	settleBackground(b, m)
	// 開いた直後に全部の子を動かす期間 (spawnHold。virt.go) を過ぎた、止まった画面にする。🚨 期間は動きの時計 (1 回の Advance の進みは
	// 0.05 秒で頭打ち) で測るので、1 秒先へ 1 回だけ進めても抜けない。60fps で 1 秒ぶん進める
	for at, end := m.lastAdv, m.lastAdv.Add(time.Second); at.Before(end); {
		at = at.Add(16 * time.Millisecond)
		m.Advance(at)
	}
	m.snapAll()
	if v := strings.Join(m.draw().plain(), "\n"); !strings.Contains(v, fmt.Sprintf("f%06d.txt", n/2)) {
		b.Fatalf("前提: big の中を描いていない:\n%s", v)
	}
	return m
}

// hugeSizes は巨大なフォルダの件数。2 つ並べるのは、1 フレームの時間が件数に比例しないことを同じ実行の中で比べるため。
var hugeSizes = []int{5000, 50000}

// eachHugeSize は件数ごとの sub-benchmark を回す。
func eachHugeSize(b *testing.B, run func(b *testing.B, m *Model)) {
	for _, n := range hugeSizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			m := hugeFolderModel(b, n)
			b.ReportAllocs()
			b.ResetTimer()
			run(b, m)
		})
	}
}

// BenchmarkViewHugeFolder は止まった画面 1 フレーム (View)。
func BenchmarkViewHugeFolder(b *testing.B) {
	eachHugeSize(b, func(b *testing.B, m *Model) {
		for range b.N {
			_ = m.View()
		}
	})
}

// BenchmarkLayoutHugeFolder は配置の計算 (動きの最中は Advance の step が毎 tick 呼ぶ。View の draw も呼ぶ)。
func BenchmarkLayoutHugeFolder(b *testing.B) {
	eachHugeSize(b, func(b *testing.B, m *Model) {
		for range b.N {
			_ = m.layout()
		}
	})
}

// BenchmarkMoveHugeFolder は巨大なフォルダの中で j を 1 回押し、動きが止まるまで 60fps で進める (キー 1 回ぶんの重さ)。
func BenchmarkMoveHugeFolder(b *testing.B) {
	eachHugeSize(b, func(b *testing.B, m *Model) {
		at := m.lastAdv // 準備の続きから進める (巻き戻すと、開いた直後の期間 spawnHold に戻って全部の子を動かす)
		for range b.N {
			m.HandleKey("j")
			for range 40 {
				at = at.Add(16 * time.Millisecond)
				m.Advance(at)
				_ = m.View()
			}
		}
	})
}

// BenchmarkSearchKeyHugeFolder は巨大なフォルダの中の検索の打鍵 (`99` から backspace + 1 文字 = 空でない検索語での一致の引き直し 2 回。
// 空欄へ戻す打鍵は列を作らないので、比べる前後で同じ数の引き直しになるよう空欄を通らない)。
func BenchmarkSearchKeyHugeFolder(b *testing.B) {
	eachHugeSize(b, func(b *testing.B, m *Model) {
		m.HandleKey("/")
		m.HandleKey("9")
		m.HandleKey("9")
		for range b.N {
			m.HandleKey("backspace")
			m.HandleKey("9")
		}
	})
}

// BenchmarkSearchStepHugeFolder は検索中の tab (次の一致へ)。
func BenchmarkSearchStepHugeFolder(b *testing.B) {
	eachHugeSize(b, func(b *testing.B, m *Model) {
		m.HandleKey("/")
		m.HandleKey("9")
		for range b.N {
			m.HandleKey("tab")
		}
	})
}

// BenchmarkReadHugeFolder はフォルダを読む 1 回 (木の load とライブ更新の readSig)。ファイル 9 割・フォルダ 1 割。
func BenchmarkReadHugeFolder(b *testing.B) {
	for _, n := range hugeSizes {
		dir := b.TempDir()
		for i := range n {
			p := filepath.Join(dir, fmt.Sprintf("e%06d", i))
			var err error
			if i%10 == 0 {
				err = os.Mkdir(p, 0o755)
			} else {
				err = os.WriteFile(p, nil, 0o644)
			}
			if err != nil {
				b.Fatal(err)
			}
		}
		b.Run(fmt.Sprintf("load/n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				nd := &node{abs: dir, dir: true}
				nd.load()
				if len(nd.kids) != n {
					b.Fatalf("読めた %d 件", len(nd.kids))
				}
			}
		})
		b.Run(fmt.Sprintf("readSig/n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if len(readSig(dir)) != n {
					b.Fatal("件数が違う")
				}
			}
		})
	}
}

// BenchmarkViewScreenSizes は止まった画面 1 フレーム (View) を画面の大きさごとに測る (木は小さい。描画は画面のセル数に比例する)。
func BenchmarkViewScreenSizes(b *testing.B) {
	for _, sz := range [][2]int{{80, 24}, {200, 50}, {300, 80}} {
		b.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(b *testing.B) {
			m := hugeFolderModel(b, 20)
			m.Resize(sz[0], sz[1])
			for at, end := m.lastAdv, m.lastAdv.Add(time.Second); at.Before(end); {
				at = at.Add(16 * time.Millisecond)
				m.Advance(at)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = m.View()
			}
		})
	}
}

// BenchmarkLiveUpdateAllChangedHugeFolder は、開いた巨大なフォルダの全部の項目が 1 周で変わったライブ更新の取り込み 1 回。
func BenchmarkLiveUpdateAllChangedHugeFolder(b *testing.B) {
	eachHugeSize(b, func(b *testing.B, m *Model) {
		big := m.cur.parent
		names := make([]string, 0, len(big.kids))
		for _, k := range big.kids {
			names = append(names, k.raw)
		}
		for range b.N {
			m.applyChange(dirChange{dir: big.abs, changed: names})
		}
	})
}
