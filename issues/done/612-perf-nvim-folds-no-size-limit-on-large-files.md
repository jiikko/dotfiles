# 612 (perf): nvim の fold の計算 (folds.lua) が大きいファイルで秒単位で固まる (サイズの上限が無い)

起票日: 2026-10-02

(手元では 608 で起票したが、push 前に別セッションの 608 と衝突したので 612 へ改番した。commit message の「608 (perf) nvim」はこの issue)

## 概要

`nvim/lua/dotfiles/folds.lua` の `refresh_win` は、バッファ全体を treesitter で同期にパースし、`foldmethod=expr` にして
`zx` で全行の foldexpr を評価してから `manual` に戻す。コストは行数に比例するのに、対象を絞る `eligible(buf)` は
`buftype == ""` と「読み込み済み」しか見ておらず、**サイズの上限が無い**。大きい JSON・ログ・ダンプを開くと、
BufWinEnter の 1 回で秒単位で固まる。

## 詳細 (実測)

計測: nvim v0.12.5、本物の設定 (`~/.config/nvim/init.lua` → `_nviminit.lua`)。入力は 1 行 1 要素の JSON
(`{"id": N, "name": "itemN", "tags": ["a", "b"]}` を N 行)。2026-10-02 に測った。
**壁時計**は `nvim --headless <file> +qa` の開始から終了まで、**autocmd** は `--startuptime` の
`sourcing nvim_exec2() called at BufWinEnter Autocommands for "*"` の行。folds を外すのは
`--cmd "lua package.preload['dotfiles.folds']=function() return {setup=function() end} end"` (`require("dotfiles.folds")` を空の setup に差し替えるだけ)。

| 入力 | 壁時計 (2 回) | BufWinEnter の autocmd 1 件 (2 回) |
|---|---|---|
| 20,000 行 | — | 203 ms / 198 ms |
| 20,000 行・folds を外す | 0.23 s / 0.24 s | (該当の行なし) |
| 200,000 行 | 3.66 s / 3.77 s | 2013 ms / 1826 ms |
| 200,000 行・folds を外す | 1.18 s / 1.18 s | (該当の行なし) |

- 200,000 行で folds の分は壁時計で約 2.5 秒。固まりの最大の原因は folds.lua
- 🚨 **folds を外しても 200,000 行では約 1.2 秒残る** (`-u NONE` なら約 0.04 秒。反証レビューの実測)。`--startuptime` の
  `NVIM STARTED` は folds なしで約 0.11 秒なので、残りの約 1 秒は `NVIM STARTED` の後 (終了まで) に走っている。
  反証レビューが `DOTFILES_NVIM_DISABLE` で vim-matchup / gitsigns / nvim-treesitter / nvim-scrollview / indent-blankline を
  外しても 1.05〜1.10 秒のままだった。**正体は未調査** (この issue の範囲外。下の「残り」)
- 3,000 行では約 10 ms (サブエージェントの実測) で、普段の大きさのファイルでは問題にならない
- **編集中 (未実測・推測)**: 同じ `refresh_win` が InsertLeave / TextChanged の 400 ms の debounce の後にも走る
  (`schedule_refresh`)。changedtick が進むたびに再計算するので、20,000 行のファイルを編集すると、打つ手を止めるたびに
  約 200 ms 止まるはず。編集中の停止は測っていない

## 対応方針

- `eligible(buf)` に**バッファの行数** (`vim.api.nvim_buf_line_count`。O(1)) の上限を置き、超えたら fold を計算しない
  (fold なしで開く)。目安は 2〜3 万行で、値は上の表で決める (20,000 行で約 200 ms)。
  - `vim.fn.getfsize` は採らない: ディスク上のファイルを stat するので、編集で膨らんだバッファ・名前なしバッファとずれる。
    `eligible` は TextChanged / InsertLeave のたびに `schedule_refresh` から呼ばれるので、判定は軽い必要もある
  - 行数で測る理由: foldexpr は行ごとに評価するのでコストは行数に比例する。1 行が数 MB の minified JSON は行数では
    素通りするが、その形は計測していない (要るなら `nvim_buf_get_offset(buf, line_count)` でバイト数も見る)
  - BufWinEnter (`setup` の autocmd)・`schedule_refresh`・`refresh_win` の 3 か所が `eligible` を通るので、1 か所で全部に効く
  - 上限を超えたバッファは `foldmethod` が既定の manual のままであることを確かめる
- 上限で外したことが分かるようにするかは決める (何も出さないと「この大きいファイルだけ fold が無い」が理由なしに見える)
- `tests/nvim/test_folds_timer.sh` と同じ形で、上限を超えたバッファで fold を計算しないテストを足し、上限の判定を外す変異で
  red になるのを確かめる
- この方式 (expr で計算して manual に凍結) そのものは変えない。理由は folds.lua 冒頭 (`tests/nvim/bench_nvim.sh` の buf_switch:
  6000 行で切り替え 1 回約 3.4 ms → 凍結で 17 ms / 200 回)

## 受け入れ条件

- [x] 200,000 行の JSON (上の形) を開く壁時計が、folds を外したとき (約 1.2 秒) とほぼ同じになる (上と同じ方法で測る。
      残りの約 1 秒はこの issue では直さない)
- [x] 上限以下のファイルの fold は今までどおり (既存の `tests/nvim` が緑)
- [x] 上限の判定を外す変異で、足したテストが red になる

## 残り (この issue では扱わない)

- folds を外しても 200,000 行で約 1.2 秒かかる分 (`-u NONE` は約 0.04 秒)。`NVIM STARTED` の後に走る何か。正体を測ってから、
  別の issue にするか決める (未調査)

## 調べたが起票しないもの (調査: 2026-10-02、サブエージェントの読み取りと実測)

- 起動時間: 31.7 ms / 31.1 ms (3 回のうち 2・3 回目。1 回目はキャッシュが冷えて 67.6 ms)。上位は init.lua の自己 7.7 ms、
  vim-matchup 2.1 ms、`require('notify')` 1.7 ms。改善の余地は小さい。UI 込み (VeryLazy / UIEnter 以降) の起動は、
  lazy の checker が git fetch を起こしうるので測っていない
- vimade / nvim-scrollview / vim-matchup / incline の再描画のコスト: 実測なし。読み込みは 1〜3 ms で問題ない。
  `issues/done/010-research-nvim-plugin-rewrite-candidates-2026-07-10.md` は書き直しの観点で見ていて、性能は見ていない。
  体感で重くなったら `DOTFILES_NVIM_DISABLE=<名前>` で 1 つずつ外して A/B する (先回りでは変えない)
- `basic.lua` の CursorHold / CursorHoldI / BufEnter / FocusGained の `checktime`: 停止 500 ms ごとに全バッファを stat する。
  実測なし。stat は軽く、`nvim-early-retirement` がバッファ数を抑えるので見送る
- 問題なしと見たもの: LSP の documentHighlight (対応するバッファだけ)、statusline の再描画 (変化したときだけ)、
  診断の `update_in_insert=false`、`smooth_scroll.lua` (キーリピート中は動かさない)、`conform` は BufWritePre だけ、
  lazy の checker は 1 日 1 回

## 関連ファイル

- `nvim/lua/dotfiles/folds.lua` (`eligible` / `refresh_win` / `schedule_refresh`)
- `_nviminit.lua` (`require("dotfiles.folds").setup()`)
- `tests/nvim/test_folds_timer.sh` / `tests/nvim/folds_timer_check.lua` / `tests/nvim/bench_nvim.sh`

## 進捗

- 2026-10-02: 起票 (上の実測)
- 2026-10-02: 反証レビュー (サブエージェント) を受けて訂正: 起動の比較を `NVIM STARTED` から壁時計に直した (folds なしでも約 1.2 秒残る)。上限の判定を `getfsize` から `nvim_buf_line_count` に直した
- 2026-10-03 「perf(nvim): fold の計算に行数の上限 (2 万行) を置く (612)」 (ユーザーの依頼で着手)
  - `eligible(buf)` に `nvim_buf_line_count(buf) <= MAX_LINES` (20,000)。BufWinEnter・`schedule_refresh`・`refresh_win` の 3 か所が全部 `eligible` を通る。
    上限を超えたバッファは fold なしで開く (foldmethod は既定の manual のまま)。外したことは知らせない (fold が無いだけで、何も壊れないため)
  - 実測 (nvim v0.12.5、`nvim -u <worktree>/_nviminit.lua --headless <file> +qa`、1 行 1 要素の JSON、各 3 回):

    | 入力 | 修正前 | 修正後 | folds を外したとき (基準) |
    |---|---|---|---|
    | 20,002 行 壁時計 | 0.56 / 0.46 / 0.46 s | 0.23 / 0.22 / 0.22 s | 0.22 / 0.22 / 0.22 s |
    | 20,002 行 BufWinEnter (*) | 179.7 / 172.9 / 171.2 ms | 0 (行なし) | — |
    | 200,002 行 壁時計 | 3.56 / 3.56 / 3.54 s | 1.13 / 1.12 / 1.12 s | 1.14 / 1.14 / 1.12 s |
    | 200,002 行 BufWinEnter (*) | 1772 / 1775 / 1770 ms | 0 (行なし) | — |

    修正後は基準と同じ。200,000 行で残る約 1.1 秒は上の「残り」(folds の外。未調査) のまま
  - テスト `tests/nvim/test_folds_size_limit.sh` (+ `folds_size_limit_check.lua`): 上限ちょうど (20,000 行) は fold あり・上限 + 1 行は fold なし (foldmethod は manual)。
    上限は `require("dotfiles.folds").max_lines` から読む。`make test-nvim` で走ることを確認 (rc=0、OK の行あり)。
    変異 (red): 上限の判定を外す → 「上限 + 1 行」/ `<=` を `<` にする → 「上限ちょうど」
  - 敵対的レビューは省略: 判定は行数の比較 1 つで、上限ちょうど・+1 の両側を変異で確かめた
  - worktree での `make test` は rc=2: 落ちたのは glogx の `TestFrameAllocBudget` (doctor-svc の確保 459 回 / 上限 458) だけ。今回の変更 (nvim の lua) と無関係で、
    単体では `-race` 付きでも 2 回とも通る (全体の負荷の下でだけ 1 回超える)
  - 残り: 上の「残り」(200,000 行で folds の外に残る約 1.1 秒) は未調査のまま。1 行が巨大な minified ファイルは行数では素通りする (測っていない)

