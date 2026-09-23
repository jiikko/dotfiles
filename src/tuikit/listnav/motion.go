// Package listnav は一覧と pager の移動 (キーの語彙・カーソル・スクロール) の部品。
// 描画フレームワークに依存しない (キーは bubbletea の KeyPressMsg.String() と同じ表記の文字列)。
package listnav

// Motion は移動の種類。キーから MotionOf で得る。
type Motion uint8

const (
	None     Motion = iota // 移動キーではない
	Down                   // 1 行下
	Up                     // 1 行上
	HalfDown               // 半ページ下
	HalfUp                 // 半ページ上
	Top                    // 先頭
	Bottom                 // 末尾
)

// MotionOf はキーを移動へ写す。移動キーでなければ None。
//
// 語彙は 2 層 (glogx の docs/glogx-ui-guide.md「キーの 3 層構造」の移動層と別名層):
//
//	         vim (一次)        emacs / 矢印 (別名。新しい意味を与えない)
//	Down     j                 ctrl+n ↓
//	Up       k                 ctrl+p ↑
//	HalfDown ctrl+d Space f    pgdown
//	HalfUp   ctrl+u b          pgup shift+space
//	Top      g                 home
//	Bottom   G                 end
//
// 🚨 画面固有の動作キー (Space = 選択、f = フィルタ 等) を持つ画面は、**それを先に捌いてから**
// ここへ渡す。この関数は語彙を 1 箇所に置くためのもので、画面ごとの例外は持たない
// (例外を持たせると、画面ごとに「どのキーが効くか」がまたずれ始める)。
//
// Space は bubbletea v1 が " "、v2 が "space" と綴るので両方を受ける (v2 へ上げたとき、" " だけを
// 見ていた glogx の Space の割当が全部無反応になった実例がある)。
//
// 🚨 shift+space は「区別して送れる端末」でしか届かない。多くの端末は shift を落として素の
// Space (= 下) を送るので、上方向の確実な経路 (b / ctrl+u / pgup) を案内から消さないこと。
func MotionOf(key string) Motion {
	switch key {
	case "j", "down", "ctrl+n":
		return Down
	case "k", "up", "ctrl+p":
		return Up
	case "ctrl+d", "pgdown", " ", "space", "f":
		return HalfDown
	case "ctrl+u", "pgup", "b", "shift+space":
		return HalfUp
	case "g", "home":
		return Top
	case "G", "end":
		return Bottom
	}
	return None
}

// Half は表示行数 rows の半ページ (最低 1 行)。
func Half(rows int) int { return max(rows/2, 1) }
