# src/glogx/

使い方・ファイル境界・設計判断の正本は README.md (「開発」「設計メモ」)。ここは「触る前に読まないと壊す」前提だけ。

## 触る前に読むもの

- Bubble Tea は v2 (`charm.land/bubbletea/v2`)。バージョンを上げる / 描画・キー入力に手を入れる前に `docs/glogx-bubbletea-v2.md` を読む (幅モデルの一致がエンジンの実装詳細に依存しており、勝手に一致し続けない)
- 画面仕様がある機能は docs が先: issues viewer = `docs/issues-viewer-spec.md`、status viewer = `docs/status-viewer-spec.md`、色 = `docs/theme-colors.md`
- 表示・レイアウトの判断は本体へ入れる前にサンプルで回す (罫線色は `tools/border-preview.sh`、全画面 ratelimit ダッシュボードの盤は `go run ./tools/dial-preview -w 120 -h 36` — 後者は本体の `usage.RenderDashboard` をそのまま呼ぶので「プレビューでは良かったのに本体で違う」が起きない。`-mono` で色を外すと幅ズレだけを見られる。`~/.claude/rules/decide-layout-in-sample-renderer-first.md`)。幅ズレは推測せず `go run ./tools/width-probe` で端末に聞く (glogx / 描画エンジン / tmux / 端末のどの層かを実測で切り分ける)

## 不変条件は lint / test が正本 (ここにもコードにも再掲しない)

render.go の純粋描画層・幅計算の単一出典・stdout / 時刻のシーム・外部プロセスの WaitDelay・toast の隠蔽・switch の網羅は `.golangci.yml` (depguard / forbidigo / exhaustive) / `ruleguard.rules.go` / `waitdelay_discipline_test.go` が強制し、理由もそこに書いてある。新しい規律を足すときも、まず lint / test で強制できないかを考える (`~/.claude/rules/comment-no-restate-enforced.md`)。

## 構造の判断 (lint では守れないもの)

- flat な `package main` は意図的。サブパッケージを切る基準は「実在する第二消費者」か「明示的な分離要望」(issues/ termsafe/ usage/ subproc/ の前例)。行数や責務の見た目で割らない (README「glog との共通コード分離について」)
- main から下位パッケージへ値・規律を共有したくなったら独立パッケージへ出す。main は下位から import できず、置くと「値を写す」運用になる (subproc がその教訓: issue 105)
- 外部由来の文字列 (git / CI ログ / issue markdown / ファイル名) は表示前に termsafe を入口で 1 回通す。出所ごとに書き分けると漏れる (issue markdown と git status のパスが実際に漏れた)
- glog (`40d4a28` で退役) の派生だが、glog との差分管理はもう無い

## ビルド・テスト

- 起動は `bin/glogx` (autobuild `--async`: 旧版で即起動し、新版は次回から。今すぐ欲しければ `GO_AUTOBUILD_SYNC=1 glogx`)。`.autobuild.*` は autobuild の作業ファイル (gitignore 済み)
- `make -C src/glogx test` は `-race` 付き。描画の確保回数は `frame_alloc_test.go`、bench の配管と予算は `tests/glogx/` (`bench_budgets.ci`)
- 幅・描画のテストは LANG 非依存が前提 (issue 027 で runewidth を排除)。ロケールで結果が変わったら幅の出典が二重化している疑い (issue 112 の系)
