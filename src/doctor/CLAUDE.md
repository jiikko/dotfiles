# doctor

ディスク掃除候補 (`diskdoctor`) と壊れた launchd 登録 (`svcdoctor`) を診断するライブラリと CLI 2 本。CLI は dry-run のみ (削除・停止はしない)。使い分け・終了コードの語彙・設計の要点の正本は README.md。

## パッケージの地図

- `disk/` — 掃除候補 (`diskdoctor`)。`catalog.go` (allowlist) / `scan.go` (走査) / `report.go` (整形) / `guard.go` (起動中プロセス等を見て「今は消してはいけない」を弾く。fail-closed) / `delete.go` (**削除経路を持つ唯一のファイル**) / `display.go` (`SanitizeForDisplay` 関門)
- `svc/` — 壊れた常駐 (`svcdoctor`)。`plist.go` (plist 読み) / `launchctl.go` (状態取得) / `brew.go` (Homebrew 台帳との突き合わせ) / `report.go` (整形) / `display.go` (関門) / `restore.go`
- `docker/` — Docker Desktop の未使用資源 (停止コンテナ・未参照イメージ・ビルドキャッシュ・参照無しボリューム)。削除経路は無く、提示するコマンドを組むだけ (`docker system df` の申告をそのまま使う)
- `brewledger/` — Homebrew の formula/cask 台帳。`disk` と `svc` が同じ集合を引くための共有パッケージ
- `cachedir/` — キャッシュ置き場 (`~/.cache/glog`。ディレクトリ名は `glogx` ではなく `glog`) の解決
- `internal/displaycheck/` — 表示用構造体への文字列フィールド追加で無害化通し忘れを止める検査の本体 (`disk` / `svc` / `docker` が共有)
- `runner/` — 外部コマンド実行口 (stdout / stderr / exit code を分けて返す。テストではここを差し替える)
- `exitcode/` — 2 CLI 共通の終了コード語彙 (`NoFindings` / `Findings` / `Undiagnosed` / `EnvFailure`)。値を変えたら README と両 CLI の `--help` も同じ変更で直す
- `testtmp/` — テストが `$TMPDIR` に作る一時ディレクトリを、中断 (SIGKILL 等) でも次回起動時に回収する共通ヘルパー
- `cmd/diskdoctor/`, `cmd/svcdoctor/` — 各 CLI の `main.go` と、判定を純関数に切り出した `exit.go` (`diskExitCode` / `svcExitCode`。テストは同ディレクトリの `exit_test.go`)

## 入口

- `bin/diskdoctor` / `bin/svcdoctor` (`cmd/diskdoctor`, `cmd/svcdoctor` の `main.go`)
- glogx の `D` 画面 (`src/glogx/doctor_view.go`) は CLI を経由せず `doctor/disk` と `doctor/svc` を直接呼ぶ

## ビルド・テスト

- `make -C src/doctor lint` / `test`。`test` は `test-derived-fields` に依存する: `disk/derived_fields_bypass_test.go` は module 外 (glogx のソースと workflow) を読むため通常の testcache が効かず、このターゲットが `-count=1` で強制実行して PASS 行を確認する
- CI は 3 レーン: `doctor.yml` (横断。テスト本数の下限 gate + fail-closed smoke)・`src_doctor.yml` (この module 単体)・`src_glogx.yml` (glogx 全体)。doctor 機能を触ったら `doctor.yml` を見る

## 詳しくは

- README.md 全体 (2 CLI の使い分け表・終了コードの語彙・glogx との関係・表示前の関門・テストと CI の 3 レーン)
