# PATH 先頭に置く shim は、実体を絶対パスで解決してから exec する — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/path-shim-must-resolve-real-binary.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## なぜ (起源: dotfiles bench, 2026-08-21)

「hook が起動する tmux クライアント数」を数える shim を書き、中で
`exec "$TMUX_BIN_PATH" ...` と呼んだ。`TMUX_BIN_PATH` の既定値が **`"tmux"` (相対名)** で、
shim は PATH 先頭に置かれていたため自分自身に解決し、無限再帰した。`-L <socket>` が延々と
積み重なった `/bin/sh` が CPU を焼き続け、**2 分以上気づかなかった** (bench の出力はファイルへ
ブロックバッファされていて無音だった)。

同じセッションの `tests/tmux/test_mark_seen.sh` では `command -v` で絶対パス化していた。
つまり **1 つの変更の中で同型の間違いを片方だけ直した**形で、規律を持っていたのに
bench 側で落とした (CLAUDE.md「同じ間違いが別の場所にもある前提で grep する」の裏面)。
