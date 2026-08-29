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

この repo には [`zsh-hook-return-via-reply.md`](../_claude/rules/zsh-hook-return-via-reply.md) が
あり、「hook 経路の fork が毎操作の体感レイテンシになる」という規律を持っている。あちらは
precmd/preexec だが、**シェル起動は 1 日に何十回も通る経路**で性質は同じ。

## ⚠️ 定数へ置き換えるだけにしないこと

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
