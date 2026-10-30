# 1013 (bug): go_autobuild の再ビルドの判定が go:embed で焼き込んだファイルを見ない

起票日: 2026-10-31

## 概要

`bin/lib/go_autobuild.zsh` の入力集合 (と glogx の `autobuildSourcesNewer`) は `.go` と go.mod / go.sum だけを見るので、`//go:embed` で焼き込んだファイル (pro-con の `pm-guide.md` など) だけを直しても古いバイナリが起動する。本文は反証レビューの後に書く。
