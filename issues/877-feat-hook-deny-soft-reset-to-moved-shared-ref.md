# 877 (feat): 自分の起点より先へ進んだ共有の ref への `git reset --soft` を hook で止める

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-22〜）

起票日: 2026-10-22

## 概要

`git reset --soft origin/master` を、自分の commit の起点が `origin/master` より古いまま打つと、index が「新しい origin/master + 自分の木
(古い起点)」になり、他のセッションの commit を打ち消す差分が入る。気づかずに commit すると上流の変更を巻き戻す。
2026-10-22 に 2 つのセッションで踏んだ (retro 875 の踏んだ所 1・retro 876 の踏んだ所 4。どちらも commit の前に気づいた)。

規範は `_claude/rules-checklists/commit-with-pathspec.md` (「squash / 付け替えの soft reset の起点は…先に新しい起点の hash へ rebase」) に既にあり、
`commit-with-pathspec.md` は soft reset の前にこのチェックリストを Read せよと言うが、2 回とも読まずに打った。読むことに頼らず、PreToolUse(Bash) の
hook で機械的に止める。

## 対応方針

- `_claude/hooks/` に PreToolUse(Bash) の hook を 1 本足す (形は `deny-piped-push-then-destroy.sh` に倣う: 冒頭に脅威モデルと検出しない形を書く、
  `_claude/settings.json` の command は実体パス、テストは `tests/claude/`)
- 対象: Bash のコマンドに `git [オプション…] reset --soft <共有の ref>` (`origin/…` などの remote-tracking ref) がある形
- 字面だけで止めない: hook の中で `git merge-base --is-ancestor <その ref> HEAD` を打ち、**その ref が HEAD の祖先でないときだけ** deny する。
  rebase を済ませた正しい手順 (ref が祖先) は通す。`-C <dir>` / `cd <dir> &&` で対象の repo が変わる形は、その dir で判定する (読めなければ判定不能として通すか止めるかを決める)
- deny の文で「先に `git rebase <ref>` してから、その ref へ soft reset する (`commit-with-pathspec.md` のチェックリスト)」を案内する
- 変異で red: 祖先の判定を外す (正しい手順まで止める) / 判定を常に真にする (事故の形を通す)。opus の敵対的レビューを最終ゲートにする
  (自作の安全機構。`adversarial-review-own-safeguards.md`)

## 未確認

- 正当な使い方で止めてしまう形が無いか (別の remote 名・`@{u}`・`FETCH_HEAD`・hash 直書き・detached の worktree)。実装のときに洗う
- 反証レビューは起票の時点では通していない (5h 枠の都合で、実装と一緒にリセットの後に回す)。通すまでは「反証されていない仕様」として扱う

## 関連ファイル

- `_claude/hooks/deny-piped-push-then-destroy.sh` (形の手本)・`_claude/settings.json` の PreToolUse
- `_claude/rules-checklists/commit-with-pathspec.md` (規範の正本)
- issues/done/875・876 (起点の retro)

## 進捗

- [ ] hook の実装とテスト
- [ ] 変異で red を確認
- [ ] 敵対的レビュー (opus)
- [ ] 反証レビュー (この issue の本文)
