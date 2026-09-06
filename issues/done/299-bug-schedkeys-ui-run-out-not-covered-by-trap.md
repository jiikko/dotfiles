# bug: `ui_run` の一時ファイルが EXIT trap に載っておらず、popup を外から閉じると残る（同型が 1 本ある）

起票日: 2026-09-06
カテゴリ: bug
優先度: 中（9 日で 5 個。issue 298 と同じスクリプトだが**機構が逆**で、あちらは正常系、こちらは中断系）
出典: /audit resource-leaks 2026-09-06（forge Minimum+）。3 エージェントが独立に検出

## 何が起きているか

`scripts/tmux_schedule_keys.sh:ui_run` は結果受け取り用の一時ファイルを作るが、
掃除は**成功パスの `rm -f "$out"` だけ**で、EXIT trap は `jobs_file` しか見ていない。

```sh
ui_run() {
  local jobs_file=$1 label=$2 start=${3:-} out rc=0
  out="$(mktemp "${TMPDIR:-/tmp}/schedkeys.XXXXXX")" || return 1
  "$UI" ... --out "$out" ... || rc=$?
  ...
  rm -f "$out"       # ← UI が走り切ればここを通る
}
...
trap 'rm -f "${jobs_file:-}" 2>/dev/null' EXIT   # out は対象外
```

**UI が走っている最中に popup を外から閉じる**（ウィンドウを閉じる / `kill-session`）と、
`"$UI"` の実行中に SIGTERM が来て `rm -f "$out"` に到達しない。EXIT trap は `jobs_file` しか
消さないので `out` が残る。

## 実測

- `$TMPDIR/schedkeys.*` = **5 個**（2026-08-28〜09-02。`schedkeys-jobs.*` の 14,147 個とは別）
- bash の EXIT trap は SIGTERM で走ることを 2 者が独立に実測済み（SIGKILL は救えない）

## 同型がもう 1 本ある（横展開）

`scripts/check_go_project_lanes.sh` の `ph_probe=$(mktemp)` が**完全に同じ形**
（trap 無し、掃除は成功パスの `rm -f` だけ）。lint スクリプトなので窓は短く実害は小さいが、
**同じ commit で洗うこと**（[`instrument-before-second-fix.md`] 系の「直したバグは別の場所にもある
前提で grep する」）。

実測: `scripts/` + `bin/` で `mktemp` を使いながら trap を 1 つも持たないのは、この 2 本
（`tmux_reap_orphan_servers.sh` はコメント内の言及のみ）。

## 推奨対応

1. スクリプト先頭で **`TMPFILES` 配列 + EXIT trap を 1 本だけ**張り、`mktemp` した側が push する形へ
   一本化する（`jobs_file` も `out` も同じ経路に載る）
2. `check_go_project_lanes.sh` にも同じ形を当てる
3. テストは「UI stub がファイルを作ったことを**上限つきポーリング**で確認 → SIGTERM →
   `$TMPDIR/schedkeys.*` が 0 件」。壁時計に依存させない
   （[`avoid-wall-clock-assertions.md`](../../_claude/rules/avoid-wall-clock-assertions.md)）

## 🚨 採ってはいけない修正案（却下理由）

- **「`ui_run` で EXIT trap を張り、関数末尾で `trap - EXIT` して `cmd_wizard` 側を張り直す」**:
  bash の EXIT trap は 1 本しか持てず、張り直しは trap 文字列の二重管理になる。
  次に触った人が片方だけ直す（パッチワーク）。`TMPFILES` 配列への一本化だけが前提の是正

## 関連

- issue 298（同じスクリプト。正常復帰パスの漏れ。**修正を混ぜないこと** — 298 の trap 差し替えは
  こちらの被覆を落とす）
- issue 307（この形を機械で止める gate の検討）

## 対応済み（2026-09-07 / commit ec1061b5）

298 と同じ `sk_mktemp` / `sk_cleanup_tmpfiles` へ寄せて閉じた（`ui_run` の
`out="$(mktemp ...)"` を `sk_mktemp ".XXXXXX"; out=$REPLY` に置換）。

### 変異検証

- `ui_run` の `sk_mktemp` を素の `mktemp` に戻す → **red**（ビルドできたことを確認したうえで rc=1）
- 中断経路のテストは `exec` でシグナルを実体へ届かせ、成立条件を上限つきでポーリングする形にした
  （`( ... ) &` のサブシェルに `kill` を撃つと**スクリプト本体に届かない**。このセッションで
  2 度踏んだので両方のテストにコメントを残した）

### 検証時に踏んだ罠（記録）

- **`make test` の並列実行下でだけ落ちる形**があった。wizard を kill したあとスタブを解放すると、
  孤児になったスタブが `$out` を書き直して後始末を打ち消す。スタブの書き込みを待ちループの**前**へ
  移し、ポーリング上限を 10 秒 → 60 秒にして 3 回連続 green を確認した
