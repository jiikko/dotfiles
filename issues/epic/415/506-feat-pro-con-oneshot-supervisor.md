# 506 (feat): pro-con を起動したら、foreman のようなワンショットの supervisor が dispatcher と見張りを子として持つ (src/procsup)

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-26): 「サービスとして起動するのはめんどいので、pro-con を起動したら foreman みたいな感じでワンショットな supervisor が起動して欲しい」
「golang で書いてね。kill -9 されたらプロセスが残るのはしょうがない。パッケージは src の下に独立して作った方がいいかな」。

きっかけ: dispatcher が落ちると (kill -9・クラッシュ)、次に人が画面を開くまで、PG・PM・取り込みの係を誰も見張らない。
launchd の LaunchAgent は「サービスとして登録するのは面倒」なので採らない。

## 今の形 (2026-09-26)

- dispatcher は画面が別のプロセスグループに起こす (`main.go` の `spawnDispatcher`)。画面が 1 つも無い状態が 1 分続けば自分で抜ける (`--exit-without-screens 1m`)。
  画面の keeper が、dispatcher が 10 秒以上回っていなければ起こし直す (画面が開いている間だけ)
- 見張り (475) は dispatcher の子。`monitorsup.go` の `superviseMonitor` が起こし・落ちたら起こし直し・落ち続けたら諦めて出来事にし・止めるときに止める。
  stdin を dispatcher のパイプにして、dispatcher が死んだら見張りも抜ける
- PG・PM・取り込みの係は Claude Code の bg の session で、pro-con の子ではない (起動の記録で追い、`claude stop` で止める)

## 期待する動作

- `pro-con` (画面) を起動すると、画面とは別にワンショットの supervisor が 1 つ立つ (既に居れば立てない)。dispatcher と見張りはその子になる
- 子が落ちたら、間を空けて起こし直す。短い間に落ち続けたら諦めて出来事にする (起こし直し続けて枠や CPU を焼かない)
- 止める条件は今の決まりのまま: 最後の持ち主の画面の quit / 画面が 1 つも無い状態が続いた / `pro-con dispatcher --stop` (人が止めた印がある間は起こさない)。
  止めたら子を止めて、supervisor も抜ける (常駐しない = ワンショット)
- **kill -9 で supervisor が死んだときに子や PG が残るのは受け入れる** (ユーザーの決定)。次の起動で、起動時の復旧 (483) が拾う
- supervisor は PG を子にできないが、dispatcher を戻せないまま諦めたときは、起動の記録にある PG を `claude stop` で止めるかを決める (枠を使い続けないため)

## 作り

- **汎用の部分は `src/procsup` に独立した Go の module として作る** (tuikit と同じく pro-con 以外からも使える部品):
  子を起こす・落ちたら間を空けて起こし直す・落ち続けたら諦める・止める合図で子を止める (SIGTERM → 猶予 → SIGKILL)・
  親が死んだら子が抜ける生命線 (stdin のパイプ。今の見張りと同じ形)
- **止める条件 (画面の数・人が止めた印・join) は pro-con 側に置く** (pro-con 固有の決まり)
- 今の `monitorsup.go` を procsup の上に載せ替える (同じ処理を 2 つ持たない)
- 505 (dispatcher が自分で新版へ切り替わる。PID はそのまま) と両立させる: 切り替えは子が自分で exec するので、supervisor からは同じ子のまま見える
- 今の画面の keeper (dispatcher を起こし直す) と supervisor の起こし直しが二重にならないよう、どちらに寄せるかを決める

## 進捗 (2026-09-26 C-061)

決めたこと (着手前の案。仕様の正本は `src/pro-con/README.md` の「画面と dispatcher のつながり」):

- [x] **keeper との重なり → 起こし直しは supervisor に寄せ、keeper は supervisor を起こす役にする**。keeper は人が止めた印・
  `supervisor.lock`・`dispatcher.lock` のどれかがあれば起こさない (supervisor が居る間は任せる / supervisor の居ない dispatcher
  = 手で起動した・kill -9 された前の supervisor の子が抜けるのを待つ)。keeper を消さないのは、supervisor が kill -9 で死んだ後の回復口として要るため
- [x] **諦めたとき → 人が止めた印を置いてから PG を止める** (`dispatcher --stop --from-screen`)。印が無いと keeper が supervisor を起こし直し、
  落ち続けるのを繰り返して枠を使う。画面の c / 手で `pro-con dispatcher` を起動すると外れる (459 と同じ口)
- [x] **505 との両立**: dispatcher の入れ替えは同じ PID の exec なので supervisor からは同じ子。supervisor.lock は CLOEXEC で子へ渡らない。
  supervisor 自身は入れ替わらない (起こし直す dispatcher は os.Executable のパス = shim が差し替えた新しいビルド)
- [x] **見張りは dispatcher の子のまま** (本文の「dispatcher と見張りはその子」からずらした)。supervisor の子にすると、手で起動した
  dispatcher に見張りが付かないか、見張りの持ち主が 2 通りになる。孫でも、dispatcher が死ねば生命線で抜け、起こし直した dispatcher が起こす
- [x] 子の終わり方: rc=0 → dispatcher が止める判断をした (一緒に抜ける) / rc=3 (lock を他が持つ。見張りと同じ `exitLockHeld`) → 数えずに 10 秒ごと /
  それ以外 → 落ちた (10 秒空けて起こし直す。10 分に 5 回を超えたら諦める)
- [x] 起こし直す前の止める条件: 人が止めた印 / 落ちた後に止め終えた (`stop-result` の mtime が最後に落ちた時刻より新しい = 起こし直しの待ちの間に
  最後の持ち主の画面が quit した) / 持ち主の画面が無い (PG を止めて抜ける。join は起こさない = 481)
- [x] 出来事は見張りと同じく受付の箱 (store `supervisor`) → 次の dispatcher が events.jsonl (kind `supervisor`) に書く (書き手は dispatcher 1 つ)。
  dispatcher.log には時刻つきですぐ出る。`pro-con ps` に supervisor の行
- [x] `src/procsup`: 独立した module (依存ゼロ)。monitorsup.go を載せ替えた。🚨 元のコメント「親が読む側を持つと EOF にならない」は誤り
  (EOF を止めるのは書く側の複製)。本当の性質「親が kill -9 で死んでも子が抜ける」を `TestLifelineSurvivesParentKill` で固定した

テスト (commit「pro-con: 画面はワンショットの supervisor を起こし…」): procsup 13 本・supervisor 11 本・keeper・spawn の親子 (dispatcher の親が
supervisor、dispatcher が rc=0 で抜けたら supervisor も抜ける)・ps。mutation: procsup 4 本 / supervisor と keeper 11 本がすべて落ちる

敵対的レビュー (最終ゲート。read-only のサブエージェント): 不変条件 (dispatcher / supervisor を 2 つ立てない・持ち主の画面がある間は起こし直し手が
0 にならない・505 の exec と両立・lock が漏れない) は壊せず、P0 / P1 無し。P2 3 件は発火条件を確かめて直した
(commit「pro-con: 506 の敵対的レビューを受けて…」。3 本とも mutation で落ちる):

- 直した: ①持ち主の画面を一瞬だけ見ていた → ctrl+r (exec) の隙間で持ち主が 0 に見え、PG を止めうる。猶予 5 秒の間に 1 度でも見えたら続ける (`ownersAppear`)
  ②rc=3 の起こし直しに上限が無い → 手で起動した dispatcher が動く間、10 秒ごとに dispatcher を起こして claude --version を引き続ける
  (dispatcher は lock の前に claude を引く)。起こし直す前に dispatcher の lock を確かめ、他が持っていれば任せて抜ける (抜けた後は keeper が拾う)
  ③起こせなかった 1 回で人が止めた印を置いていた → 印は置き場で共有なので、別のバイナリで開いた画面の keeper まで止める。印を置かずに抜ける (前の形と同じく keeper が起こし直す)
- 記録のみ: Tick の失敗で持ち主が 0 のとき、dispatcher が止め終えてから rc=1 で抜け、supervisor がもう一度 --stop を走らせる (重複だが害は無い) /
  諦める途中に c を押すと「起こした」と出るが、起こすのは supervisor が抜けた後の次の keeper / store.Hold まで失敗するとき (disk full 級) は 1 分ごとの周回が続く
- 未確認リスク: 止める合図から 3 分で dispatcher へ SIGKILL (dispatcher のプロセスグループのテストの係の子が残りうる。止める処理は普通 1 分以内) /
  最後の持ち主の quit と起こし直しが µs〜ms の窓で重なると、--stop が「確かめた直後に別の dispatcher が起動した」で失敗しうる (実測していない) /
  諦めた原因が dispatcher の起動時の panic なら、--stop も同じ所で落ちて PG は止まらず印だけが残る
- 直し方の一文: 見張りの (旧 monitorsup.go の) 「親が読む側を持つと EOF にならない」は誤りだったので procsup のコメントで直した

## 関連

- 475 (見張り。monitorsup.go) / 483 (起動時の復旧) / 505 (dispatcher の自分での切り替え) / 481 (画面の持ち主と join)
