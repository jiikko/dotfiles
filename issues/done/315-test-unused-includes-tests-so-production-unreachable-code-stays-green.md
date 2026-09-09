# test: `unused` がテストを解析対象に含めるため、「production 到達不能」を CI が構造的に検出できない

起票日: 2026-09-07
カテゴリ: test
優先度: 中（**この監査で見つかった Go 側の指摘のほぼ全部がこのクラス**。個別に潰しても再発する）
出典: /audit dead-code 2026-09-06

## 参照先の現況 (2026-09-08)

- **316**: 13 項目のうち 1 件（`DeleteReport.HasFailures` の配線漏れ）だけ対応済み。
  **残り 12 項目は継続**なので、この issue が言う「個別に潰しても再発する」構造の話はまだ生きている
- **318**: 監査の記録 issue。派生（310-317）が全部片付いたら done、という条件を本文に明記した。**2026-09-09 時点で 310 / 311 / 312 / 313 / 314 / 316 / 317 が done**（318 本文に内訳あり）。**318 を閉じるのに残っているのは本 issue（315）だけ**。なお 313 ① で `src/*/.golangci.yml` 7 本が yamllint の対象に入った（本 issue が触るのと同じファイル群なので、`unused` の設定を変えるときは yamllint も通ること）。
  この issue もその派生の 1 つ
- **この issue自体は未着手**（順番待ち）

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

[`list-masked-failure-modes-before-removing-guard.md`](../../_claude/rules/list-masked-failure-modes-before-removing-guard.md)
の観点で言うと、この 2 件が**マスクしていたもの**は「テストの可読性」。
それを列挙しないまま検出だけ強めない。

🚨 [`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §8:
ゲートを書く**前に**、脅威モデル（誰の・どの失敗を止めるか）と
**「検出しないと決めた形」**をヘッダに書く。

## 推奨対応

1. `staticcheck -checks=U1000 -tests=false` を**全 7 モジュール**へ回す make target を作る
2. **allowlist（`ファイル:シンボル` + 理由 1 行）を同じ変更で用意する**。初期登録は
   `parallel-each/runner.go:loadProcessedLines` と `schedkeys/editor.go:setValue` の 2 件
3. 集約経路（`make test` / `make lint`）へ配線する
4. 🚨 配線後は rc=0 ではなく、**その検査の出力行が集約経路のログに現れること**を確認する
   （[`verify-execution-not-just-exit-code.md`](../../_claude/rules/verify-execution-not-just-exit-code.md)）

## 受け入れ条件

- [x] 全モジュールが対象になっている（モジュール数を機械で数えて commit message に書く）
- [x] allowlist が理由つきで存在し、**stale な登録**（既に production から使われるようになったもの）を検出する
- [x] ヘッダに脅威モデルと「検出しない形」がある
- [x] **変異検証**: production の呼び出しを 1 つテストへ差し替えると red になる
- [x] 集約経路のログに検査名が出る

## 関連

- issue 316（このゲートが検出するはずだった個別の件）
- 🚨 監査の一次報告は「唯一の実害は 1 件」と書いていたが、これは Go 7 モジュール中 2 つしか
  走査していない結論だった（残り 5 つへ回すと 2 件実在した）。
  [`CLAUDE.md`](../../CLAUDE.md)「不在の主張は着手前に数え直す」の実例として issue 318 に記録した

## 進捗・結果（2026-09-09）

### 🚨 着手前に数え直したら、issue の前提が 2 つ古かった

| 本文の記載 | 実測 |
|---|---|
| Go は **7 モジュール** | **6 モジュール**。`src/parallel-each` は `e8f8a361 parallel-each を削除する (good-chrome-extensions へ移管)` で**既に無い** |
| allowlist の初期登録は `parallel-each/runner.go:loadProcessedLines` と `schedkeys/editor.go:setValue` | 前者は**モジュールごと存在しない**。実際の U1000 は **2 件**で、内訳が違う |

`staticcheck -checks=U1000 -tests=false` を 6 モジュールへ回した現在の全数:

- `src/glogx/zoom.go:(*appZoom).start` — issue 316 が「残す」と判断済み。理由は関数直上に書いてある
  （開く演出は `tui.go` の Init でコメントアウト、対の `startClose` は生きているので消すと片側だけになる）
- `src/schedkeys/editor.go:(*editor).setValue` — test seam。`editor_test` 2 / `regression_test` 4 /
  `render_test` 8 = **14 箇所**が依存
- 本文が挙げていた `src/glogx` / `src/doctor` の「issue 316 の各件」は、**316 で既に消えている**

つまり**新たに直すコードは無く**、この issue の成果物は**ゲートそのもの**。

### 新設したもの

`scripts/check_unused_excluding_tests.sh` + `make test-unused-excluding-tests`。

- 6 モジュールへ `staticcheck -checks=U1000 -tests=false` を回す
- **allowlist は `パス:シンボル|理由`**。理由を書けない登録はしない
- **stale な allowlist を検出する**（報告されなくなった登録 = production で使われるようになった /
  消された、を赤にする）
- **対象 0 件は失敗**（モジュールの探し方が壊れると「違反 0 件」で緑になるのを塞ぐ）
- **staticcheck の出力を解釈できなかったら `??` で赤にする**（判定不能を合格に畳まない）。
  実際これが効いた — 変異がコンパイル不能だったとき、緑ではなく `??` で落ちた
- ヘッダに**脅威モデル**と**検出しないと決めた形**（exported / テスト専用ファイル / 反射・生成コード）

`make test-lint` へ配線し、ログに
`✓ production 到達不能なシンボルなし (Go モジュール 6 件を … allowlist 2 件)` が出ることを確認。

### 変異検証（4 本、すべて red / baseline green）

| 変異 | 結果 |
|---|---|
| **production の呼び出しを外す**（`tui.go` の `m.zoom.startClose(...)` を無効化。`go build` rc=0 を別途確認） | `✗ production から到達できない: src/glogx/zoom.go:(*appZoom).startClose` ほか 1 件 |
| allowlist の登録対象を消す（`setValue` を production からもテストからも削除） | `✗ allowlist が stale: …:(*editor).setValue は staticcheck から報告されていない` |
| モジュール探索を壊して 0 件にする | `✗ src 配下に go.mod が 1 件も見つからない` |
| allowlist を空にする | 2 件とも `✗ production から到達できない` |

🚨 1 回目の「呼び出しを外す」変異は**コンパイル不能**になり、red でも green でもない第 3 の結果だった
（ゲートは `??` として正しく赤を出した）。`go build` が通る形に当て直して red を確認している。

### 残タスク

- **検出しないと決めた形**（ヘッダに明記）: exported シンボル / テスト専用ファイル内の未使用 /
  反射・生成コード経由の参照
### CI への配線（`make test-lint` には入れなかった）

**CI に `staticcheck` は入っていなかった**（`grep staticcheck .github/workflows/*.yml` = 0 件）ので、
そのままでは新設したゲートが CI で**落ちる**。配線先は 2 つ検討して後者にした:

- ✗ `test-lint` に置く → `lint.yml` は**全 push で走る**ので、Go に触らない push でも毎回
  staticcheck が動く。2026-07-17 に Go を `lint.yml` から分離した判断と衝突する
- ✓ `test-src`（Go の集約）に置き、CI は **paths filter つきの専用 workflow**
  `.github/workflows/unused.yml` が回す。`src_<project>.yml` に置けないのは、
  このゲートが**モジュール横断の allowlist と全数勘定**を持つため

staticcheck は **`v0.7.0` に pin** し、`--version` の値を突き合わせる（出すだけでは pin の証拠に
ならない。shellcheck / actionlint の pin と同じ規律）。

🚨 **既存のゲートに 1 度落とされた**: `setup-go` を `v6` で書いたところ
`test-workflow-action-pins` が「2 種類の版で使われている」と赤にした。`v7` へ揃えて解消。

### 残タスク

- **検出しないと決めた形**（ヘッダに明記）: exported シンボル / テスト専用ファイル内の未使用 /
  反射・生成コード経由の参照
- **未検証**: `unused.yml` が CI で実際に緑になること（push して 1 run 見るまでは未確認）。
  この workflow は paths filter 付きなので、**`src/**` を触らない push では起動しない**
