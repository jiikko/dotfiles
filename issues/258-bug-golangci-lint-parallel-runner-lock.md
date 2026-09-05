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

## 実測 (2026-09-05、14 コア機、同一 checkout)

`make -C <dir> lint` を同時起動し、rc を観測した。

| 条件 | 試行 | 結果 |
|---|---|---|
| **7 本同時** (全プロジェクト) | 4 ラウンド | **4/4 で再現**。毎回 2〜3 本が rc=2 + `parallel golangci-lint is running` |
| **2 本同時** (組を変えて反復) | 7 ラウンド = 延べ 14 実行 | **0 件**。1 本も落ちない |
| `make test` を 2 本並行 (別セッション dotfiles-c6 の報告) | 1 回 | 3 本失敗。直列で回し直すと rc=0 |

**同時数が効いている**。2 本同時は延べ 14 実行で 1 本も落ちず、「機会の数が増えれば 2 本でも
踏む」という読み方は**実験で否定された**。

🚨 3 行目 (c6 の報告) は `make test` の中で lint が直列に回るので、字面どおりなら同時に走る
golangci-lint は最大 2 本のはずで、上の 2 行目と矛盾する。ただし**この観測は条件が分離されて
いない**: 同時刻に私 (dotfiles-a2) が同じマシンで 7 本同時の計測を回しており、第 3 の
golangci-lint が走っていた。**2 本同時の反例としては採用しない**。

**rc=2 は `make` の終了コード**で、golangci-lint 自身の終了コードは make に隠れて分離できて
いない (必要なら `go run ... golangci-lint run` を直接叩いて測り直す)。

固定バージョンは 7 プロジェクトとも `GOLANGCI_LINT_VERSION := v2.5.0`。
**閾値 (何本から踏むか) は未測定**。2 と 7 のあいだは測っていない。

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
- 検証は **7 本同時を複数ラウンド**。現状 4/4 で落ちるので、修正後に 4 ラウンド連続で
  0 本ならば効いたと言える (2 本同時は元から落ちないので、検証条件に使わない)
- 測るときは **他セッションが同じマシンで lint を回していないこと**を確かめる
  (上表 3 行目がその混入で条件不明になった)

## 関連

- `59f9e48c` — go test の並列化と、lint を直列に残した理由 (Makefile の `run_go_projects` 直上コメント)
