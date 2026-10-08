# 689 (retro): treefiler の実装 (issue 662 の段階 5c〜7)

起票日: 2026-10-09

## どこで踏んだか

### 1. push 後の CI を名指しの workflow だけで確かめ、別の workflow が 5 commit 落ちたままだった

段階 5a〜6 の push のたびに `ci-log -i src/treefiler` / `src/glogx` / `src/tuikit` を名指しで待ち、緑を見て次へ進んだ。
`unused (production 到達不能)` は 24d14f36 (段階 5a) から 5 commit 続けて落ちていた (テストだけが使う `canvas.plain` が production に残っていた)。
pro-con の揺れを調べに commit の全 run を並べたときに初めて見えた。手元の `make test-unused-excluding-tests` でも出せた。

- 提案 (切り出し先: 既存ルール `.claude/rules/use-ci-log-for-ci-inspection.md` へ追記): **名指しで待った run が緑でも、報告の前に
  引数なしの `ci-log` で、その commit の全 run の失敗を見る**。ルールは「名指しで待て」(別の commit の run を拾わないため) と
  「HEAD が緑でも直前までが緑ではない」を持つが、「名指しした run 以外の workflow」は誰も見ない形が残っている

### 2. 参照実装の上限値を、実行の形が違う実装へそのまま持ち込んだ

spec (treebeard) の「Markdown は先頭 2 MiB まで」をそのまま使った。treebeard は整形を裏のスレッドで行うが、こちらは UI の goroutine で同期に整形するので、
2 MiB で 1.8 秒止まった。レビューの指摘を固定するテストの実行時間で偶然気づいた (256 KiB に下げて 0.06 秒)。

- 提案 (切り出し先: 既存ルール `_claude/rules/perf-claims-need-measurement.md` へ追記): **移植元・spec の上限値や閾値を、実行の形
  (同期か非同期か・UI のスレッドか) が違う実装へ持ち込むときは、その上限ちょうどの入力で 1 回時間を測ってから採る**

## 却下した項目

- 結果を捨てるガード (git を off にした後の取得結果) で版を進め忘れ、Busy が終わらなくなった — 却下: `adversarial-review-own-safeguards.md` §7 の
  「1 つの分岐で 2 つの判断 (呼び出し側へ何を返すか / 内部状態をどうするか) を決めていないか」が同じ形。テストが捕まえた
- 変異が緑のまま通った原因が、もう一段の守り (`gitSnapshot.state` の区切りつき照合) だった。守りを通らない別の利用者 (`branchFor`) に assert を足して
  red にした — 却下: `mutation-verify-new-tests.md` の「緑の変異はテストを書き直す」と「等価と言う前に全経路を列挙」で足りる
- Go 1.22 以降の range の変数への代入が次の周に効かない — 却下: lint (ineffassign / staticcheck) が止める
- `TestFrameAllocBudget` が全体の実行でだけ 1 回揺れた (AllocsPerRun はプロセス全体の確保を数える) — 却下: 局所的。issue 662 に記録した

## 残課題

- [ ] 1 の追記 (ユーザーの判断待ち)
- [ ] 2 の追記 (ユーザーの判断待ち)
