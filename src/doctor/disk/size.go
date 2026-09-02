package disk

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Item は走査した 1 パス (公開: glogx の doctor 画面が内訳を描く)。Dev / Ino は ④ の削除直前に Lstat を取り直して同一性を確かめるための札。
type Item struct {
	Path  string    `json:"path"`
	Size  int64     `json:"size"`
	Files int       `json:"files"`
	Mtime time.Time `json:"mtime"`
	Dev   uint64    `json:"dev"`
	Ino   uint64    `json:"ino"`
}

// duSize は `du -sk` と一致するサイズを返す。
//
//   - Stat_t.Blocks (512 バイト単位) を足す。見かけのサイズではないので sparse file は小さく、
//     APFS clone / ハードリンクは実占有で数える
//   - (dev, ino) で dedupe する (ハードリンクを 2 回数えない)。du と同じ
//   - symlink は辿らない (リンク自身のブロックだけ数える)。WalkDir の既定
//   - 権限エラー等は握り潰さず error で返す (呼び出し側が「走査できず」にする)
//   - ctx の期限で中断する (1 エントリ 60 秒の上限)
func duSize(ctx context.Context, root string, seen map[[2]uint64]struct{}) (Item, error) {
	it := Item{Path: root}
	fi, err := os.Lstat(root)
	if err != nil {
		return it, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return it, errors.New("実体の stat 情報 (Stat_t) を取れない")
	}
	it.Mtime = fi.ModTime()
	it.Dev, it.Ino = uint64(st.Dev), uint64(st.Ino) //nolint:unconvert // Dev は platform で型が違う
	n := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return fmt.Errorf("%s: %w", p, werr)
		}
		n++
		if n%256 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		s, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("%s: 実体の stat 情報 (Stat_t) を取れない", p)
		}
		key := [2]uint64{uint64(s.Dev), uint64(s.Ino)} //nolint:unconvert
		if _, dup := seen[key]; dup {
			return nil
		}
		seen[key] = struct{}{}
		it.Size += int64(s.Blocks) * 512
		if !d.IsDir() {
			it.Files++
		}
		return nil
	})
	return it, err
}
