# schedkeys

tmux 予約入力ウィザードの TUI (`prefix+m` / `Enter` / `C-m` の popup)。表示と入力だけを持ち、tmux・job ファイルには触れない。役割の境界・入力の規律・gum を使わない理由の正本は README.md。

## ファイルの地図

- `main.go` — エントリポイント。フラグ解析・`--out` への結果書き出し
- `model.go` — 全体の状態機械 (menu / form / pick の 3 画面)
- `form.go` — 新規予約フォームの状態 (何を・いつ送るか)。描画は持たない
- `form_view.go` — フォームの描画のみ (状態は変えない)
- `layout.go` — `frame`: 行を足す唯一の経路 (`add` / `addAt`)。幅で切る・高さに収める・カーソルを枠内へ入れるを一元化
- `editor.go` — 1 行テキスト編集状態 (書記素クラスタ単位のカーソル移動・削除)
- `jobs.go` — シェルが書いた予約一覧 TSV の読み込み
- `timespec.go` — 予約時刻の文字列解釈 (純関数。`time.Now()` を触らない)
- `style.go` — SGR 装飾の最小ヘルパー (lipgloss 不使用。ASCII 記号のみ)
- `toast.go` — 予約成功のトースト通知 (github.com/jiikko/dotfiles/src/tuikit/toast とは別実装。理由はファイル冒頭)
- `regression_test.go` / `render_test.go` — 過去に壊れた具体的な症状 (描画の幅・高さ・カーソル含む) の回帰テスト

## 入口

- `bin/schedkeys` (`main.go`。同期ビルド)。呼び出し元は `scripts/tmux_schedule_keys.sh`
- I/O 契約: `--label` / `--jobs <TSV>` / `--out <結果ファイル>` / `--toggle-prefix` / `--start`。結果は `new\t<epoch>\t<文字列>` / `cancel\t<id>` / `abort` の 1 行 (詳細は README.md)

## ビルド・テスト

- `make -C src/schedkeys lint` / `test`
- 実端末での見え方 (tty が要る部分) だけはテスト対象外 (human issue で確認)。描画の文字列はテストが見る

## 詳しくは

- README.md 全体 (役割の境界・gum ではなく bubbletea v2 を使う理由・入力の規律・画面組み立ての規律)
