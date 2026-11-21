# 1104 (chore): レビューの係が OS の一時フォルダに残した空の dir を片付ける

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-21〜）

起票日: 2026-11-21

## 概要

issue 1077 の 5 本目の敵対的レビューの係 (2026-11-21) が、最初のハーネスが途中で落ちたときの空の dir `/var/folders/hw/ngj8nvkn78s5p_v5cgbfwcb80000gn/T/p5rt-mtnm22f6/` を残した (2026-11-21 07:41 に中身が空なのを確かめた)。
`$TMPDIR` の直下を消す操作は hook が止めるので、`cleanup-tmp-dir` skill (`bin/tmpclean`) で承認を取って片付ける。消したら done へ移す。
