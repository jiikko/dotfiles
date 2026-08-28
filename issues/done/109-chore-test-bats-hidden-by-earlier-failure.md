# 無関係な 1 本の失敗が `test-bats` を丸ごと隠す (CI 未検証のまま緑に見える)

起票日: 2026-08-26
種別: chore
優先度: **P2** (2026-08-25 に 2 回発生し、修正が CI 未検証のまま数時間放置された)

## 確認できた事実

`Makefile:162`

```make
test-runtime-rest: test-syntax test-discovered-rest test-bats
```

`test-discovered-rest` が Error 1 で落ちると make はそこで中断し、**`test-bats` が 1 度も
実行されない**。CI のログにも bats の出力が 1 行も出ない。`Makefile:76` の `test-runtime`
(手元用) も同じ形。

## 何が起きたか (2026-08-25)

同じ日に 2 回、`tests/` 配下の 1 本が落ちて `test-bats` が走らなかった:

1. `tests/zshrc/tmux-session/test_resurrect_owner_fingerprint.sh` (platform 依存)
2. `tests/tmux/test_log_restore_hook.sh` (grep の `\t`)

どちらのときも `tests/codex_fanout.bats` の修正が既に push 済みだったが、**CI 上で 1 度も
実行されていなかった**。「CI が緑になったら確認する」と考えていると、赤の原因が別にある間は
永久に検証されない。実際、修正から CI での実行証拠が取れるまで数時間かかった。

## 下がるリスク

- 「push した修正が CI で検証されている」という前提が、**無関係な失敗によって静かに崩れる**
- 落ちたテストを直す人は自分の担当分しか見ないため、隠れた未実行に気づく契機がない

## 対応案 (どれを採るか着手時に判断)

1. **CI のジョブを分ける** — `test-bats` を `test-discovered-rest` と別 job にする。
   独立して結果が出るので隠れない。CI 時間は並列化で相殺できる可能性がある
2. **`make -k` 相当にする** — 最後まで走らせて失敗を集約する。ただし exit code の集約と
   ログの読みやすさを自前で作る必要がある
3. **現状維持 + 運用で補う** — 「CI が緑になった」ではなく「**自分のテスト名がログに出た**」を
   確認する規律にする (`verify-execution-not-just-exit-code.md` の実践)

3 は既にルールとして存在するが、**人間の注意力に依存する**ため 1 が構造的。

## trigger

CI のジョブ構成を次に触るとき。または同じ「隠れて未実行」が 3 回目に起きたとき。

## 関連

- `_claude/rules/verify-execution-not-just-exit-code.md` — 「exit code ではなく実行された証拠で判定する」

---

## 対応 (2026-08-28): 案 2 (集約) を採り、**ファイル粒度まで**塞いだ

案 1 (CI ジョブ分割) でなく案 2 を採ったのは、CI の依存解決 (matrix ごとに Makefile から
package を読むステップ) に触らずに済み、かつ**手元の `make test` にある同じ穴**も一度に
閉じられるため。

### ⚠️ 1 次修正は「壁を動かしただけ」だった

最初は `test-runtime` / `test-runtime-rest` の prerequisite を集約に変え、
「テストを 1 本壊しても `[run] tests/codex_fanout.bats` が出る」ことを確認して満足しかけた。

**敵対的レビューが、それでは不十分だと実証した**: `run_tests` (`test-discovered-rest` の本体) は
**ファイル単位で fail-fast** (`"$$t" || exit 1`) なので、落ちたテストより**ソート順で後ろの
`.sh` は 1 本も走らない**。bats が救われたのは別ターゲットだったからで、
2026-08-25 の事故 (`test_retro_open.sh` の失敗) では `.sh` 側の隠れがそのまま残っていた。

しかも同じ Makefile に非 fail-fast の `run_tests_parallel` があり、
**heavy ジョブは全失敗を報告し rest ジョブは最初の 1 本で止まる**という非対称だった。

### 直した 7 箇所

| 箇所 | 内容 |
|---|---|
| `run_tests` | ファイル単位の fail-fast をやめ、失敗を集めて最後に一覧表示 (並列版と揃えた) |
| `test-bats` | 同上。`codex_fanout.bats` は `_ensure_cli_with_brew.bats` の後ろにソートされるので、**109 が守ろうとした当のファイルが 1 段内側で隠れていた** |
| `test-lint` | 12 本の直列 → 集約。`lint.yml` は `make test-lint` の 1 ステップなので、**末尾の新設検査ほど隠れやすかった** |
| `test-runtime` / `test-runtime-rest` | prerequisite → 集約 |
| `run_all_targets` | 対象 0 件を fail に (兄弟の `run_tests` / `GO_PROJECT_DIRS` と揃える) |
| `$(MAKE)` の再帰マーク | `@+$(call ...)` に。`+` が無いと GNU make が recursive と認識せず、`make -j` で jobserver 警告、`make -n` が再帰しなくなる |
| `test-gnu` の `rc` | 未初期化で、環境に `rc` があると全 pass でも赤になった (false RED) |

### 検証 (実測)

`tests/claude/test_retro_open.sh` を意図的に壊して:

| | 修正前 | 修正後 |
|---|---|---|
| `make test-discovered-rest` で走った `.sh` | 壊れた 1 本まで | **66 本** (最後の `tests/zshrc/validate-mp4/` まで到達) |
| 失敗の報告 | 1 本目で中断 | `✗ 失敗したテスト:` に一覧 |

`make test-lint JSON_FILES=tmp/nope.json` (レビューの再現手順) で、
**今日追加した末尾の 2 検査** (`test-pipefail-grep-q` / `test-trigger-log-writers`) が
走って報告することも確認した (以前は skip されていた)。

`make test` は exit 0。

### 残した穴 → [issue 130](130-refactor-remaining-failfast-in-local-entrypoints.md)

`test:` (トップレベル) / `test-go-lint` / `test-go` / `scripts/test_changed.sh` に同型が残る。
いずれも **CI から呼ばれない**ので P3 として分けた。ただし `test:` を集約にすると
「lint が落ちていてもフルスイートが走る」ので所要時間が伸びる — 運用の好みが絡むため
着手時にユーザーへ確認する、と 130 に書いた。
