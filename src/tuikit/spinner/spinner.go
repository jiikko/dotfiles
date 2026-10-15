// Package spinner は回る印 (点字の 10 コマ) の字形と、何番目のコマを出すかの選び方を持つ。
//
// いつ描き直すか・いつ止めるか (tick の回し方) は各画面が持つ。回すものがある間だけ tick し、無くなったら止める規律は
// 画面ごとに違う (pro-con は回すカードの有無、treefiler は読み込み中か) ので、ここでは状態も tea.Cmd も持たない。
package spinner

import "time"

// frames の点字は端末で幅が揺れない 1 桁 (termwidth の注記)。
var frames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Len はコマの数。
const Len = len(frames)

// Frame は i 番目のコマ。Len を超える i や負の i も回して選ぶ (呼び出し側が増やし続ける整数をそのまま渡してよい)。
func Frame(i int) string {
	return frames[(i%Len+Len)%Len]
}

// At は時刻 now に出すコマ。step ごとに 1 コマ進む。描き直しの回数に依らず、同じ時刻なら同じコマになる。
// step が 0 以下なら最初のコマを返す (回さない)。
func At(now time.Time, step time.Duration) string {
	if step <= 0 {
		return frames[0]
	}
	return Frame(int((now.UnixNano() / int64(step)) % int64(Len)))
}
