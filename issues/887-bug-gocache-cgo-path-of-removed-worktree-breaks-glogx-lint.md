# 887 (bug): 消した worktree のパスが go のビルドキャッシュに残り、glogx の lint が手元で落ちる

起票日: 2026-10-23

## 概要

`make lint` (src/glogx) が `gofmt: unable to open file /Users/koji/wt-874/src/glogx/ime_tis_darwin.go` で落ちる。本文はレビューの後に入れる。
