# 563 (bug): nvim のプラグインの実体が lock ファイルとずれていても気づけない (nvim-treesitter が旧 master のまま)

起票日: 2026-09-27

## 概要

2026-09-27 に KOJIm2-MacBook-Air で `make test-nvim` を回すと、`tests/nvim/test_nvim.sh` が落ちた:

```
Failed to run `config` for nvim-treesitter  /Users/koji/dotfiles/_nviminit.lua:249: attempt to call field 'install' (a nil value)
```

- `_nviminit.lua` は nvim-treesitter の `main` (rewrite 系。`install()` がある) を前提にしている。`_lazy-lock.json` も `"branch": "main", "commit": "5a7e5638…"`
- このマシンの実体 `~/.local/share/nvim/lazy/nvim-treesitter` は **`e329e94a` (2025-03-24、旧 master 系。branch 無し)** のまま。旧 master に `install` は無い
- つまりテストだけでなく、**このマシンで nvim を起動するたびに config のエラーが出ている**はず (起動して目では確かめていない)
- `_nviminit.lua` のコメントは「branch を変えたら :Lazy update nvim-treesitter + :TSUpdate」と手順を書いているが、別のマシンでそれを打つ契機が無い

## 対応方針 (案)

- 直し方 (このマシン): `nvim --headless "+Lazy! restore" +qa` で lock ファイルの commit に揃え、`:TSUpdate` で parser を入れ直す
- 構造: lock ファイルと実体の commit のずれを検出する口を置く (`make doctor` 相当か、`tests/nvim` の先頭で「実体が lock と違う」を原因つきで出す)。
  今はずれが「attempt to call field 'install'」という遠い症状でしか出ない

## 受け入れ条件

- [ ] このマシンの nvim-treesitter が lock ファイルの commit になり、`make test-nvim` が通る
- [ ] 実体が lock ファイルとずれたとき、原因 (どのプラグインがどの commit か) が出る
