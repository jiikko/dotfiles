# src/glogx/

使い方・ファイル境界・設計判断の正本は README.md (「開発」「設計メモ」)。ここは「触る前に読まないと壊す」前提だけ。

## ファイルの地図

`package main` フラットな 1 package (理由は下の「構造の判断」)。責務のまとまりごとに:

- `main.go` / `options.go` — エントリポイント・CLI フラグ (`Options`)
- `tui.go` — Bubble Tea の状態機械本体 (`browseModel`)。git log の対話ブラウズ
- `render.go` / `width.go` / `box.go` — 描画層。コミット行の整形・幅計算 (`dispWidth`)・枠のレイアウト純関数
- `gitlog.go`, `gitlog_watch.go` — git log の取得 (制御文字レコードでコミット境界を識別) と外部変更の監視
- `watch_chain.go` — fsnotify のイベント待ち + 保険のポーリングの 2 本のチェーンの札 (git log と issues viewer の見張りが共有)
- `github.go`, `cache.go` — CI 状態の GraphQL 取得とローカルキャッシュ
- `external_commands.go` — git/tmux/claude/ブラウザ/クリップボードを叩くラッパー群 (テストの差し替え点)
- `cli_health.go`, `claude_version.go`, `codex_version.go` — claude/codex CLI のログイン状態検査・新版検出
- `action_modal.go` — push/pull/claude update の確認→実行→結果モーダル
- `diff_overlay.go`, `pr_status_overlay.go`, `job_detail_overlay.go` — コミットに重ねるポップアップ群 (いずれも「対象 SHA/key を持つ pager」型)
- `status_view.go`, `worktree_status.go` — status viewer (`s`)。仕様は `docs/status-viewer-spec.md`
- `issues_view.go`, `issues_drawer.go`, `issues_state.go`, `issues_watch.go`, `issues_number_filter.go`, `issues_linkjump.go`, `issues_group_expansion.go` — issues viewer (`i`) の画面側 (`issues_group_expansion.go` は epic group の展開状態 3 集合の整合) (最後は本文のファイルパスのジャンプモード `Tab`)。仕様は `docs/issues-viewer-spec.md`。ドメイン (探索・parse・表示整形) は独立パッケージ `issues/`
- `doctor_view.go`, `doctor_brew*.go`, `doctor_cache.go`, `doctor_cleanup.go`, `doctor_delete.go`, `doctor_docker.go`, `doctor_keys.go`, `doctor_resume.go`, `doctor_rowcursor.go`, `doctor_ssd.go` — doctor 画面 (`D`)。判定は `doctor/disk` `doctor/svc` `doctor/docker` `doctor/ssd` を直接呼ぶ (削除の破壊的操作は `doctor_delete.go` ではなく `doctor/disk.Delete` が持つ)
- `usage_overlay.go`, `usage_cache.go` — 右上の利用枠オーバーレイ (`U`) とそのキャッシュ
- `ratelimit_dashboard.go`, `ratelimit_resume.go` — 全画面 ratelimit ダッシュボード (`R`)。取得・整形は `ratelimit/usage`
- `filer_view.go` — 全画面 treefiler (`F`)。画面の部品は `src/treefiler` の `filer` パッケージ (replace で取り込む)。仕様は `docs/treefiler-spec.md`
- `ime.go`, `ime_tis_darwin.go`, `ime_tis_stub.go` — ブラウズ中の IME 英数切替 (macOS TIS 直接呼び出し)
- `url_picker.go` — issue 本文 URL のピッカー (本文 pager で `u`)
- `zoom.go`, `scroll_glide.go`, `hint_surfaces.go` — 開閉演出・スクロール滑走・最下行ヒントの共有ロジック
- `autobuild.go`, `cleanup_latch.go`, `probe.go`, `fullscreen.go` — 自動再ビルド通知・終了前の完了待ち latch・計測フック・全画面ビューアの排他制御
- `terminal.go` — termsafe の main 側の入口
- `open_workspace.go` — `e` で nvim を repo root で起動
- `line_cache.go` — diff / job / status が共有する行キャッシュと取得の単発化
- `issues/` — 独立パッケージ。issue markdown の探索・分類・parse・本文整形 (glogx 本体に非依存)
- `gorules/` — ruleguard のカスタム lint 規則 (`make lint` が `go vet -tags ruleguard` で型検査)
- `tools/` — 表示のサンプルレンダラ (`border-preview.sh` / `dial-preview` / `width-probe`)。本体へ入れる前にここで見た目を固める

## 触る前に読むもの

- Bubble Tea は v2 (`charm.land/bubbletea/v2`)。バージョンを上げる / 描画・キー入力に手を入れる前に `docs/glogx-bubbletea-v2.md` を読む (幅モデルの一致がエンジンの実装詳細に依存しており、勝手に一致し続けない)
- 画面仕様がある機能は docs が先: issues viewer = `docs/issues-viewer-spec.md`、status viewer = `docs/status-viewer-spec.md`、色 = `docs/theme-colors.md`
- 表示・レイアウトの判断は本体へ入れる前にサンプルで回す (罫線色は `tools/border-preview.sh`、全画面 ratelimit ダッシュボードの盤は `go run ./tools/dial-preview -w 120 -h 36` — 後者は本体の `usage.RenderDashboard` をそのまま呼ぶので「プレビューでは良かったのに本体で違う」が起きない。`-mono` で色を外すと幅ズレだけを見られる。`~/.claude/rules/decide-layout-in-sample-renderer-first.md`)。幅ズレは推測せず `go run ./tools/width-probe` で端末に聞く (glogx / 描画エンジン / tmux / 端末のどの層かを実測で切り分ける)

## 不変条件は lint / test が正本 (ここにもコードにも再掲しない)

render.go の純粋描画層・幅計算の単一出典・stdout / 時刻のシーム・外部プロセスの WaitDelay・switch の網羅は `.golangci.yml` (depguard / forbidigo / exhaustive) / `gorules/rules.go` (gocritic の ruleguard。🚨 **glogx 直下に置くと package 名衝突で型検査されない**ので独立ディレクトリ。`make lint` が `go vet -tags ruleguard ./gorules` も回す。issue 202) / `waitdelay_discipline_test.go` / `clock_rollback_test.go` (永続キャッシュの鮮度判定に時計の巻き戻しガードを強制。issue 201) / `hint_width_test.go` (最下行の hint が幅に収まること。収まらないと**抜ける手段が案内から消える**) が強制し、理由もそこに書いてある。新しい規律を足すときも、まず lint / test で強制できないかを考える (`~/.claude/rules/comment-no-restate-enforced.md`)。

## 構造の判断 (lint では守れないもの)

- flat な `package main` は意図的。サブパッケージを切る基準は「実在する第二消費者」か「明示的な分離要望」(issues/ の前例。usage / subproc / atomicfile は第二消費者 = ratelimit ができたので独立 module へ出した)。行数や責務の見た目で割らない (README「glog との共通コード分離について」)
- main から下位パッケージへ値・規律を共有したくなったら独立パッケージへ出す。main は下位から import できず、置くと「値を写す」運用になる (subproc がその教訓: issue 105)
- 外部由来の文字列 (git / CI ログ / issue markdown / ファイル名) は表示前に termsafe を入口で 1 回通す。出所ごとに書き分けると漏れる (issue markdown と git status のパスが実際に漏れた / doctor の live 経路が 1 度も通っていなかった = issue 228)
  - termsafe は **`src/termsafe` の独立 module** (各 module が replace で取り込む)。doctor 側の CLI (`bin/diskdoctor` / `bin/svcdoctor`) は **stdout へ直接書く**ので、TUI と違って描画層による後段の落としが無い = そちらこそ関門が要る、というのが分離の理由
  - doctor の走査結果は `disk.SanitizeForDisplay` / `svc.SanitizeForDisplay` を CLI と共有する。🚨 **`Item.Path` は書き換えず、制御文字を含むものを落とす** — 書き換えると画面のパスと実体が食い違い、削除の照合 (`planDelete` の itemKey) から外れて「見えているものと消えるものが違う」を作る
- glog (`40d4a28` で退役) の派生だが、glog との差分管理はもう無い

- **表示に使う記号は表示幅が安定するものだけ**。警告は 🚨 (U+1F6A8、常に 2 桁) を使い、⚠️ (U+26A0 + VS16) は
  使わない — 端末によって 1 桁と 2 桁で揺れ、行の右端がフレームごとに動いて見える (doctor 画面で実測 2026-09-02)。
  コメント内の 🚨 は表示に乗らないので対象外

## ビルド・テスト

- 起動は `bin/glogx` (autobuild `--async`: 旧版で即起動し、新版は次回から。今すぐ欲しければ `GO_AUTOBUILD_SYNC=1 glogx`)。`.autobuild.*` は autobuild の作業ファイル (gitignore 済み)
- `make -C src/glogx test` は `-race` 付き。描画の確保回数は `frame_alloc_test.go`、bench の配管と予算は `tests/glogx/` (`bench_budgets.ci`)
- 幅・描画のテストは LANG 非依存が前提 (issue 027 で runewidth を排除)。ロケールで結果が変わったら幅の出典が二重化している疑い (issue 112 の系)
