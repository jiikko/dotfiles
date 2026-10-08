// Command termwidth は、引数の文字列の端末での表示幅 (セル数) を出す。幅は termwidth.Of (tuikit の幅の単一情報源) で数える。
//
//	termwidth <文字列>
//
// シェルから幅を測るための入口で、bin/tmux-toast が通知の枠の大きさを決めるのに使う (issue 670 で python3 の
// unicodedata.east_asian_width を置き換えた)。
package main

import (
	"fmt"
	"os"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: termwidth <文字列>")
		os.Exit(2)
	}
	fmt.Println(termwidth.Of(os.Args[1]))
}
