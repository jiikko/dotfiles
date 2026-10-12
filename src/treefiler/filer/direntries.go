package filer

import (
	"errors"
	"os"
	"time"
)

// direntries.go はフォルダの中身を、名前と各項目の属性 (種類・mtime・大きさ) ごと読む (木の load・ライブ更新の readSig・配下の走査が使う)。
// macOS では getattrlistbulk(2) で 1 回の syscall にまとめる (direntries_darwin.go)。使えないとき (未対応の FS 等) は
// os.ReadDir + 項目ごとの lstat (DirEntry.Info) に戻る。どちらの経路でも属性は項目そのもの (symlink は辿らない = lstat) の値。
// 5 万件のフォルダで 128ms → 43ms (issue 714 の実測)。

// entryAttr は項目そのもの (lstat) の属性のうち、木が使うもの。
type entryAttr struct {
	dir   bool
	mtime time.Time
	size  int64
}

// dirEntry はフォルダの中の 1 項目。
type dirEntry struct {
	name string
	entryAttr
}

func attrOf(info os.FileInfo) entryAttr {
	return entryAttr{dir: info.IsDir(), mtime: info.ModTime(), size: info.Size()}
}

// errNoBulk はまとめて読む経路が使えない (この OS に無い・FS が対応していない)。readEntries は今の形に戻る。
var errNoBulk = errors.New("getattrlistbulk を使えない")

// readBulk は差し替え口 (テストが失敗の経路を作る)。
var readBulk = readEntriesBulk

// readEntries は dir の中身を読む。並びは決めない (呼び出し側が並べる)。読む間に消えた項目は落とす。
// フォルダ自体が読めないときの error は os.ReadDir と同じもの (まとめて読む経路の失敗は今の形でもう 1 度試してから返す)。
func readEntries(dir string) ([]dirEntry, error) {
	if es, err := readBulk(dir); err == nil {
		return es, nil
	}
	return readEntriesSlow(dir)
}

// readEntriesSlow は今の形 (os.ReadDir + 項目ごとの lstat)。
func readEntriesSlow(dir string) ([]dirEntry, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]dirEntry, 0, len(ents))
	for _, e := range ents {
		info, err := e.Info() // DirEntry.Info は symlink そのものを返す (lstat)
		if err != nil {
			continue // 読む間に消えた
		}
		out = append(out, dirEntry{name: e.Name(), entryAttr: attrOf(info)})
	}
	return out, nil
}
