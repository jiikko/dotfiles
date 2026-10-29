# 982 (cleanup): 使われていない CallersText / EvidenceText を消す

起票日: 2026-10-30

## 概要

`deadcode` (golang.org/x/tools、`~/go/1.25.4/bin/deadcode`) を src/dupscan で回すと、production から届かない関数が出る。
`-test` を付けてもテストからも届かないものが 2 つある (2026-10-30、HEAD `a335a9c42`)。

| 関数 | `deadcode ./...` | `deadcode -test ./...` | 実態 |
|---|---|---|---|
| `internal/report/evidence.go` の `EvidenceText` | 届かない | 届かない | `evidence_cmd.go` は `EvidenceTextGrouped` (`internal/report/grouped.go`) だけを呼ぶ。本体は Grouped とほぼ同じ (見出しの節・件数の打ち切りを写している) |
| `internal/report/callers.go` の `CallersText` | 届かない | 届かない | `callers_cmd.go` は `CallersTextHint`、`context_cmd.go` は `CallersTextGrouped` を呼ぶ。`CallersText` は `CallersTextMore` へ固定の案内を渡すだけ |
| `query_ux.go` の `scanned.relevantParseErrors` | 届かない | 届く | テスト (`parse_warning_relevance_test.go`) だけが呼ぶ 1 行の wrapper。production は `relevantParseErrorsAny` を呼ぶ |
| `internal/lang/langtest` の `Find` / `NormBoth` | 届かない | 届く | テスト専用の package (正当。対象外) |

`CallersTextMore` は `CallersTextGrouped` の 0 件の経路から呼ばれているので残る。

## なぜ今まで残ったか

`make test-unused-excluding-tests` が使う `unused` は公開名 (大文字) を使われているものと数える。`EvidenceText` / `CallersText` は公開名なので素通りした。
`EvidenceText` は `EvidenceTextGrouped` に置き換わった後に消し忘れた写しで、片方だけ直すと 2 つが黙ってずれる。

## 対応方針

1. `EvidenceText` と `CallersText` を消す
2. `relevantParseErrors` は消して、テストは `relevantParseErrorsAny([]string{name})` を呼ぶ (検査している性質は変わらない)。消さないなら、テスト専用と読める理由をコメントに書く
3. 再発防止: 公開名の未使用を検出する道具 (`deadcode -test`) を lint の段に足せるかを見る。dotfiles の他の module でも同じ形が出るかを数えてから決める (足すなら別 issue)

## 受け入れ条件

- [ ] `deadcode -test ./...` の出力が `langtest` の 2 件だけになる
- [ ] `make -C src/dupscan lint test` が通る
