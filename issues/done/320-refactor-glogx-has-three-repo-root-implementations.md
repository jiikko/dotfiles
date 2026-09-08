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

[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §0-B
（その答えを既に出している経路はないか）と
[`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md) の
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
（[`sandbox-real-destructive-test-apis.md`](../../_claude/rules/sandbox-real-destructive-test-apis.md)
の「実行の直前に取り直した値で判定する」）。最適化の射程はこの preflight の**外側**に限る。

## 対応 (2026-09-08)

commit `refactor(320): repo root の解決を 1 実装へ寄せ、失敗時の分岐だけ呼び出し側に残す`。

### 1 実装化

`issues.ResolveRepoRoot(cwd) (string, bool)` を唯一の実装にし、**失敗時の値を返さない**形にした。
3 通りの失敗値は呼び出し側の分岐として残る（どれもその場の事情として正しいので揃えない）:

| 呼び出し側 | 失敗時 | 理由 |
|---|---|---|
| `open_workspace.go:repoRoot` | `"."` | nvim を「今いる場所」で開く。cwd だと意図がぼやける |
| `worktree_status.go` | `""` | 「root 不明」の印。untracked プレビューが cwd 相対に落ちるだけ |
| `issues.RepoRoot` | cwd | git 管理外でも `issues/` を探せるように |

`rev-parse --show-toplevel` の非テスト実装は **1 箇所（`issues/discover.go`）だけ**になった
（grep で確認）。🚨 パッケージを 1 つ増やす（`gitroot` 等）案は採らなかった:
main → issues の依存は `tui.go` / `issues_view.go` に既にあり新しい依存ではないので、
**置き場所より実装が 1 つであること**を優先した。理由は関数の doc コメントに書いた。

### 🚨 キャッシュは入れない（効果が出る場所が無い）

issue が挙げた冗長 fork（対の 30〜34%）は事実だが、**削れる場所が無い**:

- `status_view.go` の 1.5 秒ポーリング → duty cycle 0.23%。体感に出ない
- `runDiscard` の preflight → **破壊的操作の直前判定なので畳めない**
  （`sandbox-real-destructive-test-apis.md`「実行の直前に取り直した値で判定する」）

3 つの罠（①「`os.Chdir` は lint で禁止済み」は事実誤認 ②`t.Chdir` を使うテスト 3 本と衝突
③`sync.Once` は失敗もキャッシュする）を踏まえるまでもなく、**効果の出る呼び出し元が
存在しない**ので凍結する。trigger: 「repo root の解決が体感に出る呼び出し元が新しくできたとき」。

### 変異検証

| 変異 | 結果 |
|---|---|
| `repoRoot()` の失敗値を `"."` → `currentDir()` | **red** |
| `ResolveRepoRoot` が失敗時に `(cwd, true)` を返す | **red** |
| `worktree_status` の root を `issues.RepoRoot(currentDir())` へ | green（下記） |

🚨 **変異検証で自分のテストの欠陥が 2 つ出た**（レビュー前に自分で検出）:

1. 前提を `t.Skipf` で書いていたため、`ResolveRepoRoot` が `(cwd, true)` を返す変異で
   **テスト全体が skip され緑で通った**。`mutation-verify-new-tests.md`「前提が早期 return で
   素通りしていないか」そのもの。→ `t.Fatalf` に変えた
2. 3 つ目のサブテストが **production を 1 行も通らない自己言及**だった（解決部分を自分で書いて
   いた）。→ 削除し、意図を `worktree_status.go` のコメントで固定した

3 つ目の変異が green なのは、差が出るのが「`git status` は成功するが `rev-parse` だけ失敗する」
ときだけで、テストで作るには seam の新設が要るため。**到達経路を列挙したうえで**
「テスト困難 × 低価値」と判断した（`refuse-low-value-coverage.md`）。

## 受け入れ条件

- [x] repo root の解決が 1 実装になり、失敗時のセマンティクスが呼び出し側で選べる
- [x] `t.Chdir` を使う既存テストが緑（`glogx` / `glogx/issues` とも `-race` で緑）
- [x] キャッシュは**入れない**判断（効果の出る呼び出し元が無い）。trigger を明記した
- [x] `runDiscard` の preflight は畳んでいない（そもそもキャッシュを入れていない）
- [x] **変異検証**: 失敗時セマンティクスを取り違える変異 2 本で red。green だった 1 本は
      到達経路を列挙して等価に近いと判定し、意図をコメントで固定した

## 関連

- issue 319（同じ「同じ問いを 2 回」ファミリーの zsh 側）— **2026-09-08 に解消**（`issues/done/319-*`）。
  指紋計算を同期分岐のローカルへ閉じ込め、F/I の呼び出し回数を pin した。この issue（Go 側）は継続
- `issues/done/273`（未実測として保留した出典。実測が揃ったのでこの issue が引き継ぐ）
