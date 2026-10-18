package build

import (
	"math"
	"slices"
)

// 区切りのカード (舞台の中央の大きなチャプター名) は、チャプターの 1 行目の音声が終わるまで出す。
// ずんだもんの質問を言い終えたところで消え、めたんの答えと図解に場所を譲る。
// 短い行でも区切りが目に入るよう最短 chapterCardMinSeconds、長い行で答えまで隠さないよう最長 chapterCardMaxSeconds。
// 1 行目に図解が出ているチャプターは最短のまま (カードはトピック名と図解を隠すので、長いと 1 行目の図解を隠したままになる)。
const (
	chapterCardMinSeconds = 1.5
	chapterCardMaxSeconds = 4.0
)

// ChapterCardEnds は、チャプターごとに区切りのカードを消す時刻 (秒) を返す。chapters と同じ順・同じ数。
func ChapterCardEnds(chapters []ChapterData, timeline []TimelineLine) []float64 {
	ends := make([]float64, len(chapters))
	for ci, c := range chapters {
		ends[ci] = c.Start + chapterCardMinSeconds
		i := slices.IndexFunc(timeline, func(l TimelineLine) bool { return l.Chapter == ci })
		if i < 0 || timeline[i].Show != nil {
			continue
		}
		ends[ci] = min(max(timeline[i].End, c.Start+chapterCardMinSeconds), c.Start+chapterCardMaxSeconds)
	}
	return ends
}

// ChapterCardRuns は、区切りのカードを出しているか (1 / 0) を、変わり目だけの列 [開始フレーム, 0 か 1] にする。
// カードはトピック名と同じ条件 (チャプターが 2 つ以上・overlays に topic) でだけ出し、出さないなら nil (データに載せない)。
// cardEnds は ChapterCardEnds の出力。次のチャプターが始まったら、その頭から数え直す。フレーム k の時刻はまばたきと同じく (k+0.5)/fps。
func ChapterCardRuns(chapters []ChapterData, cardEnds []float64, overlays []string, duration float64) [][2]int {
	if len(chapters) < 2 || !slices.Contains(overlays, "topic") {
		return nil
	}
	n := max(1, int(math.Ceil(duration*MouthFPS)))
	var runs [][2]int
	ci := -1
	for k := range n {
		t := (float64(k) + 0.5) / MouthFPS
		for ci+1 < len(chapters) && chapters[ci+1].Start <= t {
			ci++
		}
		on := 0
		if ci >= 0 && t < cardEnds[ci] {
			on = 1
		}
		if len(runs) == 0 || runs[len(runs)-1][1] != on {
			runs = append(runs, [2]int{k, on})
		}
	}
	return runs
}
