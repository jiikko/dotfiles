package main

// 常駐時のヒープ/goroutine の推移を外から採るための計測フック。
//
// 既定では完全に不活性で、環境変数 GLOGX_PROBE_DIR が空でないときだけ signal を待ち受ける。
// net/http/pprof を使わない理由: TUI バイナリに net/http を持ち込むと常駐リスナーと
// バイナリ膨張が増えるだけで、欲しいもの (差分を採れる heap profile) は runtime/pprof で
// 足りる。取得は `go tool pprof -base <t0> <t1>` で差分を見る運用。
//
//	GLOGX_PROBE_DIR=./tmp/soak glogx &
//	kill -USR1 $pid   # heap profile + MemStats 1 行 (直前に GC。live set を測るため)
//	kill -USR2 $pid   # goroutine profile (本数と内訳)
//
// ⚠️ SIGUSR1 は runtime.GC() を伴うので、これを撃つこと自体が GC 頻度を変える。
// 「撃った時点の live set」を比較する用途に限る (GC 間隔そのものを測る用途には使えない)。

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync/atomic"
	"syscall"
	"time"
)

// probeSeq は書き出したファイル名を撃った順に並べるための連番。
var probeSeq atomic.Int64

// startProbe は GLOGX_PROBE_DIR があるときだけ計測 signal の待ち受けを始める。
// TUI が端末を占有しているので出力は一切 stdout/stderr へ出さず、失敗も
// <dir>/probe_error.log へ落として黙って続ける (計測が本体を落とさない)。
func startProbe() {
	dir := os.Getenv("GLOGX_PROBE_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	ch := make(chan os.Signal, 8)
	signal.Notify(ch, syscall.SIGUSR1, syscall.SIGUSR2)
	go func() {
		for sig := range ch {
			n := probeSeq.Add(1)
			var err error
			switch sig {
			case syscall.SIGUSR1:
				err = probeHeap(dir, n)
			case syscall.SIGUSR2:
				err = probeGoroutine(dir, n)
			}
			if err != nil {
				probeLogError(dir, err)
			}
		}
	}()
}

// probeHeap は GC 後の heap profile と MemStats の 1 行を書く。GC を挟むのは
// 「まだ回収されていないゴミ」と「本当に生き残っているオブジェクト」を分けるため
// (Darwin は解放後もページが RSS に残るので、RSS だけでは live set が見えない)。
func probeHeap(dir string, n int64) error {
	runtime.GC()
	name := filepath.Join(dir, fmt.Sprintf("heap_%03d.prof", n))
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	if err := pprof.WriteHeapProfile(f); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return probeAppend(dir, "memstats.log", fmt.Sprintf(
		"seq=%d t=%s heap_inuse=%d heap_objects=%d heap_alloc=%d "+
			"total_alloc=%d sys=%d num_gc=%d goroutines=%d stack_inuse=%d file=%s\n",
		n, time.Now().Format(time.RFC3339), ms.HeapInuse, ms.HeapObjects, ms.HeapAlloc,
		ms.TotalAlloc, ms.Sys, ms.NumGC, runtime.NumGoroutine(), ms.StackInuse,
		filepath.Base(name)))
}

// probeGoroutine は goroutine profile (debug=1 = スタックごとの本数つき) を書く。
// SIGQUIT と違ってプロセスを落とさないので、シナリオの前後で撃って比較できる。
func probeGoroutine(dir string, n int64) error {
	name := filepath.Join(dir, fmt.Sprintf("goroutine_%03d.txt", n))
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	if err := pprof.Lookup("goroutine").WriteTo(f, 1); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return probeAppend(dir, "memstats.log", fmt.Sprintf("seq=%d t=%s goroutines=%d file=%s\n",
		n, time.Now().Format(time.RFC3339), runtime.NumGoroutine(), filepath.Base(name)))
}

func probeAppend(dir, base, line string) error {
	f, err := os.OpenFile(filepath.Join(dir, base), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// probeLogError は計測側の失敗を記録する。ここで失敗を握り潰すと「撃ったのに
// ファイルが無い」を「変化が無かった」と読み違えるため、痕跡は必ず残す。
func probeLogError(dir string, cause error) {
	f, err := os.OpenFile(filepath.Join(dir, "probe_error.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	fmt.Fprintf(f, "t=%s err=%v\n", time.Now().Format(time.RFC3339), cause)
	f.Close()
}
