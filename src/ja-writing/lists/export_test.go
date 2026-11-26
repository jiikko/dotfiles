package lists

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
)

// Names は埋め込んだ一覧のファイル名 (fact-gaps.txt など) を昇順で返す。
func Names() []string {
	entries, err := fs.ReadDir(files, "data")
	if err != nil {
		// embed の中身はビルドで固定されるので、読めないのはビルドの誤り。
		panic(fmt.Sprintf("lists: 埋め込んだ data を読めません: %v", err))
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// SHA256 は一覧のファイルの中身の SHA-256 を 16 進で返す。
func SHA256(name string) (string, error) {
	b, err := Read(name)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
