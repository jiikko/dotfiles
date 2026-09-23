// Package widthenv は「glogx が支持しない幅 env」の検出とその文言を 1 箇所に集める。
//
// x/ansi は method.go の init で RUNEWIDTH_EASTASIAN を読み、East Asian Ambiguous
// (罫線 ─│・ブロック要素 ░▒▁・矢印・… 等) を幅 2 として数える。glogx の UI はこれらの
// グリフで枠・影・区切り線を組み立てており、しかも複数箇所が「グリフ数 = 表示幅」を前提に
// strings.Repeat で埋めているため、この env が真だと枠が要求幅の 2 倍近くまで膨らむ
// (実測 2026-08-15: 80 桁指定のパネルが 157〜158 桁。issue 054)。
//
// 支持しない判断をしたので、せめて**黙って壊れない**ようにする: 実行時は起動直後に警告を出し、
// テストは幅系が 28 本まとめて落ちる代わりに TestMain で 1 本の説明付き失敗にする。
//
// main と issues の両方 (とそれぞれのテスト) から参照するため独立パッケージにしてある。
// termsafe に相乗りさせないのは、あちらが「外部由来文字列の無害化」の関門で、env による
// 幅モデルの判定は別の関心事だから (パッケージの責務を混ぜない)。
package widthenv

import (
	"fmt"
	"os"
	"strconv"
)

// Name は x/ansi が読む env の名前。x/ansi 側の実装 (method.go の init) と一致している
// 必要があるので、ここを変えるときは向こうを読み直すこと。
const Name = "RUNEWIDTH_EASTASIAN"

// Message は実行時警告とテストの失敗で共用する文言。1 箇所に置いて食い違いを防ぐ。
const Message = Name + " が設定されています。glogx はこの env を支持しません " +
	"(罫線・ブロック要素が幅 2 になり、枠・影・区切り線が崩れます)。unset して実行してください。"

// EastAsianAmbiguous は x/ansi が Ambiguous を幅 2 として数える設定になっているかを返す。
// 判定は x/ansi の init と同じ strconv.ParseBool で行う (真偽の解釈をずらさないため。
// 例: "1"/"true"/"TRUE" は真、"0"/"" /不正値は偽)。
func EastAsianAmbiguous() bool {
	ea, err := strconv.ParseBool(os.Getenv(Name))
	return err == nil && ea
}

// ExitIfUnsupported はテストの入口 (TestMain) から呼ぶガード。支持しない env の下では
// テストを 1 本も走らせず、理由を言って落ちる。
//
// 幅に依存するパッケージの TestMain は**全て**これを呼ぶこと (main / issues / usage /
// widthenv)。1 つでも呼び忘れると、そのパッケージだけが「幅 1 を前提にした assert が
// 大量に落ちる生ログ」を吐き、この仕組みが無くそうとしている混乱がそこから漏れる
// (実測 2026-08-15: widthenv 自身が呼んでおらず 6 行の生ログを出していた)。
func ExitIfUnsupported() {
	if !EastAsianAmbiguous() {
		return
	}
	fmt.Fprintln(os.Stderr, Message)
	os.Exit(1)
}
