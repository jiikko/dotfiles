# bug: schedkeys ウィザードの一時ファイルが正常復帰のたびに残り、TMPDIR に 14,147 個溜まっている

起票日: 2026-09-06
カテゴリ: bug
優先度: 高（実機の TMPDIR エントリの 77% を 1 スクリプトが占めている。実害は件数による圧迫で、
内容の露出ではない — 下の「露出境界」節）
出典: /audit resource-leaks 2026-09-06（forge Minimum+）。3 エージェントが独立に到達

## 何が起きているか

`scripts/tmux_schedule_keys.sh:cmd_wizard` は予約一覧を書く一時ファイルを `mktemp` で作り、
掃除を **EXIT trap だけ**に預けている。

```sh
cmd_wizard() {
  local pane label jobs_file tab ...      # jobs_file は local
  jobs_file=''
  trap 'rm -f "${jobs_file:-}" 2>/dev/null' EXIT   # シングルクォート = EXIT 時に展開
  jobs_file="$(mktemp "${TMPDIR:-/tmp}/schedkeys-jobs.XXXXXX")" || return 1
  ...
  return 0    # ← 正常復帰。この経路に rm が 1 つも無い
}
```

trap 本文は**シングルクォート**なので `$jobs_file` の展開は EXIT の瞬間に起きる。
`cmd_wizard` が `return` した後にスクリプトが終わる（= 正常系）と、その時点で `jobs_file` は
**local のスコープを抜けている**ので空文字に展開され、`rm -f ""` の no-op になる。
**予約を 1 件入れるたび、取り消すたび、UI を閉じるたびに 1 個ずつ残る。**

## 実測（2026-09-06、私が独立に再現）

| 対象 | 件数 |
|---|---|
| `$TMPDIR/schedkeys-jobs.*`（`cmd_wizard` の jobs_file） | **14,147** |
| `$TMPDIR/schedkeys.*`（`ui_run` の out。別件 → issue 299） | 5 |
| `$TMPDIR` の全エントリ | 18,297 |

機構も最小再現で確認した（bash 5.3 / macOS）:

```
正常復帰 → EXIT:   TRAP saw=[]                       ← 空展開。消えない
関数実行中に TERM: TRAP saw=[/var/folders/.../traptest2.oLp50F]  ← 見えている。消える
```

## 🚨 「trap は一度も機能していない」は誤り（着手前に読むこと）

監査の一次報告は「trap は追加された日から一度も機能していない」と書いていたが、これは**誤り**。
上の実測どおり、**関数実行中の SIGTERM（popup を外から閉じる＝ trap を足した動機だった異常系）では
local はスコープ内なので trap は正しく展開されファイルは消える**。

したがって:

- **真因は「正常復帰パスに `rm` が 1 つも無い」こと**であって、trap の不作動ではない
- **trap を差し替える方向の修正を採ってはいけない**。中断パスの被覆を落とすと、issue 299 と
  同規模の新しい漏れ（9 日で 5 個）を作る

## 推奨対応

1. **正常復帰パスに `rm -f "$jobs_file"` を足す**（trap は中断パス用にそのまま残す）。
   これで 14,147 個の 100% が止まる。グローバル変数化も `mktemp -d` 化も要らない
2. `tests/tmux/test_schedule_keys.sh` に **`export TMPDIR="$TMP_DIR"`** を張る。
   現状 `wizard` を 58 回呼びながら実 TMPDIR を隔離しておらず、**テスト自身が残骸の生産者**
3. **検証は rc でなく実行前後の `schedkeys-jobs.*` の件数**で見る
   （[`verify-execution-not-just-exit-code.md`](../../_claude/rules/verify-execution-not-just-exit-code.md)）
4. **変異検証**: 足した `rm` を消すと残留数が増えて red になることを同じ commit で確認する
5. 既存の 14,147 個を掃除する（`$TMPDIR/schedkeys-jobs.*` 前方一致に限定）

## 🚨 採ってはいけない修正案（監査が出したもの。理由つきで却下）

- **`_SK_JOBS_FILE` へのグローバル化 / `mktemp -d` + `rm -rf` 化**: どちらも環境変数汚染に無防備。
  このスクリプトは `run-shell -b` 経由で tmux サーバの環境を継承する経路を持ち
  （`cmd_fire` の `STATE_DIR` が実例。`resolve_state_dir` が同じ危険を明示的に潰している）、
  破壊的操作の根を env 由来のパスにすることになる。どうしても採るなら **trap 前の無条件初期化 +
  `${TMPDIR}/schedkeys-` 前方一致の `case` 検査**を同じ commit に入れること
  （[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §0-A）

## 露出境界（severity の根拠を分ける）

内容は「予約する送信文字列」だが、`mktemp` の既定は 0600 で macOS の TMPDIR は per-user
(`/var/folders/<...>/T`) なので**同一ユーザー以外には露出しない**。severity の主根拠は
**件数による可用性の圧迫**であって内容の露出ではない（バックアップ・スナップショットに
入る方向の懸念は別途有効）。

## 関連

- issue 299（同じスクリプトの `ui_run` の out。中断パスの漏れ。別機構）
- [`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §0-A
- [`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md)

## 対応済み（2026-09-07 / commit ec1061b5）

`scripts/tmux_schedule_keys.sh` に登録式の後始末を入れた。

- `sk_mktemp` が `mktemp` の結果を `TMPFILES` 配列へ登録し、`REPLY` で返す
- `sk_cleanup_tmpfiles` が `$SK_TMP_PREFIX` で始まるものだけを消す（prefix 不一致は消さない = fail-closed）
- `trap sk_cleanup_tmpfiles EXIT` を 1 本に集約し、`cmd_wizard` の
  `trap 'rm -f "${jobs_file:-}"' EXIT`（後から張られると前の trap を潰す形）を撤去した

### 変異検証

| 変異 | 結果 |
|---|---|
| `trap sk_cleanup_tmpfiles EXIT` を削除 | **red**（正常復帰 3 経路の残骸テストが全て落ちる） |
| `ui_run` の `sk_mktemp` を素の `mktemp` に戻す（= 299 の形） | **red**（ビルド OK を確認したうえで rc=1） |

### 実測

- テスト側の `TMPDIR` 隔離が無かったため、`tests/tmux/test_schedule_keys.sh` は
  **1 回の実行で 61 件**を `$TMPDIR` に落としていた。修正後は 0 件
- 掃除の実測: `schedkeys-jobs.*` / `schedkeys.*` を削除して **TMPDIR 18,302 → 4,136 エントリ**
  （削除 14,166 件のうち schedkeys 由来 14,152 件 ≒ 232 回ぶんのテスト実行）
