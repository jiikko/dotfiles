# 683 (chore): skill の README の動作確認の例を `./tmp` に寄せる

起票日: 2026-10-08

## 概要

`_claude/skills/zundamon-kaisetsu/README.md` の動作確認の例 (Setup の後の「動作確認」の 3 行) が `/tmp/zk` を使っている。
一時ファイルは `./tmp` に置くという規約 (`~/.claude/CLAUDE.md` の「一時ファイルの配置」) からずれている。

## 対応方針

- 例のパスを `./tmp/zk` に変える (3 行。`mkdir -p` / `synth` / `build`)

## 受け入れ条件

- [ ] README の例が `./tmp` を使う
