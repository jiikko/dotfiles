// Package lists は本家 yomiyasu の語の一覧 (references/lists/*.txt の 8 本) を埋め込んで持つ。
//
// lint・diff・check が共通に読む。中身は本家と同じバイト列で、改行の変換を .gitattributes で止めている。
// check の記録はここで返す SHA-256 を入れる。本家との一致は oracle の固定したファイルの SHA-256 と照合するテストが守る。
package lists

import (
	"embed"
	"fmt"
)

//go:embed data/*.txt
var files embed.FS

// Read は一覧のファイルの中身を返す。無い名前はエラー。
func Read(name string) ([]byte, error) {
	b, err := files.ReadFile("data/" + name)
	if err != nil {
		return nil, fmt.Errorf("語の一覧 %q を読めません: %w", name, err)
	}
	return b, nil
}
