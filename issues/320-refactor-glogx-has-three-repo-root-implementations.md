# refactor: repo root の解決が glogx に 3 実装あり、キャッシュを足すと 4 実装目になる

起票日: 2026-09-07
カテゴリ: refactor
優先度: 中（**キャッシュ提案より先にこれを通すこと**。順序を誤ると重複が増える）
出典: /audit performance 2026-09-06。3 実装は私が grep で確認

## 何が起きているか

同じ問い（repo root は？）に対して、**失敗時のセマンティクスが 3 通り**に分かれている:

| 場所 | 失敗時の値 |
|---|---|
| `src/glogx/open_workspace.go:repoRoot` | `"."` |
| `src/glogx/worktree_status.go`（インライン。`loadWorktreeStatus` 内） | `""` |
| `src/glogx/issues/discover.go:RepoRoot` | cwd |

[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) §0-B
（その答えを既に出している経路はないか）と
[`mutation-verify-new-tests.md`](../_claude/rules/mutation-verify-new-tests.md) の
「同じ判定を 2 箇所で別実装していないか」に正面から当たる。

## 併せて直す: `loadWorktreeStatus` の冗長 fork

`loadWorktreeStatus` は `git status` に加えて `rev-parse --show-toplevel` を**毎回** fork する。
値は**プロセス不変**（`os.Chdir` は非テストコードで 7 モジュール全部 0 件）。

実測（2 者の測定に幅あり）:

| | A（n=60 median） | B（N=50） |
|---|---|---|
| `--show-toplevel` | 3.40 ms | 5.53 ms |
| `git status` | 8.07 ms | 10.67 ms |

冗長な 1 本が対の **30〜34%**。呼び出し元は 2 つで、**体感に効くのは後者だけ**:

- `status_view.go` の 1.5 秒ポーリング — duty cycle 0.23% で体感には出ない
- `status_view.go:runDiscard` の破壊的操作 preflight — **Update 内の同期呼び出し**
  ＝ 確認 Y を押した瞬間の UI ブロックに丸ごと乗る

出典の `issues/done/273` が「未実測」として保留し trigger を残した項目で、**今回その実測が揃った**。

## 🚨 キャッシュを入れる前に読むこと（3 つの罠）

### ① 「`os.Chdir` は lint で禁止済み」は事実誤認

監査の一次報告は「glogx は `.golangci.yml` の forbidigo で `os.Chdir` を禁止済み = lint で
強制された不変条件」と書いていたが、**誤り**。実測:

- forbidigo が禁じているのは `^fmt\.Print(f|ln)?$` 系だけ
- `os.Chdir` は **errcheck の `exclude-functions`**（= 戻り値エラーを無視してよい）に居る。**真逆**

0 件という全数勘定自体は再現できたが、これは**「今たまたま呼んでいない」**であって
機械強制ではない。**この根拠でキャッシュを入れてはならない。**

### ② `sync.Once` + パッケージ変数はテストと衝突する

`gitlog_test.go` / `open_workspace_test.go` / `worktree_status_real_test.go` が実際に
`t.Chdir` を使っており、後者は**使い捨て repo へ chdir してから実 `loadWorktreeStatus()` を呼ぶ**。
プロセス寿命のキャッシュは「rows は temp repo 相対・root は dotfiles」という不整合を
**緑のまま**作る。さらに `tea.Cmd` goroutine と Update の両方から呼ばれるので素の `var` は
`-race` で落ちる。

### ③ `sync.Once` は失敗もキャッシュする

現行は `runGitTimeout` の一時失敗から poll ごとに自己回復する。
**成功（非空）のときだけ記憶する**条件を必ず明記すること。

## 推奨対応（順序つき）

1. **解決を 1 関数へ寄せる**。失敗時の値は呼び出し側が選べるよう `(string, bool)` を返す
2. root を **view 構築時に 1 度だけ**解決して画面が持つ（`worktreeStatus` から外す）か、
   前の値から引き継ぐ。🚨 `runDiscard` は末尾で `applyFresh(fresh)` = `v.st` の丸ごと差し替えを
   するので、root 抜きの値をそのまま流すと `v.st.root` が空になり、
   **untracked プレビューが cwd 相対へ落ちる**
3. その後で初めてキャッシュの是非を判断する

## 🚨 `runDiscard` の preflight 再読み込みは畳まないこと

破壊的操作の直前に取り直す値は、**呼び出し元から渡された申告値で代用してはいけない**
（[`sandbox-real-destructive-test-apis.md`](../_claude/rules/sandbox-real-destructive-test-apis.md)
の「実行の直前に取り直した値で判定する」）。最適化の射程はこの preflight の**外側**に限る。

## 受け入れ条件

- [ ] repo root の解決が 1 実装になり、失敗時のセマンティクスが呼び出し側で選べる
- [ ] `t.Chdir` を使う既存テストが緑（キャッシュを入れるなら、その 3 本で不整合が出ないこと）
- [ ] キャッシュを入れるなら**成功時のみ記憶**し、`-race` で緑
- [ ] `runDiscard` の preflight が畳まれていない
- [ ] **変異検証**: 1 実装化の後、失敗時セマンティクスを取り違える変異で red

## 関連

- issue 319（同じ「同じ問いを 2 回」ファミリーの zsh 側）
- `issues/done/273`（未実測として保留した出典。実測が揃ったのでこの issue が引き継ぐ）
