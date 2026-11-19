# 1098 (bug): scratch の popup が開いたまま見えなくなり、打ったキーが scratch のペインへ入る（表示が壊れ、window も切り替えられず、ビープが鳴り続ける）

起票日: 2026-11-19

## 概要

ユーザー報告 (2026-11-19 18:0x、air5): 「my-products の session で表示が壊れている。同じステータスラインが画面を埋め尽くしている」
「good-chrome-extensions の session は window を切り替えられないし、入力しても受け付けない」「ビープ音が鳴り続けている」「前にもこれが起きた」。

調べた結果、tmux のサーバ・セッション・ペインはどれも正常に動いていた。2 つの Terminal のウィンドウに **scratch の popup
(prefix + `t` / `C-t` → `scripts/tmux_scratch_popup.sh`) が開いたまま残っていて**、打ったキーは全部 popup の中の
入れ子の `tmux attach -t scratch` を通って scratch のペインへ入っていた。ユーザーには popup が見えておらず
（あるいは壊れた形でしか見えておらず）、固まったように見えた。

**分かったこと**: キーの行き先（下の観測 1〜3）。**分かっていないこと**: popup が開いたこと自体は誤操作なのか、
なぜ popup が見えない・壊れて見えるのか、ビープの出どころ。

## 観測（2026-11-19 18:09〜18:20、air5 / macOS 27.2.1 / tmux 3.7b / Terminal.app 2.15）

### 1. クライアントの最終アクティビティと、scratch の入れ子のクライアントの作られた時刻が秒まで一致する

`tmux list-clients -F '#{client_tty} #{client_session} #{t:client_created} #{t:client_activity}'`（18:16）:

| tty | session | 作られた時刻 | 最後にキーが届いた時刻 |
|---|---|---|---|
| ttys001 (Terminal の window id=89) | my-products | 11/12 11:16:01 | **18:02:23** |
| ttys000 (Terminal の window id=90) | good-chrome-extensions | 11/13 16:12:59 | **18:07:30** |
| ttys102 | scratch | **18:02:23** | 18:14:42 |
| ttys103 | scratch | **18:07:30** | 18:13:38 |
| ttys002 (Terminal の window id=91) | monolink-server | 18:08:00 | 正常に更新され続けた |

- ttys102 / ttys103 は Terminal のタブではない（login の親が無く、TERM が `tmux-256color`）。親はどちらも tmux サーバ (pid 2429) で、
  コマンドは `tmux_scratch_popup.sh` の popup の中身そのもの（`zsh -c unset TMUX TMUX_TMPDIR; tmux attach -t scratch …`）
- `client_discarded` はどれも 0。サーバ (pid 2429) の CPU は 3〜7%。watchdog (1087) の `server-busy` の記録も無し → **1087 (サーバが固まる) とは別の形**

### 2. 打ったキーが scratch のペインに残っていた

`scratch:3.1`（`~/src/working/antena` の zsh。最初に見た 18:09 は nvim、18:18 には zsh に変わっていた）:

```
~/src/working/antena [master]❯❯❯ qqqaaaaaaaaqq my-products:12.1 q
~/src/working/antena [master]❯❯❯ ぽーんq
~/src/working/antena [master]❯❯❯  っdっっq
```

`scratch:2.1` に `gstqqqqqq`、`scratch:1.1`（dotfiles の Claude Code）の入力欄に `q`。`a` を数回は、調査中にこちらが
「固まったタブで打ってみて」と頼んだもの。**閉じようとした・切り替えようとしたキーが scratch の作業に入った**
（nvim が終わったのがこのキーによるかは未確認。保存されていない変更が失われたかも未確認）。

### 3. tmux・Terminal のどちらも固まっていなかった

- 外側のクライアント (pid 93085 / 10031) の `sample` は `client_main → proc_loop → select` だけ（入力待ち）。端末は raw モード・`-ixon`
- ペインに `pane_input_off` / コピーモード / 同期入力は無し
- Terminal.app (pid 1723) のメインスレッドはイベント待ち。最初に見えた CPU 69.5% は一時的で、測り直すと 2〜4%
- my-products の各 Claude のペインには、ステータスラインの繰り返しは無かった (`capture-pane` で 2〜3 行)。つまり
  「同じステータスラインが画面を埋め尽くす」はペインの中身ではなく、**外側のクライアントへの描画（popup の部分を含む）**で起きている

### 4. ビープの設定

`bell-action any`・`visual-bell off`・`monitor-bell on`。scratch の zsh の行エディタが受け付けないキーで鳴らした BEL が
外側まで届いた、と見ているが**未確認**（BEL はペインの grid に残らないので、`capture-pane` では確かめられない）。

## 原因の仮説（どれも未検証）

1. **popup を開いたのは誤操作**。prefix が `C-t` で、`C-t C-t`（prefix の 2 度押し）も `C-t t` も scratch の popup を開く (`_tmux.conf` の
   `bind C-t` / `bind t`)。18:02:23 と 18:07:30 に、別々の Terminal のウィンドウで 1 回ずつ開いている
2. **popup の描き直しが外側に届かず、見えない・壊れて見える**。3.5a のときの popup の再描画のバグ (#4920) の対策 (閉じた後の refresh) は、
   「3.7 で直った」として commit「refactor(tmux): 3.5a popup 再描画 workaround … を撤去」で外した。今回は閉じる前（開いている間）の描画で、
   同じバグの残りか別のものかは分からない。567（popup の全体の描き直し）も関係しうる
3. 1 と 2 が重なると、「固まった」と思って打ったキー（`q`・Enter・window の移動）が全部、見えないまま scratch の作業に入る。
   **この形が一番害が大きい**（他の作業に気づかないまま入力が入る）

## 次に採るもの（また起きたとき）

- `tmux list-clients -F '#{client_tty} #{client_session} #{t:client_created} #{t:client_activity}'`。外側のクライアントの最終アクティビティと
  同じ時刻に作られた scratch のクライアントがあれば、この issue の形
- 🚨 **`tmux display -p -c /dev/ttysNNN '#{client_*}'` は指定したクライアントを見ない**（2026-11-19 の調査で、どの tty を指定しても
  自分のクライアントの値が返った）。クライアントごとの値は `list-clients -F` で取る
- そのときユーザーに見えているもの（ピンクと青の枠・「⚡ SCRATCH」のタイトルが見えるか）を聞く。見えていれば仮説 2 は外れ、
  「popup に気づかない」の問題になる
- 復旧: 固まったウィンドウで **`C-t t`**（popup の中で押すと popup を閉じる）。それで閉じなければ、新しい Terminal のウィンドウで
  `tmux attach -t <session>` してから、古いウィンドウを閉じる（閉じて止まるのは attach のクライアントだけ）

## 対応方針（候補。決めていない）

- 誤って開かない: `C-t C-t` に scratch を割り当てるのをやめる（`bind C-t` を外し、`C-t t` だけにする）。失うもの: prefix の 2 度押しで開く操作
- 開いていることに気づける: popup が開いている外側のクライアントのステータスに印を出す、または一定時間キーが来ない scratch の popup を閉じる
- 描画: 開くときの refresh を戻す・3.7c 以降で再現するかを隔離サーバで確かめる（仮説 2 を確かめてから）

## 関連

- 1087（scratch の popup を 2 つのクライアントで同時に開いていた時刻に、サーバが 20 秒固まった。今回はサーバは固まっていない）
- 567（scratch の popup の全体の描き直し）・637（開いたままの scratch の popup がサーバの CPU の最大）

## 進捗

- 2026-11-19: 起票（観測 1 回。観測の生データは air5 の `~/dotfiles/tmp/tmux-terminal-hang-20261119/`。gitignore なので、結論は本文に写した）
  - [ ] ユーザーに見えていたもの（popup の枠が見えたか）を確かめる
  - [ ] 次に起きたら「次に採るもの」で同じ形か確かめる
  - [ ] 対応方針を決める
