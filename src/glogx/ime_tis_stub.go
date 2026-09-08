//go:build !darwin || !cgo

// TIS 直接呼び出し (ime_tis_darwin.go) の非 darwin / 非 cgo スタブ。常に「取れない/失敗」を
// 返し、IME 自動切替を安全に無効化する。
//
// 🚨 **repo は macOS 専用だが、このファイルは消せない** (issue 316 で判定)。
// 相方の `ime_tis_darwin.go` は `//go:build darwin && cgo` なので、**`CGO_ENABLED=0` で
// ビルドすると黙って除外され**、ここが無いと未定義シンボルになる
// (`.github/workflows/_go-project.yml` が `CGO_ENABLED: "1"` を明示しているのはこの罠のため。
// 同ファイルのコメントが「IgnoredGoFiles に落ちて黙って除外される」と警告している)。
// どの lint / test lane もこのファイルをコンパイルしないので、**中身の退行は誰も検出しない**。
// 触るときは `CGO_ENABLED=0 go build ./...` を手で 1 回通すこと。
package main

func tisCurrentSourceID() (string, bool) { return "", false }

func tisSelectSourceID(string) bool { return false }
