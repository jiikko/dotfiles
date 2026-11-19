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
- Terminal の window id と tty の対応は、18:15 に打った osascript (`tell application "Terminal" to … tty of tabs of windows`) の出力による（生データの dir に保存した方の osascript は `-1700` で失敗していて、残っていない）
- `client_discarded` はどれも 0。サーバ (pid 2429) の CPU は 7.0 / 2.9 / 2.6%（18:17、`ps -o %cpu=,time=,rss= -p 2429` を 2 秒おき）。
  watchdog (1087) の `server-busy` の記録も無し（`~/.cache/tt-restore-trigger.log` の 11/19 の最終行は 17:29 の periodic-save）
- **1087 との関係**: 1087 も「scratch の popup が 2 つのクライアントで開いていた」ときで、前提は同じ。違うのは、今回はサーバが忙しくなかったこと。
  1087 の「固まった」も、popup が開いたままでキーが外側のペインに届かなかった体感を含んでいた可能性がある（1087 の観測ではサーバが CPU 99.7% だったので、全部がそれではない）
- 「秒まで一致」は、popup を開いたキーが外側のクライアントの最後のキーだった、というだけのこと。後続のキーが popup へ行ったことの根拠は、
  外側の最終アクティビティが進まなかったことと、下の観測 2 のペインの文字の方。popup が開いている間のキーで外側の `client_activity` が更新されないことは、
  tmux の実装では確かめていない（未確認）

### 2. 打ったキーが scratch のペインに残っていた

`scratch:3.1`（`~/src/working/antena` の zsh。18:09 の `tmux list-panes -a` では `pane_current_command` が nvim、18:18 には zsh に変わっていた。
18:09 の値は会話の中で読んだもので、生データの dir には無い）:

```
~/src/working/antena [master]❯❯❯ qqqaaaaaaaaqq my-products:12.1 q
~/src/working/antena [master]❯❯❯ ぽーんq
~/src/working/antena [master]❯❯❯  っdっっq
```

`scratch:2.1` に `gstqqqqqq`、`scratch:1.1`（dotfiles の Claude Code）の入力欄に `q`（1.1 は 18:09 の `capture-pane` で見たもの。生データの dir には無い）。`a` を数回は、調査中にこちらが
「固まったタブで打ってみて」と頼んだもの。**閉じようとした・切り替えようとしたキーが scratch の作業に入った**
（nvim が終わったのがこのキーによるかは未確認。保存されていない変更が失われたかも未確認）。

### 3. tmux・Terminal のどちらも固まっていなかった

- 外側のクライアント (pid 93085 / 10031) の `sample` は `client_main → proc_loop → select` だけ（入力待ち）。端末は raw モード・`-ixon`
- ペインに `pane_input_off` / コピーモード / 同期入力は無し
- Terminal.app (pid 1723) の `sample` (18:17、3 秒) はメインスレッドの大半が `mach_msg` の待ち（イベント待ち）。CPU は 18:11 に 69.5% → 1 秒おきに 39.9 / 2.5 / 3.6%、
  18:17 に 9.2%（`ps.txt`）
- my-products の各 Claude のペインには、ステータスラインの繰り返しは無かった (`capture-pane` で 2〜3 行)。
  「同じステータスラインが画面を埋め尽くす」はペインの中身ではない。外側のクライアントへの描画（popup の部分を含む）か、Terminal 側の描画のどちらかで、
  外側の画面を実測していないので区別できていない（18:12 に `tmux refresh-client -t /dev/ttys001` を送ったが、その後の見え方は聞いていない）

### 4. ビープの設定

`bell-action any`・`visual-bell off`・`monitor-bell on`。scratch の zsh の行エディタが受け付けないキーで鳴らした BEL が
外側まで届いた、と見ているが**未確認**（BEL はペインの grid に残らないので、`capture-pane` では確かめられない）。

### 5. 復旧と、その間の新しいタブ

- ユーザーが新しい Terminal のタブで `tmux attach -t my-products` し、操作が戻った（新しいクライアント ttys101、18:18:42）。
  18:20 には古いクライアント ttys001 も、その上の scratch の popup の ttys102 も消えていた（古いウィンドウを閉じたと見ているが、閉じたかは聞いていない）
- 復旧の前、ユーザー報告「新しいタブを開くと zsh が起動せず、何も表示されない」。18:17:19 に開いたタブ (ttys104) では、`login` と `-zsh` がその時刻に起動していた
  （18:20 の `ps -t ttys104`）。zsh は起動していて、何も表示されなかった理由は分かっていない。少し後に開き直すと表示された
- 18:20 の時点で、good-chrome-extensions のウィンドウ (ttys000) には scratch の popup (ttys103) がまだ開いていた

## 原因の仮説（どれも未検証）

1. **popup を開いたのは誤操作**。prefix が `C-t` で、`C-t C-t`（prefix の 2 度押し）も `C-t t` も scratch の popup を開く (`_tmux.conf` の
   `bind C-t` / `bind t`)。18:02:23 と 18:07:30 に、別々の Terminal のウィンドウで 1 回ずつ開いている
2. **popup の描き直しが外側に届かず、見えない・壊れて見える**。3.5a のときの popup の再描画のバグ (#4920) の対策 (閉じた後の refresh) は、
   「3.7 で直った」として commit「refactor(tmux): 3.5a popup 再描画 workaround … を撤去」で外した。今回は閉じる前（開いている間）の描画で、
   同じバグの残りか別のものかは分からない。567（popup の全体の描き直し）も関係しうる
3. 1 と 2 が重なると、「固まった」と思って打ったキー（`q`・Enter・window の移動）が全部、見えないまま scratch の作業に入る。
   **この形が一番害が大きい**（他の作業に気づかないまま入力が入る）

## 次に採るもの（また起きたとき）

- **開閉のログを読む**（2026-11-20 から自動で残る）: `grep scratch-popup ~/.cache/tt-restore-trigger.log`。
  start の `key=` で、誤操作の仮説 1（`C-t C-t` で開いた）を確かめる。start の `tty=` と `list-clients` の scratch のクライアントの `client_tty` を突き合わせる。
  `size=` `term=` は外側のクライアントの大きさと端末で、仮説 2（描画）を疑うときに使う。end が `signal=` なら Terminal の窓を閉じて終わり、`rc=` なら中で detach して終わった。
  end の無い start が、開いたままの popup。行の書き方は docs/tmux-plugins.md の「観測ログの読み方」
- `tmux list-clients -F '#{client_tty} #{client_session} #{t:client_created} #{t:client_activity}'`。外側のクライアントの最終アクティビティと
  同じ時刻に作られた scratch のクライアントがあれば、この issue の形
- 🚨 **`tmux display -p -c /dev/ttysNNN '#{client_*}'` では、ほかのクライアントの値は取れない**。`display-message` の `-c` はメッセージを表示する
  クライアントの指定で、format の値は target-pane（打ったクライアント）から取る（man tmux の display-message）。2026-11-19 の調査でこれを取り違え、
  どの tty を指定しても自分のクライアントの値を読んでいた。クライアントごとの値は `list-clients -F` で取る
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
- 2026-11-19: 反証レビュー（sonnet 1 体、読み取りのみ）。採ったもの: 1087 と「別の形」を「前提は同じで、サーバが忙しくなかった点が違う」に弱めた /
  生データの dir に無い観測（window id と tty の対応・18:09 の nvim と scratch:1.1 の `q`・Terminal の CPU の測り直し）に出典を書いた /
  「秒まで一致」が示す範囲と、`client_activity` の更新の規則が未確認なことを書いた / ステータスラインの繰り返しを、外側の描画か Terminal 側か区別できていないと書いた /
  `display -c` を「仕様の取り違え」に直した。
  却下したもの: 「server-cpu.txt の 42960 はサーバと別の pid」— 42960 は `ps -o %cpu=,time=,rss= -p 2429` の RSS (KB) の列で、pid ではない
- 2026-11-19: できるかの見立て（別のセッション。手は入れていない）。手元の tmux は 3.7c で、観測時の 3.7b と違う（3.7c で起きるかは未確認）。
  仮説 2（描画）は、ユーザーに見えていたものが無いので今は直せない（隔離サーバでの再現も、見え方は人の目でしか判定できない）。
  原因によらず害を止めるものとして、①`_tmux.conf` の `bind C-t` を外す（`unbind C-t` も conf に残す。失うもの: prefix の 2 度押しで開く操作）
  ②popup を開いている外側のクライアントのステータスに印を出す（`scripts/tmux_scratch_popup.sh` で立てて下ろす。異常終了で印が残る経路の検査が要る）を挙げ、①を先に推した。どれを採るかはユーザーの返事待ち
- 2026-11-20: ユーザー指示「3 をやりますか。体験は変わらず、情報収集して根本解決できると嬉しい」で、開閉を観測ログに残すようにした
  （commit「feat(tmux): scratch の popup の開閉を観測ログに残す (1098)」）。
  - 書く行: `scratch-popup-start`（外側のクライアント・セッション・大きさ・端末・中の pty・押したキー）/ `scratch-popup-end`（`rc=` か `signal=HUP|TERM`）/
    `scratch-popup-close-key`（popup の中の `C-t t`）。書き手は `scripts/tmux_scratch_popup_log.sh`。押したキーは `_tmux.conf` の bind が第 3 引数で渡す
  - 使い心地を変えないために: 書き手はどれも背景で起こし、開く・閉じるを待たせない（待つと閉じるのが約 40ms 遅れ、その間のキーが消える）。
    セッション名は display-popup の argv に載せない（`;` で終わる引数を tmux がコマンドの区切りと読む。隔離サーバで `set -g @x 'foo;'` が `foo` になるのを確認）。
    popup のコマンドは以前と同じく default-shell を通す
  - 隔離サーバでの実測: 外側のクライアントが消えると、tmux は popup の sh に TERM を送り、pty が閉じて HUP も来る。TERM の trap が無いと sh は HUP の trap より先に死んで end が残らない。
    背景の書き手は、HUP / TERM を無視させないと popup の片付けで書く前に消える。無視を子の中で入れると、sh（pty のセッションリーダー）の終了の HUP に先を越されることがある
    （`script` の pty で attach がすぐ返る形を回して、修正前 end の欠けが 1/100・1/300、fork の前に親で無視を入れる形で 0/100・0/300）
  - 本番のサーバでないと書かない（-L の隔離サーバも同じ bind を持つ）。`$TMUX` の socket の字面を先に比べる（`tt_on_default_server` はサーバに届かないと本番と見なすので、
    隔離サーバを popup ごと kill した後の end が通っていた）。期待するパスは `scripts/lib/tmux_resurrect_guards.sh` の `tt_default_socket_path` に寄せた
  - テスト（`tests/tmux/test_scratch_popup.sh`）: 変異で red を確認したもの — 書き手の無視を外す / TERM の trap を外す / HUP の trap を外す / 書き手の既定のパスの書き損じ /
    セッション名を argv に戻す / 外側の TMUX を渡さない / キー・自分のパスを渡さない / close-key を書かない / 本番の判定を外す / socket の字面の判定を外す / client-gone を書かない /
    close-key で list-clients を引く / tty の `/dev/` を落とす / 書き手を待つ（開く側・閉じる側）
  - 敵対的レビュー 3 周（opus）。3 周目は P1・P2 なし。
    受け入れたもの: 固まったサーバで書き手が返らずに残る（起動から問い合わせまでの約 40ms に固まりが始まったときだけ。理由はコードに）/
    手で sh だけに TERM を送ると attach が終わるまで popup が残る / 開いて約 40ms 以内に閉じると start が end より後に並ぶ・start の外側が client-gone になりうる（docs に書いた）/
    無視を子の中で入れる形に戻す変異はテストで決定論に再現できない（根拠は上の `script` の実測だけ）
  - 仮説 2（描画）の切り分け（隔離サーバで、tmux が popup の枠を外側へ送っているかをバイト列で見る）は未着手
  - [ ] 次に起きたら、上の「開閉のログを読む」で仮説 1 と見え方を確かめる
  - 本番への反映（2026-11-20）: `~/dotfiles` へ pull し、本番の tmux に `source-file ~/.tmux.conf` で conf を読み直させた（起動から 39 日で、continuum の発火窓の外）。
    `list-keys` で bind が押したキーを第 3 引数で渡す形になったことを確かめた。本番で popup を開いて行が書かれるところはまだ見ていない（ユーザーの画面に popup が開くので、こちらからは開いていない）
  - CI: push した先端の Tests・Lint・Bench・Issues がすべて success。開発機では skip される `tests/tmux/test_fork_scratch.sh` も CI の Tests で通った
  - [ ] 次に `C-t t` を使ったとき、`grep scratch-popup ~/.cache/tt-restore-trigger.log` に start / end の行が出るかを確かめる
