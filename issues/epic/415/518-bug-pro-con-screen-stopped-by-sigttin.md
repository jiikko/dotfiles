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

- [ ] 止まる経路が特定され、本文に書かれている (特定できないなら、観測を入れて「再発を待つ」と書く)
- [ ] 上の表の 3 通り (画面の中の ctrl+z / 外からの SIGTSTP / SIGTTIN) のどれで止めても、止まっている間のシェルの端末が普通に使え、`fg` で画面が描き直されてキーが効く
      (隔離した tmux の `-L` サーバで、3 通りそれぞれ止めて `fg` して確かめる。本番の tmux サーバでやらない)
- [ ] `ExecProcess` から戻ったときに前面が外れていたら取り戻し、出来事に残る
- [ ] 戻し方が `pro-con help` にある

## 関連ファイル

- `src/pro-con/ui/switchfade.go` (`execOnTerminal`) / `src/pro-con/upgrade/screen.go` / `src/pro-con/main.go` (画面の起動・子の `Setpgid`)
- `src/pro-con/presence/presence.go` (持ち主の数)

## 進捗

(まだ無い)
