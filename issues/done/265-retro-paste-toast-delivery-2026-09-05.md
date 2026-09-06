# 265 retro: ペースト通知の実装で「動く場所に届いていない」を 2 回やった (2026-09-05)

起票日: 2026-09-05
種別: retro
関連: [248](248-human-verify-ctrl-v-paste.md) / commit `ffe49966` `8bc7062d` `25b6060b` /
`scripts/tmux_paste_clipboard.sh` / `bin/tmux-toast`

## 何をしたセッションか

issue 248 (C-v の直接ペーストの実機確認) を人が判定できるようにするため、貼った直後に
バイト数のトーストを出す機能を足した。3 commit。fable の敵対レビューを 2 周通し、
P2 4 件を反映した。

## 反省

### 1. push しただけで「直った」と報告した (2 回)

worktree で作業 → `origin/master` へ push → **`~/dotfiles` へ pull していない**。
tmux が実際に読むのは `~/dotfiles/scripts/...` と `~/dotfiles/bin/...` なので、
ユーザーが reload しても**古い版が動き続けていた**。ユーザーから「出ない」と
2 回言われ、2 回目でようやく `git status -sb` の `behind 1` に気づいた。

同型の罠が repo に既にある: `.claude/rules/worktree-per-session.md` は
「`_claude/` の変更は master へ push するまで動作確認できない」と書いているが、
**`scripts/` と `bin/` も同じ**ことに触れていない (hook と rules だけの話に読める)。
worktree を既定にした以上、**「push = 反映」ではない対象は `_claude/` に限らない**。

切り出し先の提案: `.claude/rules/worktree-per-session.md` へ 1 節追記
(「`~/dotfiles` の実体パスから起動されるものは pull するまで反映されない」。対象は
`_claude/` だけでなく `scripts/` `bin/` も。統合の手順を push → **`git -C ~/dotfiles pull --rebase`**
までを 1 セットにする)。新規ルールは立てない (発動点は既存ルールと同じ「この repo で作業を始める」)。

### 2. 「実機で確認した」の中身が隔離サーバだった

隔離サーバ (`-L`) では最初から出ていたので「出る」と報告したが、**ユーザーの環境で出るか**は
別問題だった (項目 1 が原因)。隔離サーバの成功を本番の成功として報告している。
`verify-execution-not-just-exit-code.md` の「その機構を外したら観測結果は変わるか」は通していたが、
**観測した対象が本番ではなかった**。

切り出し先の提案: `_claude/rules/verify-execution-not-just-exit-code.md` へ 1 行追記
(「隔離環境での成功は本番の成功ではない。ユーザーの環境で動く経路を触ったなら、
その経路の実体 (pull 済みか / 稼働中のサーバが読む版か) を確認してから報告する」)。

### 3. ユーザーの「ペーストしたら」を確認せずに C-v と読み替えた

issue 248 の文脈で `C-v` と解釈して実装し、**⌘V では出ない**という前提を提案に書かなかった。
ユーザーの主戦場が ⌘V なら成果物はほぼ無価値だった。1 行の確認で防げた。

切り出し先の提案: 却下 (既存の CLAUDE.md「依頼されたスコープをそのまま完遂する」の
「解釈が分かれると成果物が実質的に変わるときだけ確認する」がまさにこれ。ルールは足りていて、
守れなかっただけ。ルールを増やしても同じことが起きる)。

### 4. 変異検証の復元で自分の変更を消した / `git stash` を使った

- `git checkout -- <path>` の変異ループで、**未コミットの実装を丸ごと消して**書き直した
  (`mutation-verify-new-tests.md` が「復元の作法」で名指ししている形をそのまま踏んだ)
- プローブの書き損じで `git stash` を実行した (`~/.claude/CLAUDE.md`「Git 禁止操作」違反)。
  即座に `stash pop` で戻したので実害なし

切り出し先の提案: 却下 (どちらも既存ルールが明記済み。「変異の前に commit する」は
`mutation-verify-new-tests.md` に、stash 禁止は CLAUDE.md にある)。

### 5. 観測の設計を 3 回やり直した (押下待ちのポーリング)

`sort -u` をパイプ終端に置いたため、**ループが終わるまで 1 行も書き出されない**ログを作った
(10 分待つ設計)。その前も 40 秒 / 60 秒の固定窓で「その間に押してください」を 2 回外している。
人の操作を待つ観測は、**時間窓ではなく追記型のログ**にすべきだった。

切り出し先の提案: 新規 issue にはしない。`_claude/rules/instrument-before-second-fix.md` の
観測手段の表へ 1 行 (「人の操作を待つ観測は時間窓で区切らず、追記型のログにする
(`sort`/`uniq` をパイプ終端に置くとループ終了まで書き出されない)」)。

## うまく機能したもの (記録のみ)

- fable の敵対レビュー 2 周で P2 が計 4 件出た。特に「-F で偽の閉じ通知が出る」は
  自分の変更が**新規に**作った到達経路で、レビュー無しでは気づけなかった
  (`adversarial-review-own-safeguards.md` §7「指摘を直した差分にもう 1 周」の実例)
- P2-1 を直す過程で、レビューが指摘していない**別の原因** (read-modify-write の競合で
  toast pane の id が消える) が実験から出た

## 残課題

- [x] 項目 1 → `.claude/rules/worktree-per-session.md` に「push は反映ではない」節を追記
- [x] 項目 2 → `_claude/rules/verify-execution-not-just-exit-code.md` に
      「隔離環境での成功は本番での成功ではない」節を追記
- [x] 項目 5 → `_claude/rules/instrument-before-second-fix.md` の観測手段の表に 1 行追記

ルールに落ちた項目の正本は `_claude/rules/` 側 (retro に要約を残さない)。
- [x] 項目 3 却下: 既存ルールで足りている (守れなかっただけ)
- [x] 項目 4 却下: 既存ルールが明記済み
