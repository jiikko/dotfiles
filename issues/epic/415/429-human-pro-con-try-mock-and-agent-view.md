# 429 (human): pro-con の模擬版を触り、agent view との重なりと attach からの戻り方を確かめる

起票日: 2026-09-24
期限: 2026-10-01

親: [415](415-design-claude-pm-worker-orchestration.md)

## なぜ人が要るか

使い勝手 (やりたい操作があるか・戻り方が自然か) は、コードやテストからは判断できない。
agent view との重なりは「どちらを使いたいか」の判断で、ユーザーが決めること。

## 手順

1. `bin/pro-con` を起動し、しばらく触る。**やりたいのに操作が無いもの**があれば書き出す
   - `x` で片付けた完了のカードを見返したくなるか (見返す画面はまだ無い。要るなら子 issue を起こす)
2. `claude agents` を端末で開き (agent view)、session の一覧と対話が pro-con とどれだけ重なるかを見る。
   重なるなら「一覧と対話は agent view に任せ、pro-con はカード・キュー・レビュー・watchdog に絞る」を検討する
3. pro-con でカードを選んで `a` (attach) し、抜けて pro-con に戻る。← で抜けると agent view に出るので、そこから戻る操作が自然か

## 期待と違ったとき

- 1 で足りない操作があれば新しい子 issue を `issues/epic/415/` に起こす
- 2 で役割を絞ると決めたら [426](426-design-pro-con-open-decisions.md) に追記する
- 3 で戻り方が不自然なら 415 の論点 7 を open に戻して案内を決め直す

## 進捗

- [ ] 未確認
