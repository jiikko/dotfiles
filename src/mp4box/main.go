// Command mp4-effective-size は、MP4 のトップレベルの box を先頭から辿り、壊れていない box の大きさの合計 (実効サイズ) をバイト数で出す。
//
//	mp4-effective-size <file>
//
// どの box からも参照されない末尾のごみ (中断した書き込みの残り等) を数えないための値で、zshlib/_concat_helpers.zsh の
// concat の出力サイズの診断 (__concat_mp4_effective_size) が使う。読めない・壊れているときは 0 を出す (診断を飛ばす合図)。
// 常に rc=0 で数を 1 行出す (呼び出し側は数だけを読む)。Python 版 (python3 -c) を置き換えた (issue 670)。
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: mp4-effective-size <file>")
		os.Exit(2)
	}
	fmt.Println(effectiveSize(os.Args[1]))
}

// effectiveSize はトップレベルの box の大きさの合計。次のどれかに当たったら、そこで数えるのをやめる (それまでの合計を返す):
// ヘッダが 8 バイトに満たない / 64 bit の大きさが読めない / 型が印字できる ASCII でない / 大きさが 8 未満かファイルの外へ出る。
// 大きさ 0 は「ファイルの終わりまで」、1 は直後の 64 bit が大きさ (ISO/IEC 14496-12 §4.2)。開けない・読めない (ディレクトリを含む) なら 0。
func effectiveSize(path string) uint64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0
	}
	fsize := uint64(st.Size())
	var total, pos uint64
	hdr := make([]byte, 8)
	for pos < fsize {
		if _, err := f.ReadAt(hdr, int64(pos)); err != nil {
			if err == io.EOF {
				break
			}
			return 0
		}
		size := uint64(binary.BigEndian.Uint32(hdr[:4]))
		typ := hdr[4:8]
		switch size {
		case 1:
			ext := make([]byte, 8)
			if _, err := f.ReadAt(ext, int64(pos)+8); err != nil {
				if err == io.EOF {
					return total
				}
				return 0
			}
			size = binary.BigEndian.Uint64(ext)
		case 0:
			size = fsize - pos
		}
		for _, b := range typ {
			if b < 0x20 || b >= 0x7f {
				return total
			}
		}
		if size < 8 || size > fsize-pos {
			return total
		}
		total += size
		pos += size
	}
	return total
}
