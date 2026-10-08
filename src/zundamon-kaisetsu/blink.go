package main

import (
	"hash/fnv"
	"math"
	"slices"
	"strconv"
)

// まばたき: blinkCell 秒ごとの区間に 1 回、blinkClosed 秒だけ目を閉じる。区間の中の位置はキャラと区間から決まる
// 疑似乱数で、[blinkMinOffset, blinkCell - blinkMinOffset) に置く (続くまばたきの間隔は 2〜6 秒)。
// 時刻だけから決まる (乱数の状態を持たない) ので、シークしても、HTML と mp4 で撮り直しても同じ所でまばたく。
const (
	blinkCell      = 4.0
	blinkClosed    = 0.12
	blinkMinOffset = 1.0
)

// blinkBit は、目を閉じているキャラのビット列 (PlayerData.Blinks) でキャラ key を表すビット (castOrder の i 番目が 1<<i)。
// プレイヤーは castData.BlinkBit でこの値を受け取る。
func blinkBit(key string) int {
	return 1 << slices.Index(castKeys(), key)
}

// blinkAt はキャラ key が時刻 t に目を閉じているか。
func blinkAt(key string, t float64) bool {
	if t < 0 {
		return false
	}
	cell := math.Floor(t / blinkCell)
	h := fnv.New64a()
	_, _ = h.Write([]byte(key + "/" + strconv.FormatFloat(cell, 'f', 0, 64)))
	frac := float64(h.Sum64()>>11) / float64(1<<53) // [0, 1)
	at := cell*blinkCell + blinkMinOffset + frac*(blinkCell-2*blinkMinOffset)
	return t >= at && t < at+blinkClosed
}

// blinkRuns は、目を閉じているキャラのビット (blinkBit) を、変わり目だけの列 [開始フレーム, ビット] にする。
//
// 目を閉じるのは、そのフレームで見せている表情に閉じ目の版があるときだけ (blinkable[キャラ][表情])。見せている表情は
// プレイヤーと同じく、字幕の行 (frames の行) の faces から引く。誰もまばたかないなら nil (データに載せない)。
// frames (frameRuns の出力) は Python 版と同じ形のまま残すため、まばたきは別の列に持つ。
func blinkRuns(frames [][4]int, timeline []timelineLine, blinkable map[string]map[string]bool, duration float64) [][2]int {
	keys := castKeys()
	n := max(1, int(math.Ceil(duration*mouthFPS)))
	var runs [][2]int
	ri, blinked := 0, false
	for k := range n {
		for ri+1 < len(frames) && frames[ri+1][0] <= k {
			ri++
		}
		li := -1
		if len(frames) > 0 {
			li = frames[ri][1]
		}
		t := (float64(k) + 0.5) / mouthFPS
		mask := 0
		for _, key := range keys {
			face := defaultFace
			if li >= 0 && li < len(timeline) {
				face = timeline[li].Faces[key]
			}
			if blinkable[key][face] && blinkAt(key, t) {
				mask |= blinkBit(key)
			}
		}
		blinked = blinked || mask != 0
		if len(runs) == 0 || runs[len(runs)-1][1] != mask {
			runs = append(runs, [2]int{k, mask})
		}
	}
	if !blinked {
		return nil
	}
	return runs
}
