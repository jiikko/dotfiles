# 454 (feat): push の結果をパイプに通したまま worktree を消す Bash を、PreToolUse の hook で止める

起票日: 2026-09-25

## 概要

retro 450 の 1 (と retro 266 の同じ形): `git push … | tail -1 && … && git worktree remove …` のように、push の rc をパイプの終端で読んだまま
`&&` で worktree の削除を繋ぐと、push が non-fast-forward で弾かれても後段が走り、未 push の commit を持つ worktree を消す。
規範は既にある (`_claude/rules/verify-execution-not-just-exit-code.md` の「成否で後段を走らせる `&&` のつなぎ」/ `.claude/rules/worktree-per-session.md`) が、
2026-09-05 と 2026-09-25 に同じ形を踏んだ。規範で止まらないので機械で止める (ユーザーの依頼「これもやって」で起票)。

## 詳細

- PreToolUse(Bash) の hook が、コマンド文字列に「`git push` の出力を `|` へ流している」かつ「その後に `&&` で `git worktree remove`
  (と、候補として `git branch -D` / `rm -rf`) が続く」形を見つけたら deny し、正しい形 (`git push … > log 2>&1; rc=$?; [ $rc -eq 0 ] && …`) を案内する
- 🚨 自作の検査 (字句の gate) なので、書く前に `adversarial-review-own-safeguards.md` の §8 に従い、脅威モデルと「検出しない形」をヘッダに書く。
  候補: 守るのは「Claude が 1 本の Bash に push と削除を同居させる」形だけ。変数に入れたコマンド・スクリプトの中・`;` で繋いだ形は検出しない (後者は規範の側)
- 引用符の中・コメント・heredoc の本文 (commit message) は偽陽性になりうる。`deny-bare-tmux-kill.sh` の扱い (引用符とコメントを外す) を参考にする
- テスト: deny すべき形 / 通すべき形 (rc を変数に取ってから繋ぐ・push だけ・パイプ無しの `&&`) を並べ、hook の判定を外す変異で red を見る

## 関連

- retro 450 (切り出し元) / retro 266 / `_claude/hooks/deny-bare-tmux-kill.sh` (同じ字句の PreToolUse の hook)
