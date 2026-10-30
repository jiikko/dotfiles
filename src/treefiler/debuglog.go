package main

import (
	"bytes"
	"encoding/json"
	"os"
	"time"

	"treefiler/filer"
)

// debugEnv は内部の状態を 1 行ずつ書くログの置き場 (tools/tfperf の -snap が隔離した tmux で起動するときに渡す。
// Claude が実物を動かして、画面の文字ではなく状態の値で動作を判定するため)。
const debugEnv = "TREEFILER_DEBUG"

// debugLog は Update のたびに filer.DebugState を JSON で 1 行書く。nil のとき何もしない。
// tick で状態が前の行と同じなら書かない (動いている間は 1 秒に 60 行になり、読み手が変化を探しにくい)。
// 書くたびにファイルへ出す (溜めない): 読み手は treefiler が動いている間に読む。debug のときだけなので所要は問わない。
type debugLog struct {
	f    *os.File
	now  func() time.Time
	last []byte
}

// debugLine は 1 行の形。状態の欄は filer.DebugState をそのまま平らに並べる。
type debugLine struct {
	T   int64  `json:"t"`   // unix ns (tfperf が打鍵を送った時刻と同じ時計)
	Msg string `json:"msg"` // key / tick / other
	Key string `json:"key"` // msg が key のときの名前 (それ以外は "-")
	filer.DebugState
}

func openDebugLog(path string, now func() time.Time) (*debugLog, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &debugLog{f: f, now: now}, nil
}

func (d *debugLog) write(msg, key string, s filer.DebugState) {
	if d == nil {
		return
	}
	state, err := json.Marshal(s)
	if err != nil {
		return
	}
	if msg == "tick" && bytes.Equal(state, d.last) {
		return
	}
	d.last = state
	line, err := json.Marshal(debugLine{T: d.now().UnixNano(), Msg: msg, Key: key, DebugState: s})
	if err != nil {
		return
	}
	_, _ = d.f.Write(append(line, '\n')) // debug のログの書き損じで本体を止めない
}

func (d *debugLog) Close() error {
	if d == nil {
		return nil
	}
	return d.f.Close()
}
