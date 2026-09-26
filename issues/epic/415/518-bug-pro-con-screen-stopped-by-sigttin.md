# 518 (bug): 持ち主の画面が「suspended (tty input)」で止まり、fg で戻しても描き直されない

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 起きたこと (2026-09-26)

- 21:05:43 に zsh (tmux `scratch:5.1`、PID 4155、`/dev/ttys061`) から `bin/pro-con` (持ち主の画面、PID 27868) を開いた
- 21:40 ごろまでの間のどこかで、zsh が `[1] + 27868 suspended (tty input)  bin/pro-con` を出した。画面は止まり (`ps` の STAT `T`)、
  端末の前面のプロセスグループは zsh (TPGID 4155) で、pro-con のグループ (PGID 27868) ではなかった
- 止まった後も端末は ctrl の組み合わせを区別して送る設定のままで、zsh に `;5;117~` (ctrl+u) `;5;104~` (ctrl+h) `;5;99~` (ctrl+c) が文字として届いた
- `fg` で `27868 continued` になったが、画面は描き直されず、キーも効かなかった (zsh が端末を cooked に戻した後で、pro-con は端末を入れ直していない)
- 戻し方: 別の pane で持ち主の画面 (PID 40953) を開いてから、zsh 4155 に SIGHUP (27868 も終わった)。dispatcher・supervisor・PG は止まらなかった
- 止まる直前に人が何をしていたかは未確認 (聞いたが答えは未着)

## 分かっていること

- SIGTTIN で止まるのは、**前面でないプロセスグループが端末を読んだ**とき。何かが端末の前面を pro-con のグループから外した
- pro-con 自身は端末の前面を切り替えない (`tcsetpgrp` / `Foreground` / `Ctty` は src/pro-con と tuikit・process_supervisor に無い)。
  子を起こすときは `Setpgid` で切り離している (`main.go` の supervisor・dispatcher、`dispatcher/runner.go`)
- 端末を外のコマンドに渡すのは `tea.ExecProcess` だけ (`ui/switchfade.go` の `execOnTerminal`: attach の `claude`・エディタ)。
  渡した子が自分で前面を取って (`tcsetpgrp`) 終わると、前面が死んだグループのまま残り、戻った pro-con が読むと SIGTTIN になりうる (仮説。未確認)
- 509 (21:14 以前に入った ctrl+r の演出) で、切り替えのときに alt screen を抜けずに exec する形とシグナルの見張り (`upgrade/screen.go` の `GuardAltScreen`) が入った。
  この画面は ctrl+r をしていない (「新版あり ctrl+r」が出たまま) ので、関係は薄い (未確認)

## 追加の依頼 (同日): 裏に回しても壊れないようにする

ユーザー: 「さっきのように ctrl+z みたいに background に移行させてしまうとスタックするというか崩壊することが起きないようにしたい」。
今は止める・戻すの扱いが何も無い (`ctrl+z`・`tea.Suspend`・`SIGTSTP`・`SIGCONT` は src/pro-con と tuikit に 0 件)。止まり方は 3 通りあり、どれでも壊れないようにする:

| 止まり方 | 今 | 期待 |
|---|---|---|
| 画面の中で `ctrl+z` (raw なので信号でなくキーで届く) | 何もしない | bubbletea の `tea.Suspend` で、端末を戻してから止まる。`fg` で `tea.ResumeMsg` を受けて入れ直して描き直す |
| 外からの SIGTSTP・端末が cooked の間の ctrl+z (509 の切り替えの途中・`ExecProcess` の前後) | 既定の動きで止まり、端末は raw・alt screen のまま | 受けたら端末を戻してから止まり、SIGCONT で入れ直す |
| 前面でないのに端末を読んだ・書いた (SIGTTIN / SIGTTOU。今回の形) | 止まり、`fg` でも戻らない | SIGCONT で入れ直して描き直す (前面を取り戻す) |

- 止まっている間も dispatcher・PG は別のプロセスで動く (画面が止まっても作業は止まらない) ことを、止まるときに 1 行出す
- 止まったままの持ち主の画面は flock を持ち続ける (数えられる) ので、最後の持ち主が止まっている間に閉じた扱いにはならない。これは今のままでよい

## 対応方針

1. 起きる経路を観測で特定する (`instrument-before-second-fix.md`): 止まる前に何をしたか (attach・エディタ・ctrl+z 等) を人に聞く / `tea.ExecProcess` の前後で
   端末の前面のグループ (`tcgetpgrp`) を記録して、戻ったときに自分のグループでなければ出来事に残す
2. 止まっても戻れるようにする: SIGCONT を受けたら端末を入れ直して描き直す (raw・alt screen・キーの設定・カーソル)。
   `ExecProcess` から戻ったとき、前面が自分でなければ取り戻す (`tcsetpgrp` を自分のグループへ。SIGTTOU を無視して呼ぶ)
3. 手順書に書く: 画面が止まったら「別の pane で持ち主の画面を開いてから、止まった画面を閉じる」「その pane で `reset`」(持ち主が 0 になると 1 分で PG が止まるため)。`pro-con help debug` (510) に載せる

## 受け入れ条件

- [x] 止まる経路が特定され、本文に書かれている (特定できないなら、観測を入れて「再発を待つ」と書く) → 下の「特定した経路」
- [ ] 上の表の 3 通り (画面の中の ctrl+z / 外からの SIGTSTP / SIGTTIN) のどれで止めても、止まっている間のシェルの端末が普通に使え、`fg` で画面が描き直されてキーが効く
      (隔離した tmux の `-L` サーバで、3 通りそれぞれ止めて `fg` して確かめる。本番の tmux サーバでやらない)
- [x] `ExecProcess` から戻ったときに前面が外れていたら取り戻し、出来事に残る
- [x] 戻し方が `pro-con help` にある

## 関連ファイル

- `src/pro-con/ui/switchfade.go` (`execOnTerminal`) / `src/pro-con/upgrade/screen.go` / `src/pro-con/main.go` (画面の起動・子の `Setpgid`)
- `src/pro-con/presence/presence.go` (持ち主の数)

## 特定した経路 (2026-09-26 21:39:29。観測で確定)

**テストの係の実行 (C-071 の `verify.sh` = dotfiles の `make test` 全体) の中の `zsh -i -c` が、端末の前面を自分のグループへ移したまま終わった。**
「関係は薄い」とした attach・エディタ (`ExecProcess`) ではなかった。

- supervisor → dispatcher → テストの係の実行は、`Setpgid` で別のプロセスグループにしていたが、**持ち主の画面と同じ session (= 同じ制御端末 ttys061) のまま**だった
- `tests/zshrc/av1ify/test_av1ify_clipboard.sh:212` の `zsh -i -c "…" < /dev/null` などの対話 zsh は、job control を入れるとき、
  自分がグループの先頭でなければ `setpgrp` して `tcsetpgrp` で前面を取る (SIGTTOU を塞いだ上で。zsh の `acquire_pgrp`)。終わると前面は死んだグループのまま
- 前面を外された画面が端末を読む → SIGTTIN で止まる → ログインシェル (zsh 4155) が止まったのを見て「suspended (tty input)」を出し、端末を取って cooked に戻した
- 証拠:
  - 実行のグループ (PGID 96616) が **21:39:29〜30 から丸ごと STAT `T`** のまま残っていた (`make test` の親まで。前面でないグループに SIGTTIN が配られた形)。
    `test_av1ify_clipboard.sh` (21:39:28 開始) の子が 21:39:29 に defunct。実行の出力は 21:39 で止まり、「終わった」の出来事が無い
  - 画面の最後の操作は 21:28:40 の回答 (C-070)。その後 attach・エディタの出来事は無い
  - 同じ形を隔離した tmux で再現した: 前面で端末を読むプロセスが `Setpgid` の子に `sh -c 'zsh -f -i -c true </dev/null'` を走らせると
    `zsh: suspended (tty input)`。子を `Setsid` にすると起きない。検査 `foreground_test.go` が擬似端末 (script(1)) の上で同じことを確かめる
- 巻き添え: 止まった実行はテストの係の順番を塞ぎ続ける (dispatcher は別 session の親なので、グループは孤児扱いにならず SIGCONT も来ない)。
  2026-09-26 22:00 時点で PGID 96616 が止まったまま残り、C-070 / C-072 / C-074 の実行が待っている。**人が `kill -CONT -96616` か `kill -TERM -96616` で外す**
  (この PG は止めようとしたが、他のカードの実行なので権限で止められた)

## 進捗

- 2026-09-26 (本 commit): 直した
  - 根本: 画面の外で走る処理を別の session で起こし、制御端末を持たせない (`foreground.Detached()` = `Setsid`)。
    supervisor (`spawnDetached` の子。dispatcher・見張り・テストの係はその下)・画面の quit の `dispatcher --stop`・テストの係の実行 (dispatcher が端末から起こされたときの備え)。
    グループの先頭は子自身のままなので、グループごと止める作法は変わらない。`Setsid` と `Setpgid` を一緒に立てると fork/exec が EPERM (実測)
  - 止まっても戻れる: SIGCONT を受けたら (main の `notifyContinued`) 何も走らせない `tea.Exec` で bubbletea の手放し → 入れ直しを通し、描き直す。
    外のコマンド (attach・エディタ) から戻ったとき、渡す前に前面を持っていて戻ったら外れていれば取り戻す (`foreground.Reclaim`)。
    `tcsetpgrp` は使い捨ての子 (自分のバイナリを環境変数つきで起こす) が SIGTTOU を無視してやる。画面のプロセスで無視すると
    `signal.Reset` で既定の動作に戻らず (Go の runtime が SIG_IGN を残す。敵対的レビューの指摘を隔離 tmux で実測)、以後は止まるべき所で止まらず、子へも引き継がれるため
    どちらも画面の出来事 (`pro-con log` の kind `screen`) に残す (`ui/terminal.go`)。SIGCONT の出来事は、外のコマンドの中で ctrl+z → fg したときにも出る (画面ごと止まるので区別できない。文言にそう書いた)
  - 手順書: `pro-con help debug` の「止まっている・動かない」に戻し方
  - 確かめたこと: 隔離した tmux で模擬の画面を `kill -TTIN` → `fg`。直す前は崩れたまま `?` が効かない / 直した後は描き直して `?` で表が開く (カード C-078 に添付)。
    新しい検査 3 か所 (foreground・テストの係・ui) は、直した箇所を戻す変異で落ちることを確かめた
  - 残り: 本物の持ち主の画面での再発の有無は観測待ち (出来事の `screen` に「前面を取り戻した」「SIGCONT」が出たら経路を調べる)
