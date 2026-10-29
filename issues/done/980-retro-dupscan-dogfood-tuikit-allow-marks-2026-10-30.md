# 980 (retro): tuikit の dupscan-dogfood と、sweep・allow-group の修正 (966・973)

起票日: 2026-10-30

## 概要

2026-10-29〜30 のセッションの振り返り。一時領域の掃除・tuikit の dupscan-dogfood (966)・その所見の修正と、レビューで出た P2 の修正 (973)。

## どこで踏んだか / 何が回りくどかったか

1. **自分の修正の実物の確認を 1 つの起点でしか打たなかった**。sweep の allow-group の食い違いの表示を tuikit を起点にだけ確かめて push し、
   treefiler を起点にすると正しい印 4 本を誤報した (966 F4)
2. **挙動を「起点が repo 全体か」で分ける設計を、CI が実際に渡す起点を見ずに決めた**。CI と make の gate は起点 `src` で打つので、
   正しい L2 の印まで効かなくなる。敵対的レビュー (opus) の P1 で取り下げ、ユーザーに候補 2 つを諮って作り直した (973)
3. **敵対的レビューの指摘を直した差分に、また P1 が出た** (966 で 2 周。数え方を直すたびに別の入力で数が崩れた)。最初に「状態 × 読み手」の表に当たる
   「消える経路 × 節」の表を作っていれば 1 周で済んだ可能性がある
4. **残骸と読んで止めた隔離 tmux サーバが、走行中の `make test-lint` のものだった可能性がある**。コマンド行に自分の worktree のパスがあり PPID=1 だったので残骸と読んだが、
   起動時刻は 1 分 24 秒前で、止めた run より新しかった
5. **検査をシェルの `&` で起こした** (Bash ツールの背景機能を使わず、完了の通知が来ない)。後から done のファイルを待つ待ちを別に張った
6. **push の取り合いで 3〜4 回続けて負けた** (並行セッションが dupscan を触っていて、rebase のたびに検査を回し直した)。
   最後は「rebase → 取り込んだ差分が dupscan に触れるなら検査 → push」を 3 回まで繰り返す 1 本のコマンドにして 1 回目で通った

## 提案と決着

| # | 一般形 | 切り出し先 | 決着 |
|---|---|---|---|
| 1 | 範囲 (起点・対象 dir) を取る道具の修正は、範囲を変えた 2 通りで実物を確かめる | skill dupscan-dogfood の 4 節 | 対応済み (commit「skill(dupscan-dogfood): 修正の実物の確かめは、module の dir と repo root の両方の起点で打つ」)。rule への一般化は却下: `verify-execution-not-just-exit-code.md` の「観測できた経路と、主張する範囲を一致させる」が既に言っている |
| 2 | 既にある入力で挙動を分ける条件を足すときは、production・CI・make が渡す値でどちらの枝に入るかを先に並べる | `_claude/rules/survey-receiver-guards-before-passing-new-values.md` に追記 | 対応済み (この issue と同じ commit) |
| 3 | 指摘ごとに直すと周回が増える | 却下: `survey-receiver-guards-before-passing-new-values.md` の「状態 × 読み手の表を先に作る」が既に言っている (守らなかっただけ) |  |
| 4 | 残骸として止める前に、起動時刻を自分の run と突き合わせる | `_claude/rules/parallel-write-agents-need-worktree-isolation.md` の残骸の項に追記 | 対応済み (この issue と同じ commit) |
| 5 | 背景の検査はツールの背景機能で起こす | 却下: `verify-execution-not-just-exit-code.md` の「自分が起動した子は wait して rc を取る」と、背景の完了を成果物で判定する規律で足りる (不注意) |  |
| 6 | push の取り合いは再試行を 1 本のコマンドにする | 却下: `verify-execution-not-just-exit-code.md` の non-fast-forward の再試行の項 (触れる範囲を数えて検査を回す) が既にある。1 本にまとめるのは局所の工夫 |  |

残課題なし (全項目が対応済みか却下)。
