# 1099 (retro): 1097 (mutate が読む obaket の出力の形の突き合わせ) の振り返り

起票日: 2026-11-19

## 概要

1097 の作業 (obaket の gate `check-mutate-format-contract` と dotfiles の `test_mutate_obaket_contract.sh`) で踏んだこと・回りくどかったことと、
他の作業に転用できる改善。

## 踏んだこと・回りくどかったこと

1. **突き合わせの検査を、本番と違う形で組んだ**。最初に書いたものは (a) 要約の区間を label `summary` (空白なし) で回し、(b) 通しの区間で
   `run-logged-command` を単独で起こしていた。本番の `make test` / `test-only` は label が `"make test"` / `"make test-only"` (どちらも空白入り) で、
   `run-with-test-profile-residue -- run-logged-command -- bin/xcodebuild-test-nonempty …` の連鎖で起こす。red team の 1 周目が、読み手が label の形に依存する変更と、
   residue が command の出力を飲み込む変更の 2 つを、緑のまま通る P2 として再現した。red team の前に自分で書き手を壊して試した 6 通り (行の文言 4 通り・`** TEST FAILED **` を落とす・末尾の窓を縮める) は全部 red だったが、
   どれも書き手 1 つの出力を変える形で、連鎖や label の形は自分が想定した不変条件の外だった。
   直した後も gate は連鎖の `bin/xcodebuild-test-nonempty` を挟まない (出す行は空振りの成功のときの 1 行だけで、読み手の判定に関わらない)。挟まない理由と、挟むべきになる条件を
   gate のコメントに書き足した (obaket commit「docs(lint): check-mutate-format-contract が xcodebuild-test-nonempty を挟まない理由を書く」)
2. **背景で lint-gates を回している間に、obaket の `bin/CLAUDE.md` を書き換えた**。既存の規範
   (`verify-execution-not-just-exit-code.md`「検証・計測がバックグラウンドで走っている間は…編集しない」) どおりにしていれば要らなかった、
   文書の gate の回し直しが 1 回増えた
3. **一時 dir を論理パス (`/var/folders/…`) のまま、別の道具が出すパスと文字列で照合した**。今の residue は HOME をそのまま使うので一致しているが、
   residue が実パスを出すようにすると偽の赤になる形だった (red team 2 周目が再現)。CLAUDE.md の「在るローカルのファイルどうしは実体で比べる」の範囲
4. **着手時の claim が別マシン (`kojiM3MBP (glogx)`) の名義で、生きているかを確かめる手段が無かった**。ListAgents には他のマシンが出ず、
   ユーザーに聞いて引き継いだ。claim の規約 (`issue-rules.d/claim-issue-in-next-and-push.md`) の「居たら勝手に始めない」どおりで、往復は 1 回で済んだ

## 提案と切り出し先

- **1 → `_claude/rules/survey-receiver-guards-before-passing-new-values.md` の「production・CI・make が実際に渡す値を先に読む」の項の下へ、同じ規範の 1 例として追記 (済み)**。
  一般形: 書き手と読み手 (別の repo・別の道具) が出力の形を突き合わせる検査は、入力 (label・パス) と起こし方 (包む wrapper の連鎖) を本番の recipe から写し、省いた wrapper は理由を書く。
  既存の項は「挙動を分ける条件を足すとき」が発動点で、検査を組む場面を名指ししていなかったので、新規ルールにせず同じ項の下に足した
- 2 → 却下: 既存の規範がそのまま当てはまり、書き足しても同じ文の重複になる。守らなかっただけ
- 3 → 却下: CLAUDE.md の「不具合対応の原則」の「パスの文字列は表記の揺れ・symlink で一致しない」が既に言っている。gate は `cd -P` で直した
- 4 → 却下: 局所 (claim の規約どおりに動いて 1 回の確認で済んだ)

## 反証レビュー (2026-11-19、read-only の sonnet 1 体)

採った指摘:
- `test` の label は `"make test"` (`"make test-only"` は test-only だけ)
- gate が連鎖の `xcodebuild-test-nonempty` を挟まないことを書いていなかった
- 6 通りが文言だけではない (窓の大きさを含む)
- 追記先は fake・stub の項より「本番が渡す値を先に読む」の項が発動点に合う
- Remote Control の一文に出典が無い

反証できなかった: red team 1 周目の P2 2 件・2 周目の物理パスの指摘・引用した規範の文言の実在。6 通りの数はこのセッションの 1 回目の mutate-verify の記録で数え直した。

## 進捗

- [x] 提案 1 を `survey-receiver-guards-before-passing-new-values.md` に追記
- [x] 提案 2〜4 の却下の理由を書いた

残課題なし (全項目が rule 化か却下で決着)。
