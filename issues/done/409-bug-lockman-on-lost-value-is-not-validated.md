# 409 (bug): lockman の `--on-lost` が値検証されておらず、綴り間違いが黙って kill になる

起票日: 2026-09-21
出典: [359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md) の残タスク (最後の 1 件。切り出して 359 を done にした)

## 概要

`--on-lost` は `kill | warn` の 2 値だが、**判定が `!= "warn"` の否定形**なので、
`--on-lost=warm` のような綴り間違いが**黙って kill 扱い**になる。
[091](091-feat-lockman-directory-lease-lock.md):282 は `--on-lost=kill|warn` と明記しており、
未知の値の扱いは規定していない。

現状 (2026-09-21 に実コードで確認):

- `main.go:124` — `fs.StringVar(&o.onLost, "on-lost", "kill", ...)` で受けるだけ
- `main.go:306` — `runWith(l, o.ttl, o.label, o.onLost != "warn", child)`

**黙って倒れる向きが危険側**なのがこの issue の本体。`warn` のつもりで `warm` と打った人は
「lease を失っても子は走り続ける」と思っているのに、実際は SIGTERM → 猶予 5s → SIGKILL
(`with.go` の `onLostGracePeriod`) が撃たれる。

## 対応方針

`--io-timeout` の値検証 (357 で入れた `main.go:197` の形) と**同じ場所・同じ形**で弾く。

```go
if o.onLost != "kill" && o.onLost != "warn" {
    warnf("--on-lost が不正 (%q)。kill | warn のどちらかを指定すること", o.onLost)
    return failCode(cmd, exitError)
}
```

- `failCode` を通すこと (`with` では `exitError` を 125 = `exitWithInvalid` へ寄せる。`main.go:385`)
- **検証は subcommand を問わず無条件で行う**のを推奨。`acquire` 等では `--on-lost` は読まれないが、
  そこで黙って受けると「効いたつもり」を作る。狭めるなら `cmd == "with"` に限る判断もありうるので、
  実装時にどちらかを選んで理由をコード直近へ書く

## 受け入れ条件

- [x] `kill` / `warn` 以外を渡すと `with` は rc=125、それ以外の subcommand は rc=1 で止まる
- [x] エラー文に**受け付ける値**が出る (人がその場で直せる)
- [x] `kill` / `warn` の正常系が退行していない (既定 `kill` を含む)
- [x] 変異検証: 足した検証を外す (= `!= "warn"` のままに戻す) 変異で、**このテストだけが** red になることを
      package + テストケース名で確認する ([`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md))

## 着手前のメモ (2026-09-21 の反証レビューで確認済み)

- **先例テストがそのまま雛形になる**: `src/lockman/io_timeout_test.go` の
  `TestIOTimeoutValueIsValidated` が「`check` で rc=1 / `with` で rc=125」を
  テーブルで固定している。同じ形で書ける
- `--on-lost` の**値検証・enum チェックはコード中に 1 つも無い** (`main.go` / `with.go` / `lock.go` の
  全数 grep + 既存テスト `on_lost_kill_test.go` / `on_lost_deadline_test.go` / `main_test.go` を確認)
- **フラグ登録は subcommand 分岐の外**なので、`acquire` 等でも `--on-lost` は無条件にパースされて黙って受理される
- 上の提案パッチを置く区間 (`main.go` の `--io-timeout` 検証 〜 `NewLocker` 呼び出しの間) に**到達不能にする早期 return は無い**
- **typo 単体が即 kill を起こすわけではない** — lease 喪失 / 判定不能が実際に起きたときに kill 側の挙動になる

## 同じ commit で揃えたい: `with` の「引数が不正」の終了コードが 3 経路だけ 1 のまま

[091](091-feat-lockman-directory-lease-lock.md):397 は **「`with` の終了コードは子と衝突させない」**と定め、
:405 で 0〜120 を子の透過、:408 で **125 = lockman 自体のエラー**としている。
ところが `run()` の検証分岐は `failCode` を通すものと通さないものが混在しており、
**`with` なのに rc=1 を返す経路が 3 つ残っている** (1 は子が返しうる値なので、呼び出し側から
「引数を間違えた」と「子が 1 で終わった」を区別できない)。

実測 2026-09-21 (`go build` した実バイナリ。stdout / stderr / rc を分離して採取):

| 実行 | rc | 出典 |
|---|---|---|
| `with --bogus d -- /bin/echo hi` | **1** | `parseFlags` のエラー返し |
| `with -- /bin/echo hi` (dir なし) | **1** | 「対象ディレクトリを指定すること」 |
| `with --ttl 1s d -- /bin/echo hi` | **1** | 「--ttl が短すぎる」 |
| `with --io-timeout 0 d -- /bin/echo hi` | 125 | `failCode(cmd, exitError)` (357 で入れた) |
| `with d --` (子が空) | 125 | `exitWithInvalid` を直に返す |
| `with --on-lost warm d -- /bin/echo hi` | **0** | **この issue の本体**。typo が素通りして子が走る |

🚨 **`--on-lost` の検証を足すと、揃っていない経路が 4 つ目になる**。`failCode` を通す形で足したうえで、
**同じ commit で上の 3 経路も `failCode` へ寄せる**のを推奨する
(寄せるのは `with` のときだけ 125 に化ける関数なので、他 subcommand の rc は変わらない)。

- [x] 追加の受け入れ条件: 上の表の 3 経路が `with` で **125** を返し、`check` 等では **1** のままであること
- [x] 変異検証: 各経路の `failCode` を外す変異で、**その経路のテストケースだけ**が red になること
- 🚨 寄せる前に [`list-masked-failure-modes-before-removing-guard.md`](../../_claude/rules/list-masked-failure-modes-before-removing-guard.md) の
  逆向き (値を変える側) として、**rc=1 に依存している呼び出し側が無いか**を確認する
  (`zshlib/_av1ify_lock.zsh` は rc=3 を SKIP・rc≠0 を中止に分けているので `with` は使っていないが、
  着手時に grep で数え直すこと)

## 関連

- [357](357-bug-lockman-with-bypasses-io-timeout.md) — `--io-timeout` の範囲検証を入れた先例 (同じ族)
- [385](385-design-lockman-on-lost-kill-vs-keep-renewing.md) — `--on-lost` の意味論を決めた issue (値の集合はここで確定している)

## 進捗

対応 commit: `fix(lockman,409): --on-lost を値検証し、with の「引数が不正」を 125 へ揃える`

### 実装

- `main.go` — `--io-timeout` 検証の直後に `--on-lost` の enum 検証を足した。
  **subcommand を問わず無条件**に行う方を選んだ (issue の推奨どおり)。理由はコード直近のコメントに
  書いた: フラグ登録が subcommand 分岐の外なので、`--on-lost` を読まない `acquire` 等でも黙って
  受理され「効いたつもり」を作るため
- `main.go` — 揃っていなかった 3 経路 (`parseFlags` のエラー / 対象ディレクトリなし / `--ttl` が短すぎる) を
  `failCode(cmd, exitError)` へ寄せた
- `on_lost_validation_test.go` (新規) — 上記 2 つのテスト

### 実測 (`go build` した実バイナリ。stdout / stderr / rc を分離、2026-09-22)

対応前の表と同じ実行を取り直した。**6 行すべてが揃った**:

| 実行 | 対応前 | 対応後 |
|---|---|---|
| `with --bogus d -- /bin/echo hi` | 1 | **125** |
| `with -- /bin/echo hi` (dir なし) | 1 | **125** |
| `with --ttl 1s d -- /bin/echo hi` | 1 | **125** |
| `with --io-timeout 0 d -- /bin/echo hi` | 125 | 125 (変化なし) |
| `with d --` (子が空) | 125 | 125 (変化なし) |
| `with --on-lost warm d -- /bin/echo hi` | **0 (素通り)** | **125** |

`check` 側は `--on-lost warm` / `--ttl 1s` とも **rc=1 のまま** (番号空間を変えたのは `with` だけ)。
正常系 (`--on-lost warn` / `kill` / フラグ未指定) はいずれも rc=0。
エラー文は `--on-lost が不正 ("warm")。kill | warn のどちらかを指定すること` で、受け付ける値が出る。

### 変異検証 (package `lockman`、ケース名まで確認)

🚨 判定の grep は `^--- FAIL` では**サブテスト名を取りこぼす** (インデントが付く)。
`^[[:space:]]*--- FAIL:` で採り直した。

| 変異 | red になったケース |
|---|---|
| `--on-lost` の検証ブロックを外す | `TestOnLostValueIsValidated/{typo-warm, empty, uppercase-KILL, with-uses-125}` |
| `parseFlags` の `failCode` を外す | `TestWithInvalidArgsExitWithInvalid/unknown-flag` **のみ** |
| 対象ディレクトリなしの `failCode` を外す | `TestWithInvalidArgsExitWithInvalid/missing-dir` **のみ** |
| `--ttl` の `failCode` を外す | `TestWithInvalidArgsExitWithInvalid/ttl-too-short` **のみ** |

検証を外す変異でも `kill` / `warn` / `default-passes` は緑のまま = 正常系の対照が効いている。
4 本とも復元後 green。

### 呼び出し側の確認 (rc=1 → 125 の影響)

`with` サブコマンドの実呼び出しは **repo 内に 0 件** (2026-09-22 に grep で数え直した)。
`zshlib/_av1ify_lock.zsh` は `acquire` / `release` を使っており、`with` はコメントで言及しているだけ。
`check` 等の rc は変えていないので、`rc=3` を SKIP に分けている同ファイルの判定にも影響しない。

### 残タスク

なし (受け入れ条件はすべて満たした)。
