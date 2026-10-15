// Package flockmark は「開いている間 flock を持ち続ける印」(<id>.lock) の作り方と、作りかけの残りの見分け方を持つ。
// presence (画面が開いている印) と relay (画面の中継の印) が使う。他人の印をどう確かめるか (EX を取って消す / SH で見るだけ) は呼び出し側が持つ。
package flockmark

import (
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// StaleTmp は作りかけの印 (.tmp-<id>) を落ちた画面の残りとみなすまでの時間 (作ってから rename するまでは一瞬)。
const StaleTmp = time.Minute

const tmpPrefix = ".tmp-"

// Create は d の中に、flock (LOCK_EX) を持った印 <id>.lock を置き、開いたファイルと id と印のパスを返す。id は idBytes バイトの乱数の hex。
// body が nil でなければ、印の名前にする前にその中身を書く (数える側には書き終えた印しか見えない)。
//
// 🚨 数える対象にならない名前 (.tmp-<id>) で作って flock してから、印の名前へ rename する。印の名前で作ってから flock すると、その間は
// 読む側に「誰も持っていない印」に見え、消される (presence: 敵対的レビューで 20000 回中 146 回再現。O_EXLOCK でも open の中で作成と lock の
// 間に割り込まれて再現した) か、読む側が共有の flock を取ってこちらの flock が EWOULDBLOCK で失敗する (relay: 敵対的レビュー 2026-09-25 P2)。
func Create(d string, idBytes int, body func(id string) ([]byte, error)) (*os.File, string, string, error) {
	b := make([]byte, idBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, "", "", err
	}
	id := hex.EncodeToString(b)
	tmp := filepath.Join(d, tmpPrefix+id)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, "", "", err
	}
	fail := func(err error) (*os.File, string, string, error) {
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, "", "", err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail(err)
	}
	if body != nil {
		data, err := body(id)
		if err == nil {
			_, err = f.Write(data)
		}
		if err != nil {
			return fail(err)
		}
	}
	p := filepath.Join(d, id+".lock")
	if err := os.Rename(tmp, p); err != nil {
		return fail(err)
	}
	return f, id, p, nil
}

// IsTmp は name が作りかけの印の名前か (数える対象にしない)。
func IsTmp(name string) bool { return strings.HasPrefix(name, tmpPrefix) }

// IsStaleTmp は e が StaleTmp より古い作りかけの印か (作っている最中の新しいものには触らない)。
func IsStaleTmp(e fs.DirEntry) bool { return IsTmp(e.Name()) && Stale(e) }

// Stale は e が普通のファイルで、StaleTmp より古いか (FIFO などは開くと止まるので偽)。
func Stale(e fs.DirEntry) bool {
	if !e.Type().IsRegular() {
		return false
	}
	info, err := e.Info()
	return err == nil && time.Since(info.ModTime()) > StaleTmp
}
