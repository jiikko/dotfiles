# 1104 (chore): レビューの係が OS の一時フォルダに残した空の dir を片付ける

起票日: 2026-11-21

## 概要

issue 1077 の 5 本目の敵対的レビューの係 (2026-11-21) が、最初のハーネスが途中で落ちたときの空の dir `/var/folders/hw/ngj8nvkn78s5p_v5cgbfwcb80000gn/T/p5rt-mtnm22f6/` を残した (2026-11-21 07:41 に中身が空なのを確かめた)。
issue 1102 の敵対的レビューも `$TMPDIR/tmp.EpG1Lb7GJ3` (中身は out.txt・err.txt・repo/.git・repo/sub。再現の残り) を残したので、一緒に片付ける。
`$TMPDIR` の直下を消す操作は hook が止めるので、`cleanup-tmp-dir` skill (`bin/tmpclean`) で承認を取って片付ける。消したら done へ移す。

## 進捗

- 2026-11-26: ユーザーが「消していいよ」と答えた。`bin/tmpclean plan -kinds tmpdir -tmpdir-days 3` は $TMPDIR の 3 日より古い 2,839 件を候補にし、2 つだけを選ぶ口が無い。
  plan をこの 2 つに絞ると apply が「plan の中身が ID と合わない」で止める (書き換えの検出)。ゴミ箱へ移す操作も hook (一時領域の浅い所を消す・移すのを止める) が止めた。
  どちらも意図して守られた所なので回避しない。ユーザーが端末で 2 つを消すのを待つ (中身は確かめた: p5rt-mtnm22f6 は空、tmp.EpG1Lb7GJ3 は
  out.txt・err.txt・repo/.git・repo/sub。どちらも開いているプロセスは無い)
- 2026-11-26: ユーザーが端末で 2 つを消した。`ls -d` で 2 つとも「No such file or directory」になったのを確かめた。残りなし
