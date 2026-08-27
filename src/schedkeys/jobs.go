// 予約一覧の受け渡し。シェルが TSV (id / 発火 epoch / 送り先の表示名 / 文字列) を書いたファイルを
// 読むだけにして、job ファイルの書式を知るのはシェル側の 1 箇所に保つ。
package main

import (
	"bufio"
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
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), "\t")
		if len(fields) < 4 {
			continue
		}
		epoch, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || fields[0] == "" {
			continue
		}
		out = append(out, job{id: fields[0], at: time.Unix(epoch, 0), label: fields[2], text: fields[3]})
	}
	return out, sc.Err()
}
