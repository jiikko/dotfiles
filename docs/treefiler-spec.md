# treefiler — 仕様 (glogx の `F` / 単体の `bin/treefiler`)

issue 662 の実装前の正本。**§0 が glogx での決定 (treebeard から変える所と glogx への組み込み)、§1〜9 は treebeard のソースから抜き出した見た目と挙動**。
§0 と §1〜9 が食い違うときは §0 が勝つ。

- 写す元: [treebeard](https://github.com/freakinfrick/treebeard) (`tb`、Rust/ratatui、**MIT OR Apache-2.0**)。抜き出しは commit `691788d` (2026-10-07 取得)。
  定数・配色・アルゴリズムを写すので、実装のファイル冒頭に出典 (repo URL・commit・ライセンス) を書く
- 抜き出しの裏取り: 配色 (`DARK`・`EMBER`)・`MOVE`・`POPUP_T`・`WALK_CAP`・バイナリ判定 (8192 バイトの NUL)・テキストの上限 (2 MiB)・既定の線 (double) を原文で照合した。
  既定の線はデモ動画のフレームを拡大しても double だった。他の数値は抜き出しを信じている (実装時に該当箇所の原文を開いて写す)

## 0. glogx での決定

### 0.1 treebeard から変える所 (ユーザーの決定。日付はすべて 2026-10-07)

| 項目 | treebeard | glogx |
|---|---|---|
| 起動 | `tb [DIR]` | glogx の**どの画面からでも** `F` (§0.3) |
| 起点 | 起動 dir の**親**を root、起動 dir にカーソル (§5.5) | **pwd を root** にして始める。`-` `Backspace` で 1 段上へ (§5.7 の reroot_up はそのまま) |
| プレビュー | 中央 90% のポップアップ 1 枚 (§4.6) | **ドラクエの対戦画面のように重なるタイル** (§0.2)。枠の中の飾り (§8.3) は写す |
| `q` `Esc` | 終了 (`q` は cd 連携) | glogx 流・issues viewer に揃える: タイルの上は 1 枚閉じる / 木の上は **glogx ごと終了** / `F` で閉じて **git log 一覧へ戻る**。`Ctrl-C` は glogx の即終了 |
| `q` の cd 連携 (`--cwd-file`) | あり | **写さない** |
| `o` `O` / ソート | 巡回・逆順 | **ソートは後回し** (名前順だけ)。`o` は glogx の「外のアプリで開く」のまま (`open <path>`) |
| 他の衝突するキー | `s` シェル / `C` 経路以外を畳む / `e` explode / `J` `K` / `Space` `Tab` / `n` `N` / `Enter` | **treebeard のまま** (`o` 以外は上書き)。ファイラーの中では glogx の `s` (status viewer)・`C` (claude update) は効かない |
| `j` `k` の歩き方 | 既定 Step through = tree (開いたフォルダへ潜る) | **同じ階層 (兄弟) の中だけを動く** (2026-10-08 ユーザー回答。押しっぱなしで潜らない)。`J` `K` も兄弟を 10 |
| `↓` `↑` | `j` `k` と別の意味 (列の中を移動) | **`j` `k` の別名** (`docs/glogx-ui-guide.md` §2: 矢印と emacs は新しい意味を持たない)。移動の語彙は `tuikit/listnav.MotionOf` を通す |
| emacs のキー | 無い | **`Ctrl-N` `Ctrl-P` `Ctrl-F` `Ctrl-B` を ↓ ↑ → ← の別名にする** (2026-10-08 ユーザー回答)。`Ctrl-B` を ← にするのは ui-guide §2 の例外 (pro-con のボードと同じ「← が左の列へ動くだけ」の画面) |
| タイルの中のキー | `q` `Esc` `h` `←` で閉じる / `Space` 1 ページ | ui-guide に揃える: `Enter` も閉じる (§3 の開閉 toggle) / `Space` `Ctrl-D` `f` は半ページ (§2) / **`J` `K` で開いたまま同じフォルダの隣のファイルへ差し替え、木のカーソルも動かす。端では toast** (§6) / ジャンプモードで捌かないキーはモードを抜けてから効く (§6) |
| 動きの速さ | `MOVE` 0.11 / `CAM` 0.16 / `POPUP_T` 0.085 / 出現 0.07 / 消滅 0.05 / スクロール 0.07 | **短くして機敏に** (2026-10-08 ユーザー回答): 0.06 / 0.09 / 0.045 / 0.04 / 0.03 / 0.04 (モックで確かめた値。実装で詰めてよい) |
| マウス | クリック・ホイール・慣性 | **写さない** (設定の Mouse / Wheel speed / Momentum も外す) |
| 音声 | 再生・波形 | **開かない**。toast で断る (§0.4) |
| バイナリ | `file -b` の説明を出す | **開かない**。toast で断る (§0.4) |
| 画像・PDF | ピクセル (kitty/sixel/iTerm2) かブロック | **後回し (pending。2026-10-07 ユーザー回答)**。入れるときはブロックだけ (§9 の quadrants / sextants / half) で、`i` の切替と設定の Image previews は外す。それまでは開かずに toast で断る (§0.4) |
| テキストの描画 | `glow` / `bat` (外部コマンド) | **外部コマンドを呼ばず** bubbletea v2 と tuikit で描く: markdown = `tuikit/markdown`、コード = `tuikit/highlight` (chroma)。`docs/glogx-bubbletea-v2.md` の幅の前提に乗る |
| 巨大なテキスト | 先頭 2 MiB だけ読む | **先頭だけ読み、ページ送りで下へ進むたびに続きを読む** (§0.4) |
| 設定の保存先 | `~/.config/tb/config.toml` | **`~/.config/glogx/` の下** (§0.5) |
| 既存の `E` (外部ファイラー) | — | 消した (commit「refactor(glogx): 使っていない E (外部ファイラーの起動) を消す」)。`F` の別名にもしない |

### 0.2 タイル (ドラクエの対戦画面)

- ファイルで `l` `→` `Enter` を押すとタイルが開き、中身はテキスト (または画像・PDF のブロック) のプレビュー
- タイルの中から別のファイルへ飛ぶと、**今のタイルを閉じずに新しいタイルを手前に重ねる**。枚数の上限は無い
- **置き場所は 4 箇所**。新しいタイルは 4 箇所を**順番に**使い、5 枚目は 1 枚目の場所に重なって上書きする (以降も同じ順で回る)。
  一番手前のタイルを閉じると、その場所の下に隠れていたタイル (4 枚前) が見える
- 枠の中の飾りは treebeard のプレビューから写す (§8.3: 題 `名前 · サイズ`、右下の `d diff · 1/13`、右端のスクロールバー、読込中のスピナー)。
  中のキーは §6.4 (テキスト) から、閉じるキーだけ §0.1 に置き換える
- **見た目はモックで決まった** (2026-10-08。ユーザーが `./tmp/treefiler-mock` を動かして「ui や肌触りはかなりいい」)。モックの式をここへ写す
  (モックは `./tmp` にあり消えうるので、式と値はこの節を正本にする):
  - 置き場所 i (0〜3): キャンバス (ステータスバーを除く) を W × H として、タイルは幅 `tw = W*60/100`・高さ `th = H*70/100`。
    **1 枚目は中央** (2026-10-08 ユーザー回答「preview のタイルはセンタリングして。最初はセンターで」): `cx = (W-tw)/2`、`cy = (H-th)/2`。
    2〜4 枚目は中央から右下へ同じ幅ずつずらす: `dx = (W-tw-2-cx)/3`、`dy = (H-th-1-cy)/3`、位置 `(cx + i*dx, cy + i*dy)` (整数除算)。
    200×55 のキャンバスで 1 枚目は (40, 8)、4 枚目は (76, 14)。奥のタイルは手前の右下にのぞく (モックを tmux で撮って確かめた)
  - 何番目の場所に置くか = 開く時点のタイルの枚数 mod 4 (閉じれば番号も戻る)
  - 枠は Rounded、背景 `pop`。**手前のタイル**: 枠 `mix(pop, accent.route, p)`・題は太字 `text`。**奥のタイル**: 枠 `mix(pop, accent.active, p)`・題は `muted`
  - 手前のタイルの外 (木と奥のタイル) は `dim_backdrop` の `0.55*p` で沈める
  - 開く動き: 元の矩形 (1 枚目 = 木のカーソル行の pill、2 枚目以降 = ジャンプで選んだリンクの文字列) から置き場所へ、`popupT` のばね (§0.1 の値) で `lerp_rect`。
    中身は `p > 0.9` から描く。閉じるときは逆に縮み、`p < 0.03` で捨てる
  - 🚨 **ばねが止まったら値を目標へ合わせる** (§4.1 の treebeard の `Damped` は合わせない)。合わせないと `|v-1| < 0.02` で止まった `p` が
    `lerp_rect` で増幅され、遠くから飛んできたタイルが置き場所から 1 桁以上ずれたまま止まる (モックで実測: 角が `╭╭` と二重になった)
  - 右下の位置表示 `1/243`、右端のスクロールバー (track `│` accent.dim / thumb `┃` accent.route)、題 ` 名前 · サイズ ` (§8.3)
- **タイルの中で「ファイルへ飛ぶ」= `Tab` のジャンプモード** (モックで採用。issues viewer の本文と同じ形): `Tab` で入り、本文中の
  **実在するファイルに解決できるパスだけ**を下線で示し、`Tab` / `j` `k` (と別名) で選択を移し、`Enter` で新しいタイルを手前に重ねる。
  `Esc` `q` `h` `←` でモードだけ抜け、捌かないキーはモードを抜けてからタイルのキーとして効く。パスの解決は
  `src/glogx/issues/filelink.go`、位置は `tuikit/markdown` の `RenderLinks` を流用できないか実装で確かめる
  (モックは「長いパスから先に照合し、重なる短いパス (`src/tuikit/README.md` の中の `README.md`) を別に拾わない」を手で書いた)

### 0.3 glogx への組み込み (2026-10-07 にコードで確かめた事実)

- **全画面の板として足す**: `src/glogx/fullscreen.go` の `fullScreenCount` の直前に ID を足す。足すと `exhaustive` の lint と
  `TestFullScreenSurfacesWireEverySite` が配線の漏れを捕まえる。描画は `finishWithGlobalChrome` を通す (通さないと toast・usage が載らない)
- **`F` をどの画面からでも受ける**: `tui.go` の `handleKey` で、`C` / `X` (update) と同じく**全画面の dispatch より前**に置く
  (後ろに置くと各 viewer がキーを飲む)。他の viewer から開くときはその viewer を閉じてから開く (全画面は同時に 1 枚)
- 🚨 **ファイラー自身の入力モードに `F` と `C` を先取りさせない**: dispatch より前の判定は、ファイラーの検索欄 (`/`)・`!` のコマンド行・設定の板で
  打った大文字の `F` / `C` を横取りする。さらにファイラーは木の上でも `C` (経路以外を畳む) を使う。そこで:
  - ファイラーに `ownsKeys()` (入力モード中は true) を実装し、`updateKeyReachable` と `F` の判定の両方でそれに譲る
    (入力中の issues の絞り込み・URL ピッカー・目印の確認 / status の pager・破棄の確認 / doctor の削除の確認と実行中に譲るのと同じ形)
  - `updateKeyReachable` に「ファイラーが出ていれば `C` は常に譲る」分岐を足す (status viewer の `X` = 変更を捨てる、と同じ形)
  - `overlay_ownership_test.go` の `overlayOwnershipTable` に 1 行足す。🚨 足し忘れを `TestOverlayOwnershipTableCoversAllParticipants` が
    red にするのは、ファイラーが `ownsKeys()` を実装しているときだけ (実装していなければ足し忘れても緑)
- **木の上の `Esc` の優先順**: treebeard の `Esc` は explode 中なら中断だけ (§6.1)。検索中は検索の取り消し (§6.2)。それ以外のときだけ §0.1 の「glogx ごと終了」
- **ファイラーの中で効かせる glogx の横断キー**: 衝突しない `R` `D` `U` `X` は他の viewer と同じく効かせる。`s` `C` は treebeard が使うので効かない。
  `i` は treebeard の木では未使用なので効かせる (画像プレビューの `i` は写さないので衝突しない)。🚨 この行は推奨で、ユーザー未確認
  (issue 662 の衝突表の節は当初「`s` `C` `i` は効かない」と書いていた。`i` の衝突は画像プレビューの中だけで、それは写さないと後で決まった)
- **フレーム周期**: glogx は既定 12.5fps (`spinnerInterval` 80ms)。treebeard のばね (§4.1) は経過時間 `dt` を受けるのでフレーム周期には依存しないが、
  滑らかに見せるには treebeard の 60fps (`FRAME` 16.7ms) に近づけたい。動いている間だけ `tickInterval()` で `scrollInterval` (16ms) か
  `zoomInterval` (8ms) へ上げる。🚨 `tui.go` の周辺のコメントは 16ms を「~30fps」、8ms を「60fps」と書いていて値と合わない。ms の値を正にする。🚨 高 FPS の登録先は `tickInterval()` だけ (ここに足せば `spinnerActive` にも効く)。
  止まったら描かない、は treebeard (§4.7) と glogx (「動くものがある間だけ tick を回す」) で同じ方針
- **1 フレームの確保の上限**: `frame_alloc_test.go` の `TestFrameAllocBudget` にファイラーの行を足す (既存の画面は余裕が数回しかない)
- **toast**: `m.toast.Show(text, false)` (✗ 赤)。キーが効かなかった理由は失敗で出す (`docs/glogx-ui-guide.md` §9)
- **外部由来の文字列**: ファイル名・ファイルの中身・git の出力は表示前に `termsafe` を入口で 1 回通す (`src/glogx/CLAUDE.md`)。
  名前は `termsafe.PlainLine`、本文は色を付ける前の生テキストに通す
- **幅**: 全角は 2 桁。幅の計算は `termwidth` (glogx の単一の出典) を使い、treebeard の `truncate_to` の規則 (§3.1) をその上で書く
- **git**: treebeard は `git --no-optional-locks status --porcelain=v1 -z --branch --ignored --untracked-files=normal` を repo top ごとに 3 秒周期で回す (§5.3)。
  glogx の status viewer (`worktree_status.go`) は `status --porcelain --branch -z` で、`--ignored` を取らず `--no-optional-locks` も付けないので、そのまま使い回せない
- **ライブ更新**: treebeard は 1 秒のポーリング (§5.2)。glogx の issues viewer は fsnotify で起こして指紋 (mtime + サイズ) で判定する (`issues_watch.go`)。
  どちらで作るかは実装で決める (`fsnotify` は既に依存にある)
- **画像のデコード** (画像・PDF ごと後回し。入れるときの判断材料として残す): Go の標準は png / jpeg / gif だけ。webp / bmp / tiff は `golang.org/x/image` (新しい依存)、
  svg / heic / avif / psd などは treebeard と同じく ImageMagick の `convert`、PDF は poppler の `pdfinfo` / `pdftoppm` (外部コマンド)。依存と外部コマンドを足すかは再開するときに決める
- **キー一覧・README**: `src/glogx/README.md` のキー表・`--help` (`options.go`)・`docs/glogx-ui-guide.md` §2 の表を同じ変更で直す (ui-guide §10)

### 0.4 開けないもの・巨大なもの

- **バイナリ** (先頭 8192 バイトに NUL。§5.5) と**音声** (拡張子で判定。treebeard の対応形式 mp3 / flac / wav / ogg / m4a / aac) は、
  タイルを開かず toast で「表示できない」旨を出す (✗ 赤)
- **画像・PDF** (§9 の拡張子) も、後回しの間は開かず toast で断る。🚨 文言は「表示できない」ではなく「まだ対応していない」に分けるかを実装で決める
  (再開したら開ける側へ移す)
- **テキストは先頭だけ読む**。下へページ送りしてまだ読んでいない所に近づいたら続きを読む。1 回に読む量と「近づいた」の閾値は実装で決める。
  行数の表示 (`1/13` の総行数) は読み終えるまで確定しないので、未確定の間の見せ方 (`1/200+` など) も実装で決める

### 0.5 設定

- `~/.config/glogx/` の下に置く (ファイル名は実装で決める。例 `filer.toml`)。形式は treebeard と同じフラットな `key = value` で、
  変えた 1 行だけ書き換える (§7)。依存を足さずに書ける
- 写す項目は §7 の表から × を除いたもの。ソート (Sort by / Reverse) も後回しなので外す
- Remember place (§7 の places) の保存先も `~/.config/glogx/` の下にする (treebeard は `~/.local/state/tb/places`)。🚨 推奨で、ユーザー未確認

### 0.6 単体でも起動する (2026-10-08 ユーザー回答「このファイラー単体でも起動できるようにして」)

- glogx の `F` から開くのに加えて、**ファイラーだけを端末で起動するコマンド**も持つ
- そのため**ファイラーは glogx の `package main` に置かず、独立した module** にする。glogx の構造の判断
  (`src/glogx/CLAUDE.md`「サブパッケージを切る基準は実在する第二消費者か明示的な分離要望」) の両方に当たる
- 形は `ratelimit` と同じ: `src/ratelimit` が独立 module で、取得と整形の `usage/` パッケージを glogx が `replace ratelimit => ../ratelimit` で取り込み、
  単体の入口は `bin/ratelimit` (`bin/lib/go_autobuild.zsh` で自動ビルドして exec する zsh)。ファイラーも
  - `src/treefiler/` の module に、画面の部品のパッケージ (状態・キー・描画。glogx からも単体からも使う) と、単体用の `main.go` を置く
  - glogx は `replace` で取り込み、全画面の 1 枚として部品を埋め込む (§0.3 の配線はそのまま)
  - `bin/treefiler` を `go_autobuild_exec` で書く
- 部品は glogx に依存しない: termsafe・tuikit (toast・markdown・highlight・termwidth・layout) は既に独立 module なので両方から使える
- 単体のときの違い:
  - `q` `Esc` (木の上) は単体のプログラムを終了する。`F` で閉じる・git log 一覧へ戻るは glogx に埋め込んだときだけ
  - glogx の横断キー (`R` `D` `U` `X` `i`) は単体では無い
  - 起点は単体でも pwd (引数でディレクトリを渡せるようにするかは実装で決める。treebeard は `tb [DIR]`)
  - 設定は単体でも glogx でも同じ `~/.config/glogx/` の下を読む (§0.5)
- **名前は `treefiler`** (2026-10-08 ユーザー回答): module は `src/treefiler/`、単体のコマンドは `bin/treefiler`。どちらも既存と衝突しない (2026-10-08 確認)
- 単体で起動できると、見た目の確認 (撮影) も glogx を経由せずに回せる

### 0.7 本体へ入れる順

0. **見た目を自分で確かめる道具** (§0.8.4)。これが無いと各段の「撮る → treebeard と比べる → 直す」を人の目に頼ることになる
1. 独立 module (`src/treefiler`) と単体の起動 (`bin/treefiler`)、横に育つ木と固定の選択線 (§1〜3、§4.2・4.5)。glogx の `F` への配線は 1 段目の最後に
2. 熱の色と git の印 (§1.3、§5.1、§5.3)
3. タイル 1 枚 (§0.2、§8.3、§0.4)
4. タイルを 4 箇所に重ねる・タイル内のジャンプ
5. ばねの動きの残り (ビーズ・ripple) とライブ更新 (§4.3・4.4、§5.2)
6. 残りの UI (検索・explode・シェル・設定の板・help)。画像と PDF は後回し (§0.1)

### 0.8 実装の設計 (2026-10-08。着手前の計画。実装で変えたらここを直す)

#### 0.8.1 置き場所と依存

- `src/treefiler/` を独立 module にする。前例の `src/ratelimit` に倣って揃えるもの: `Makefile` (`lint` = `vet-gorules` + 版固定の golangci-lint、
  `test` = `go test -race ./...`)・`.golangci.yml`・`gorules/` (規則の正本は `src/glogx/gorules/rules.go`)・`.gitignore` (autobuild の成果物)・
  `CLAUDE.md` (ファイルの地図と入口)・`README.md`・`.github/workflows/src_treefiler.yml` (`_go-project.yml` を呼ぶ薄い caller。paths に
  replace で取り込む `src/termsafe/**` `src/tuikit/**` `src/subproc/**` などを並べる)・`bin/treefiler` (`go_autobuild_exec`)
- 🚨 配線の漏れは `scripts/check_go_project_lanes.sh` が止める (Makefile の lint/test・workflow の存在・paths・replace 先の paths・go.sum・
  workflow の `dir`)。glogx が treefiler を取り込んだら **`src_glogx.yml` の paths に `src/treefiler/**` を足す** (足さないと同じ検査が落ちる)
- `make test` は `src/*/go.mod` の存在で Go プロジェクトを見つけるので、Makefile への手での登録は要らない
- 依存: bubbletea v2 (glogx と同じ版 `charm.land/bubbletea/v2 v2.0.8`)・`termsafe`・`tuikit` (termwidth / markdown / highlight / toast / listnav)・
  `subproc` (外部プロセス = git の起動。ratelimit の `exec_boundary_test.go` と同じく `os/exec` の直接 import を止める)・`atomicfile` (設定の書き込み)。
  lipgloss / bubbles は使わない (glogx が使っていない)

#### 0.8.2 パッケージの形

- `src/treefiler/main.go` — 単体の入口 (`bin/treefiler`)。pwd を root にして部品を `tea.NewProgram` で回す
- `src/treefiler/filer/` — 画面の部品 (glogx と単体の両方が使う)。glogx に依存しない。ファイルは責務で分ける:
  木の状態とディレクトリの読み込み (`tree.go`) / 列と線の配置の純関数 (`layout.go`、§3) / ばねとシーン (`anim.go`、§4) /
  セルの格子から文字列への描画 (`render.go`。I/O をしない純関数) / 配色と熱 (`palette.go`、§1) / タイル (`tile.go`、§0.2) /
  テキストの遅延読み込みとバイナリ・音声の判定 (`preview.go`、§0.4) / 再帰 mtime の走査 (`walk.go`、§5.1) / git (`git.go`、§5.3) /
  ライブ更新 (`watch.go`、§5.2) / 設定 (`settings.go`、§0.5) / キー (`keys.go`)
- 外へ出す面 (案): `New(root string, opts) *Model` / `Update(tea.Msg) (Result, tea.Cmd)` (Result = 何もしない / 閉じたい / 終了したい) /
  `View(w, h int) string` / `OwnsKeys() bool` (入力モード中。§0.3) / `Animating() bool` (glogx の `tickInterval` が周期を上げるのに使う)。
  時刻は `opts.Now` で注入する (熱の色が「今」に依存するので、テストと撮影で固定するため)
- glogx 側: `fullscreen.go` に ID、`handleKey` の `C`/`X` の位置に `F` (§0.3)、`overlayOwnershipTable`、`tickInterval`、`TestFrameAllocBudget` の行

#### 0.8.3 テスト (段ごとに書き、変異を当てて red を確かめてから commit する)

- 配置: 決め打ちの木で `layout` の結果 (各ノードの x, y と線の格子) を固定する。spine の y=0、ブロック間の空白 1 行、肘の track、列の x の式 (§3.1〜3.3)
- 線: mask から文字 (§2.1)、管の継ぎ目、強調の優先 (DIM < ACTIVE < ROUTE)
- 熱: 停止点ちょうどの色と、区間の途中が smoothstep であること (§1.3)
- キー: 兄弟の中だけを動く `j` `k` と別名 (`↓` `↑` `Ctrl-N` `Ctrl-P`)・`l` `→` `Ctrl-F` `Enter` で潜る / 開く・`h` `←` `Ctrl-B` で親・`Space` `Tab` の開閉・`c` `C`・`g` `G` (§0.1、§6.1)
- タイル: 置き場所の巡回 (5 枚目が 0 番)・閉じると番号が戻る・`Enter` / `q` で 1 枚閉じる・`J` `K` の差し替えと端の toast・ジャンプモードの選択と抜け方
- 読み込み: 先頭だけ読む・ページ送りで続きを読む境界・バイナリ (先頭 8192 バイトの NUL) と音声の拡張子で toast・タブの展開・termsafe を通すこと
- git: `--porcelain=v1 -z --branch --ignored` の出力の fixture から印と branch 表記 (§5.3)
- 時間: アニメは `opts.Now` と dt の注入で進める (壁時計を待たない。`avoid-wall-clock-assertions`)
- glogx: `F` がどの全画面からも開くこと・入力中は譲ること・`C` を譲ること (`overlay_ownership_test.go` と `motion_vocabulary_test.go` の流儀)

#### 0.8.4 見た目を自分で確かめる道具 (0 段目)

- 目的: 実装を頼まれたら、人が見なくても「撮る → treebeard / モックと比べる → 直す」を回せるようにする (2026-10-08 ユーザー要望)
- 方式 (vhs は使わない。中の xterm.js が罫線と幅の曖昧な字を本物の端末と違えて描くため。`src/tuikit/README.md` の「デモ」):
  1. 隔離した tmux (`unset TMUX TMUX_PANE` + 一意の `-L`、`TMUX_TMPDIR` を使い捨ての dir に。`tmux-probe-requires-socket-isolation`) で
     `bin/treefiler` を決まった大きさ (例 200×56) で起動し、キーの台本を送る
  2. `capture-pane -p` で文字の格子、`capture-pane -p -e` で色を取る。**位置・桁・文字の確認は格子を機械で照合する** (安くて正確)
  3. 色・継ぎ目・全体の印象の確認だけ、格子と色を PNG に描いて読む (手元のフォント Menlo / Hiragino)
  4. 撮る木は決め打ちの fixture (更新時刻をずらしたディレクトリを生成する) と、注入した「今」で固定し、毎回同じ絵にする
- 置き場所: `src/treefiler/tools/shot/`。🚨 PNG を描くには Go の `golang.org/x/image` (フォントの読み込み) か Python の Pillow (今は入っていない) が要る。
  推奨は `tools/shot` を**別 module** にして `x/image` をそこに閉じ込める (本体の go.mod に依存を足さない)。別 module は `src/*/go.mod` の
  1 段下なので CI のレーンを持たない (手で使う道具として扱う)
- 撮ったものは `./tmp` に出す (commit しない)。比べる相手は treebeard のデモ動画のフレーム (`docs/demo.mp4`) とモック


---

以下 §1〜10 は treebeard のソースからの抜き出し。出典は `file:関数名`、RGB は `[R,G,B]` を原文のまま。ソースに無いものは「ソースに無い」と書く。
座標は「セル」。world 座標 = レイアウト上の座標、screen 座標 = world - カメラ。§0 と食い違う所は §0 が勝つ。

## 1. 配色

### 1.1 ground (ui.rs: `DARK` / `PARCHMENT` / `VELLUM`、`GroundColors`)

| 要素 | dark | parchment | vellum |
|---|---|---|---|
| bg (キャンバス) | [9,10,15] | [242,232,207] | [248,242,226] |
| bar (ステータスバー背景) | [17,19,29] | [226,212,178] | [229,223,208] |
| pop (ポップアップ/ヘルプ/設定板の背景) | [13,15,23] | [236,224,194] | [242,236,220] |
| text (バー・ポップアップの文字) | [238,240,250] | [40,32,24] | [36,30,26] |
| route_text (カーソル経路の名前) | [238,240,250] | [24,76,38] | [27,46,99] |
| flash (検索一致の文字・光の先端) | [235,242,255] | [20,16,12] | [18,15,13] |
| dot (エラー・「no match」) | [255,58,58] | [190,30,30] | [190,30,30] |
| muted (薄い文字・ignored の git 色) | [110,118,150] | [130,118,100] | [153,147,136] |
| ignored (dim floor 10 の灰) | [214,216,224] | [80,74,66] | [71,65,58] |
| match_bg (検索一致の背景) | [92,70,22] | [236,204,120] | [236,204,120] |
| ripple (ライブ変更の閃光色) | [255,200,150] | [190,70,20] | [190,70,20] |
| ripple_bg (変更した名前の背景の熾火) | [110,38,28] | [240,196,160] | [240,196,160] |
| git: untracked/staged/modified/conflict | [110,200,225] [120,215,130] [240,200,90] [255,90,120] | [30,118,150] [40,130,50] [176,120,0] [180,30,60] | parchment と同じ |
| default_fg (端末既定色のフェード元) | [200,200,200] | [40,32,24] | [36,30,26] |

ground 切替で使える heat palette が変わる (settings.rs: `Ground::palettes`): dark = ember magma neon aurora glacier sepia mono / parchment = growth ink gilded / vellum = manuscript ink irongall。

### 1.2 accent (ui.rs: `ACCENTS` / `PAPER_ACCENTS`、`acc()`)

`dim` = カーソル経路から外れた枝、`active` = line (spine) に掛かる枝、`route` = line 本体・キー・強調、`pill` = 選択行の背景。

| accent | dim | active | route | pill |
|---|---|---|---|---|
| indigo (既定) | [33,39,68] | [68,86,168] | [140,168,255] | [34,39,72] |
| teal | [24,50,54] | [44,124,126] | [110,222,212] | [22,52,56] |
| violet | [45,33,68] | [108,70,170] | [198,152,255] | [48,32,74] |
| amber | [58,44,24] | [150,108,40] | [255,198,110] | [62,46,22] |
| mono | [44,44,50] | [100,100,110] | [212,212,222] | [46,46,54] |
| forest (parchment 専用) | [176,158,130] | [96,120,70] | [34,92,48] | [208,222,182] |
| lapis (vellum 専用) | [184,178,166] | [98,113,155] | [34,58,124] | [204,212,230] |

紙 ground では accent 設定は無視され forest/lapis 固定 (`acc()`、`Settings::adjust` の `"accent" if paper => {}`)。

### 1.3 熱 (recency) の色 (anim.rs: `heat_scaled`、定数 `EMBER` ほか)

- 停止点は 7 個。年齢 (秒) = 60, 3600, 86400, 7日, 30日, 365日, 5*365日。
- 補間: `a = ln(max(age*scale, 1))`。`a <= ln(最初の停止点)` なら最初の色。区間 [ln t0, ln t1] で `t=(a-ln t0)/(ln t1-ln t0)`、**smoothstep** `t*t*(3-2t)` で RGB を線形 mix。最後の停止点より古ければ最後の色。
- `scale = (最後の停止点の年齢) / heat_range の秒数` (`range_scale`)。heat range: day=86400, week=7d, month=30d, year=365d, 5y=5*365d。5y なら scale=1。
- age = `now - tree.heat(id)` (秒、負は 0)。

| palette | 60s | 1h | 1d | 7d | 30d | 1y | 5y |
|---|---|---|---|---|---|---|---|
| ember | 255,70,60 | 255,118,48 | 255,172,64 | 240,190,70 | 196,112,170 | 108,118,196 | 66,80,214 |
| magma | 255,248,180 | 255,190,120 | 250,120,96 | 222,72,120 | 184,70,160 | 146,86,196 | 120,96,210 |
| neon | 255,140,200 | 255,110,225 | 215,120,255 | 160,135,255 | 110,160,250 | 70,170,225 | 50,165,185 |
| aurora | 253,231,37 | 170,220,50 | 84,197,104 | 36,166,136 | 44,136,156 | 70,106,170 | 96,80,170 |
| glacier | 180,240,255 | 130,220,255 | 90,200,242 | 60,170,228 | 70,136,230 | 96,112,226 | 118,100,214 |
| sepia | 255,226,170 | 248,210,150 | 234,188,118 | 214,160,90 | 192,134,80 | 170,114,78 | 152,100,80 |
| mono | 250,250,250 | 220,220,226 | 190,190,198 | 160,160,170 | 132,132,142 | 108,108,118 | 90,90,100 |
| growth | 34,128,40 | 78,124,18 | 140,116,0 | 170,104,0 | 166,70,28 | 120,64,36 | 70,44,30 |
| ink | 28,22,18 | 56,36,26 | 88,50,24 | 116,66,22 | 136,82,30 | 146,94,40 | 150,102,50 |
| manuscript | 196,40,30 | 186,74,14 | 164,108,0 | 106,112,18 | 38,106,60 | 32,80,96 | 46,40,70 |
| gilded | 160,108,0 | 146,110,0 | 128,118,10 | 104,112,20 | 86,96,22 | 70,76,26 | 54,58,28 |
| irongall | 150,102,50 | 146,94,40 | 136,82,30 | 116,66,22 | 88,50,24 | 56,36,26 | 28,22,18 |

既定: dark = ember、paper = growth (`paper_palette`)。`Settings::heat_palette`: paper で `paper_palette` が ground の提供外なら `palettes()[0]`。

### 1.4 どの要素にどの色か (ui.rs: `frame`)

| 要素 | 色 |
|---|---|
| 名前 (カーソル経路上) | `route_text` |
| 名前 (経路外・line 上の枝 = `Placed.active`) | `heat(age)` |
| 名前 (経路外・line から外れた枝) | `off_line` = `mix(heat, bg, 0.08*(10-focus_dim))` (focus_dim 既定 6 → 0.32、0 で 0.8) |
| 名前 (git ignored かつ dim_ignored かつ経路外) | `ignored_shade` = `mix( mix(bg, ground.ignored, 0.1+0.08*dim_floor), heat, 0.12 )` (dim_floor 既定 5 → 0.5) |
| 名前 太字 | フォルダ、または経路上 |
| 最終 fg | `mix(bg, mix(rgb, ripple, glow), alpha)` (alpha = 出現/消滅フェード) |
| name details (age/size) | `mix(bg, rgb, alpha*0.5)` |
| 検索一致 | fg `mix(bg, flash, alpha)`、bg `mix(bg, match_bg, alpha)`、太字 |
| ライブ変更の名前背景 (カーソル行以外) | `mix(bg, ripple_bg, glow*alpha)` (glow>0.02 のとき) |
| 経路の線 (ROUTE) | `mix(accent.route, sap, 0.18)` |
| line 上の枝の線 (ACTIVE) | `mix(accent.active, sap, 0.42)` |
| その他の線 (DIM) | `mix(accent.dim, mix(sap, bg, 0.55), 0.35)` |
| sap | 線セルの `owner` (その枝が生える親フォルダ) の `heat(now - tree.heat(owner))` |
| 色の変化 | 名前の rgb は目標色へ `approach(tau=RECOLOR 0.12)` でチャンネルごとに cross-fade。目標色は relayout 時だけ再計算。初回 (rgb==[0,0,0]) は即時 |
| カーソル行の pill | bg を `accent.pill` に (フォアの上書きはしない) |
| ステータスバー ● | `heat(age(カーソル))` |

`heat(age)` の age は `tree.heat(id)` (§5)。経路は root→cursor。`Placed.active` = その block の親が spine (root→cursor→cursor 以降の「記憶した子」) 上にある (layout.rs: `layout_with`)。

## 2. 線と記号

### 2.1 tree lines (layout.rs: `GLYPHS`、`LineStyle` の順 = rounded, square, heavy, double, ascii)

方向ビット UP=1 DOWN=2 LEFT=4 RIGHT=8。mask 0..15 の文字 (0 は `·`、ascii は `.`):

| mask | 1 U | 2 D | 3 UD | 4 L | 5 UL | 6 DL | 7 UDL | 8 R | 9 UR | 10 DR | 11 UDR | 12 LR | 13 ULR | 14 DLR | 15 全 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| rounded | │ | │ | │ | ─ | ╯ | ╮ | ┤ | ─ | ╰ | ╭ | ├ | ─ | ┴ | ┬ | ┼ |
| square | │ | │ | │ | ─ | ┘ | ┐ | ┤ | ─ | └ | ┌ | ├ | ─ | ┴ | ┬ | ┼ |
| heavy | ┃ | ┃ | ┃ | ━ | ┛ | ┓ | ┫ | ━ | ┗ | ┏ | ┣ | ━ | ┻ | ┳ | ╋ |
| double | ║ | ║ | ║ | ═ | ╝ | ╗ | ╣ | ═ | ╚ | ╔ | ╠ | ═ | ╩ | ╦ | ╬ |
| ascii | \| | \| | \| | - | + | + | + | - | + | + | + | - | + | + | + |

(ascii の mask 0 は `.`、他スタイルは `·`)。既定は **double** (`Settings::default`)。

- **管 (tube)**: `Cell.route` に LEFT/RIGHT が含まれるセル (経路の水平走行) は、rounded/square では次に置換 (`styled_cell_glyph`): mask 15→`╪` 13→`╧` 14→`╤` 11→`╞` 9→`╘` 10→`╒` 7→`╡` 5→`╛` 6→`╕`、その他 (水平のみ等)→`═`。つまり経路の水平線だけ二重線になり、継ぎ目は「二重横 + 単縦」の文字。heavy/double は管なしで色だけで区別 (置換しない)。ascii は縦成分なしなら `=`、あれば `+`。
- **thin (ignored の枝)**: heavy/double のとき、ignored エントリだけが寄与したビット (`Cell.thin`) の軸は細線に落とす。軸ごとに `axis(bits) = (mask&bits&!thin != 0) || mask&bits==0` で太い/無いを判定し、(縦太, 横太)=(t,t)→通常、(t,f)→`MIXED[style][0]`、(f,t)→`MIXED[style][1]`、(f,f)→square の細線 (`GLYPHS[1]`)。`MIXED` 表 (mask 順):
  - heavy・縦太横細: `· ┃ ┃ ┃ ─ ┚ ┒ ┨ ─ ┖ ┎ ┠ ─ ┸ ┰ ╂` / 縦細横太: `· │ │ │ ━ ┙ ┑ ┥ ━ ┕ ┍ ┝ ━ ┷ ┯ ┿`
  - double・縦二重横細: `· ║ ║ ║ ─ ╜ ╖ ╢ ─ ╙ ╓ ╟ ─ ╨ ╥ ╫` / 縦細横二重: `· │ │ │ ═ ╛ ╕ ╡ ═ ╘ ╒ ╞ ═ ╧ ╤ ╪`
  - thin 判定: 線を描く間 `thick = !ignored(kid)` (kid の join と tick)。縦棒と elbow は `thick = ブロック内に非 ignored が 1 つでもある`。`thin = mask & !thick_bits`。`ignored(id) = dim_ignored && git_state(id)==Ignored`。
- 強調 (emph) は最大が勝つ: DIM=0 < ACTIVE=1 < ROUTE=2。`owner` は emph が `>=` のものが後勝ち。

### 2.2 芽・git 印・末尾記号 (ui.rs: `frame`、git.rs: `St::glyph`)

- **芽 `›`**: 閉じたフォルダ (`is_dir && !expanded && (children 未読 || children 非空)`)。位置 = 名前の右端 + 1 セル空けた次 (`sx + a.w + 1`)。色 = git 状態があればその色 (ignored 除く)、無ければ `mix(bg, rgb, alpha*0.55)`。フォルダの git 状態は配下の最重 (§5)。explode 中の対象フォルダは芽が braille スピナーに置換 (太字・route 色)。
- **git 印 (ファイルのみ、芽と同じ位置)**: `?` untracked / `+` staged / `M` modified / `!` conflict。太字、色は §1.1 の git 4 色。ignored は印なし (名前を灰色に)。
- **末尾記号**: ツリー上の名前に `~` は付かない (`App::label` は純粋な名前。幅が変わるとその右の全列が動くため)。`…` は列幅で切ったときだけ (§3)。ステータスバーの `+` (フォルダ総量が走査上限で未完) と ` (partial)` は §3.4。
- **スピナー**: `⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏`、80ms/フレーム (`ui.rs:spinner`)。
- **pill**: カーソル行の背景のみ。x 範囲 = `[cx-2, cx+cw]` の両端含む (名前の左に 2 セル、右に 1 セル余白)、cw = ラベル (名前+details) の表示幅。pill は線・文字より先に描く。`pill` は **spine 上の固定の縦位置 (y=cy)** に固定され動かない (アイテムが滑り込む)。
- ホバー pill・クリックは写さない。

## 3. レイアウト

### 3.1 列と行 (layout.rs: `layout_with`)

- 各 depth が 1 列。root は x=0, y=0。展開済みかつ kids 非空のフォルダ (= parent) の子は次列に **連続した行ブロック** で並ぶ。`kids()` は dotfile を `show_hidden` か `reveal` (root..cursor) でなければ除外。
- **spine** (`spine()`) = root..cursor、さらに cursor が展開済みなら「最後にいた子 (`last`) か先頭の子」を辿って続ける。spine の全ノードは **y=0 固定** (= 選択線の縦位置。カメラは `ty = cy - canvas高/2` で中央)。
- ブロック位置 (列ごとに parents を y 順に処理):
  - `ideal(py,len) = py - (len-1)/2` (整数除算。親の行を中心に)。
  - spine を含むブロックはピン止め: `y0 = -(spine 子のブロック内 index)` (spine 子が y=0 に来る)。それより上のブロックは `y0[j] = min(ideal, 次ブロックy0 - 1 - len[j])` で上へ詰める。下のブロックは `y0[j] = max(ideal, 前ブロック末尾 + 2)`。**ブロック間は常に空白 1 行**。
  - spine が無い列 (pin なし) は先頭から `max(ideal, prev_end+2)`。
- `row_spacing` (0–3): まず間隔 0 でレイアウトし、全 y に `pitch = 1+row_spacing` を掛ける。線は実際の行位置間に引くので隙間を貫く。
- ラベル: 名前を `truncate_to(name, max_name)` (表示幅 > max なら、幅の累計が `max-1` に収まる文字だけ残して `…` を足す。全角は幅 2)。

### 3.2 列幅

| 設定 | 内容 |
|---|---|
| columns=fit (既定) | 列幅 `colw` = その列で最長のラベルの表示幅 |
| columns=equal | details off のとき `colw = max(最長, max_name)`。details on のときは名前部を `max_name` に揃える |
| max_name (列幅) | 既定 **28**、12–60、step 2。`MAXW=28` |
| name details | ラベル = 名前 + `pad` + details。`pad = namew - 名前幅 + 2 + dw - details幅` (details は列内右揃え、名前との最小間隔 2)。namew = fit なら最長名前幅、equal なら max_name。age: `now 5m 3h 2d 4w 8mo 3y` (<60s/<1h/<1d/<7d/<30d/<365d/それ以上)、size: `980B 4.2K 12M 1.3G` (4 桁以内、`short_size`)、both: `format!("{age:>4}{dot}{size:>4}")`、dot = size 空なら 3 空白、他は ` · `。フォルダの size は走査完了前は空 |
| column_gap | 既定 **3**、3–12 (`gap.max(3)` で下限 3: 名前の次セルに芽/git 印、その先から線) |
| branch_offset | 既定 **1**、0–4。join と名前の間の水平線のセル数 |

次列の x: `next_x = x + colw + lanes + max(gap,3) + max(branch,0)`。`lanes` は pipes ごと (下)。各列の帯 `cols[i] = (x-2, next_x-2)` (ホイール用。写さない)。

### 3.3 pipes (既定 capped) の track 割り当て (layout.rs: `Pipes::Capped`)

各 parent の block を、親行との関係で分類: `Less`=ブロック全体が親より上 (raised)、`Greater`=ブロック先頭が親より下 (hanging)、`Equal`=親の行がブロック内 (level)。

- `cap = max(colw/3, 2)` (`LANES_PER_WIDTH=3`)。`ups`=Less の数。
- track (slot k、バーから左へ数える): Less は出現順に `up++` して `min(ups-up, cap-1)`、Greater は `down++` して `min(down-1, cap-1)`、Equal は 0。
- `lanes = max(1, min(cap, max(ups, downs)))` (最低 1)。上限を超える長い肘は最外 lane を共有。
- 線の組み立て (`lines()`): `start = 親の x + 親ラベル幅`、`bar_x = min(子x) - 1 - branch`。`bar_x < start` ならまだ展開途中として描かない。
  - 縦棒 `vseg(bar_x, y0..y1)`。各子: `branch>0 && x-1>bar_x` なら `hseg(bar_x, x-1)` + `(x-1)` に RIGHT 追加、それ以外は `bar_x` に RIGHT のみ (offset 0 は名前に接する)。
  - 親の行が [y0,y1] 内 (level): `hseg(start, bar_x, py)` で直線 join。
  - 外 (raised/hanging): ty = 先頭の子の行 y0 (`top_entry`、crossing 以外)。`tx = max(bar_x - 1 - k, start)`。`hseg(start→tx @py)`、`vseg(tx, py→ty)`、`hseg(tx→bar_x @ty)` の肘。
  - 経路: 親が route 上でかつ子に route があれば、その子の行と肘の上端が ROUTE。さらに `vseg(bar_x, ty→経路子の行, ROUTE)`。
- 他の pipes (river / nested / crossing / tidy) は設定にあるが **既定外**。アルゴリズムは layout.rs の `river()` / `tidy()` / `Nested` / `Crossing` 分岐 (river は `PipeMemory` と idle のスイープ `pipes_poll` を要する)。移植スコープ外なら capped のみで足りる (ソースに「必須」とは書かれていない)。

### 3.4 ステータスバー (ui.rs: `status_bar`、背景 `bar`)

左: パンくず ` ` + `root › … › leaf`。区切り ` › ` (色 `accent.active`)、祖先は muted、leaf は太字 `text`。右詰めの右側に収まらないとき先頭から削り、先頭に `… › ` (muted)。`room = 幅 - (右幅+2)`、`width_from(s) = Σ(名前幅+3) + (s>0 ? 4 : 0)` が room 以下になるまで start を進める (最低 1 要素残す)。

右 (左から順、右端揃え。幅 > 右幅のときだけ描く):

1. explode 中: `{spinner} ` (route) + `exploding · {n} folders · ` (n=0 なら `reading`、text 色) + `esc` (太字 route) + ` stops · ` (muted)。そうでなく note が 3 秒 (`NOTE`) 以内なら `{msg} · ` (text)。
2. git repo 内: `⎇ ` (route) + `{branch} · ` (text)、状態があれば `{ignored|untracked|staged|modified|conflict} · ` (git 色)。branch は `main ↑1 ↓2` 形式 (§5)。
3. ソートが既定でなければ `⇅ {説明} · ` — **ソートは名前順のみ移植なら不要**。
4. `{meta} · ` (muted): フォルダ = 読込済みなら `{n} items` (kids の数、dot 除外)、未読なら `folder`。walk 結果があれば ` · {human}{+ (未完のとき)}` を足す。ファイル = `human(size)` (`{n} B` / `{v:.1} KB|MB|GB|TB`、1024 進)。
5. `● ` (heat 色) + `{ago}` + 未完なら ` (partial)` (text)。`ago`: `{s}s ago` / `{m}m ago` / `{h}h ago` / `{d}d ago` (<365d) / `{y:.1}y ago`。
6. 空白 3 個 (muted)。
7. 凡例 (legend 設定 on かつ幅に余裕): `now ` (muted) + 停止点 7 個の `▮` (各 heat 色) + ` old   ` (muted)。
8. キー案内 (キーは太字 `route`、説明は muted): `!` ` cmd  `、`s` ` shell  `、`q` ` quit  ` (cd 連携なしなら `quit`)、`?` ` keys `。

幅の落とし方: `base = 右の既存幅合計 + 24`、`hw` = 案内 4 つの幅合計、`legend_w = 4 + 7 + 7`。`w >= base+legend_w+hw` なら凡例+4 案内、`w >= base+hw` なら凡例なし+4 案内、それ以外は `?` 案内のみ (凡例は `w >= base+legend_w+? 案内幅` のときだけ)。

`!` プロンプト行: ` {フォルダ名} $ ` (太字 route) + 入力 (text) + 反転空白 (カーソル) + (余裕があれば) `  enter run · esc cancel · $f = selection ` (muted)。入力は幅超過時に先頭から削って末尾を見せる。
`/` 検索行: ` / ` (太字 route) + 入力 + 反転空白 + 件数 (`  {at}/{n}`、n>0 route 色、0 件は `  no match` を dot 色) + ヒント (空入力または n>0: `  tab ↑↓ cycle · enter stay · esc back `、0 件: `  esc back `、muted)。

## 4. 動き

### 4.1 ばね (anim.rs)

- `Damped::step(target, smooth, dt)` = Unity SmoothDamp: `omega=2/smooth; x=omega*dt; exp=1/(1+x+0.48x²+0.235x³); change=v-target; temp=(vel+omega*change)*dt; vel=(vel-omega*temp)*exp; out=target+(change+temp)*exp`。オーバーシュートしたら target に固定して `vel=0`。`|v-target|<0.02 && |vel|<0.2` で完了 (false を返す)。位置は描画時に `round()`。
- `approach(cur, target, tau, dt)`: `cur += (target-cur)*(1-exp(-dt/tau))`、`|差|<0.004` で完了。

| 定数 | 値 | 出典 |
|---|---|---|
| ノード移動 x,y | smooth `MOVE=0.11` | anim.rs |
| ノード出現 alpha | approach tau `FADE_IN=0.07` | anim.rs |
| ノード消滅 alpha | tau `FADE_OUT=0.05`、alpha<=0.02 で破棄 | anim.rs |
| 名前色 cross-fade | tau `RECOLOR=0.12` | anim.rs / ui.rs |
| カメラ x,y | smooth `CAM=0.16` | ui.rs |
| 信号ビーズ | smooth 0.13 | ui.rs `frame` |
| プレビュー開閉 | smooth `POPUP_T=0.085` | ui.rs |
| プレビュー縦スクロール | smooth 0.07 | ui.rs |
| help の開閉 | approach tau 0.06 | ui.rs |
| 設定板の開閉 | approach tau 0.05 | ui.rs |
| ripple の段間隔 | `RIPPLE_STEP=90ms` | main.rs |
| ripple の寿命 | `RIPPLE_LIFE=2200ms` | main.rs |
| pulse 立ち上がり / 減衰 | `RISE=0.07s` (線形) / `FADE=0.45s` (指数) | anim.rs `pulse` |
| 1 フレーム | `FRAME=16_667µs` (60fps) | main.rs |

`motion` 設定 (speed) は **dt に掛ける係数**: slow 0.5 / normal 1.0 / fast 2.0 / instant 1000.0 (`Speed::factor`)。ホイール慣性 (glide) だけは実時間。`dt` は `min(実経過, 0.05)`、idle から再開した最初のフレームは `FRAME` 1 枚分。

### 4.2 展開/畳み込み・兄弟の滑り (anim.rs: `Scene::sync`)

- 各可視ノードがレイアウト目標へ向かう独自のばねを持つ。**新ノードは「最も近いアニメ中の祖先の (x+w, y)」から湧き出る** (親から広がる)。祖先が無ければ目標位置に直接。レイアウトは列順なので親が先に生まれる。
- レイアウトから消えたノードは **ghost**: 最も近い生存祖先の (x+w, y) へ向けて縮みつつ alpha を 0 へ (`FADE_OUT`)。ghost は子を持たない描画 (ghost を先に、生きているノードを後に描く)。ghost には git 印・芽・hit を出さない。
- 線は **アニメ中の位置**から毎フレーム引く (`pos(id)` = ghost でなく alpha>0.05 のノードのみ)。だから肘が伸びる。
- 兄弟の滑り: 目標が変わると velocity を持ったまま新目標へ (再始動のジャークなし)。

### 4.3 光が線を走る (ビーズ) (ui.rs: `frame`)

カーソルが変わるたび `bead = Damped(start)`。`start = (前の cursor の x != 今の cx) ? 前の x - 2 : cx - 10`、目標 `cx - 2`、smooth 0.13。`head=round(v)`、`dir = vel>=0 ? -1 : +1` (尾は進行方向の後ろ)、`tail = clamp(|vel|*0.05, 1, 12)`。i=0..=tail について x=head+dir*i、cursor 行 (`cy-oy`) の既に描いた非空白セルだけ、fg = `mix(accent.route, flash, 1 - i/(tail+1))`、i==0 のときセル bg = `mix(bg, route, 0.35)`。空白セルには描かない。最後に描く (ラベルの上を通る)。終了で破棄。

### 4.4 ripple (ライブ変更) (main.rs: `ripple`, `apply_changes`; ui.rs)

- 起点 (変更/追加エントリ。消えた場合はそのフォルダ) から root へ `path_to` を逆順に、k 段目の `start = now + 90ms*k`、`strength = max(0.8^k, 0.35)`。既存 ripple より早ければ置換 (strength は大きい方)、既存が `RIPPLE_STEP*4` (360ms) より前に始まっていれば再点灯。dotfile (非表示) は `show_hidden` でないと起点にしない。
- 明るさ = `pulse(t)*strength`、t = 現在時刻 - start (負は 0)。`pulse`: t<0→0、t<0.07→t/0.07、以後 `exp(-(t-0.07)/0.45)`。`start + RIPPLE_LIFE` で破棄。
- 名前: fg を `mix(rgb, ripple, glow)`、bg を熾火 (§1.4)。
- 肘: ノード→親フォルダの肘 (親ラベル右端〜子 x の範囲・親行と子行の間の行、かつ「両行 or 縦成分のあるセル」) を、`pulse(t + STEP/2)*strength` で `mix(線色, ripple, 0.9*g)`。owner = その親。
- 設定 `ripples` off で全消去 / 新規なし。

### 4.5 カメラ (ui.rs: `frame`)

- 目標 `tx = cx - canvas幅*0.38` (cx = カーソル列の**ラベル左端**。ラベル中央にしない = j/k で名前の長さにより横揺れしない)。`ty = cy - canvas高/2`。両方 `CAM` のばね。screen = world - `round(cam)`。
- (`cam_hold` はホイール用 = 写さない)。

### 4.6 プレビューの開閉

POPUP_T のばね `open` (0=カーソル行の 1 行 pill、1=全開)。矩形 = `lerp_rect(from, full, p)` (x,y,w,h をそれぞれ線形補間→round)、`from` = (pill の x、cursor 行、幅 `pill_w+3`、高さ 1)、`full` = canvas の中央 90% (`popup()`: `w=W*9/10, h=H*9/10`、整数除算、中央寄せ)。背後は `dim_backdrop(area, 0.55*p)`: 矩形外の全セルの fg/bg を `mix(元, bg, 0.55*p)` (非 RGB 色は fg なら `default_fg`、bg なら `bg` から)。枠色 = `mix(pop, route, p)`。閉じるときは `closing=true` で目標 0、`open<0.03` で破棄。中身の本文/スクロールバーは `p>0.9` 以降に描く (ポップアップ全開前は枠のみ)。

### 4.7 再描画の仕組み (main.rs: main ループ)

- `dirty || animating` のときだけ `term.draw`。idle は **フレームを描かない**。アニメ中は `FRAME` 経過までホールド (入力は溜めて処理)。`frame()` は「まだ動いているか」を返し、それが次フレームの `animating`。
- イベント待ちの timeout: アニメ中 = 次フレームまで、`ticking` (explode 中・note 表示中) または river スイープ残 = 50ms、それ以外 100ms。タイムアウト (入力なし) のたびに mtime 結果・ライブ変更・git・explode を poll して変化があれば dirty。
- 入力はキューを空にしてから 1 回描く。キーでレイアウト `epoch` を +1 (レイアウトは `epoch` か canvas 高が変わるときだけ再計算。アニメフレームは再利用)。

## 5. データ

### 5.1 再帰 mtime (mtime.rs)

- 裏スレッド 1 本。要求 (`request(dir)`) はキャッシュが無いか「未完 かつ 直接走査でない」ときだけ。要求は**新しい順**に処理 (キューは push_front)。可視フォルダ (描画した dir) を毎フレーム request。
- `walk`: 深さ優先。**エントリ 1 件ごとに予算 `WALK_CAP=50_000` を 1 減らす** (全再帰で共有)。0 で打ち切り `complete=false`。通過した各ディレクトリの結果をすべて返す (子を後で開くとキャッシュヒット)。
- 結果 = (`newest` = 配下全ての mtime の最大 (ディレクトリ自身の mtime も含む)、`complete`、`direct` = その dir を根にした走査か、`vis`、`bytes`)。
  - `vis` = 非 dotfile の**非ディレクトリ**の mtime の最大。dot ディレクトリ配下とディレクトリ自身の mtime は無視 (dotfile の増減で dir の mtime が動くため)。
  - `bytes` = 非ディレクトリ全部 (dotfile 含む) の `len` の合計。
- 質: `rank = 2*complete + direct`。既存より rank が同等以上の結果だけ上書き (complete > 直接上限 > 通りがかり上限)。直接要求で上限に達した結果は最終。
- **symlink は辿らない**: `lstat` (`DirEntry::metadata` / `symlink_metadata`)、`file_type().is_dir()` も symlink には false。`tree::make` も lstat なので、**ディレクトリへの symlink は「ファイル」扱い**。
- **ignored の扱い**: 走査は git ignored を見ない (除外しない)。ignored が効くのは表示 (灰色・thin 線・芽の色)、`e` (explode) の降下除外のみ。
- `Tree::heat(id)`: `show_hidden` なら `max(全体 newest, 自身の mtime)`、そうでなければ `vis` (> UNIX_EPOCH のとき)、それも無ければ自身の mtime。ファイルは自身の mtime (lstat)。walk が無いフォルダは自身の mtime。
- 完了結果は `Node.rec = (newest, complete, vis, bytes)` にコピー。`r` で再走査 (`invalidate`: 配下と祖先のキャッシュを消す)。

### 5.2 ライブ更新 (watch.rs)

- 方式 = **ポーリング** (inotify/FSEvents 不使用)。`EVERY=1000ms`。対象 = 開いているフォルダ (`open_dirs` = 展開済み + root で、children が読込済みのもの)。深く閉じたフォルダの変化は見ない。
- スナップショット `name → (mtime, size, is_dir)` を保持し、周期ごとに `read_dir` して差分。新しく開いたフォルダの最初の観測は基準 (変更扱いしない)。
- `Change { dir, listing (増減あり), newest, newest_vis }`。`newest_vis` は非 dotfile の非ディレクトリのみ。`listing` ならフォルダ自身の mtime も newest に含める。
- 適用 (`apply_changes`): `Tree::refresh` でノード・展開状態・アニメを保ったまま差し替え (消えたものは arena に残るが未接続)。listing 変化なら mtime を `forget`+`request` (正確な熱を再計算)。変更の `newest` を祖先全てへ `bump` (dot フォルダを通ると `vis` は EPOCH にして上へ伝えない)。git を poke。カーソルが消えたら「それを置き換えたエントリ (`cmp>=`) か最後 / 無ければフォルダ」へ (`reattach`)。

### 5.3 git (git.rs)

- 裏スレッド。開いているフォルダの集合を受け取り、各フォルダの repo top (祖先で `.git` (dir/file) を持つ最も近いもの) ごとに実行。**`EVERY=3s`** で全 repo を再実行 (コミット・`git add` はウォッチャが見ないため)、集合の変化時は新規 repo だけ、ライブ変更/`r` で poke 時は全 repo。出力が前回と同じなら送らない。
- コマンド (cwd = repo top、stdin/stderr null): `git --no-optional-locks status --porcelain=v1 -z --branch --ignored --untracked-files=normal`。
- 状態 (優先度 低→高): `Ignored < Untracked < Staged < Modified < Conflict`。XY 判定 (`classify`): `??`→Untracked、`!!`→Ignored、`U?`/`?U`/`AA`/`DD`→Conflict、Y が空白でない→Modified、他→Staged。R/C は次フィールド (旧パス) を捨てる。
- フォルダ: 配下の最重状態を持つ (**Ignored は祖先へ昇らない**)。末尾 `/` のパス (未追跡/ignored フォルダ一括) は `whole` に入り、配下全部がその状態。`Repo::get`: `paths` 直接 → なければ祖先の `whole`。
- branch 表記 (`branch()`): `## main...origin/main [ahead 1, behind 2]` → `main ↑1 ↓2`、`No commits yet on X` → `X`、`HEAD (no branch)` → `detached`。
- ステータスバーの状態語は `St::describe` (`ignored/untracked/staged/modified/conflict`)、色は git 色 (Ignored は muted)。
- `d` の diff 可否: ファイルの状態が `>= Staged` (staged/modified/conflict) のとき。

### 5.4 explode (explode.rs, main.rs: `explode`/`burst`)

- `e`: 対象 = カーソルがフォルダならそれ、ファイルならその親。**幅優先**を裏スレッド。`MAX_DIRS=400` フォルダ読み込み済み (`out.len() >= 400` で打ち切り) か `MAX_ENTRIES=6000` エントリ累計 (各フォルダ読み込み前に判定するので最後の 1 つで超えうる) の先に来た方で止まる。8 フォルダごとに進捗。
- 降りない: dot フォルダ (`show_hidden` でなければ)、git ignored のフォルダ (`explode_ignored` が off のとき。**対象フォルダ自身は常に開く**)。降りないものも一覧には載る。symlink は辿らない。
- 完了で全フォルダを一括 `expanded=true` にして一斉に展開アニメ (`burst`)。note: `opened N folders` / `opened the first N folders (stopped there)` (N=1 は `1 folder`)。Esc で中断 (note `explode cancelled`)。対象が接続されていなければ捨てる。
- 実行中: 芽の位置にスピナー、ステータスバーに `exploding`。

### 5.5 dotfiles / symlink / バイナリ / テキスト

- 既定 `show_hidden=false`。`.` 始まりを隠す。ただし root..cursor の経路 (`reveal`) は常に見える (カーソルが不可視の中に入らない)。起動フォルダ自体が dot でもよい。起動時は `root = 起動dirの親`、起動 dir にカーソル、root 展開。
- **バイナリ判定 (head)**: ファイル先頭 **8192 バイト**に NUL (0x00) があれば binary。表示 = `""`、`  binary file · {human(size)}`、`  {file -b の出力}` (取れなければ `binary`)。
- **テキストの読み方**: 先頭 **2 MiB** (`head()`: `take(2<<20)`) だけ読む (bat の場合は `--line-range :5000`、glow は stdin に先頭 2MiB)。全文読みはしない。`size==0` は `  (empty file)`。読み込みは裏スレッド (`pending`)、完了までスピナー (§8)。折り返し (`wrap`、plain のとき): 幅 `w = popup幅-3` (最低 20) で表示幅ごとに改行を挿入 (`wrap_text`、全角 2)。wrap off なら素の行 (ポップアップは**はみ出しを切る**。折り返しは自前)。
- 外部コマンド (glow/bat/file/git diff) は写さない前提。plain 経路 (`TextPreview::Plain`) が写す対象: head + wrap_text、色なし。
- diff の取得 (git diff を写すなら): `git --no-optional-locks -c color.diff=always diff HEAD -- <path>` を repo top で。HEAD が無ければ `--cached`。両方空は `  (no changes against HEAD)`。

### 5.6 ソート・名前比較 (tree.rs: `Tree::cmp`、名前順のみ)

比較キー: `lowercase(name)` を `natural_cmp` (natural_sort on) か通常比較し、同じなら元の名前で比較。`folders_first` なら先に「ディレクトリが上」。`natural_cmp`: 数字連の比較は先頭 0 を除いた桁数→辞書順→元の桁数、非数字は文字比較。`file2 < file10`、`file2 < file02`。(他のソートキーと `o`/`O` は写さない。)

### 5.7 reload / 他

- `r`: 対象 = カーソルが展開済みフォルダならそれ、ほかは親。mtime キャッシュを配下と祖先から消し、git poke、`Tree::reload(id)` (children を捨てて再読。**配下の展開状態は失われる**、`last` も消える)。ファイルに戻したカーソルは同名を探す。
- `Tree::reroot_up` (`-`/Backspace): root の親を新 root にして現 root を接木 (親は展開、`last=旧root`)。
- shell (`!`/`s`) は shell.rs: `$SHELL -ic LINE`、cwd = `work_dir` (カーソルがフォルダならそれ、ファイルなら親)、環境 `f=選択パス`。3 秒未満で終わる `!` コマンドは `[done]`/`[exit N]`/`[interrupted]`/`[signal N]` + ` any key returns to tb` でキー待ち。戻ったら `r` 相当。移植対象かはユーザー判断 (指定の「写さない」には入っていない)。

## 6. キー (main.rs: `App::key` を正本)

優先順位: プロンプト(`!`) > 検索(`/`) > 設定の板 > `i` (画像プレビュー中) > プレビュー(開いていて閉じ途中でない) > Esc (explode 中) > help > 木。

### 6.1 木

| キー | 動作 |
|---|---|
| `j` `k` | Step through 設定 (既定 tree) で 1 つ進む/戻る。folder = 兄弟、column = カーソル列の全フォルダのリスト、tree = 開いたツリー全体を読み順 (親→開いた子→次の兄弟) |
| `J` `K` | 同上を 10 |
| `↓` `↑` | 列内を 1 (folder 設定のときは兄弟) |
| `PgDn` `PgUp` | 列内を 10 |
| `g` `G` / `Home` `End` | 兄弟の最初/最後 (範囲外へ出ない) |
| `l` `→` `Enter` | フォルダ: 読込・展開してカーソルを「最後にいた子 / 先頭の子」へ。ファイル: プレビューを開く |
| `h` `←` | root なら先に reroot_up、親へカーソル (展開は保つ) |
| `Space` `Tab` | フォルダの展開をトグル (カーソルは動かない)。ファイルは何もしない |
| `-` `Backspace` | 1 段上へ reroot |
| `.` | dotfiles 表示切替 (設定 show_hidden を反転) |
| `c` | カーソルのフォルダを畳む (ファイルは無反応) |
| `C` | 経路の祖先以外を全部畳む (カーソル自身が展開中でも畳まれる。README は「cursor path 以外を畳む」) |
| `e` | explode (§5.4) |
| `r` | reload (§5.7) |
| `/` | 検索開始 (カーソルの列を fuzzy。起点を覚える) |
| `n` `N` | 直前の確定した検索の次/前の一致 (列内で循環、最後の検索列を使う) |
| `,` | 設定の板を開く (行 0 から、help は閉じる) |
| `?` | help をトグル |
| `!` | コマンド行を開く |
| `s` | シェル |
| `q` | 終了 (cd 連携は写さないので単に終了) |
| `Esc` / `Ctrl-C` | 終了 (explode 中の Esc は explode 中断のみ) |
| `o` `O` | ソート切替 — **写さない** |

help が開いているとき: `?` 以外のキーは help を閉じるだけで他は何もしない (`q` でも終了しない)。

### 6.2 検索入力中 (`search_key`)

- 入力で即カーソルが追従: `find(query)` (最良 rank、同順位は先頭)、空または 0 件なら起点に戻る。
- 文字入力 (ctrl なし)、`Backspace` (空なら検索を閉じる)、`Ctrl-U` 全消去、`Ctrl-W` 末尾の英数字連とその前の非英数字を削る (`trim_end_matches(!alnum)` → `trim_end_matches(alnum)`)。
- `Tab` `↓` `Ctrl-N` = 次の一致、`⇧Tab` `↑` (入力が空でないとき) `Ctrl-P` = 前の一致。入力が空で `↑` = 直前の確定検索の語を呼び戻す。
- `Enter` = 確定してその位置に留まる (何も開かない。カーソルが一致内なら `last_search` に保存)。`Esc` `Ctrl-C` = キャンセルして起点へ。
- fuzzy (`main.rs: fuzzy`): smart case (query に大文字があれば完全一致、なければ小文字化)。rank 0 = 前方一致、1 = 単語先頭 (非英数の直後 / camelCase の小→大)、2 = 部分一致、3 (`LOOSE`) = 文字が順に含まれる。返すスパンは一致位置 (loose は連続を併合)。`matches`: rank<3 が 1 つでもあれば loose は除外、全部 loose なら全部。

### 6.3 `!` プロンプト (`prompt_key`)

`Esc`/`Ctrl-C` 閉じる、`Backspace` (空で閉じる)、`Ctrl-U` 全消去、`Ctrl-W` 直前の単語削除、`↑` `↓` 履歴 (重複は新しい方だけ残す)、`Enter` 実行 (空は無視)、文字入力。

### 6.4 プレビュー (テキスト)

`q` `Esc` `←` `h` 閉じる / `j` `↓` +1 行 / `k` `↑` -1 / `Ctrl-D` +page/2 / `Ctrl-U` -page/2 / `Space` `PgDn` +page / `PgUp` -page / `g` `Home` 先頭 / `G` `End` 末尾 / `d` (Ctrl なし) diff⇄file (diff があるときのみ。取得中は無視、切替でスクロール 0 に戻す)。page = popup 高 - 2。スクロール値は 0..(行数 - page) にクランプ。**`Ctrl-C` はテキスト/画像プレビューでは何もしない** (§食い違い)。
- 画像/PDF (media): `q` `Esc` `←` `h` 閉じる / `j` `l` `n` `Space` `→` `PgDn` 次ページ / `k` `p` `PgUp` 前ページ / `g` `Home` 先頭 / `G` `End` 末尾 / `↓` `↑` = フォルダ内の次/前の画像・PDF へ (閉じずに。空ファイルと非画像はスキップ、端では動かない) / `i` pixels⇄blocks (今回のランのみ)。
- 音声のキーは写さない。

### 6.5 設定の板 (`menu_key`)

`Esc` `,` `q` `Ctrl-C` 閉じる / `j` `↓` `Tab` 次行 / `k` `↑` `⇧Tab` 前行 (どちらも循環) / `g` `Home` 先頭 / `G` `End` 末尾 / `l` `Space` `→` `Enter` 値を +1 / `h` `←` 値を -1 / `r` その項目を既定へ。値変更のたびに適用して保存 (§7)。マウス・プロンプト・検索はこの間無効。

### 6.6 help (`?`)

help は閉じる専用 (上記)。内容 (`ui.rs: KEYS`, 24 行)。ここに載るが写さないもの: `mouse` `wheel` `image / pdf` の pixels 説明 `audio` `o O`。

## 7. 設定 (settings.rs)

保存先: `$TB_CONFIG` > `$XDG_CONFIG_HOME/tb/config.toml` (絶対パスのとき) > `~/.config/tb/config.toml`。フラットな `key = value` TOML。
読み込み: 行を ` #` 以降で切って trim、`#` 始まり・空行は無視、`=` で分割、値は trim して `"` を除去。範囲外/未知は**その行だけ**無視 (既定のまま) し `line N: ...` を板の足元 (最大 2 件) に出す。
保存: 変更した 1 キーの行だけ書き換え (無ければ追記)。他の行・コメントは保つ。新規ファイルの先頭は `# tb settings. Press , in tb to change them, or edit this file.`。値の表記: 数値は素、bool は `true/false`、文字列は `"..."`。Heat colors は paper ground のとき `paper_palette` に保存 (dark の `palette` は残す)。`sort` を変えたら `sort_reverse` も保存。環境変数 `TB_SORT` `TB_LIVE=off` `TB_GIT=off` `TB_GRAPHICS` はその回だけ優先。

板の項目 (`ITEMS`、36 件、順序どおり)。「保存キー」= 板のキー名 (file_key の例外は上記)。印: **×** = 写さない。

| セクション | 板の名前 (保存キー) | 値 / 範囲 | 既定 | 印 |
|---|---|---|---|---|
| Layout | Row spacing (`row_spacing`) | 0–3 | 0 | |
| Layout | Column gap (`column_gap`) | 3–12 | 3 | |
| Layout | Pipes (`pipes`) | river nested crossing capped tidy | capped | 既定外 4 つは任意 |
| Layout | River tracks (`tracks`) | 1–6 | 3 | river 用 |
| Layout | Column width (`max_name`) | 12–60 (step 2) | 28 | |
| Layout | Columns (`columns`) | fit equal | fit | |
| Layout | Name details (`details`) | off age size both | off | |
| Order | Sort by (`sort`) | name modified size type | name | **×** (name 以外) |
| Order | Reverse (`sort_reverse`) | on/off | off | **×** |
| Order | Folders first (`folders_first`) | on/off | off | |
| Order | Natural sort (`natural_sort`) | on/off | on | |
| Order | Dotfiles (`show_hidden`) | shown/hidden (保存は true/false) | hidden | |
| Look | Ground (`ground`) | dark parchment vellum | dark | |
| Look | Accent (`accent`) | indigo teal violet amber mono (紙では forest/lapis 固定表示) | indigo | |
| Look | Heat colors (`palette`/`paper_palette`) | §1.3 | ember / growth | |
| Look | Heat range (`heat_range`) | day week month year 5y | 5y | |
| Look | Off-line dim (`focus_dim`) | 0–10 | 6 | |
| Look | Tree lines (`lines`) | double heavy rounded square ascii (循環順) | **double** | |
| Look | Branch offset (`branch_offset`) | 0–4 | 1 | |
| Look | Legend (`legend`) | on/off | on | |
| Look | Motion (`speed`) | slow normal fast instant | normal | |
| Behavior | Live updates (`live`) | on/off | on | |
| Behavior | Ripples (`ripples`) | on/off | on | |
| Behavior | Git status (`git`) | on/off | on | |
| Behavior | Dim ignored (`dim_ignored`) | on/off | on | |
| Behavior | Dim floor (`dim_floor`) | 0–10 | 5 | |
| Behavior | Explode ignored (`explode_ignored`) | on/off | off | |
| Behavior | Step through (`step`) | folder column tree | tree | |
| Behavior | Mouse (`mouse`) | on/off | on | **×** |
| Behavior | Wheel speed (`wheel_speed`) | 1 2 3 5 (端で止まる) | 1 | **×** |
| Behavior | Momentum (`momentum`) | off short medium long (tau 0 / 0.12 / 0.25 / 0.5 s) | short | **×** |
| Behavior | Image previews (`graphics`) | auto pixels blocks off | auto | **×** (ピクセル画像) |
| Behavior | Block glyphs (`blocks`) | half quadrants sextants | quadrants | 画像と一緒に後回し |
| Behavior | Text preview (`preview`) | styled bat plain | styled | **×** (glow/bat。plain のみ) |
| Behavior | Wrap lines (`wrap`) | on/off | on | |
| Behavior | Remember place (`remember`) | on/off | off | |

操作: 数値は `step` で ±1 (clamp)、選択肢は循環 (`cycle`)、bool は反転、`wheel_speed` は `[1,2,3,5]` を端で止まる。ground を循環したあと palette は新 ground の提供外なら先頭にフォールバック (ink は共通)。`reset` は `Settings::default()` の値へ。

Remember place (places.rs): `$XDG_STATE_HOME`(絶対) か `~/.local/state` の `tb/places`。行形式 `= <起動dir>` / `+ <開いていたフォルダ>` (root 以外の展開済み) / `@ <選択パス>`、新しい起動が先頭、最大 50 件 (`KEEP`)。終了時に保存 (その時点の設定で判定)。起動時は一致する起動 dir の記録を読み、存在しないパスは飛ばす。改行を含むパスは保存しない。

## 8. help overlay と settings panel

### 8.1 help (`ui.rs: help`)

- 幅 `64` (canvas が狭ければ canvas 幅)、全高 `KEYS.len()+6` (=30、画面高で頭打ち)。**x,y は全高基準で中央**、`h = round(full_h*t)` (最低 1) と**上端固定で下へ伸びる**。`t` = `help_anim` (tau 0.06)。`>0.01` で描く。
- Block: 枠 Rounded、枠色 `fade(route)`、背景 `pop`、題 ` treebeard ` (太字)。`fade(c) = mix(pop, c, t)`。
- 本文: 空行、`KEYS` 24 行 = `format!("  {k:>17}  ")` (太字 route) + 説明 (text)、空行、凡例行 `  color = last change inside  now ` + `▮`×7 (heat 色) + ` 5y` (muted)。
- `KEYS` (key / 説明): `h j k l / arrows` / `move · j k walk the open tree, ↑ ↓ the column`、`l / enter` / `open folder · preview file`、`space / tab` / `fold / unfold`、`J K / pgup pgdn` / `jump 10 (pgup pgdn in the column)`、`g G` / `first / last sibling`、`/ n N` / `fuzzy find in column · next / previous`、`tab ↑↓ while /` / `cycle matches · Caps = exact case`、`-` / `re-root one level up`、`c C` / `fold this folder · collapse other branches`、`e` / `explode: open every folder inside · esc stops`、`.` / `show / hide dotfiles`、`o O` / `sort: name · newest · largest · type · reverse`、`r` / `reload (open folders update live)`、`mouse` / `click select · click again open`、`wheel` / `scroll the column under the pointer`、`preview` / `j k · space · ctrl-d/u · g G · q`、`d in a preview` / `git diff ⇄ file · M + ? ! marks`、`image / pdf` / `j k page · ↑ ↓ image · i pixels ⇄ blocks`、`audio` / `space pause · ← → seek · ↑ ↓ volume · 0-9`、`!` / `run a command here ($f = selection)`、`s` / `shell here · exit / ctrl-d returns`、`,` / `settings: layout · colors · mouse · previews · …`、`?` / `toggle this help`、`q / esc` / `quit (q + tb.bash: cd there)`。

### 8.2 settings panel (`ui.rs: settings_panel`)

- 位置 = canvas の**右端ドック**。`w = min(52, canvas幅)`、`x = canvas.right - round(w*t)` (右からスライドイン)、y = canvas 上端、高さ = canvas 高。t = `menu_anim` (tau 0.05)。`<3` なら描かない。`inner = w-4`。
- 枠 Rounded、枠色 `fade(route)`、背景 `pop`、題 ` settings ` (太字)。ステータスバーは覆わない。
- 本文: セクション見出し ` {SECTION}` (大文字・太字・muted)、セクション間に空行。行 = `  {label}` + 余白 + (選択行 `‹ ` / 他 `  `) + 値 + swatch + (選択 ` ›` / 他 `  `)。余白 `pad = inner - (labelW + 2 + (valueW + 4 + swatchW))`。選択行: 行背景 `mix(pop, pill, t)`、ラベル太字 `text`、`‹ ›` は `route`、値は `text`。非選択ラベルは `mix(muted, text, 0.5)`。swatch: accent 行 ` ━━` (route 色)、palette 行 ` ` + `▮`×7 (現在 palette の停止点色)。
- 足元 (固定、空行から): 選択項目の `help` 文を `inner` 幅で単語折り返し (先頭 2 空白)、空行、保存/設定ファイルのエラー (dot 色、最大 保存失敗 + 設定エラー 2 件、`truncate_to(inner)`)、設定ファイルのパス (`~` 短縮、muted、保存先が無ければ `not saved: no home directory`)、キー行 `  j k move  h l change  r reset  esc close` (キー太字 route、説明 muted)。
- スクロール: 本文の可視行数 `room = (高-2) - 足元行数` (最低 1)、`skip = min((選択行+1)-room, 本文行数-room)` (飽和減算)。足りない分は空行で足元を下に寄せる。
- 各項目の `help` 文は `settings.rs: ITEMS` にある (36 文。英語の短文。移植時はそのまま使える)。

### 8.3 プレビュー枠の飾り (写す)

題 ` {ファイル名} · {human(size)} ` (太字 text、diff 表示中は ` {...} · diff `)。右下 (muted、右揃え): テキスト = ` {d}{scroll+1}/{総行数 (最低1)} ` で `d` は diff 可能なら `d diff · `、diff 表示中なら `d file · `、無ければ空。画像 = ` {mode}page {n}/{N} ` (複数ページ) / ` {mode}{w}×{h} `、mode = `i {blocks|kitty|sixel|iterm2} · ` (picker があるとき)。スクロールバー: 縦の最大スクロール > 0 かつ p>0.9 で、枠の右辺 (上下に 1 セルずつ内側) に track `│` (fg `accent.dim`) と thumb `┃` (fg `accent.route`)。読込中: 空行を積んで縦中央に `{spinner} ` (route) + `loading` / `rendering markdown` / `diffing` (muted) を中央寄せ。失敗 `(preview failed)`。

## 9. 画像・PDF のブロック描画 (media.rs)

- 対応: 拡張子 `png jpg jpeg gif webp bmp tif tiff ico svg svgz avif heic tga ppm pgm pnm qoi xcf psd` と `pdf` (`shows`)。`image` クレートで直接デコード、不可 (または svg 系) は ImageMagick: `convert -background none -density 150 <path>[0] png:-` (標準出力の PNG)。サイズ 0 のファイルは media にしない (テキスト扱い)。
- **PDF**: ページ数 `pdfinfo <path>` の `Pages:` 行 (読めなければ 1)。ページ画像 `pdftoppm -f N -l N -png -singlefile -scale-to 1600 <path>` (標準出力の PNG、**長辺 1600px**、`PDF_PX`)。ツール = poppler (`pdfinfo` / `pdftoppm`)。失敗は `can't render · pdftoppm: ...`。
- 配置: `area` = ポップアップ内側 (margin 縦横 1)。`s = min(area.w*fw/img.w, area.h*fh/img.h)` (fw,fh = 端末のフォントセル px、取れなければ (10,20))。`cw = clamp(img.w*s/fw, 1, area.w)`、`ch = clamp(img.h*s/fh, 1, area.h)` (**整数切り捨て**: 端数セルは切る)。中央寄せ。画像を `resize_to_fill(cw*fw, ch*fh, Lanczos3)` したあと `resize_exact(cw*2, ch*rows, Lanczos3)`。rows = quadrants/half 2、sextants 3。
- **quadrants/sextants** (`blocks`): セルごとに 2 × rows のサブピクセル (インデックス `i`: x = i%2、y = i/2)。各サブピクセルは α を背景色 (**popup の `pop`**) に合成 (`c*a + bg*(1-a)`)。2 色分割の全パターン `mask ∈ 0..2^(n-1)` (最後のサブピクセルは常に背景側) を総当たりし、on 群の平均 = fg、off 群の平均 = bg、**RGB 二乗誤差の合計が最小**のものを採る (同点は先に見つけた方、`<` で更新)。`mask==0` は fg=bg 平均。fg/bg は四捨五入して u8。
  - quadrants の文字 (`QUADS`、bit0=左上 bit1=右上 bit2=左下): `[' ', '▘', '▝', '▀', '▖', '▌', '▞', '▛']`。
  - sextants の文字 (`sextant(m)`、m = 6bit、bit は左上, 右上, 左中, 右中, 左下, 右下): 0→' '、21→'▌'、42→'▐'、63→'█'、他は `U+1FB00 + m - 1 - (m>21) - (m>42)`。
- **half**: ソースでは `ratatui-image` クレートに任せる (自前の文字決定はソースに無い)。`Half` 選択時は `quad=false` で上記クレートの Halfblocks 経路。
- 描画セルは `set_char(ch).set_fg(fg).set_bg(bg)`。キャッシュは (area, glyph 種) が変わるまで。
- ピクセル (kitty/sixel/iTerm2) と `i` の pixels⇄blocks は写さない (blocks 固定でよい)。画像ポップアップでピクセルを使うのは開き切った (`p>0.97`) 後のみ。

## 10. README とソースの食い違い

- **`~` 印**: README「folders ... trailing `~` means the cap was hit」。ソースの ui.rs / layout.rs に `~` 印は無い。実際は status bar のフォルダ総量の末尾 `+` と、age の後ろの ` (partial)`。ツリーのラベルは常に純粋な名前 (`App::label` のコメント)。
- **設定の数**: README「`,` opens 35 settings」。`ITEMS` は 36 件 (`[Item; 36]`)。
- **Tree lines の既定**: `settings-v2.md` は default `rounded`。ソース (`Settings::default`) と README は **double**。
- **settings-v2.md の他の古い記述**: 25 行のメニュー順 (実際は 36 行・順序も違う)。`details=both` の例 `2d · 12M` (実際は幅 4 の右揃え `{age:>4} · {size:>4}`)。Heat range 以外にもある pipes / tracks / focus_dim / branch_offset / dim_floor / step / blocks 等が v2 には無い。
- **`Ctrl-C`**: README「`Esc` `Ctrl-C` quit」。ソースは木でのみ終了。テキスト/画像プレビュー中は `Ctrl-C` が何もしない (音声・設定板・プロンプト・検索では閉じる)。`q` で閉じる。
- **`C`**: README「collapse everything off the cursor path」。実装は祖先以外すべてを畳むので、カーソル自身が展開中のフォルダも畳まれる (`collapse_others`: `path[..len-1]` の祖先だけ展開を残す)。
- **`g` `G` / `Home` `End`**: README 表は「first / last」。実装は**兄弟 (そのフォルダ内) の先頭/末尾**で、Step through 設定に依らない (Step through の項は README にも書いてある)。
- **`J` `K` と `PgUp` `PgDn`**: README 表は同列に「jump 10」。実装では J/K は Step through に従い、PgUp/PgDn は常に列内。
- **help の `q / esc`**: help は「quit (q + tb.bash: cd there)」。cd 連携を写さないなら文言を変える。
- **README のキー表に無い入力**: 検索中の `Ctrl-N` / `Ctrl-P`、`BackTab`、プロンプトの履歴、`i` (画像プレビューで picker があるとき)。
- **Linux/macOS 前提・ビルド**: Cargo の bin は `src/main.rs`、このディレクトリはフラット (`audio.rs` / `alsa.rs` は無い。`mod audio` は main.rs が宣言)。
- **`reload` (`r`)**: README「reload, re-walking heat deep inside closed folders」。実装は children を捨てて再読するので、配下の展開状態も失う (記述なし)。
- **symlink**: README は「Symlinks are never followed」を explode の項に書く。実装は全体 (走査・ツリー) で lstat のため、ディレクトリへの symlink は展開できない「ファイル」になる (これを意図としたコメントはソースに無い)。
