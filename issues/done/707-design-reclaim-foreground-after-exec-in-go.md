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

## trap で守れていない形 (2026-10-11 の敵対レビューと codex の反証レビューで実測)

| 形 | 修正前 | 修正後 (leader の形) | 修正後 (job の形) | 備考 |
|---|---|---|---|---|
| `$SHELL` が fish・dash など | 未測定 | 未測定 | 未測定 | trap の構文・exec を省く振る舞いがシェルごとに違いうる |
| zsh で、コマンドの中に `trap - EXIT; …` / `trap '' EXIT; …` | 固まる | 前面を失ったまま戻る | 包みの stty が SIGTTOU で止まる | 包みの trap を外すので、最後のコマンドが exec される。bash 3.2 では戻った |
| コマンドの中の `kill -KILL $$` | 固まる | (未記録) | 包みの stty が SIGTTOU で止まる | シェルが自分の終了処理を通らずに死ぬ |
| ユーザーの rc の EXIT の trap・zsh の `TRAPEXIT` | 走った | 走らない | 走らない | 包みの trap で上書きする代償 (`zshexit` フックは走る) |

leader = glogx が tmux のペインで直に動く形 (session の先頭)、job = 外のシェルから起こした treefiler の形 (前景ジョブ)。
`src/treefiler/filer/shell_pty_test.go` の冒頭に同じ区別がある。

🚨 **Go 側で ExecProcess から戻った後に取り返すだけでは、上のどの形も直らない** (codex の反証レビューの P1)。`waitIfQuick` は
内側のシェルが終わった後、Go に戻る前にキー待ちの `stty`・`dd` を走らせるので、job の形ではそこで SIGTTOU により止まり、
Go まで戻ってこない。包みの側にも「前面でないなら端末に触らずに戻る」処理が要る。

- 包みで TTOU / TTIN を無視する試作 (レビュアーと codex の 2 者が試した) では戻ったが、前面は戻らず、キー待ちも消えた
  (dd が EIO で即座に抜ける)。前面の取り返しは Go に任せ、キー待ちは失う形になる
- 未測定の案: 包みの sh が `ps -o tpgid= -p $$` と `ps -o pgid= -p $$` を比べ、前面でなければ stty・dd を飛ばして戻る
  (perl も TTOU の無視も要らない)。Go が戻った後に取り返す。キー待ちは前面が奪われた場合だけ失う

## 対応方針 (案)

- `src/pro-con/foreground` (`Owner` / `Reclaim`。使い捨ての子に SIGTTOU を無視させて前面を移す。理由はパッケージのコメント) を、
  共有の module (`src/tuikit` が候補) へ持ち上げ、pro-con・treefiler・glogx から呼ぶ
  - `Reclaim` は自分のバイナリを環境変数つきで起こし、その子の `init` が前面を移す。持ち上げた先の package を import した
    バイナリ (treefiler・glogx) でも、この `init` が走ることを確かめる
- pro-con の `ui/terminal.go` の `termExec` (`tea.ExecCommand` を包み、`Run` の中で子が終わった後、bubbletea が端末を入れ直す前に
  取り返す) と同じ形を、treefiler の `main.go` (`tea.ExecProcess`) と glogx の `runEditorCmd` (`tui.go`。`F` の `!` `s` `e` と
  issues viewer の `e` などがここを通る) に入れる。あわせて、上の「包みの側で前面でないなら端末に触らずに戻る」を `waitIfQuick` に入れる
- 入れた後も `trap 'exit $?' EXIT` は外さない。実行中の Ctrl-Z で永久に固まる形 (止まったジョブを待つ者が居ない) は trap が
  直しているため
- 持ち上げる前に、pro-con で並行して進んでいる作業 (issue 415 の epic) と触るファイルが重ならないかを確かめる

## テストの組み方の注意 (codex の反証レビューの P2)

- 今の `shell_pty_test.go` の helper (`TestBangHelperOnPTY`) は、自前の `exec.Cmd.Run()` の直後に前面を見る。treefiler の `main.go` も
  glogx の `runEditorCmd` も通らないので、**アプリ側に取り返しを入れてもこのテストの結果は変わらない**。helper にだけ取り返しを
  足すと、アプリ側の呼び出しを外す変異を検出できない
- 取り返しを持つ実行の包み (`tea.ExecCommand` の実装) を 1 つにし、helper もアプリ (treefiler・glogx) もそれを使う形にする。
  加えて、両アプリがその包みを通っていることを別に固定する (配線の検査)
- pro-con の `ui/terminal_test.go` の取り返しのテストは `reclaim` を差し替えた stub で、実際の ioctl は `foreground_test.go` が見ている

## 受け入れ条件

- [x] treefiler・glogx の ExecProcess から戻った直後に、前面が自分のグループでなければ取り返す
- [x] `waitIfQuick` は、前面でないときに stty・dd で止まらずに戻る
- [x] pty のテストに、trap では戻らない形 (zsh の `trap - EXIT; /bin/echo x`) を leader・job の両方の形で足し、アプリと同じ実行の包みを
  通す。取り返しを外す変異・包みの「前面でないなら戻る」を外す変異・アプリ側の配線を外す変異で、それぞれ red になる
- [x] pro-con の既存のテスト (`foreground_test.go` と `ui/terminal_test.go` の取り返しのテスト) が、持ち上げた後も同じ実装を通る

## 関連ファイル

- `src/treefiler/filer/shell.go` (`waitIfQuick` と、そのコメントの「採らなかった案」)
- `src/treefiler/filer/shell_pty_test.go`
- `src/treefiler/main.go` / `src/glogx/tui.go` (`runEditorCmd`) / `src/glogx/filer_view.go`
- `src/pro-con/foreground/foreground.go` / `src/pro-con/ui/terminal.go` (`termExec`)
- issue 518 (pro-con の画面が SIGTTIN で止まった件。foreground パッケージの起点)

## 進捗

- 2026-10-11: 起票。trap による修正は push 済み (上の commit)
- 2026-10-11: codex の反証レビュー。採用: Go 側の取り返しだけでは job の形で包みの stty が先に止まる (P1。表と方針に反映) /
  今の pty テストはアプリの実行経路を通らない (P2。テストの組み方に反映)。反証できなかった主張: ファイル名・シンボル名・
  runEditorCmd を通る経路の範囲・Reclaim の自己バイナリ再起動と init・518 / 415 の参照
- 2026-10-11: 実装 (commit「feat(tuikit): 端末を子へ渡して戻った後に前面を取り戻す foreground.Exec を足し、treefiler・glogx・pro-con で使う」)
  - `src/pro-con/foreground` (ptytest を含む) を `src/tuikit/foreground` へ移し、`Exec` (bubbletea の ExecCommand。bubbletea は import しない) を足した。
    ゼロ値でも stdin で確かめて取り戻す。渡す前に前面を持っていたときだけ取り戻し、戻ったときに前面が session の先頭 (= 外のシェル) にあり
    自分がその先頭でないなら奪わない (`shellTookBack`)
  - treefiler `main.go` と glogx `runEditorCmd` を `tea.Exec(foreground.NewExec(cmd), …)` にし、取り戻せなかったら警告を出す。pro-con の
    `termExec` は `foreground.Exec` を埋め込む (handedOver だけを pro-con に残した)
  - `waitIfQuick`: キー待ちの前に `ps -o tpgid=,pgid= -p $$` で前面を確かめ、前面でなければ stty・dd を飛ばす。制御端末が無い (tpgid が 0 以下)・
    ps が何も出さないときは従来どおり待つ
  - 配線の検査: gorules (treefiler・glogx) の `execViaForeground` が `tea.ExecProcess` を止める
  - pty テストに足したケース: kill-self (sh・zsh × leader・job)、unwrapped-reclaim (同)、trap-removed (zsh × leader・job)、shell-took-back (job)。
    負の対照 unwrapped は素の Run で起こす。33 本 PASS (`-race`)
  - 変異 (bin/mutate-verify。どれも想定どおり red): 取り戻しを外す → kill-self・unwrapped-reclaim・trap-removed の 10 本 / `waitIfQuick` の前面の判定を
    `if true` → kill-self・trap-removed の 6 本 (job は stty が止まり hang guard) / treefiler・glogx の配線を `tea.ExecProcess` に戻す → lint が red /
    `shellTookBack` のガードを外す → shell-took-back / pro-con の handedOver を立てない → `TestTermExecHandsOverWhileChildRuns`
  - 敵対レビュー (観点を分けて直列に 3 本。①端末とシグナル ②素通り ③回帰 + 直した差分の 2 周目)。採用して直したもの:
    - ① P2: 子の実行中に Ctrl-Z → bg で外のシェルが取り戻した前面を、戻った画面が奪い返す (pty で実測。孤児のシェルの読みが EIO) → `shellTookBack`
    - ② P2: pro-con の handedOver をどのテストも見ていない (前からの穴) → テストを足した / P3: `foreground.Exec{Cmd: c}` と直に組むと取り戻しが
      黙って消える → ゼロ値でも stdin で確かめる既定にした / P3: lint は別名 import・関数値を見逃す → 規則のヘッダの「検出しない形」に書いた
    - ③ P2: init の合図の環境変数を改名すると、ライブアップグレードの窓の間に旧版が起こした取り戻しの子が新版の画面として起動する (推論) →
      改名をやめて旧名 `PRO_CON_FOREGROUND_RECLAIM` のまま (理由をコメントに書いた。旧名で init が受けることを実バイナリで確認) /
      P3: handedOver のテストを取り戻しの時点でも見る形に強めた
    - 3 周目の修正は判定のロジックを足さず (定数・文書・assert)、各修正を実測で確かめたので周回を打ち切った (§7 の例外)
  - 古いコメント (glogx `open_workspace.go`・`external_commands.go`、tuikit `editor/editor.go`、treefiler `filer/shell.go`) の `tea.ExecProcess` を直した。
    tuikit の README・CLAUDE.md、pro-con の CLAUDE.md、`src/PACKAGES.md` を更新した

## 未対応・未確認リスク (2026-10-11 の敵対レビューで出て、直していないもの)

- `trap - EXIT` の形 (zsh が最後のコマンドを exec する) で実行中に Ctrl-Z すると、止まったジョブを待つ者が居ず、包みの sh が待ち続ける
  (Run が戻らないので Go の取り戻しに届かない)。今回の変更の前から同じ。推測 (未実測)
- `Reclaim` の子に時間の上限が無い。`os.Executable()` の先が foreground を import しない版に入れ替わると、子が TUI として起動して SIGTTIN で
  止まり、画面が固まる。推測。trigger: 取り戻しの直後に画面が固まった報告が出たら、子に上限を付ける
- stdin が端末でない (bubbletea が /dev/tty を開く) とき、取り戻さない。従来の `tea.ExecProcess` と同じ
- 外のシェルが session の先頭でない (zsh の中で起こした bash から treefiler を起こした等) とき、bg の後にその入れ子のシェルが取り戻した前面は
  見分けられず、奪い返す。従来の pro-con と同じ
- `waitIfQuick` の「制御端末が無いときは待つ」の分岐を守るのは `TestWaitIfQuickRunsCommand` だけで、端末の前面で `make test` を回すと
  同じ変異でも緑で通る (CI では守られる)
- `ptytest.Run` は script・zsh が無い / 前面に居られないときに無言で Skip する (移設しただけで、前から同じ)
- fish・dash は未測定
