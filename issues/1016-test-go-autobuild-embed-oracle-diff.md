# 1016 (test): go_autobuild の embed の展開を、本物の go list と突き合わせる差分テストを足す

起票日: 2026-10-31

## 概要

`bin/lib/go_autobuild.zsh` の `_go_autobuild_embed_files` (Go の `path.Match` の pattern を zsh の glob へ写して展開する) を、本物の `go list -f '{{.EmbedFiles}}'` と同じ入力で比べるテストを足す。retro 1015 の提案 D。
