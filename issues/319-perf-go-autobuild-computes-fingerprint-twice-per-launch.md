# perf: `go_autobuild_exec` が --async 経路でビルド指紋を 2 回計算し、1 回目を捨てている

起票日: 2026-09-07
カテゴリ: perf
優先度: 中（**glogx を起動するたびに毎回**払う。3.83ms/起動）
出典: /audit performance 2026-09-06（forge Minimum+）。変異検証済み

対象: `bin/lib/go_autobuild.zsh:go_autobuild_exec`

## 何が無駄か

```zsh
local fp
_go_autobuild_fingerprint "$src_dir"     # ← ① 無条件に計算
fp=$REPLY

if [[ ! -x "$bin" ]]; then
  _go_autobuild_build ...
elif (( async )); then
  go_autobuild_spawn_if_stale "$src_dir" "$name"   # ← ② この中で自分で計算し直す
elif _go_autobuild_stale "$src_dir" "$bin" "$fp"; then   # ← fp を読むのはここだけ
  ...
fi
```

`fp` の読み手は**同期分岐の `_go_autobuild_stale` だけ**。
`--async`（既定の経路）では ① の結果は一度も使われず、`go_autobuild_spawn_if_stale` が
自前で `_go_autobuild_fingerprint` を呼び直している。

## 実測

| 内訳 | 時間 |
|---|---|
| 入力 glob | 3.542 ms |
| `zstat` × 2 | 0.288 ms |
| **計** | **3.83 ms / 起動** |

位置づけ: shim オーバーヘッド 17.4ms の **22%**、`bin/glogx --help` 45.52ms の **8.4%**。
実行証跡は `FIFI` → 変異後 `FI` で、挙動不変まで確認済み。

## 🚨 修正の形（素朴なガードは採らない）

**✗ `if (( ! async ))` で囲むだけ**にすると、`fp` が**空のまま外側スコープに残る**。
`_go_autobuild_stale` には

```zsh
if [[ -z "$fp" ]]; then
  # 指紋が取れない環境 (zstat 不在) では順序比較へ縮退する
  _go_autobuild_sources_newer_than "$bin" "$src_dir"
```

という縮退分岐があり、**2026-08-01 に 2 件のバグを出して廃した mtime 順序比較**へ、
将来の改修で無言で戻る余地を作る。

**✗ 計算済みの `fp` を `go_autobuild_spawn_if_stale` へ引き回す**のも不可。
同ファイルの doc が「`_go_autobuild_build` が**開始時点で**指紋を取り直す = 鮮度」を
不変条件として書いており、採取から判定までの窓に着地した編集を「最新」と誤認して
**旧バイナリで exec する**余地を作る。

**✓ 採る形**: 指紋計算を**同期分岐のローカルへ閉じ込める**（`elif` の直前ではなく、
その分岐の中で計算する）。空の `fp` が外側に残らない構造にする。

## 受け入れ条件

- [ ] `--async` 経路で `_go_autobuild_fingerprint` の呼び出しが 1 回になる
- [ ] `fp` が空のまま `_go_autobuild_stale` へ渡る経路が構造的に存在しない
- [ ] **退行検出**: `tests/tmux/bench_tmux.sh` と同じ計数ラッパーで F/I の回数を pin する
- [ ] **変異検証**: 指紋計算を async 経路へ戻すと回数が増えて red
- [ ] 挙動不変（同期ビルド / `GO_AUTOBUILD_SYNC=1` / 初回のバイナリ不在）を確認する

## 関連

- issue 320（同じ「同じ問いを 2 回計算している」ファミリーの Go 側）
