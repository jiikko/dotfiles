// 予約一覧の受け渡し。シェルが TSV (id / 発火 epoch / 送り先の表示名 / 文字列) を書いたファイルを
// 読むだけにして、job ファイルの書式を知るのはシェル側の 1 箇所に保つ。
package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

type job struct {
	id    string
	at    time.Time
	label string
	text  string
}

// readJobs は TSV を読む。壊れた行は黙って捨てる (一覧が出ないより、出せる分を出す方がよい)。
func readJobs(path string) ([]job, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []job
	// 🚨 bufio.Scanner を使わない: 1 行でも上限を超えると Scan が止まり、以降の行を読めないまま
	//    エラーになる。呼び出し側はそれを「一覧が読めない」= UI 起動失敗として扱うので、
	//    長い予約が 1 件あるだけで一覧も取消も二度と開けなくなる (監査 2026-08-28 で再現)。
	//    Reader なら長すぎる行だけを捨てて残りを読める (壊れた行を捨てる既存の方針と同じ)。
	br := bufio.NewReader(f)
	for {
		line, err := readLimitedLine(br)
		if line == "" && err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return out, err
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 4 {
			continue
		}
		epoch, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || fields[0] == "" {
			continue
		}
		out = append(out, job{id: fields[0], at: time.Unix(epoch, 0), label: fields[2], text: fields[3]})
		if err != nil {
			break
		}
	}
	return out, nil
}

// maxJobLine は 1 行の上限 (バイト)。これを超える行は丸ごと捨てる。
const maxJobLine = 64 * 1024

// readLimitedLine は 1 行読む。上限を超えた行は読み飛ばして空文字を返す (err は EOF のときだけ非 nil)。
func readLimitedLine(br *bufio.Reader) (string, error) {
	var b strings.Builder
	tooLong := false
	for {
		chunk, isPrefix, err := br.ReadLine()
		if b.Len()+len(chunk) > maxJobLine {
			tooLong = true
		} else {
			b.Write(chunk)
		}
		if err != nil {
			if tooLong {
				return "", err
			}
			return b.String(), err
		}
		if !isPrefix {
			if tooLong {
				return "", nil
			}
			return b.String(), nil
		}
	}
}
