package filer

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

	"atomicfile"
)

// history.go は `!` のコマンドの履歴をプロセスと dir をまたいで残す。保存先は設定と同じ置き場の treefiler-history (configDir)。
//
// 形式は 1 行 1 コマンドで、コマンドを strconv.Quote した文字列 (改行・制御文字を含んでも 1 行に収まる)。
// 🚨 打つたびに O_APPEND で 1 行足す。「閉じるときにまとめて書く」にしない: Close は F を閉じる・他の画面へ移るたびに呼ばれ、
// 逆にシグナル・panic で落ちたときは呼ばれない。「読む → 足す → 全体を書く」(places.go) にもしない: treefiler を 2 つ開いていると
// 後から書いた方が先の方の履歴を全部消す。開いている間に他方が足した分は、次に Model を作るときに読まれる。
// 履歴にはコマンドの中身 (パス・引数) がそのまま残るので 0600 にする (既にあるファイルも書くたびに直す)。先頭が空白のコマンドは残さない
// (zsh の HIST_IGNORE_SPACE と同じ逃げ道。判定は unicode.IsSpace で、全角空白で始まるものも残さないが、そのコマンドはシェルでも走らない)。
// 続けて同じコマンドもファイルには毎回足し、読むときに畳む (メモリの上の直前と比べて足さないと、他方が間に打ったとき打ち直したコマンドが残らない)。

const (
	historyKeep    = 200      // 読むのは末尾のこの件数だけ
	historyLineMax = 64 << 10 // これより長い行は読み飛ばす (壊れたファイル・巨大な 1 行で全部を抱えない)
)

func historyPath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "treefiler-history")
}

// readHistory は末尾 historyKeep 件を古い順に返す。続けて同じコマンドは 1 件にする (2 つの treefiler が交互に足すと続くことがある)。
// lines はファイルの行数 (切り詰めて書き直すかの判断に使う)。読めない・戻せない行は捨てて読み進める (1 行ずつ独立なので)。
func readHistory(path string) (hist []string, lines int) {
	if path == "" {
		return nil, 0
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for {
		line, ok, present, err := readLimitedLine(r)
		if present {
			lines++
		}
		if ok {
			if cmd, uerr := strconv.Unquote(line); uerr == nil && cmd != "" && (len(hist) == 0 || hist[len(hist)-1] != cmd) {
				hist = append(hist, cmd)
				if len(hist) > 2*historyKeep { // 末尾だけ残す (読みながら捨てて、全部を抱えない)
					hist = append(hist[:0:0], hist[len(hist)-historyKeep:]...)
				}
			}
		}
		if err != nil {
			break
		}
	}
	if len(hist) > historyKeep {
		hist = hist[len(hist)-historyKeep:]
	}
	return hist, lines
}

// readLimitedLine は 1 行を読む。present = 行があった (読み飛ばした行も含む)、ok = 使える行 (historyLineMax 以下)。
func readLimitedLine(r *bufio.Reader) (line string, ok, present bool, err error) {
	var b strings.Builder
	over, seen := false, false
	for {
		chunk, isPrefix, rerr := r.ReadLine()
		if len(chunk) > 0 {
			seen = true
		}
		if !over {
			if b.Len()+len(chunk) > historyLineMax {
				over = true
				b.Reset()
			} else {
				b.Write(chunk)
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return b.String(), seen && !over, seen, io.EOF
			}
			return "", false, false, rerr
		}
		if !isPrefix {
			return b.String(), !over, true, nil
		}
	}
}

// appendHistory は cmd を 1 行足す。失敗はコマンドの実行を止めない (履歴は付け足しの機能なので知らせも出さない)。
func appendHistory(path, cmd string) {
	if r, _ := utf8.DecodeRuneInString(cmd); path == "" || cmd == "" || unicode.IsSpace(r) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	// O_NOFOLLOW: 履歴の場所が symlink なら追記しない (先のファイルへ書き、chmod してしまう。読む側の loadHistory も symlink を辿らない)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return
	}
	_ = f.Chmod(0o600)                              // OpenFile の mode は作るときにしか効かない
	_, _ = f.WriteString(strconv.Quote(cmd) + "\n") // 1 回の write で書く (O_APPEND の追記が他方の行と混ざらない)
	_ = f.Close()
}

// loadHistory は Model を作るときに履歴を読む。ファイルが上限の 2 倍の行を超えていたら末尾 historyKeep 件で書き直す
// (読んでから書き直すまでの間に他方が足した 1 行は消えうる。頻度が低いので受ける)。
func loadHistory() []string {
	path := historyPath()
	hist, lines := readHistory(path)
	if lines > 2*historyKeep {
		var b strings.Builder
		for _, c := range hist {
			b.WriteString(strconv.Quote(c) + "\n")
		}
		// 🚨 symlink を辿って link 先を書き直さない: link 先が設定の dir の外 (別の道具のファイル) だと、その中身を切り詰めて消す。
		// rename は link を普通のファイルに置き換えるだけで、link 先は残る
		_ = atomicfile.Write(path, []byte(b.String()), 0o600)
	}
	return hist
}
