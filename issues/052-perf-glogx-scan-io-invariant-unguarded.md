# glogx: 「issue 一覧の生成で全文を読まない」という不変条件を守るテストが無い

起票日: 2026-08-14
種別: perf
優先度: **P2** (050 で入れた性質が、表示を変えない変更で無言に失われる)

## 何が起きるか

issue 050 で `LoadMeta` を H1 で打ち切り、一覧の生成が全 issue の全文を読まないようにした。
だが**その不変条件そのものを守るテストが無い**。

050 の敵対的レビュー R3 が実証した変異:

```go
// scanIssues に 1 行足すだけ (表示は何も変わらない)
_, _ = iss.ReadBody()
```

- 期待: 「全件の全文を読むのをやめた」が壊れるので red
- 実際: **`go test ./...` が全 green**

今あるテストが守っているのは:

- `TestLoadMetaStopsAtH1` — `LoadMeta` **単体**が打ち切ること
- `TestIssuesViewerReloadsAfterEditorCloses` — 一覧に進捗が**表示されない**こと

どちらも「scan の経路が全体としてどれだけ読むか」を見ていないので、
`LoadMeta` の外側で全文を読む経路が足されると素通りする。

## なぜ塞ぎにくいか (素朴な方法が効かない理由)

- **時間で測る**のは flaky (050 の起票時に検討して却下済み)
- **「読まれていないこと」をファイル内容で観測する**のは vacuous になる
  (`h1Re` はゴミバイトでエラーにならないので、EOF まで読んでも外から見える差が出ない)
- `LoadMeta` 単体なら `bufio.Scanner` のトークン上限を観測点にできる (050 で採用) が、
  `ReadBody` は `os.ReadFile` なのでその手は効かない

## 対応方針 (案)

**読んだバイト数を数える seam を入れる**。案:

1. `issues.Scan` / `LoadMeta` がファイルを開く関数を差し替え可能にし (パッケージ変数か
   引数)、テストで counting reader を挟んで「1 ファイルあたり読んだバイト数が
   ファイルサイズ未満」を assert する
2. あるいは `scanIssues` を通した後の総 read バイト数を返す debug seam

⚠️ seam を足すこと自体が production を複雑にするので、**入れるなら 1 箇所に閉じる**こと。
`refuse-low-value-coverage.md` の「テストのために production の複雑性を上げる」に
該当しないかを着手時に評価する (この不変条件は 050 で実測 2〜4 倍の差があったので
「高価値」側だが、seam の設計次第では見送りもありうる)。

## 関連する穴 (同じ根)

- `tests/glogx/bench_budgets.ci` に **issue scan の metric が無い**。描画系
  (`view_steady` / `model_init_200` 等) だけなので、起動時の I/O 退行は CI で見えない。
  → issue 051 (確保のゲート) と同じ「CI が見ていない軸」の話
- `TestLoadMetaStopsAtH1` の観測点 (`sc.Err()`) は **production が捨てている値**
  (`_ = iss.LoadMeta()`)。打ち切り除去では確実に red になるが、
  「エラーを飲む」refactor には無力 (R3 が実証済み)

## 関連

- `issues/done/050-perf-glogx-issue-list-reads-full-body.md` (「残した穴」節が一次情報)
