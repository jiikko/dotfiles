package main

import (
	"math"
	"slices"
)

// chapterCardSeconds は、チャプターの区切りのカード (舞台の中央の大きなチャプター名。issue 681) を出しておく長さ。
// チャプターの頭の 1 行目の途中で消える長さにする (長いと、その行の図解をカードが隠したままになる)。
const chapterCardSeconds = 1.5

// chapterCardRuns は、区切りのカードを出しているか (1 / 0) を、変わり目だけの列 [開始フレーム, 0 か 1] にする。
// カードはトピック名と同じ条件 (チャプターが 2 つ以上・overlays に topic) でだけ出し、出さないなら nil (データに載せない)。
// 次のチャプターが始まったら、その頭から数え直す。フレーム k の時刻はまばたきと同じく (k+0.5)/fps。
func chapterCardRuns(chapters []chapterData, overlays []string, duration float64) [][2]int {
	if len(chapters) < 2 || !slices.Contains(overlays, "topic") {
		return nil
	}
	n := max(1, int(math.Ceil(duration*mouthFPS)))
	var runs [][2]int
	ci := -1
	for k := range n {
		t := (float64(k) + 0.5) / mouthFPS
		for ci+1 < len(chapters) && chapters[ci+1].Start <= t {
			ci++
		}
		on := 0
		if ci >= 0 && t < chapters[ci].Start+chapterCardSeconds {
			on = 1
		}
		if len(runs) == 0 || runs[len(runs)-1][1] != on {
			runs = append(runs, [2]int{k, on})
		}
	}
	return runs
}
