# 683 (chore): skill の README の動作確認の例を `./tmp` に寄せる

起票日: 2026-10-08

## 概要

`_claude/skills/zundamon-kaisetsu/README.md` の動作確認の例 (105〜107 行目の 3 行) が `/tmp/zk` を使っている。
SKILL.md の手順 3 は作業ディレクトリを `./tmp/<名前>/` としていて、README の例だけが違う。
(`~/.claude/CLAUDE.md` の「一時ファイルの配置」は Claude がセッション中に作る成果物の規約で、利用者向けの例には直接かからない。
揃える理由は、同じ skill の中で置き場の例が 2 通りあること)

## 対応方針

- 例のパスを `./tmp/zk` に変える (3 行。`mkdir -p` / `synth` / `build`)

## 受け入れ条件

- [x] README の例が `./tmp` を使う

## 進捗

- 2026-10-08: README の動作確認の例を `./tmp/zk` にした。同じ機会に、Setup の必要なものの表に mermaid の図の Node (`npx`) を足した
  (Setup の点検で見つかった。`check` が `npx` を見ない点は、表に「使うときに `npx --version` で確かめる」と書いて済ませた)
