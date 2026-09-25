package store

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// HeldFile は人が意図して dispatcher を止めた印 (`pro-con dispatcher --stop`。issue 459)。印がある間は、画面は dispatcher を起こさない
// (開いたときも、keeper の起こし直しも)。外すのは画面の明示の操作 (c) か、手で dispatcher を起動したとき。
// 🚨 画面の quit で止めたとき (最後の画面) は置かない (次に開いたら今までどおり起こす)。
const HeldFile = "dispatcher-held"

// Hold は止めた印を置く (置いた時刻を中に書く。あれば上書き)。
func Hold(dir string, at time.Time) error {
	return writeAtomic(filepath.Join(dir, HeldFile), []byte(at.Format(time.RFC3339)+"\n"))
}

// Held は止めた印があるか。読めない (権限等) ときは、ある側に倒す (人が止めたものを起こし直さない)。
func Held(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, HeldFile))
	return !errors.Is(err, os.ErrNotExist)
}

// Release は止めた印を外す。外したら真 (無ければ偽)。
func Release(dir string) (bool, error) {
	err := os.Remove(filepath.Join(dir, HeldFile))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
