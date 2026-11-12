// Package lists は本家 yomiyasu の語の一覧 (references/lists/*.txt の 8 本) を埋め込んで持つ。
//
// lint・diff・check が共通に読む。中身は本家と同じバイト列で、改行の変換を .gitattributes で止めている。
// check の記録はここで返す SHA-256 を入れる。本家との一致は oracle の固定したファイルの SHA-256 と照合するテストが守る。
package lists

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
)

//go:embed data/*.txt
var files embed.FS

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

// Read は一覧のファイルの中身を返す。無い名前はエラー。
func Read(name string) ([]byte, error) {
	b, err := files.ReadFile("data/" + name)
	if err != nil {
		return nil, fmt.Errorf("語の一覧 %q を読めません: %w", name, err)
	}
	return b, nil
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
