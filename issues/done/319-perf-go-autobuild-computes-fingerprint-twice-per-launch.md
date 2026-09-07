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

---

## 対応結果 (2026-09-08)

### todolist (受け入れ条件)

- [x] `--async` 経路で `_go_autobuild_fingerprint` の呼び出しが 1 回になる
- [x] `fp` が空のまま `_go_autobuild_stale` へ渡る経路が構造的に存在しない
- [x] 退行検出: F/I の回数を pin する
- [x] 変異検証: 指紋計算を async 経路へ戻すと回数が増えて red
- [x] 挙動不変（同期ビルド / `GO_AUTOBUILD_SYNC=1` / 初回のバイナリ不在）を確認する

### 採った形

issue の「✓ 採る形」どおり、`elif _go_autobuild_stale ...` を `else` + 内側 `if` に開き、
**指紋計算をその分岐のローカルに閉じ込めた**。`local fp` の宣言ごと中へ入れたので、
async / 初回ビルドの経路では `fp` が**存在しない** (空の `fp` が縮退経路へ渡る形を作れない)。

`_go_autobuild_stale_now` のような「指紋 + 判定」のヘルパーへの共通化は**採らなかった**。
`_go_autobuild_stale` は内部で `_go_autobuild_slurp` を呼んで `REPLY` を潰すため、
ヘルパーは `REPLY` を退避・復元する必要があり、2 行の重複を消すために
この機構でいちばん避けたい種類の細工が増える。

### 実測

| 経路 | 修正前 | 修正後 |
|---|---|---|
| `--async` + 最新 (既定の経路) | `FIFI` (2 回) | `FI` (1 回) |
| 同期 (`GO_AUTOBUILD_SYNC=1`) + 最新 | `FI` | `FI` |
| 初回 (バイナリ不在 → 同期ビルド) | `FIFI` (2 回) | `FI` (1 回) |

F = `_go_autobuild_fingerprint` / I = `_go_autobuild_inputs` の呼び出し。

削減量は **1 起動あたり 1 回ぶんの入力走査**。この repo の `src/glogx` で実測すると
**min 4.225ms / avg 4.574ms** (n=15、`zsh/datetime` で計測。1 回捨ててから測定)。
issue 起票時の監査値 3.83ms と同オーダー。

### 退行検出 (tests/bin/test_go_autobuild.sh)

`bin/tool` ラッパーを「`_go_autobuild_fingerprint` / `_go_autobuild_inputs` を数える版」に
差し替えて trace を取り、3 経路すべてで `FI` であることを pin した。

- **F だけでなく I も数える**: 重いのは I の glob なので、入力走査を指紋の外へ持ち上げる
  改修では F は 1 のまま I だけ増える
- **stale でない状態で測る**: stale だと builder が裏で走り、その指紋計算が trace に混ざる
- **前提の pin を置いた**: 最初に書いた版は「バイナリを作らずに `freeze` した」ため
  `[[ ! -x $bin ]]` の初回ビルド分岐へ落ちており、**async のケースが async を一度も通って
  いなかった** (`freeze` の `touch -t` は不在のファイルを非実行で作る)。
  変異 2 を当てて初めて気づいたので、`[[ -x ... ]]` / `[[ ! -e ... ]]` を前提として assert してある

### 変異検証

| 変異 | async | 同期 | 初回 | 既存テスト |
|---|---|---|---|---|
| ① 指紋計算を分岐の手前へ戻す (= 修正前の形) | **red** `FIFI` | green | **red** `FIFI` | 全 green (64 ✓) |
| ② `_go_autobuild_stale` が渡された指紋を捨てて取り直す | **red** `FIFI` | **red** `FIFI` | green | 全 green (64 ✓) |

3 ケースすべてが少なくとも一方の変異で red。**変異では既存テストは 1 件も落ちない**
(= 変異が挙動中立 = この修正も挙動中立) ことを事前に予測し、実測で一致した。

🚨 3 つ目の変異候補「`local fp` を外側にも残したまま `else` 側でも宣言する」は**採らなかった**。
zsh は同一スコープで 2 度目の `local <name>` を打つと**変数の内容を stdout へ表示する**ため、
指紋が起動出力に混ざって既存テストが 6 件落ちる。実際の退行の形ではないので変異として不適格。

### 検証

- `bash tests/bin/test_go_autobuild.sh` rc=0 (68 ✓)
- `bash tests/bin/test_go_autobuild_warmup.sh` rc=0
- `make test`: 失敗 1 件 = `tests/claude/test_dangling_symlinks.sh`。**この変更とは無関係**で、
  `~/.config/nvim/*.pre-dein-vim` が削除済みの `~/dotfiles/_nvimconfig` を指したまま残っている
  環境の状態 (symlink の作成日は 2023-11 / 2024-12、`_nvimconfig` は commit `502cb237` で削除済み)。
  掃除は `./setup.sh` の担当なので触っていない

### 残タスク

- なし (スコープ外: issue 320 = 同じ「同じ問いを 2 回計算している」ファミリーの Go 側)
