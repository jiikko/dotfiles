# doctor

ディスク掃除候補 (`diskdoctor`)・壊れた launchd 登録 (`svcdoctor`)・Mac 全体の健康診断 (`mac-healthcheck`)・アプリと formula の片付け (`mac-uninstall`) のライブラリと CLI 4 本。`diskdoctor` / `svcdoctor` / `mac-healthcheck` は読み取りだけ (削除・停止はしない)。動かすのは `mac-uninstall apply` が承認された計画を実行するときだけ。使い分け・終了コードの語彙・設計の要点の正本は README.md。

## パッケージの地図

- `disk/` — 掃除候補 (`diskdoctor`)。`catalog.go` (allowlist) / `scan.go` (走査) / `report.go` (整形) / `guard.go` (起動中プロセス等を見て「今は消してはいけない」を弾く。fail-closed) / `delete.go` (**削除経路を持つ唯一のファイル**。ゴミ箱への移動の本体は共有 module `src/safefs` の `TrashMove` で、検査点 `allowDestructive` を渡して呼ぶ) / `display.go` (`SanitizeForDisplay` 関門) / `size.go` (公開型 `Item` と du 互換のサイズ計算) / `paths.go` (glob の展開・escape)
- `svc/` — 壊れた常駐 (`svcdoctor`)。`scan.go` (`Scan` / `DefaultDirs`。走査の本体) / `plist.go` (plist 読み) / `launchctl.go` (状態取得) / `brew.go` (Homebrew 台帳との突き合わせ) / `report.go` (整形) / `display.go` (関門) / `restore.go` (保存した Report を読み戻すときの信頼境界)
- `docker/` — Docker Desktop の未使用資源 (停止コンテナ・未参照イメージ・ビルドキャッシュ・参照無しボリューム)。削除経路は無く、提示するコマンドを組むだけ (`docker system df` の申告をそのまま使う)
- `ssd/` — 内蔵 SSD の健康状態 (glogx の SSD タブ。issue 578)。diskutil と `smartctl -j` を読むだけで、修復・sudo・APFS の検査の経路は持たない。シリアル番号は読む構造体に欄を置かない
- `health/` — Mac 全体の健康診断 (`mac-healthcheck`。skill mac-healthcheck の本体)。観点ごとに 1 ファイル (`hw.go` / `proc.go` / `net.go` / `disk.go` / `apps.go` = tmux・nvim・Chrome・その他)、`ps.go` は 1 回の診断で共有する ps の 2 回読み、`report.go` が関門と Markdown。読み取りだけで、止める・消す経路は持たない (Fix は人へ見せる文字列)。判定不能を ok に丸めない不変条件は `TestFailingCommandsNeverYieldOK` が守る
- `uninstall/` — アプリと brew の formula の片付け (`mac-uninstall`。skill mac-uninstall の本体)。`inventory.go` (候補の一覧)・`leftovers.go` (残骸を bundle ID の完全一致と cask の zap で探し、段に分ける。🚨 動かしてよい置き場の一覧 cacheDirs / dataDirs もここ)・`plan.go` (計画と ID)・`apply.go` (**動かす経路を持つ唯一のファイル**。brew uninstall と safefs.TrashMove だけ)。apply は候補を作り直して計画との交わりだけを動かす
- `brewledger/` — Homebrew の formula/cask 台帳。`disk` と `svc` が同じ集合を引くための共有パッケージ
- `cachedir/` — キャッシュ置き場 (`~/.cache/glog`。ディレクトリ名は `glogx` ではなく `glog`) の解決
- `internal/displaycheck/` — 表示用構造体への文字列フィールド追加で無害化通し忘れを止める検査の本体 (`disk` / `svc` / `docker` / `ssd` の `display_coverage_test.go` が使う。production のコードからは呼ばない)
- `runner/` — 外部コマンド実行口 (stdout / stderr / exit code を分けて返す。テストではここを差し替える)。`LookPath` もここ
- `exitcode/` — 4 CLI 共通の終了コード語彙 (`NoFindings` / `Findings` / `Undiagnosed` / `EnvFailure`)。値を変えたら README と各 CLI の `--help` も同じ変更で直す
- `internal/homepath/` — ホームの下のパスを `~` で始まる形にする (レポート・表示にユーザー名を出さない。`health` と `uninstall` が共有)
- `testtmp/` — テストが `$TMPDIR` に作る一時ディレクトリを、中断 (SIGKILL 等) でも次回起動時に回収する共通ヘルパー
- `cmd/diskdoctor/`, `cmd/svcdoctor/`, `cmd/mac-healthcheck/`, `cmd/mac-uninstall/` — 各 CLI の `main.go` と、判定を純関数に切り出した `exit.go` (`diskExitCode` / `svcExitCode` / `healthExitCode`。テストは同ディレクトリの `exit_test.go`)

## 入口

- `bin/diskdoctor` / `bin/svcdoctor` / `bin/mac-healthcheck` / `bin/mac-uninstall` (`cmd/<名前>` の `main.go`)
- glogx の `D` 画面 (`src/glogx/internal/doctorview/`。地図は src/glogx/CLAUDE.md) は CLI を経由せず disk / svc / docker / ssd を直接呼ぶ (brewledger は svc と disk の中から使う)。削除は `doctor_delete.go` → `disk.Delete`

## ビルド・テスト

- `make -C src/doctor lint` / `test`。`test` は `test-derived-fields` に依存する: `disk/derived_fields_bypass_test.go` は module 外 (glogx のソースと workflow) を読むため通常の testcache が使われず、このターゲットが `-count=1` で強制実行して PASS 行を確認する
- CI は 3 レーン: `doctor.yml` (横断。テスト本数の下限 gate + fail-closed smoke)・`src_doctor.yml` (この module 単体)・`src_glogx.yml` (glogx 全体)。doctor 機能を触ったら `doctor.yml` を見る

## 詳しくは

- README.md 全体 (2 CLI の使い分け表・終了コードの語彙・glogx との関係・表示前の関門・テストと CI の 3 レーン)
