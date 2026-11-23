# 1106 (feat): treefiler を q で閉じたら、呼び出したシェルがそのフォルダへ cd する

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-23〜）

起票日: 2026-11-23

## 概要

`tf` (ペインで直接) と tmux の `C-t f` (popup) の両方で、treefiler を `q` で閉じたら呼び出し元の zsh がカーソルのフォルダへ cd する。

## 決定 (ユーザー 2026-11-23)

- 「filetree を呼び出した shell で cd したい。q で閉じたら cd している感じ」「C-t f も同じでよくない？」
- cd するのは `q` だけ。`esc`・`Ctrl-C`・popup の prefix f は cd せずに抜ける道として残す (提案の推奨をそのまま採用)
- 行き先はカーソルのフォルダ (ファイルの上ならその親。`s` と同じ `WorkDir`)
- docs/treefiler-spec.md §0.1 の「`q` の cd 連携: 写さない」(2026-10-07。glogx の F を前提に決めた) を、単体の treefiler では写す形に改めた。glogx の F は変えない

## 仕組み

- treefiler: `--cwd-file PATH` を受け、木の上の `q` で終えたときだけ行き先を改行なしで atomicfile で書く (`src/treefiler/main.go` の `run`)。
  フラグのあるときはキー案内の q を `cd`、`?` の一覧の終了の行を q と esc の 2 行にする (`filer.Options.CdOnQuit`)
- `tf` (zsh の関数。`zshlib/_treefiler_cd.zsh`): 一時ファイルを渡して起動し、書かれていれば cd する。行き先が消えていたら知らせて rc=1
- `C-t f`: bind が `@treefiler_pane` に開いたペインを控え (popup のコマンドも `-e` も format を展開しない)、popup の中の
  `scripts/tmux_treefiler_popup.sh` が行き先をペインの option `@treefiler_cd` に置いて widget のキー列 (`ESC[9119~`) だけを送る。
  ペインの zsh の widget (`_treefiler_cd_widget`。emacs・viins・vicmd に bind) が読んで cd し、precmd を回してプロンプトを描き直す。
  `cd ... Enter` を送らないのは打ちかけの行を壊すため
- 送るのは次を全部満たすときだけ (満たさなければ display-message で理由を出す): ペインがコピーモードでない・window が同期中でない・
  受け口を読んだ zsh の pid の一覧 (`@treefiler_cd_ready`。zsh が起動時に足し、抜けた pid は次に足すときに外す) に端末の前面のプロセスグループがある・
  前面のプロセスが zsh・前面のグループに zsh のほかのプロセスがいない (widget のコマンド置換で動く `^R` の fzf など)

## 受け入れ条件

- [x] `tf` で q → cd / esc → cd しない / 行き先が消えていたら知らせて rc=1 / 一時ファイルを残さない / 改行で終わる名前も (tests/zshrc/test_treefiler_cd.sh)
- [x] 本物の bubbletea で q → `--cwd-file` に改行なしで書く / Ctrl-C → 書かない (src/treefiler/cdquit_test.go の TestRunWritesCwdFileOnQ)
- [x] `C-t f` で q → 開いたペインの zsh が cd し、打ちかけの行と `%?` が残る (tests/tmux/test_treefiler_popup_cd.sh。隔離サーバ・本物の zsh)
- [x] 前面が zsh でない・zsh のスクリプトが入力を待つ・widget のコマンド置換が端末を読む・コピーモード・同期中・受け口の無い zsh・pid の再利用・@treefiler_pane が空 には送らない (同上)
- [x] glogx の F と、フラグ無しの treefiler の挙動は変わらない (`CdOnQuit` は glogx から渡さない。既存の treefiler / glogx のテストが緑)
- [ ] 実機の popup で 1 回通す (send-keys は key table を通らず、display-popup は attach したクライアントが要るので、自動テストは bind の登録とスクリプトに分けている)

## 進捗

- 2026-11-23 「treefiler を q で閉じたら呼び出したシェルが cd する」: 上の仕組みと、tests/zshrc/test_treefiler_cd.sh・tests/tmux/test_treefiler_popup_cd.sh・src/treefiler/cdquit_test.go。
  実装をわざと壊してテストが落ちることを確かめたもの (計 21 本): esc でも cd / 案内が quit のまま / 書き出しを消す / 改行を付ける / @treefiler_pane を読まない / 受け口の確認を外す /
  cd と Enter を送る / 同期・コピーモード・前面の pgid・前面が zsh か・前面のグループの人数の確認を外す / 一覧を上書きにする / precmd を回さない /
  emacs・viins・vicmd の bind を消す / tf が cd の失敗を返さない / read を `$(<f)` に戻す / 一時ファイル・popup の一時 dir を消さない

## 反証レビュー (opus の red team を 4 体。観点: 壊す・回帰 / 素通り / 直した差分 2 周)

採ったもの: synchronize-panes・コピーモード・zsh のスクリプトの入力待ちへの誤送信、widget のコマンド置換 (fzf) への誤送信 (隔離サーバで実測: `$(cat </dev/tty)` の cat は zsh と同じ PGID・TPGID)、
`$PWD` のパターン照合、vicmd の bind 漏れ (実測: 打ちかけの行の大文字と小文字が入れ替わった)、入れ子の zsh を抜けた後に届かない形、main の書き出しと @treefiler_pane の経路を通るテストが無い、
テストの keymap が EDITOR 次第、待ちが前の状態で成り立つ形、`?` の一覧で cd 用の説明が切れる、tf の rc。

却下・記録:
- 「widget の中で precmd を回すと `%?` が 0 に変わる」: 再現しない。zle は widget の間も直前のコマンドの終了コードを保つ (precmd を 0 で返すテストで `%?` が 1 のまま。足した守りを外してもテストが落ちなかったので、守りは外した)。テストは `%?` の照合を残す
- 「`?` の一覧の cd の行にタイルの注記が無い」: 「タイルの中」の節が `q esc: 1 枚閉じる` を書いている。注記を足すと 100 桁の端末で説明が切れる (help.go にコメント)
- zsh-autosuggestions・syntax-highlighting の下の挙動: テストの zshrc は読まない。前面のグループの人数の判定が普段の利用で送らない側に倒れないかは、本番の待機中の zsh のペイン 8 つ (と レビュワーの別の観測で 13 端末) で、前面のグループが zsh 1 人だったことで確かめた。autosuggestions の裏の取得は MONITOR の下で別のグループを作る (プラグインの `_zsh_autosuggest_async_request`)

未確認のリスク (観測ポイント):
- 見分けられない形: zsh の組み込み (`read`・`vared`・`select`) が入力を待っている間、受け口を読んだ zsh から `exec zsh -f` した後、抜けた zsh の pid が後で zsh のスクリプトに再利用された形 (scripts/tmux_treefiler_popup.sh 冒頭)
- `tmux show -pv` がフォルダ名の制御文字 (改行・タブ) を書き換えるか (推測。未実測)。書き換えると popup の経路だけ「cd 先がありません」になる
- 反映の直後: bin/treefiler は新しいバイナリを裏でビルドし、その回は古いバイナリを起動する (go_autobuild の --async)。古いバイナリは `--cwd-file` を知らず rc=2 で終わるので、pull の後に一度ビルドを済ませる

## rule-harvest (2026-11-23)

候補「ペインのシェルへキーを送って状態を変えるときは、widget のキー列だけを、受け口の zsh が前面に 1 人でいるときだけ送る」(`.claude/rules/` に paths 付き) は却下。
観点 1〜4 の反証レビュー (事実・重複衝突・機械化・恒久性と置き場所) で、次の理由が残った:
- 再発の母集合が 1 件: 今ペインのシェルの状態を send-keys で変えるのは scripts/tmux_treefiler_popup.sh だけ。ほかの send-keys は利用者の文字を送るもの (予約入力・extract・C-v) か隔離した session への E2E・計測
- 「自動で送る send-keys 全般」へ広げると予約入力の設計 (Claude のプロンプトなど zsh 以外が前面のペインへ送る) と食い違う
- 規範の中身は scripts/tmux_treefiler_popup.sh の冒頭のコメントとこの issue にあり、そこを触る人には届く。機械の検査も、対象外の逃がしが既存の該当の大半を占めて見合わない
同じ判定の弱い既存の規範 (docs/tmux-window-fade.md の「一括移行は pane_current_command == zsh に限って send-keys」) は直した。

## 残り

- [ ] 実機の popup の確認 (上の受け入れ条件)。prefix f → 別のフォルダへ移って q → ペインのプロンプトがそのフォルダになる / 打ちかけの行が残る / esc では変わらない
