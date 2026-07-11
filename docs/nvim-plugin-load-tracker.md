# Neovim プラグインロードトラッカー

「使っていないプラグイン」を勘でなく数値で棚卸しするための仕組み。lazy.nvim がプラグインをロードした瞬間に発火する `User LazyLoad` autocmd を拾い、ロード回数をファイルに永続化する。

- 実装: `nvim/lua/dotfiles/plugin_load_tracker.lua`(`_nviminit.lua` 末尾で setup)
- 保存先: `~/.local/state/nvim/plugin-loads.json`(マシンローカルな使用実績のため state 側。repo では同期しない)
- 確認: `:PluginLoadStats` — 計測対象プラグインをカウント少ない順に表示(count 0 が先頭に来る)
- **off にする**: `~/.zshenv` 等で `export DOTFILES_PLUGIN_LOAD_TRACKER=0`(記録もコマンド登録も止まる)
- **リセット**: `:PluginLoadStatsReset` — 実績ファイルを削除して計測をやり直す(棚卸しの計測期間を仕切り直すときに使う)

## 使い方(棚卸しの手順)

1. 普段どおり 1〜2 週間 nvim を使う
2. `:PluginLoadStats` を実行する
3. `count = 0` のまま、かつ `last: -` のプラグインは、その期間一度もトリガーされていない = 削除候補
4. 削除したら `docs/nvim-plugins.md` の棚卸し表も更新する

`count` は「そのプラグインを使ったセッション数」に近い値。lazy.nvim のロードは 1 セッションに 1 回しか起きないため、キーを何回押したかではなく「使ったセッションがどれだけあったか」を数えている。

## ⚠️ 計測できるものとできないもの(重要な制約)

この仕組みは「**ロード = 使用**」が成立するプラグインしか計測できない。

### 計測できる(トリガーゲート型)

ユーザーの明示的な操作・特定のファイル種で初めてロードされるもの。ロードされた事実がそのまま使用の証拠になる。

| プラグイン | トリガー |
|---|---|
| telescope.nvim | `<leader>ff` 等のキー / `:Telescope` |
| nvim-tree.lua | `<C-e>` / `:NvimTreeToggle` 等 |
| mason.nvim | `:Mason` |
| render-markdown.nvim | `.md` / `.markdown` を開いた |
| sidekick.nvim | `<C-Space>` / `<leader>sc` 等 |

(一覧は固定ではない。`:PluginLoadStats` が spec から動的に判定して表示する)

### 計測できない(無条件ロード型)— カウント対象外

`event = "VeryLazy"` やパターン無し event(`BufReadPre` / `InsertEnter` 等)のプラグインは、**使っていなくても毎セッションロードされる**ため、カウントしても起動回数にしかならない。ノイズを避けるため計測対象から除外している。

該当: which-key / bufferline / noice / vimade / incline / neoscroll / nvim-scrollview / nvim-early-retirement / mini.trailspace / toggle.nvim / gitsigns / conform / nvim-lint / blink.cmp / indent-blankline / lspconfig ほか eager ロードの全プラグイン。

これらの「本当に使っているか」は load イベントでは観測できず、プラグインごとに個別のプロキシ(which-key なら popup 表示、neoscroll ならスクロール関数呼び出し…)を仕込む必要がある。コストの割に得るものが少ないため実装していない(2026-07-11 の判断。UI 系は使用実感ベースで棚卸しする)。

なお conform のように keys/cmd を持っていても、無条件 event(`BufWritePre`)を併せ持つものは「使わなくてもロードされる経路がある」ため除外側に倒している(判定ロジックは `is_trackable()` 参照)。

## 判定ルール(is_trackable)

1. eager(`lazy = false` 相当)→ 対象外
2. パターン無し event が 1 つでもある → 対象外(無条件ロード経路がありカウントが汚染される)
3. keys / cmd / ft ゲートがある → 対象
4. パターン付き event(`BufReadPre *.md`)のみ → 対象

## 関連

- `docs/nvim-plugins.md` — プラグイン棚卸し表(削除判断の本体)
- lazy.nvim `User LazyLoad`: <https://lazy.folke.io/usage#-user-events>
