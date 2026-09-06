# 140 perf: `_zshrc` が対話シェル起動ごとに `brew --prefix` を fork している

起票日: 2026-08-29 / 種別: perf / 優先度: P2

## 事実 (実測)

`_zshrc:673`:

```zsh
if command -v brew > /dev/null 2>&1; then
  brew_prefix="$(brew --prefix)"
```

- `brew --prefix` の実測: **10 回 96ms = 1 回あたり 9.6ms** (この機体)
- zsh の startup 実測 (`tests/zshrc/bench_zsh.sh`) は **51ms**
- → **startup の約 19%** を、値がほぼ不変の 1 コマンドの fork に払っている

返る値は `/opt/homebrew` で、マシンを変えない限り変わらない。

## なぜ直す価値があるか

この repo には [`zsh-hook-return-via-reply.md`](../../rules/zsh-hook-return-via-reply.md) が
あり、「hook 経路の fork が毎操作の体感レイテンシになる」という規律を持っている。あちらは
precmd/preexec だが、**シェル起動は 1 日に何十回も通る経路**で性質は同じ。

## 🚨 定数へ置き換えるだけにしないこと

`/opt/homebrew` は Apple Silicon の既定で、**Intel Mac は `/usr/local`**。決め打ちにすると
別マシンで壊れる。キャッシュする形にして、キャッシュが無効なときだけ `brew --prefix` を呼ぶ。

## 対応案

- 解決結果をファイル等にキャッシュし、`brew` の実体が変わったときだけ再解決する
- あるいは `HOMEBREW_PREFIX` (brew 自身が export する) が使えるなら fork ゼロで済む。
  **要調査**: 非対話・新規シェルで既に入っているか

## 受け入れ条件

- [ ] `tests/zshrc/bench_zsh.sh` の `startup` が改善することを **before/after の数字で示す**
      (`perf-claims-need-measurement.md`。数字なしで「速くなった」と書かない)
- [ ] Intel Mac (`/usr/local`) でも壊れない形であること (決め打ちにしない)
- [ ] brew 未導入の環境で従来どおり静かに skip されること

---

## 対応 (2026-08-29): brew 実体の位置から導出して fork を消した

`HOMEBREW_PREFIX` は**新規シェルでは未設定**だった (この repo は `brew shellenv` を呼んでいない)
ので、その案は使えなかった。代わりに **brew 実体のパスから導出**する:

```zsh
brew_prefix="${commands[brew]:h:h}"                          # /opt/homebrew/bin/brew → /opt/homebrew
[[ -d "$brew_prefix/share" ]] || brew_prefix="$(brew --prefix)"   # 非標準の導入だけ fork
```

`$commands` は zsh のコマンドハッシュなので **fork しない**。決め打ちではないので Intel Mac
(`/usr/local`) でも正しい。

### 実測 (bench_zsh.sh。各 metric は min-of-5)

| metric | before | after | 差 |
|---|---|---|---|
| startup | 52.5 ms | **42.3 ms** | **-10.2 ms (-19.4%)** |
| first_command | 95.4 ms | 75.6 ms | -19.8 ms |
| prompt_lag | 24.2 ms | 22.3 ms | -1.9 ms |

startup の削減幅は、事前に測った `brew --prefix` 1 回のコスト **9.6ms** とほぼ一致する
(予測と実測が合っている)。

### 正しさの確認

- 対話シェルで `brew_prefix=/opt/homebrew`、autosuggestions が読め、未導入の案内は **0 件**
- **フォールバック経路も実測**: 2 つ上に `share/` が無い偽の導入を作ると、導出を捨てて
  `brew --prefix` の値 (`realprefix`) を採ることを確認。正しさは維持したまま通常経路の fork だけ消した
- `brew` 未導入の環境では外側の `command -v brew` で従来どおり skip される (変更なし)

### 受け入れ条件

- [x] before/after の数字を示した
- [x] Intel Mac (`/usr/local`) でも壊れない (決め打ちにしていない + フォールバックを実測)
- [x] brew 未導入の環境で従来どおり skip される

### 残課題

なし。
