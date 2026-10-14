# 762 (chore): codex-run / codex-fanout が、利用上限で落ちた run を「上限」と分かる形で返す

起票日: 2026-10-14

- 起源: obaket retro 1077 の提案 C

## 概要

obaket 1073 の作業で、`ratelimit -source codex -check` が rc=0 を返した直後に codex を 3 本起こしたところ、3 本とも約 30 秒で
`You've hit your usage limit` (再開 10/10) を出して落ちた。`ratelimit` が判定に使うのは 5 時間の枠で、この上限は見ていない。

落ちた run は driver の集計では他の失敗と区別が付かない。`bin/codex-fanout` は `runs.tsv` に rc を書き、失敗を `label:rc=N` で数えるだけで
(全本失敗なら「全 N 本が失敗」)、上限で落ちたことは各 run の log を読まないと分からない。`bin/codex-run` は fanout に委譲している。

## 対応方針

- 失敗した run の log に codex の利用上限の行 (`usage limit`) があれば、`runs.tsv` と stderr の要約に「上限による失敗」と出す。
  rc の値は変えない (呼び出し側の判定を変えないため)。文言は実測した log から採り、ここに転記しない
- 実装の前に決めること (反証レビュー 2026-10-14 の指摘。根拠の行は確認済み):
  - 出す場所: `runs.tsv` は label / rc / started_at / elapsed_s / out / log の固定 6 列 (`bin/codex-fanout` の冒頭の出力の説明)。
    列を足すと読む側とヘッダの説明が変わるので、列を足すか stderr の要約だけにするかを決める。`codex-run` は自分でも
    `codex-run: rc=%d ...` の行を出すので、そこにも出すか
  - 見る対象: `-J` の run は完了イベントの無い rc=0 を rc=65 にする。上限の行が `<label>.log` ではなく events に出る場合と、上限で落ちた run が rc=0 で返る場合の扱い
  - merger への注記: merger の prompt には失敗した run が `label:rc=N` で渡り「この観点は未実施」と書かせる。上限による失敗だと書き添えるか
  - rc: fanout は一部失敗で 2、全滅で 1 を返す。`codex-run` は 1 本なので 0 か 1。どちらも変えない
- `ratelimit` の判定 (`-check`) に weekly 系の枠は入れない。weekly を判断材料にしない方針 (`_claude/rules/subagent-model-tiering.md`) と衝突する
- テストは `tests/codex_fanout.bats` / `tests/codex_run.bats` に、上限の行を出して落ちる fake の codex を足す

## 受け入れ条件

- [ ] 上限の行を含む log の run が、要約で上限による失敗と分かる
- [ ] 上限以外の失敗 (rc=124 の時間切れ等) の表示は変わらない
- [ ] 上の 2 つを fake の codex で固定し、検出を外す変異で red になることを確かめた

## 関連ファイル

- `bin/codex-fanout` (結果の集計: `runs.tsv` と `failed` 配列)
- `bin/codex-run`
- `tests/codex_fanout.bats` / `tests/codex_run.bats`
