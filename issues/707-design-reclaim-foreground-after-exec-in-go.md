# 707 (design): treefiler / glogx の `!` `s` `e` から戻った後に、Go 側で端末の前面を取り返す

起票日: 2026-10-11

## 概要

2026-10-11 に、treefiler (glogx の `F`) の `!` で `ls` を走らせた後、「[done] 何かキーを押すと…」のまま固まった。
原因は、`!` の包み (`src/treefiler/filer/shell.go` の `waitIfQuick`) の `$SHELL -ic` が、自分のグループで端末の前面を取ったまま
最後のコマンドを exec し、前面を戻す者が居なくなったこと。commit「fix(treefiler): ! の後に端末の前面が戻らず treefiler / glogx の F が
固まるのを直す」で、`-c` の先頭に `trap 'exit $?' EXIT` を置いてシェルに exec させない形で直した。

この直し方は **「シェルが EXIT の trap を持つときは最後のコマンドを exec せず、自分が抜けるときに前面を戻す」というシェルの振る舞いに
頼っている**。zsh 5.9 と bash 3.2 でしか実測していない。シェルによらず「`!` `s` `e` から戻ったら前面は treefiler / glogx に在る」を
保証するには、起こした側 (Go) で戻った直後に前面を取り返す層が要る。

## trap で守れていない形 (2026-10-11 の敵対レビューで実測)

| 形 | 修正前 | 修正後 | 備考 |
|---|---|---|---|
| `$SHELL` が fish・dash など | 未測定 | 未測定 | trap の構文・exec を省く振る舞いがシェルごとに違いうる |
| zsh で、コマンドの中に `trap - EXIT; …` / `trap '' EXIT; …` | 固まる | 固まる | 包みの trap を外すので、最後のコマンドが exec される。bash 3.2 では戻った |
| コマンドの中の `kill -KILL $$` | 固まる | 固まる | シェルが自分の終了処理を通らずに死ぬ |
| ユーザーの rc の EXIT の trap・zsh の `TRAPEXIT` | 走った | 走らない | 包みの trap で上書きする代償 (`zshexit` フックは走る) |

`kill -KILL $$` は、Go 側で取り返すだけでは直らない (レビュアーの指摘・未検証)。包みの sh 自身がキー待ちの `stty` で SIGTTOU により
止まり、ExecProcess から戻ってこないため。シェルによらず保証するなら「包みの sh で TTOU / TTIN を無視する + Go で取り返す」の
組が要る。ただし包みで無視すると、前面でないまま `stty`・`dd` が通る (dd は EIO で即座に抜け、キー待ちが消える) ので、その扱いも決める。

## 対応方針 (案)

- `src/pro-con/foreground` (`Owner` / `Reclaim`。使い捨ての子に SIGTTOU を無視させて前面を移す。理由はパッケージのコメント) を、
  共有の module (`src/tuikit` が候補) へ持ち上げ、pro-con・treefiler・glogx から呼ぶ
  - `Reclaim` は自分のバイナリを環境変数つきで起こし、その子の `init` が前面を移す。持ち上げた先の package を import した
    バイナリ (treefiler・glogx) でも、この `init` が走ることを確かめる
- pro-con の `ui/terminal.go` の `termExec` (`tea.ExecCommand` を包み、`Run` の中で子が終わった後、bubbletea が端末を入れ直す前に
  取り返す) と同じ形を、treefiler の `main.go` (`tea.ExecProcess`) と glogx の `runEditorCmd` (`tui.go`。`F` の `!` `s` `e` と
  issues viewer の `e` などがここを通る) に入れる
- 入れた後も `trap 'exit $?' EXIT` は外さない。実行中の Ctrl-Z で永久に固まる形 (止まったジョブを待つ者が居ない) は trap が
  直しているため
- 持ち上げる前に、pro-con で並行して進んでいる作業 (issue 415 の epic) と触るファイルが重ならないかを確かめる

## 受け入れ条件

- [ ] treefiler・glogx の ExecProcess から戻った直後に、前面が自分のグループでなければ取り返す
- [ ] `src/treefiler/filer/shell_pty_test.go` に、trap では戻らない形 (zsh の `trap - EXIT; /bin/echo x`) を足し、取り返しを外す変異で red になる
- [ ] pro-con の既存のテスト (`foreground_test.go` と `ui/terminal_test.go` の取り返しのテスト) が、持ち上げた後も同じ実装を通る

## 関連ファイル

- `src/treefiler/filer/shell.go` (`waitIfQuick` と、そのコメントの「採らなかった案」)
- `src/treefiler/filer/shell_pty_test.go`
- `src/treefiler/main.go` / `src/glogx/tui.go` (`runEditorCmd`) / `src/glogx/filer_view.go`
- `src/pro-con/foreground/foreground.go` / `src/pro-con/ui/terminal.go` (`termExec`)
- issue 518 (pro-con の画面が SIGTTIN で止まった件。foreground パッケージの起点)

## 進捗

- 2026-10-11: 起票。trap による修正は push 済み (上の commit)
