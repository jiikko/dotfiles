# 410 (retro): settings.json のキー順 churn を SessionStart hook で畳んだセッションの振り返り

起票日: 2026-09-21

## 概要

`_claude/settings.json` のキー順が Claude Code の書き込みのたびに入れ替わる件に対し、
既存の `normalize-settings.sh` にキーソートを足して SessionStart hook へ配線した
(fca113fe / 3a546a40 / 39738535)。その作業中の気づき。

反証レビューは通していない。1 項目の retro で、提案先も既存ルールへの追記 1 本に
収まっているため (issue 運用規約の「対象外」の趣旨)。

## 気づき

### 1. A-B の「片腕」が no-op で、対照実験が何も確かめていなかった

hook が実際に発火することを確かめるため「settings.json のキー順をわざと崩す →
headless セッションを起こす → 直っているか見る」という A-B を組んだ。崩す式を

    jq -S 'to_entries | reverse | from_entries'

と書いたが、**`-S` は出力時にキーをソートし直すので reverse を打ち消す**。ファイルは
1 バイトも変わらず、tree は clean のまま。「崩した」つもりで走らせた対照は、
実際には「ソート済みの入力を渡して、ソート済みで返ってきた」だけだった。

気づけたのは `git status` が想定と違って空だったから。**崩しに揮発キーの追加も
混ぜていたので、最初の周は「dirty → clean になった」という別の理由で red/green が
付いてしまい、順序については何も言えていなかった**。

一般形: **A-B の片腕 (崩し・fixture・変異体) を作ったら、走らせる前に「その腕が
本当に違う状態になっているか」を assert する。** 片腕が作れていない A-B は、
結果がどちらに出ても「機構の有無で結果が変わらない観測」になる。

### 切り出し先

**[`_claude/rules/verify-execution-not-just-exit-code.md`](../../_claude/rules/verify-execution-not-just-exit-code.md)
への追記**（新規ルールは立てない）。

同ファイルの「『有無で結果が変わらない観測』を証拠に数えない」節に、発動点違いの
1 項として足すのが素直:

- 既存節の発動点は「観測結果を**読む**瞬間」
- 今回の発動点は「**崩し / fixture を作った**瞬間」(読む前に空振りが確定している)
- [`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md) の
  手順 1.6 (変異の diff を目視する) が同型だが、**あちらはコードへの変異が発動点**で、
  live な A-B の入力づくりには効かなかった (実際に読んでいたのに踏んだ)

追記する規範の案:

> 崩し・fixture・変異体を作ったら、走らせる前に「作る前と違う状態になった」ことを
> 機械で確認する (`diff -q` / 対象の値を読み直す)。整形ツールを通して作る場合は、
> **その整形が崩しを打ち消さないか**を特に見る (`jq -S` は出力時にキーを
> ソートし直すので、`to_entries|reverse|from_entries` を無効化する)。

## 未確認リスク

- **並行 SessionStart の read-modify-write**。hook は毎 SessionStart (headless の
  `claude -p` や subagent 由来のセッションを含む) に settings.json を読み書きする。
  書き戻しは 1 回の `cat >` で、内容が変わらなければ書かないので、交錯しても最悪
  「もう一周ぶんの churn」で壊れはしないはずだが、実測はしていない。
  **trigger**: settings.json が壊れた形 (途中で切れた JSON 等) で観測されたら、
  並行 SessionStart の read-modify-write を疑う。

## 進捗

- [x] 気づき 1 を一般形に引き上げて切り出し先を提案
- [x] `verify-execution-not-just-exit-code.md` への追記 (2026-09-21)
  - 「有無で結果が変わらない観測を証拠に数えない」節へ 1 項を追加。発動点は
    「A-B の片腕を作った瞬間」で、既存項 (観測を読む瞬間) と区別して書いた
  - 起票時の案に **負の対照** (「別の config を見せた hook では直らない」を挟む) を足した。
    片腕が崩れていることを確認しても、「勝手に直った」possibility は別に潰す必要がある
  - 経緯・実例 (`jq -S` が `reverse` を打ち消した実コマンドと、最初の周で別の理由で
    緑が付いた話) は `_claude/rules-rationale/verify-execution-not-just-exit-code.md` へ
- [x] 未確認リスク (並行 SessionStart の read-modify-write) は trigger つきで本文に残した。
  実測はしていないので、この issue を閉じても観測ポイントとしては生きている

残タスクなし。
