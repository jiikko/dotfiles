# vim-ambiwidth (vendored, Lua 移植)

- 取得元: https://github.com/rbtnn/vim-ambiwidth
- コミット: 4fd05792acac85d29bb0dc08f720a49093abefad (2025-08-02)
- vendored: 2026-07-09
- 理由: East-Asian あいまい幅 (ambiwidth) 文字と Nerd Font(Cica) PUA を setcellwidths で全角補正する
  プラグイン。良い Lua 代替が無く日本語環境で必要なため取り込み、Lua へ移植して自前保守する。
- ライセンス: **MIT** (Copyright (c) 2022 Naruhiko Nishino。LICENSE 同梱)。

## ローカル改変 = Lua 移植 (**上流の Vimscript から fork**)

**2026-07-09: 上流 Vimscript を Lua へ全面移植した。** 実体は「`ambiwidth=single` +
`setcellwidths([...])`」だけなので、上流の巨大な生成器 (autoload/ambiwidth_generator.vim ~3800 行) と
データ (list.txt ~4200 行) は移植後不要となり削除。以下に置換:

- `lua/ambiwidth.lua`   — 幅テーブル (base 32 + Cica/Nerd Font PUA 63) と setup()
- `plugin/ambiwidth.lua` — utf-8 かつ setcellwidths 対応時に setup() を呼ぶ loader

移植方針・検証:

- 幅テーブルは上流の**生成物** autoload/ambiwidth.vim (list.txt から生成された 95 レンジ) を機械抽出して
  Lua table 化。生成器自体は移植せず、生成済みの結果だけを持つ (この値は Unicode データのスナップショット)。
- `g:ambiwidth_cica_enabled` (既定 on。false/0 で Cica レンジを除外) と `g:ambiwidth_add_list` は原版どおり尊重。
- 読み込み: `_nviminit.lua` の lazy spec で `dir = config_dir .. "/vendor/nvim-plugins/vim-ambiwidth"`。
  起動時に setcellwidths を張るため遅延トリガは付けず eager ロード。
- **原 Vimscript との A/B で getcellwidths() 完全一致を確認** (既定=95 / cica off=32 / add_list=96 レンジ、
  いずれも差分ゼロ)。

## 更新手順 (fork のため上流とは自動同期しない)

上流 rbtnn/vim-ambiwidth に更新が入ったら、上流の生成物 autoload/ambiwidth.vim (初回起動で list.txt から
生成される) を取得し、base/Cica のレンジを `lua/ambiwidth.lua` へ再抽出する。plugin/ambiwidth.lua の
条件に変更があれば併せて反映し、A/B (原 .vim vs 本 Lua の getcellwidths) を取り直して本ファイルを更新する。
