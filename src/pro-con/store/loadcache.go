package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"time"
)

// Load の読み直しを省く (issue 528)。dispatcher の Tick 1 回は記録を十数回読み、画面は 3 秒ごと + 知らせのたびに読む。
// 完了して 24 時間以内のカードが溜まる日は記録が数百 KB になり、その解析 (json.Unmarshal) が Tick と refresh の大半を占めた。
//
// 不変条件: Load が返す State は、呼んだ時点のファイルの中身を解析した結果と同じで、呼び手がどう書き換えてもほかの呼び手に漏れない。
//   - 同じ中身か: 記録の書き手は writeAtomic (一時ファイル → rename) だけなので、書くたびに inode が変わる。(inode, 大きさ, 更新時刻) が
//     前に読んだときと同じなら中身も同じとみなす。鍵は開いた fd から取る (stat と read の間に差し替わっても、鍵と中身がずれない)
//   - 漏れない: キャッシュの State は外へ出さず、返すたびに深い複製 (deepCopy) を渡す
//
// 🚨 書き手の Update と Apply はキャッシュを使わず必ず読み直す (readState)。古い記録の上に書く事故の芽を、鍵の判定に預けない。

type fileKey struct {
	ino  uint64
	size int64
	mod  time.Time
}

func keyOf(fi os.FileInfo) fileKey {
	k := fileKey{size: fi.Size(), mod: fi.ModTime()}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		k.ino = st.Ino
	}
	return k
}

type cachedState struct {
	key fileKey
	st  State
}

var loadCache = struct {
	sync.Mutex
	m map[string]cachedState // 記録のパス → 最後に解析した中身
}{m: map[string]cachedState{}}

// maxCached は覚えておく記録の数。本番のプロセスが読む記録は 1 つ (テストは dir ごとに別の記録を読むので、溜めずに捨てる)。
const maxCached = 4

// loadDecodes はキャッシュに無くて記録を解析した回数 (テストがキャッシュに当たったかを見る)。
var loadDecodes int

// Load は記録を読む。無ければ空の記録。壊れていたらエラー (空と区別する)。
// ファイルが前に読んだときから変わっていなければ解析し直さず、前の結果の複製を返す (返した State は呼び手が自由に書き換えてよい)。
func Load(dir string) (State, error) {
	path := filepath.Join(dir, StateFile)
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{NextID: 1}, nil
	}
	if err != nil {
		return State{}, err
	}
	loadCache.Lock()
	c, ok := loadCache.m[path]
	loadCache.Unlock()
	if ok && c.key == keyOf(fi) {
		return cloneState(c.st), nil
	}
	st, key, err := readState(path)
	if err != nil {
		return st, err
	}
	loadCache.Lock()
	if len(loadCache.m) >= maxCached {
		clear(loadCache.m)
	}
	loadCache.m[path] = cachedState{key: key, st: st}
	loadDecodes++
	loadCache.Unlock()
	return cloneState(st), nil
}

// readState は記録を読んで解析する (キャッシュを見ない)。鍵は読んだ fd から取る。
func readState(path string) (State, fileKey, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{NextID: 1}, fileKey{}, nil
	}
	if err != nil {
		return State{}, fileKey{}, err
	}
	defer func() { _ = f.Close() }() // 読むだけ (閉じる失敗で中身は変わらない)
	fi, err := f.Stat()
	if err != nil {
		return State{}, fileKey{}, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return State{}, fileKey{}, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fileKey{}, fmt.Errorf("カードの記録 (%s) を読めない: %w", path, err)
	}
	if st.NextID < 1 {
		st.NextID = 1
	}
	return st, keyOf(fi), nil
}

// loadFresh は書き手 (Update / Apply) の読み。キャッシュを見ずに必ず読み直す。
func loadFresh(dir string) (State, error) {
	st, _, err := readState(filepath.Join(dir, StateFile))
	return st, err
}

// cloneState は st の深い複製 (slice・pointer・map の先まで新しく持つ)。カードの型に欄が増えても追従するよう reflect で辿る。
// 🚨 非公開の欄は浅く写す (time.Time の *Location は変わらないので共有してよい)。記録の型に参照を持つ非公開の欄を足さない
// (TestLoadedStateHasNoUnexportedRefs が落とす)
func cloneState(st State) State {
	var out State
	deepCopy(reflect.ValueOf(&out).Elem(), reflect.ValueOf(st))
	return out
}

func deepCopy(dst, src reflect.Value) {
	switch src.Kind() {
	case reflect.Slice:
		if src.IsNil() {
			dst.SetZero()
			return
		}
		n := src.Len()
		s := reflect.MakeSlice(src.Type(), n, n)
		if hasRef(src.Type().Elem()) {
			for i := range n {
				deepCopy(s.Index(i), src.Index(i))
			}
		} else {
			reflect.Copy(s, src)
		}
		dst.Set(s)
	case reflect.Pointer:
		if src.IsNil() {
			dst.SetZero()
			return
		}
		p := reflect.New(src.Type().Elem())
		deepCopy(p.Elem(), src.Elem())
		dst.Set(p)
	case reflect.Map:
		if src.IsNil() {
			dst.SetZero()
			return
		}
		m := reflect.MakeMapWithSize(src.Type(), src.Len())
		for it := src.MapRange(); it.Next(); {
			v := reflect.New(src.Type().Elem()).Elem()
			deepCopy(v, it.Value())
			m.SetMapIndex(it.Key(), v)
		}
		dst.Set(m)
	case reflect.Struct:
		dst.Set(src)
		t := src.Type()
		for i := range t.NumField() {
			if f := t.Field(i); f.IsExported() && hasRef(f.Type) {
				deepCopy(dst.Field(i), src.Field(i))
			}
		}
	case reflect.Array:
		for i := range src.Len() {
			deepCopy(dst.Index(i), src.Index(i))
		}
	case reflect.Interface:
		if src.IsNil() {
			dst.SetZero()
			return
		}
		v := reflect.New(src.Elem().Type()).Elem()
		deepCopy(v, src.Elem())
		dst.Set(v)
	default:
		dst.Set(src)
	}
}

var refTypes sync.Map // reflect.Type → bool

// hasRef は t の値を代入で写したとき、公開の欄のどこかに共有される参照 (slice・pointer・map・interface) が残るか。
func hasRef(t reflect.Type) bool {
	if v, ok := refTypes.Load(t); ok {
		return v.(bool)
	}
	var r bool
	switch t.Kind() {
	case reflect.Slice, reflect.Pointer, reflect.Map, reflect.Interface:
		r = true
	case reflect.Array:
		r = hasRef(t.Elem())
	case reflect.Struct:
		for i := range t.NumField() {
			if f := t.Field(i); f.IsExported() && hasRef(f.Type) {
				r = true
				break
			}
		}
	default: // 数・文字列など、代入で写せば共有の残らないもの
	}
	refTypes.Store(t, r)
	return r
}
