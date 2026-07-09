# vim-ambiwidth (vendored)

- 取得元: https://github.com/rbtnn/vim-ambiwidth
- コミット: 4fd05792acac85d29bb0dc08f720a49093abefad (2025-08-02)
- vendored: 2026-07-09
- 理由: 東アジアあいまい幅 (ambiwidth) 文字の表示幅を setcellwidths で補正する小さな Vimscript
  プラグイン。良い Lua 代替が無く、日本語環境で必要なため repo 内に取り込む (Lua 移植はしない)。
- 読み込み: `_nviminit.lua` の lazy spec で `dir = config_dir .. "/vendor/nvim-plugins/vim-ambiwidth"`。
  plugin/ambiwidth.vim は起動時 (has('vim_starting')) に ambiwidth#set_ambiwidth() を呼ぶため、
  遅延トリガを付けず eager ロードする (上流と同じ挙動)。
- ライセンス: **MIT** (Copyright (c) 2022 Naruhiko Nishino。LICENSE 同梱)。

## 生成ファイルについて (重要)

`autoload/ambiwidth.vim` は `list.txt` から `autoload/ambiwidth_generator.vim` が生成する派生物で、
**上流は autoload/.gitignore で追跡除外**している (初回起動時に生成)。本 vendor では生成済みファイルを
**同梱して追跡**する (上流の .gitignore は持ち込まない):

- 目的: plugin/ambiwidth.vim は `filereadable(autoload/ambiwidth.vim)` が false のときだけ生成を走らせる。
  同梱しておけば runtime 生成 (= vendor ディレクトリ = repo への書き込み) が起きない。
- 生成物は list.txt からの決定的な出力でマシン非依存。

## 更新手順

上流の該当コミットから plugin/ / autoload/ambiwidth_generator.vim / list.txt / LICENSE / README.md を
再コピーし、`autoload/ambiwidth.vim` を list.txt から再生成 (nvim で ambiwidth_generator#make_vimscript()
を呼ぶ) して同梱、本ファイルのコミット/日付を更新する。autoload/.gitignore は持ち込まない。
