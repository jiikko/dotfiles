# 583 (retro): retro 579 の提案の切り出しと issue 580 (bin/mutate-verify-list)

起票日: 2026-09-29

## 概要

2026-09-29 のセッションの後半の振り返り。retro 579 の提案 A・B をルールへ追記して C を issue 580 に切り出し、続けて 580 を
実装した (`bin/mutate-verify-list`。一覧の変異を 1 本ずつ `bin/mutate-verify` に渡して表にする)。敵対レビューは opus 2 周。
前半 (glogx のパスジャンプと issue 577) は retro 579。

## どこで踏んだか / 何が回りくどかったか

1. **「bash 5 系と 3.2 の両方で通った」と報告したが、実際には道具を 3.2 でしか走らせていなかった**。self-test は
   `bash tests/…` (PATH の bash = 5.3) と `/bin/bash tests/…` (3.2) で 2 回起動したが、テストは道具を `"$MVL"` で直接
   起動しており、道具は毎回 shebang の `/bin/bash` (3.2) で走っていた。2 周目の敵対レビューが、bash 5 でだけ起きる素通り
   (下の 2) を実測して初めて分かった。テストを起動した版と、検査対象が走った版は別物
2. **`( set -e; … ) || die` と書き、bash 5 では中の set -e が効いていなかった**。`||` の左 (条件文脈) に置いたサブシェルでは、
   POSIX どおりの bash は set -e を無効にする (3.2 は source した行だけ偶然止まる)。同じ仕組みの注意は
   `mutation-verify-new-tests.md` に「条件文脈から呼ばれる関数の中の assert は拾われない」としてあったが、それは**テストの assert**
   の項で、**守りの側 (本体のガード) を書くとき**には思い出さなかった
3. ログの置き場のガードを「repo の中なら拒否」と見た目の条件で書き、自分の使い方 (ignore された `tmp/` の下) を弾いた。
   rc=9 の原因は「repo の中」ではなく「untracked の差分として出る」ことで、条件をその仕組みへ絞り直した
4. 1 周目の敵対レビューの P1 3 件 (途中の書き損じ / LIST の `exit 0` / LIST の `set -e`) は、どれも「LIST を親のプロセスで
   そのまま source している」ことが根で、1 件ずつ塞がずにサブシェルへ隔離して 1 回で閉じられた (うまくいった点だが、
   最初の設計の時点で「信頼する入力でも、書き損じの影響を親へ漏らさない」を問えば 1 周目の P1 は出なかった)
5. worktree で `cd <worktree> && git commit` と打つたびに、PostToolUse の git state の hook が**本体の checkout** の state
   (無関係な未 push の commit 20 件以上) を出した。hook は `git -C` の行き先は拾うが、`cd X &&` は拾わない。cwd は本体のまま

## 次に効きそうな改善 (提案)

- **A. 「複数の版 (bash / Go / Python) で通った」と書く前に、検査対象そのものがその版で走ったことを出力で確かめる**
  (項目 1)。テストの起動に使った版は、子プロセス (shebang・PATH で決まる) の版を決めない。版が効く検査なら、テストの側で
  検査対象を版ごとに明示して起動し、走った版を出力に出す
  - 切り出し先: `_claude/rules/verify-execution-not-just-exit-code.md` の「隔離環境での成功は、本番での成功ではない」へ追記
    (「動かしたものは本番で読まれるものか」の版の軸)
- **B. set -e を効かせたいブロック (サブシェル・関数) を、条件文脈 (`||` / `&&` / `if` / `!`) に置かない。rc は後で見る**
  (項目 2)。テストの assert の項 (`mutation-verify-new-tests.md`) と同じ仕組みを、守りの側の書き方として足す
  - 切り出し先: `_claude/rules/adversarial-review-own-safeguards.md` §2 の shell の罠の並び (`set -e` の下の trap・`grep -q` の
    SIGPIPE の項の隣) へ追記。発動点は「守りを書くとき」で、mutation-verify の「テストを書くとき」と違う

却下 / 局所:

- 項目 3: §8 の「迂回を直すたびに新しい迂回が出るなら、構文でなく効果へ軸を移す」と同じ形 (見た目の条件 → 仕組みの条件)。
  既存の規範で足りる
- 項目 4: 局所 (今回の道具の設計)。一般化すると「信頼する入力でも影響を隔離する」だが、脅威モデルの節 (§8 ①②) を書く段で
  問う内容と同じなので新しい規範にしない
- 項目 5: 局所。worktree の commit は `git -C <worktree> commit` の形で打てば hook が行き先を拾う (hook の冒頭の注記どおり)。
  次から `git -C` で書く

## 残課題

- [x] A: verify-execution-not-just-exit-code.md の「隔離環境での成功は、本番での成功ではない」へ追記した
- [x] B: adversarial-review-own-safeguards.md §2 へ仕組みの正本として追記し、mutation-verify-new-tests.md の条件文脈の項は
  そこを参照する形にした (同じ規範を 2 か所に書かない)

## 進捗

- 2026-09-29 起票。反証レビュー (sonnet 1 本): 指摘なし。提案 A・B の追記先の実在・既存の記述との重複なし・項目 5 の hook の
  挙動 (`lib/git_cmd_detect.sh` が `cd X && git commit` を拾わないと明記)・580 の実装との一致は、どれも反証できなかった
- 2026-09-29 ユーザー指示で A・B を適用した。残課題が空になったので done へ
