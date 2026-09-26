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

## 関連

- 475 (見張り。monitorsup.go) / 483 (起動時の復旧) / 505 (dispatcher の自分での切り替え) / 481 (画面の持ち主と join)
