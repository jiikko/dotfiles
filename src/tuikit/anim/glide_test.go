package anim

import "testing"

// testFrames は glogx が使うフレーム数と同じ (テストの距離・収束の目安をそれに合わせてある)。
const testFrames = 6

// advance は上下どちらの向きでも表示 offset を論理 offset へ寄せ、有限フレームで着地して
// active を下ろす (geometry 非依存)。
func TestScrollGlideConverges(t *testing.T) {
	for _, tc := range []struct{ from, to int }{
		{from: 0, to: 7},  // 下スクロール (1 コミット ~7 行)
		{from: 7, to: 0},  // 上スクロール
		{from: 3, to: 4},  // 残り 1 行
		{from: 5, to: 5},  // 動きなし → 即座に active を下ろす
		{from: 0, to: 40}, // 半ページ相当の大きな距離 (2026-07-31 で glide 対象になった)
		{from: 40, to: 0}, // 同・上向き
	} {
		var g ScrollGlide
		g.from, g.shown, g.frame, g.frames, g.active = tc.from, tc.from, 0, testFrames, true
		prevShown := g.shown
		frames := 0
		for g.active {
			g.Advance(tc.to)
			frames++
			// ease-in: 表示 offset は目標を通り越さず単調に近づく
			if (tc.to > tc.from && (g.shown < prevShown || g.shown > tc.to)) ||
				(tc.to < tc.from && (g.shown > prevShown || g.shown < tc.to)) {
				t.Fatalf("from=%d to=%d: 非単調/行き過ぎ (shown=%d)", tc.from, tc.to, g.shown)
			}
			prevShown = g.shown
			if frames > 20 {
				t.Fatalf("from=%d to=%d: 収束しない (shown=%d)", tc.from, tc.to, g.shown)
			}
		}
		if g.shown != tc.to {
			t.Errorf("from=%d to=%d: 着地 shown=%d, want %d", tc.from, tc.to, g.shown, tc.to)
		}
		if frames > testFrames {
			t.Errorf("from=%d to=%d: %d フレーム (testFrames=%d 以内のはず)", tc.from, tc.to, frames, testFrames)
		}
	}
}

// 距離に依らず所要フレーム数が一定 = 半ページでも 1 行でも同じ時間で着地する
// (フレーム数で終わる設計。距離で終わると背高コミットや半ページで間延びする)。
func TestScrollGlideDurationIndependentOfDistance(t *testing.T) {
	framesFor := func(from, to int) int {
		var g ScrollGlide
		g.Start(from, to, testFrames)
		n := 0
		for g.active {
			g.Advance(to)
			n++
		}
		return n
	}
	short, long := framesFor(0, 1), framesFor(0, 200)
	if short != long {
		t.Errorf("所要フレームが距離依存: 1 行=%d / 200 行=%d", short, long)
	}
}

func TestScrollGlideStart(t *testing.T) {
	t.Run("距離 0 は開始しない", func(t *testing.T) {
		var g ScrollGlide
		if g.Start(5, 5, testFrames) || g.active {
			t.Error("距離 0 で glide が始まった")
		}
	})

	t.Run("進行中の glide は積まず即時へ倒す", func(t *testing.T) {
		var g ScrollGlide
		if !g.Start(0, 10, testFrames) {
			t.Fatal("初回の start が false")
		}
		if g.Start(10, 20, testFrames) {
			t.Error("連打で glide が積まれた (即時に倒すはず)")
		}
		if g.active {
			t.Error("連打後も active (描画は論理 offset に戻すべき)")
		}
	})

	t.Run("進行中でも距離 0 なら進行中の glide を壊さない", func(t *testing.T) {
		var g ScrollGlide
		g.Start(0, 10, testFrames)
		if g.Start(7, 7, testFrames) {
			t.Error("距離 0 で start が true")
		}
		if !g.active {
			t.Error("距離 0 の呼び出しで進行中の glide が消えた (カーソルが画面内で動いただけ)")
		}
	})
}

// offset は glide 中だけ途中位置を返し、それ以外は論理 offset をそのまま返す。
func TestScrollGlideOffset(t *testing.T) {
	var g ScrollGlide
	if got := g.Offset(42); got != 42 {
		t.Errorf("非 glide 時 offset = %d, want 42 (論理 offset をそのまま)", got)
	}
	g.Start(0, 10, testFrames)
	if got := g.Offset(10); got != 0 {
		t.Errorf("glide 開始直後 offset = %d, want 0 (開始位置)", got)
	}
	// ease-in (t^2) なので最初の 1 フレームは距離が短いと動かない (round で 0 に落ちる)。
	// 中盤まで進めれば開始位置と着地点の間にいる。
	for range 3 {
		g.Advance(10)
	}
	if got := g.Offset(10); got <= 0 || got >= 10 {
		t.Errorf("glide 途中 offset = %d, want 0 < x < 10 (開始と着地の間)", got)
	}
	g.Stop()
	if got := g.Offset(10); got != 10 {
		t.Errorf("stop 後 offset = %d, want 10 (即時表示)", got)
	}
}

// glide 中に論理 offset が動いても (resize・追加ロード) その時点の着地点へ向かう。
func TestScrollGlideFollowsMovingTarget(t *testing.T) {
	var g ScrollGlide
	g.Start(0, 100, testFrames)
	g.Advance(100)
	// 途中で着地点が縮む (resize で maxOffset が下がった等)
	for g.active {
		g.Advance(5)
	}
	if g.shown != 5 {
		t.Errorf("動いた着地点に着かない: shown=%d, want 5", g.shown)
	}
}
