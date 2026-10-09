# treefiler

横に育つ木のファイラー。単体の `bin/treefiler` と、glogx の `F` (filer パッケージを取り込む) の両方で使う。
**見た目と挙動の正本は `docs/treefiler-spec.md`** (§0 = glogx での決定、§1〜 = treebeard のソースからの抜き出し)。進捗は issue 662。

## ファイルの地図

- `main.go` — `bin/treefiler` の入口。端末 (bubbletea) と toast の面倒だけを見る
- `filer/model.go` — 状態・キー・時間 (`Advance`)・描画の組み立て。外へ出す面は `New` / `Resize` / `HandleKey` (`HandleInput`) / `Advance` / `View` /
  `Animating` / `Busy` / `OwnsKeys` / `Binds` / `CaretPos` / `Paste` / `TakeExec` / `ExecDone` / `TakeNotices` / `Refresh` / `Changed` / `WatchingChan` / `Close`
- `filer/tree.go` — ディスクの木 (開いたときに 1 段ずつ読む・名前の自然順・symlink は辿らない)
- `filer/layout.go` — 列と行の配置・線の格子 (spec §3・§2.1)
- `filer/anim.go` — 時間で進む補間 (ease-out quint・キーで終点へ飛ばす)
- `filer/tile.go` / `filer/preview.go` — プレビューのタイル (置き場所・リンク) とテキストの遅延読み込み
- `filer/palette.go` — 配色と熱の色
- `filer/render.go` — セルの格子 → 文字列 (I/O をしない純粋な描画層。`.golangci.yml` の render-pure)
- `filer/theme.go` — 配色の表 (地・アクセント・熱の色。設定の板で選ぶ)
- `filer/walk.go` — 配下でいちばん新しい更新と大きさを裏で数える (熱の色・Name details)
- `filer/git.go` / `filer/diff.go` — git の状態 (印・芽・ブランチ) と、タイルの `d` の diff
- `filer/watch.go` — ライブ更新 (開いているフォルダを読み比べる)
- `filer/search.go` / `filer/shell.go` — `/` の検索と、`!` / `s` (呼び出し側に起こしてほしいプロセスを渡す)
- `filer/settings.go` / `filer/places.go` — 設定の板と保存、前回の場所
- `filer/open.go` / `filer/explode.go` — `o` (外のアプリで開く)・`e` (配下を全部開く)
- `filer/tileview.go` — タイルに出す表示の行 (本体 / diff・Markdown の整形)

## 触る前に読むもの

- 🚨 filer は描画の tick を張らない。時間は呼び出し側が `Advance(now)` で進め、`Animating()` の間だけ高い周期で、`Busy()` の間は遅い周期で呼ぶ
  (glogx の「動くものがある間だけ tick を回す」に乗るため)。裏の走査・git・ライブ更新のポーリング (`watch.go` の ticker) は filer の
  goroutine で動き、結果は `Advance` で取り込む (ライブ更新の合図は `Changed()` のチャネルで呼び出し側を起こす)
- 🚨 キーの語彙は glogx-ui-guide に従う (`docs/glogx-ui-guide.md` §2: 矢印と emacs は vim の別名・移動は `tuikit/listnav.MotionOf`)。
  treebeard と食い違う所は spec §0.1 の表が正本
- 表示する文字列 (ファイル名・ファイルの中身) は入口で termsafe を 1 回通す (`newNode` / `cleanLine`)

## ビルド・テスト

- `make -C src/treefiler lint` / `test`。動かして見るなら `bin/treefiler [DIR]`
