# research: dead-code / broken-code 監査の記録（2026-09-06）— 全数勘定・却下理由・未決着

起票日: 2026-09-07
カテゴリ: research
出典: `/audit` dead-code + broken-code / forge Minimum+
（`go-architecture-designer` / `architecture-reviewer` / `test-coverage-advisor` + クロスレビュー + 統合。
各 7 体、dead-code 約 35 分 / broken-code 約 37 分）

却下理由を残すための issue（残さないと次の audit が同じ指摘を再生成する）。
resource-leaks の記録は [issue 308](308-research-resource-leaks-audit-2026-09-06.md)。

## 全数勘定

| | dead-code | broken-code |
|---|---|---|
| 統合後 | High 4 / Medium 15 / Low 5 | High 3 / Medium 9 / Low 8 |
| 除外 | 3 | 4 |
| 未決着（両論併記） | 5 | 3 |
| **issue 化** | **315 / 316 / 317**（+ 314） | **310 / 311 / 312 / 313** |

## main agent が実測で裏を取ったもの

| 主張 | 実測 |
|---|---|
| `git-state-verify.sh` のトリガが `git -C` 形を拾わない | ✅ 7 形式で確認。`git -C` / `--no-pager -C` / `-c k=v` が NO-MATCH |
| 同 hook が**このセッションで実際に誤発火**した | ✅ `echo`+`grep` だけのコマンド / heredoc の例示文で発火し、`~/dotfiles` の state と**別セッションの commit message** を注入した |
| `--branches --not --remotes` が detached を見ない | ✅ 隔離 repo で再現。同型は **3 ファイル 4 箇所**（`grep` で機械計数） |
| `git-state-verify.sh` にテストが 0 本 | ✅ 兄弟 hook は 1〜2 本ずつ持つ |
| `YAML_FILES` の漏れ | ✅ 実在 27 本中 **13 本**が未登録 |
| 3 桁決め打ち | ✅ 3 箇所（テスト 1 / hook 2） |
| `RenderLine` / `RenderTable` の production 参照 | ✅ **0 件**（`RenderTableGroups` / `RenderDashboard` は各 2 件で生存） |
| `anchorCursor` / `HasFailures` / `tt_capture_contents_on` / `assert_function_exists` | ✅ production 参照 0 |
| `ratelimit_resets.yaml` の削除 | ✅ commit `80c1a5ad` で削除済み |
| `lockman.Renew` の非対称 | ✅ `Release` は `expired()` を見るが `Renew` は token 照合のみで `O_TRUNC` する |

## 🚨 監査自身の全数勘定が破れていた（最重要の申し送り）

dead-code の一次報告は **「唯一の実害は 1 件」**と書いていたが、これは
**Go 7 モジュール中 2 つ（glogx / doctor）しか走査していない**結論だった。
決定打だった `staticcheck -checks=U1000 -tests=false` を残り 5 モジュールへ回すと、
同種のヒットが **2 件実在**した（`parallel-each/runner.go:loadProcessedLines` /
`schedkeys/editor.go:setValue`。`disassemble_excel` / `lockman` / `termsafe` は 0 件）。

[`CLAUDE.md`](../CLAUDE.md)「issue の『不在の主張』は、着手前に数え直す」の実例。
**指摘の質は高かったが、結論の射程だけが誤っていた**。

🚨 なおこの 2 件は**死蔵ではなく正当な test seam** なので、削除対象として扱わないこと（issue 315）。

## 🚨 申し送りの中に誤った一般則があった

> 「Go の field/func 削除はコンパイラが全 callsite を指すので取りこぼしは構造的に起きない」

**誤り**。コンパイルを通したまま意味が変わる経路が 3 つあり、うち 2 つはこの repo に実在する:

1. **`==` で比較される構造体**（`ratelimitCacheKey`）— フィールドを落とすとキャッシュキーの同一性が変わる
2. **位置指定の複合リテラル** — 増減で別フィールドへ値が入る
3. **reflect 経由**（`overlay_ownership_test.go` がフィールドを数え上げる）— 到達解析の外

issue 316 の冒頭に注意として転記済み。

## 却下した指摘（理由つき。再生成防止）

### ① zshlib の 8 本を `ZSH_SYNTAX_FILES` へ移す → **カバレッジを下げる誤った修正**

`Makefile:39` が `SHELLCHECK_FILES := $(filter-out $(ZSH_SYNTAX_FILES), ...)` で両集合を
**排他**にしているため、移動 = **shellcheck 対象から外すこと**。
これらは動画ファイル名（ユーザー由来の任意文字列。空白・グロブ・改行を含みうる）を扱うコードで、
**SC2086 系（未クォート展開）はこの層で最も効く検査**。`zsh -n` は置き換えにならない。
`Makefile:7` が「同じ `.zsh` でも `zshlib/_av1ify.zsh` は sh 互換で shellcheck 側」と
意図を明記しており、[`verify-design-intent-before-refactor.md`](../_claude/rules/verify-design-intent-before-refactor.md)
の「意図的に選ばれた設計」に当たる。**ヘッダの射程を実装に合わせる案のみ採用**（issue 313 ③）。

### ② `case ... in *[!0-9]*)` の数値ゲートが全角数字を通す → **発火条件を示せない**

実測: bash の `[[ =~ ^[0-9]+$ ]]` は全角数字を**弾く**（`LANG=ja_JP.UTF-8` / bash 3.2 と 5 の両方）。
通すのは glob の `case` の方だけ。repo 内のヒットは**すべて「非数値を既定値へ倒す fallback」**で、
[`shell-numeric-gate-explicit-digits.md`](../_claude/rules/shell-numeric-gate-explicit-digits.md) が
明示的に射程外としている形。唯一の reject ゲート `tmux_schedule_keys.sh:new_reservation` は
`=~` を使っており安全。`tmux_resurrect_guards.sh` の入力源も
`tmux display-message -p '#{pid}'` / `date +%s` / `stat` の出力だけで全角も 19 桁も流入しない。
**触る機会があれば `[0123456789]` へ寄せる**、に留める。

### ③ `pace5_raw`（`tests/claude/test_statusline.sh`）の削除 / `bin/reset-universalcontrol` の finding 化

前者は監査自身が除外、後者は 2 行の `killall` スクリプトで finding に値しない。

### ④ 既に 308 で決着済みのもの（3 エージェントとも重複確認のうえ再提起せず）

- `gitlog.go` の全称 doc と `gitlog_watch.go` の受容の文面不一致 → 308 §① で「コード変更不要」
- `tt_on_default_server` の fail-open → 308 §⑧ で「発火を再現できていないため据え置き」。
  **再現ハーネスを作れた時点で初めて起票する**

**308 に却下理由を残したことが、この監査で実際に再生成を止めた。** 記録の費用対効果の実証。

## 未決着（両論併記。着手時に判断）

1. **未 push 判定の代替式**（issue 311）— `--all` は **stash を未 push commit として拾う**
   （実測: `git stash` 1 回で `WIP on master` / `index on master` の 2 件）うえ、
   **別 worktree の detached commit まで返る**（`--single-worktree` を足しても抑止されない）。
   `HEAD --not --remotes` 推奨
2. **未 push 判定を lib へ 1 本化するか 2 本に分けるか**（issue 311）—
   `git-state-verify:35` と `next-claim-unshared:88` は「まだ共有されていないか」（over-report が安全側）、
   `next-claim-unshared:112` は「この push で送られる集合」（over-report が**嘘になる**）。
   意味論が違うので `issue_hook_unshared` / `issue_hook_will_push` の 2 本に分ける案がある。
   🚨 detached HEAD からの素の push は既定で何も送らないので、:112 の案内文言は detached 時の手当てが要る
3. **`issue-progress-check.sh` の `next/` 走査**（未起票）— claim があるだけで無関係セッションを
   block する偽陽性がある。差分方式（SessionStart 時点の `next/` 集合と比較）は systematic な
   偽陽性を消すが、**「昨日 claim して今日も番号を出さずに作業している多セッション作業」**という
   next/ 経路の最大の価値を落とす。`next/` 由来は block でなく「参考」扱いにし、
   block を commit subject 由来に限る案が対立している
4. **`anchorCursor` の修正方針と severity**（issue 316 に両論併記）
5. **`stoppedByCancelAll` 列の severity**（issue 316 の対象外。下記）
6. **`usage.RenderLine` / `RenderTable` の凍結理由が今も有効か**（issue 316 / 317）
7. **`tt_capture_contents_on` — 削除か共有ガードへ寄せるか**（issue 316 に両論併記）
8. **`doctor-history` の prune**（308 §⑥ から継続）

## 起票しなかったが記録に残すもの

- **`overlay_ownership_test.go:stoppedByCancelAll` 列が書き込み専用**: 表は
  `stoppedByCancelAll: false` + 「🚨 既知の穴（issue 213 が記録）」を明記しているので、
  「表を読んだ人が守られていると誤解する」という high の根拠は弱い。
  ただし**検査を足すべき**という方向は両者一致。足すなら
  `TestOverlayStoppedByCancelAllMatchesCancelAll` を表駆動で作り、散在する既存テスト 3 本
  （`issues_watch_seam_test.go` / `doctor_view_test.go` / `gitlog_watch_test.go`）をそこへ寄せる。
  🚨 変異は**ケース名ごとの pass/fail 一覧**で読む（スイートの rc だと他ケースの red に隠れる）
- **Stop hook の順序**: idle 通知（完了の合図・ベル）が `issue-progress-check` の block より
  先に走る（`_claude/settings.json`）。「終わった音がしてから差し戻される」形
- **`setup.sh:73-75` の `_common` リンクのコメントが実依存と乖離**: 消すと 4 本の agent が静かに壊れる
- **`src/glogx/ime_tis_stub.go`**: `//go:build !darwin || !cgo` は macOS 専用 repo では
  **どの lane もコンパイルしない**（issue 316 の表に含めた）

## 攻めたが 0 件だった範囲

- **Go の素朴な未使用シンボル: 構造的にゼロ**。`unused` + deadcode が CI で緑を保っている
  （検出できないのは「テストだけが呼んでいる」クラスだけ = issue 315）
- **shell の死蔵**: glob 発見型の配線（`discover_shell_scripts.sh`）のおかげで起きにくい設計。
  例外は手書き列挙の 2 箇所（issue 313）
- **broken-code の Go 側**: `lockman.Renew` の 1 件のみ。監査全体の射程は
  **shell の hook / セットアップ配線に偏っていた**（射程の申告として記録）
- **issue 298-308 との重複**: 3 エージェントとも 0 件
