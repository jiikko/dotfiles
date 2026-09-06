# test: `unused` がテストを解析対象に含めるため、「production 到達不能」を CI が構造的に検出できない

起票日: 2026-09-07
カテゴリ: test
優先度: 中（**この監査で見つかった Go 側の指摘のほぼ全部がこのクラス**。個別に潰しても再発する）
出典: /audit dead-code 2026-09-06

## 何が起きているか

`src/*/.golangci.yml`（7 モジュール）の `unused` は**テストファイルも解析対象に含める**のが既定。
そのため「**production の最後の呼び出し元が消えて、テストだけが呼んでいる関数**」は
`unused` にとって「使われている」ことになり、**CI は永久に緑**のままになる。

issue 316 に挙げた到達不能シンボルは**全部このクラス**。個別に消しても、
次に production の呼び出しを差し替えた人が同じ状態を作る。

## 検出できる道具は既にある

```
staticcheck -checks=U1000 -tests=false
```

`-tests=false` でテストを母集合から外すと、このクラスが出る。
実測（監査が 7 モジュール全部へ回した結果）:

- `src/glogx` / `src/doctor`: issue 316 の各件
- `src/parallel-each/runner.go:loadProcessedLines` — production 参照 0、`runner_test.go` 4 箇所のみ
- `src/schedkeys/editor.go:setValue` — production 参照 0、テスト 10 箇所超
- `src/disassemble_excel` / `src/lockman` / `src/termsafe`: **0 件**

## 🚨 素朴に有効化してはいけない（allowlist を同じ変更で作る）

上の `parallel-each` / `schedkeys` の 2 件は**死蔵ではなく正当な test seam**
（production ファイルに置かれた、テストが依存するフック）。ゲートを allowlist 無しで入れると、
**この 2 件にそのまま「unused だから消せ」の圧力がかかる**。
`schedkeys` の `setValue` はテスト 10 箇所超が依存しているので、消せば大量に壊れる。

[`list-masked-failure-modes-before-removing-guard.md`](../_claude/rules/list-masked-failure-modes-before-removing-guard.md)
の観点で言うと、この 2 件が**マスクしていたもの**は「テストの可読性」。
それを列挙しないまま検出だけ強めない。

🚨 [`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) §8:
ゲートを書く**前に**、脅威モデル（誰の・どの失敗を止めるか）と
**「検出しないと決めた形」**をヘッダに書く。

## 推奨対応

1. `staticcheck -checks=U1000 -tests=false` を**全 7 モジュール**へ回す make target を作る
2. **allowlist（`ファイル:シンボル` + 理由 1 行）を同じ変更で用意する**。初期登録は
   `parallel-each/runner.go:loadProcessedLines` と `schedkeys/editor.go:setValue` の 2 件
3. 集約経路（`make test` / `make lint`）へ配線する
4. 🚨 配線後は rc=0 ではなく、**その検査の出力行が集約経路のログに現れること**を確認する
   （[`verify-execution-not-just-exit-code.md`](../_claude/rules/verify-execution-not-just-exit-code.md)）

## 受け入れ条件

- [ ] 7 モジュール全部が対象になっている（モジュール数を機械で数えて commit message に書く）
- [ ] allowlist が理由つきで存在し、**stale な登録**（既に production から使われるようになったもの）を検出する
- [ ] ヘッダに脅威モデルと「検出しない形」がある
- [ ] **変異検証**: production の呼び出しを 1 つテストへ差し替えると red になる
- [ ] 集約経路のログに検査名が出る

## 関連

- issue 316（このゲートが検出するはずだった個別の件）
- 🚨 監査の一次報告は「唯一の実害は 1 件」と書いていたが、これは Go 7 モジュール中 2 つしか
  走査していない結論だった（残り 5 つへ回すと 2 件実在した）。
  [`CLAUDE.md`](../CLAUDE.md)「不在の主張は着手前に数え直す」の実例として issue 318 に記録した
