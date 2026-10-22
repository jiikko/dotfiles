# treefiler

横に育つ木のファイラー。単体の `bin/treefiler` と、glogx の `F` (filer パッケージを取り込む) の両方で使う。
**見た目と挙動の正本は `docs/treefiler-spec.md`** (§0 = glogx での決定、§1〜 = treebeard のソースからの抜き出し)。実装の経緯は issue 662 (完了済み)。

## ファイルの地図

- `main.go` — `bin/treefiler` の入口。端末 (bubbletea) と toast の面倒だけを見る
- `filer/model.go` — 状態・キー・時間 (`Advance`)・描画の組み立て。外へ出す面は `New` / `Resize` / `HandleKey` (`HandleInput`) / `Advance` / `View` /
  `Animating` / `Busy` / `OwnsKeys` / `Binds` / `CaretPos` / `Paste` / `TakeExec` / `ExecDone` / `TakeNotices` / `Refresh` / `Changed` / `WatchingChan` / `Close`
- `filer/tree.go` — ディスクの木 (開いたときに 1 段ずつ読む・名前の自然順・symlink は辿らない)
- `filer/direntries.go` (`_darwin.go`) — フォルダの中身を属性ごと読む (getattrlistbulk でまとめて・だめなら ReadDir + lstat。load / readSig / walkDir が使う。issue 714)
- `filer/layout.go` — 列と行の配置・線の格子 (spec §3・§2.1)
- `filer/kids.go` — 木に見せる子 (dotfile を隠す・カーソルへの経路は見せる) とその覚え (`visKids`。並びが変わったときだけ作り直し、各子に順位を振る)
- `filer/virt.go` — 巨大なフォルダの仮想化 (画面の近くの子だけを配置しアニメを持たせる。issue 712)。何が全部を動かす場合と違うかは冒頭の doc
- `filer/anim.go` — 時間で進む補間 (ease-out quint・キーで終点へ飛ばす)
- `filer/tile.go` / `filer/preview.go` — プレビューのタイル (置き場所・リンク) とテキストの遅延読み込み
- `filer/palette.go` — 配色と熱の色
- `filer/render.go` — セルの格子 → 文字列 (I/O をしない純粋な描画層。`.golangci.local.yml` の render-pure)
- `filer/theme.go` — 配色の表 (地・アクセント・熱の色。設定の板で選ぶ)
- `filer/walk.go` — 配下でいちばん新しい更新と大きさを裏で数える (熱の色・Name details)
- `filer/git.go` / `filer/diff.go` — git の状態 (印・芽・ブランチ) と、タイルの `d` の diff
- `filer/watch.go` — ライブ更新 (開いているフォルダを読み比べる)
- `filer/search.go` / `filer/shell.go` — `/` の検索と、`!` / `s` (呼び出し側に起こしてほしいプロセスを渡す)
- `filer/fileops.go` — `a` 新規作成・`m` 名前の変更・`d` ゴミ箱へ移す (issue 722)。本体は `src/safefs`。名前の入力は `!` と同じ欄 (`promptState.kind`)、確認の板は `Model.ask`
- `filer/copy.go` — `y` / `Y` でカーソルの項目のパスをクリップボードへ (絶対 / root からの相対。issue 718)
- `filer/history.go` — `!` のコマンドの履歴 (`treefiler-history` に 1 行ずつ追記。issue 721)
- `filer/goto.go` — `:` 入力したパスへカーソルを飛ばす (root の外なら root を上げる。issue 746)。入力は `!` と同じ欄
- `filer/settings.go` / `filer/places.go` — 設定の板と保存、前回の場所
- `filer/open.go` / `filer/explode.go` — `o` (外のアプリで開く)・`e` (配下を全部開く)
- `filer/tileview.go` — タイルに出す表示の行 (本体 / diff・Markdown の整形)
- `tools/tfprobe/` — 実物のディレクトリで台本を回して所要と資源を測る道具 (下の「実物で測る」)

## 触る前に読むもの

- 🚨 filer は描画の tick を張らない。時間は呼び出し側が `Advance(now)` で進め、`Animating()` の間だけ高い周期で、`Busy()` の間は遅い周期で呼ぶ
  (glogx の「動くものがある間だけ tick を回す」に乗るため)。裏の走査・git・ライブ更新のポーリング (`watch.go` の ticker) は filer の
  goroutine で動き、結果は `Advance` で取り込む (ライブ更新の合図は `Changed()` のチャネルで呼び出し側を起こす)
- 🚨 キーの語彙は glogx-ui-guide に従う (`docs/glogx-ui-guide.md` §2: 矢印と emacs は vim の別名・移動は `tuikit/listnav.MotionOf`)。
  treebeard と食い違う所は spec §0.1 の表が正本
- 表示する文字列 (ファイル名・ファイルの中身) は入口で termsafe を 1 回通す (`newNode` / `cleanLine`)
- 🚨 全部のキーを取る層 (キー一覧・設定の板・検索・入力欄・確認の板) を足すときは Model の欄に `layer:"…"` の札を付け、`filer/layers_test.go` の表に
  足す (`OwnsKeys` と `HandleInput` の対を確かめる。札の無い層は検出しない)
- 🚨 ファイル操作のテストは本物の作成・rename・ゴミ箱への移動を呼ぶ。`filer/main_test.go` が `fsGuard` を sandbox へ差し替え、一時ディレクトリの外は
  実行前に拒否してスイートを落とす。ゴミ箱の場所 (`trashDir`) はテストで差し替える (差し替え忘れは本物の `~/.Trash` を指して拒否される)

## 実物で測る (tools/tfprobe)

リソースリークと重さは、本物のディレクトリでヘッドレスに台本を回して数で見る。`make -C src/treefiler probe` (既定は `src/` を 5 サイクル。
`ROOT=` `CYCLES=` `ARGS="-cpuprofile -memprofile -live 0"`)。結果は本体の checkout の `tmp/tfprobe-<日時>/` に出る。

- 台本: 開く → `e` (配下を全部) → root の直下の列で上下 約 800 打 → 列の中の検索 → コードと Markdown のタイル (スクロール・diff・閉じる)
  → `C` → `r` → 閉じる → 裏の処理の最中に閉じて、goroutine と子プロセスが戻るまでを測る (`busy-close`。戻らなければ止まる)。続けて一時フォルダ (git の repo) で diff のタイルとライブ更新
  (`-live` 件の mtime を一斉に進める。既定 3000。5000 を超えると見張りが間引く経路に入る)。root は書き換えない (読むキーだけ)。
  打鍵の間は 16ms とみなし、Model に見せる時計をその分進める (main.go では動きか裏の処理がある間、打鍵の間に 16ms / 80ms の
  tick が挟まる)
- `summary.txt`: 段ごとの 1 打の p50 / p95 / 最大・待ちの間のフレーム・確保 (サイクル間の中央値) と、最悪の 1 回の出どころ (`worst_at`)、
  段の後の状態 (開いたフォルダの数・タイル。**空振りの見分け**)、資源の推移。全サイクルは `steps.tsv` / `cycles.tsv`
- PDCA: 直す前のバイナリを `go build -o` で残し、**直した後のバイナリと続けて同じ条件で**回して比べる (このマシンは load average が数十まで
  揺れ、時間の絶対値は run をまたいで比べられない)。遅い 1 打は `worst_at` の打鍵で名指しされるので、そこを使い捨てのテストで観測してから直す
- 読み方の注意: diff の段は変更の無いファイルでは「空振り」と出る (正常)。`-cpuprofile` は goroutine と fd を 1 つずつ足す。
  heap_objects はサイクルごとに十数〜数十増える (Go ランタイムの goroutine の管理構造は同時に動いた最大数まで残る・git を起こすと
  Go の os/exec の側で少し残る。どちらも頭打ちに近い。2026-10-22 の調べ。issue に記録)。goroutine・fd・children が増えたらリーク。
  🚨 数で見えるのは「増え続ける」形だけで、閉じた Model を 1 つだけ掴み続ける形は増えないので見えない (疑うなら `-memprofile`)
- 速さだけの退行 (描画結果は同じ) はテストで固定していない (壁時計を合否にしない)。ここで見る: 例 `live-touch` のフレーム、`tile-code` の 1 打

## ビルド・テスト

- `make -C src/treefiler lint` / `test`。動かして見るなら `bin/treefiler [DIR]`
- 🚨 配置・アニメ・描画 (`layout.go` / `model.go` の step・sync・draw / `virt.go` / `kids.go`) を変えたら、仮想化の等価性のテスト
  (`filer/virt_test.go`。仮想化しない Model を正解役に毎フレームの画面を比べる) を乱数の種を広げて回す:
  `TREEFILER_VIRT_SEEDS=300 go test -run TestVirtualizedRandomOps ./filer` (約 2 分。CI は 12 本だけ回す)
