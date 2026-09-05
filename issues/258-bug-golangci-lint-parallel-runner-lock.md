# golangci-lint のグローバル file lock で、並行実行すると lint が落ちる

起票日: 2026-09-05
カテゴリ: bug
優先度: 中

## 症状

golangci-lint は起動時にグローバルな file lock を取り、別インスタンスが走っていると
`parallel golangci-lint is running` を出して失敗する。src/ の 7 プロジェクトは
`.golangci.yml` に `run:` 節を持たず、`allow-parallel-runners` が未設定のため既定 (false) で動く。

実害は 2 つある。

1. **並行セッション間で `make test` が落ちる**。同じマシンで 2 セッションが `make test` を
   同時に回すと、後発の lint が落ちる。実例 (別セッション dotfiles-c6 の報告、2026-09-05):
   `make test` を 2 本並行させて src/disassemble_excel・src/glogx・src/parallel-each の
   3 つが落ち、直列で回し直すと rc=0
2. **`run_go_projects` の lint を並列化できない**。59f9e48c で go test は並列化したが、
   lint はこの lock のため直列のまま残した

## 実測 (2026-09-05、14 コア機)

`make -C <dir> lint` を同時起動し、stdout / stderr / rc を分離して観測した。

| 同時数 | 結果 |
|---|---|
| 2 (lockman + termsafe) | **再現せず**。両方 rc=0 |
| 7 (全プロジェクト) | **3 本が失敗**。disassemble_excel / doctor / schedkeys が rc=2 + `parallel golangci-lint is running` |

同時数が増えるほど踏む。**rc=2 は `make` の終了コード**で、golangci-lint 自身の終了コードは
make に隠れて分離できていない (必要なら `go run ... golangci-lint run` を直接叩いて測り直す)。

固定バージョンは 7 プロジェクトとも `GOLANGCI_LINT_VERSION := v2.5.0`。

## 対応案

各 `src/*/.golangci.yml` に次を足す:

```yaml
run:
  allow-parallel-runners: true
```

これは lock を無効化するのではなく「並行して走ってよい」と宣言するもの。7 プロジェクトの
契約変更になるため 59f9e48c のスコープには入れなかった。

## 着手前に確かめること

- 🚨 **`allow-parallel-runners: true` が何をマスクしていた failure mode を外すのかを先に列挙する**
  (`_claude/rules/list-masked-failure-modes-before-removing-guard.md`)。この lock は
  「同じキャッシュディレクトリを複数インスタンスが同時に書く」ことを防いでいる可能性があり、
  外した結果がキャッシュ破損や結果の取りこぼしなら、落ちる方がまだ安全。
  **v2.5.0 のドキュメントと実装で、この lock が何を守っているかを確認してから入れる**
- 入れたら `run_go_projects` の lint も並列化できるか実測する (`$(if $(filter lint,$(1)),;,&)`
  の分岐を外せるか)。**速くなったと書く前に before/after を測る**
- 検証は「7 本同時で 0 本落ちる」を数回。1 回の緑は上表のとおり当てにならない (2 本では
  再現しなかった)

## 関連

- `59f9e48c` — go test の並列化と、lint を直列に残した理由 (Makefile の `run_go_projects` 直上コメント)
