# 689 (retro): treefiler の実装 (issue 662 の段階 5c〜7)

起票日: 2026-10-09

## どこで踏んだか

### 1. push 後の CI を名指しの workflow だけで確かめ、別の workflow が 5 commit 落ちたままだった

段階 5a〜6 の push のたびに `ci-log -i src/treefiler` / `src/glogx` / `src/tuikit` を名指しで待ち、緑を見て次へ進んだ。
`unused (production 到達不能)` は treefiler の最初の commit (a85e833b。その run は次の push で取り消し) の直後の 4e0b0195 から、
9e63a1f5 まで 10 本続けて落ちていた (テストだけが使う `canvas.plain` が production に残っていた。途中には treefiler 以外の commit の run も含む)。
最初の push から見逃しており、pro-con の揺れを調べに commit の全 run を並べたときに初めて見えた。手元の `make test-unused-excluding-tests` でも出せた。

- 提案 (切り出し先: 既存ルール `.claude/rules/use-ci-log-for-ci-inspection.md` の待ち方の項へ 1 行追記): **名指しで待った run が緑でも、
  報告の前に引数なしの `ci-log` を 1 回打ち、その commit の全 run の失敗を見る**。ルールは既定 (引数なし = 全失敗)・「名指しで待て」
  (別の commit の run を拾わないため)・「HEAD が緑でも直前までが緑ではない」を持つ。足りないのは、名指しの待ちと全体の確認をつなぐ 1 行だけ
  (paths 付きの workflow は、待つ対象として名指しする発想に入りにくい)

### 2. 参照実装の上限値を、実行の形が違う実装へそのまま持ち込んだ

spec (treebeard) の「Markdown は先頭 2 MiB まで」(spec §5.5) をそのまま使った。treebeard は整形を裏のスレッドで行うが、こちらは UI の goroutine で
同期に整形する。レビューの指摘を固定するテスト (TestMarkdownReadsUpToCap。上限 + 1 MiB のファイルを開いて G) の所要が 1.83 秒だったことで
偶然気づき、256 KiB に下げた (同じテストで 0.06 秒。ファイルの書き出しを含み、整形だけの時間は分けていない。出典は issue 662 の段階 6 の節と
`src/treefiler/filer/tileview.go` の `markdownCap` のコメント)。

- 提案 (切り出し先: 既存ルール `_claude/rules/measure-external-cli-streams-separately.md` の「外部ソースは仮説」の項へ追記): **移植元・upstream の
  上限値や閾値も仮説。実行の形 (同期か非同期か・UI のスレッドか) が違う実装へ持ち込むときは、その上限ちょうどの入力で 1 回時間を測ってから採る**。
  `perf-claims-need-measurement.md` は発動点が「変更の主題が性能のとき」で、機能の実装の途中で値を持ち込む場面に当たらないので選ばない

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
