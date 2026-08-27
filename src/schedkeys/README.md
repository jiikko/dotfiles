# schedkeys — tmux 予約入力ウィザードの TUI

`prefix + m` / `Enter` / `C-m` の popup で「いつ・何を、この pane へ打ち込むか」を決める対話 UI。
bubbletea v2。呼び出し元は `scripts/tmux_schedule_keys.sh`、起動は `bin/schedkeys`（同期ビルド）。

## 役割の境界

このバイナリは **表示と入力だけ**を持つ。tmux にも job ファイルにも触れない。

```
schedkeys --label "main:3 claude" --jobs <一覧 TSV> --out <結果ファイル>
```

`--out` に 1 行書いて exit 0:

| 行 | 意味 |
| --- | --- |
| `new<TAB><発火 epoch><TAB><送る文字列>` | 新規予約 |
| `cancel<TAB><予約 id>` | 一覧から取消を選んだ |

中止 (Esc / Ctrl-C) は exit 1（`--out` は使わない）。

予約の作成・sleeper の起動・**取消の確認 (`gum confirm --default=false`) と実行**は呼び出し元の
シェルが行う。破壊的な操作をシェル側のテスト済み経路に残すための分担で、UI を差し替えても
その安全機構は動かない。

## なぜ gum ではないのか

gum (bubbletea v1) は本物のカーソルを隠して偽カーソルを描くため、**IME の未確定文字が入力位置に
出ない**（pty で実測 2026-08-27: 既定ではヘルプ行 = 入力行の 2 行下、`--no-show-help` でも入力欄の
右端）。bubbletea v2 は `tea.View.Cursor` で本物のカーソル位置を渡せるので、日本語入力が入力欄に出る。
`model_test.go` の `TestViewPlacesRealCursorAtFocusedField` がこの配線を守っている。

副産物として「いつ送る」と「文字列」を 1 画面に置け、発火時刻をその場で見せられる。

## 表示の規律

**絵文字と曖昧幅の記号を表示文字列に使わない**（端末と描画側の幅計算が食い違い、行ごとに左右へ
ずれてノイズになる）。`tests/tmux/test_schedule_keys.sh` がシェルと Go の引用文字列を静的に検査する。
桁揃えは `ansi.StringWidth`（東アジア文字 = 2 セル）で行う。byte 数で詰めると日本語で崩れる。

## テスト

`make -C src/schedkeys test`（root の `make test` からも走る）。時刻の解釈・入力欄の編集・
カーソル位置・結果行の整形は全てここ。tty が要る描画そのものは対象外で、実機確認は human issue。
