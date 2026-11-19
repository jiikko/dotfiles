package check

import (
	"errors"
	"os"
	"syscall"
	"time"

	"ja-writing/internal/pyjson"
)

// RecordStatus は _read_record_status の 1 つ目の値。
type RecordStatus string

const (
	RecordMissing RecordStatus = "missing"
	RecordBad     RecordStatus = "bad"
	RecordOK      RecordStatus = "ok"
)

const emptyRecordWait = 50 * time.Millisecond

// ReadRecordStatus は _read_record_status。大きさ 0 のファイルは書いている途中とみなし、50 ミリ秒待って 1 回だけ読み直す。
// 無いのは ENOENT だけ (本家の FileNotFoundError)。ほかの読めない理由は bad。
// 入れ子が深すぎる JSON は本家では RecursionError で落ちる (捕まえていない)。ここでは error で返し、呼び出し側が落とす。
func ReadRecordStatus(path string) (RecordStatus, *pyjson.Object, error) {
	var data []byte
	for attempt := range 2 {
		read, err := os.ReadFile(path)
		if errors.Is(err, syscall.ENOENT) {
			return RecordMissing, nil, nil
		}
		if err != nil {
			return RecordBad, nil, nil
		}
		data = read
		if len(data) > 0 {
			break
		}
		if attempt == 0 {
			time.Sleep(emptyRecordWait) // sleep-ok: 本家の待ち: 書きかけの記録を読み直す前の間隔は本家と同じ 50 ミリ秒
		}
	}
	if len(data) == 0 {
		return RecordBad, nil, nil
	}
	value, err := pyjson.LoadsBytes(data)
	if err != nil {
		var loadErr *pyjson.Error
		if errors.As(err, &loadErr) && loadErr.IsValueError() {
			return RecordBad, nil, nil
		}
		return "", nil, err
	}
	record, ok := value.(*pyjson.Object)
	if !ok {
		return RecordBad, nil, nil
	}
	return RecordOK, record, nil
}

// ReadRecord は _read_record。読めなければ (無い・空・JSON でない・dict でない) nil。
func ReadRecord(path string) (*pyjson.Object, error) {
	_, record, err := ReadRecordStatus(path)
	return record, err
}
