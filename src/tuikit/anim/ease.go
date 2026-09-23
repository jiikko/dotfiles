package anim

// EaseOutCubic は 0..1 の進みを easeOutCubic で写す (最初速く、終点付近で減速)。
// 着地するもの (引き出し・開く演出) に使う。減速しないと「カクッ」と止まって見える。
func EaseOutCubic(p float64) float64 {
	q := 1 - p
	return 1 - q*q*q
}

// EaseOutBack は ease-out-back (最初速く、着地の手前で少し行き過ぎて戻る)。t=0 で 0、t=1 で 1。
// 行き過ぎの量は s が決める (大きいほど大きく戻る。1.25 で半ページ移動なら 1〜2 行ぶん)。
func EaseOutBack(t, s float64) float64 {
	u := t - 1
	return 1 + u*u*((s+1)*u+s)
}
