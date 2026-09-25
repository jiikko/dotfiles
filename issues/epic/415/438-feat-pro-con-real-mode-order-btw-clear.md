# 438 (feat): 本物のモードで追加オーダー・btw・片付けを受ける

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

本物のモード (live backend) が受ける操作は新しい依頼 (`n` / `i`) と回答 (`r`) だけで、追加オーダー (`+`)・btw (`w`)・
完了のレーンの片付け (`x`) は押した時点で断る (`live.Backend.Accepts`)。模擬のモードでは動く。

## 対応方針 (候補)

- 追加オーダー / btw: カードの PG に届ける。425 の実測で、idle の bg session には SendMessage が届き、busy なら今の turn の後に届く
  (AskUserQuestion で止まっている session には届かない)。受付の箱に種類を足し、dispatcher が PG へ送る
- 片付け: 完了のカードを画面から外す (記録からは消さない)。受付の箱で dispatcher が印を付ける
- どれも「書き手は dispatcher だけ」(426 の決定 1) を守る

## 関連

- `src/pro-con/live/live.go` の `Accepts` / `Apply`

## 決めたこと (2026-09-25)

- **届け方は「止めて同じ session を再開」** (回答と同じ口)。SendMessage は使わない: dispatcher は Go の常駐で、SendMessage は Claude Code の
  ツールなので呼べない (426 の決定 2 と同じ理由)。425 結果 3 の「busy には turn の区切りで届く」性質は、再開の時機を idle まで待つことで再現する
  (426 の決定 3「追記は PG の turn の区切りを待って届ける」)
- PG の様子ごとの扱い (`dispatcher/orders.go` の冒頭が正本):

  | PG の様子 | 追記 | 方針変更 |
  |---|---|---|
  | 作業中 (busy) | 積んで待つ (未達がカードに見える) | 止めて、指示を差し替えて再開 |
  | 作業中の列のまま turn を終えた (idle) | 止めて再開して届ける。箱に適用待ちがあれば次の Tick (PG が turn の最後に置いた `card review` を追い越さない) | 同左 |
  | `card ask` / `card run` / レビュー待ちで turn を終えた | 回答・テストの結果・差し戻しの再開に添えて届ける。レビュー待ちに未達が残っていたら PG へ戻す | 同左 (レビュー待ちは戻す) |
  | AskUserQuestion / 権限の確認で止まった (status waiting) | 届けない (止めると問いが消える。attach で進めれば idle で届く) | 止めて再開 |
  | 落ちている (一覧に無い / pid 無し) | 待つ (自動の再開の後の turn で届く。落ち続けて止めたら回答の再開に添える) | prepare の待ち (restartWait) の後、止めずに再開 |
  | まだ起動していない | 起動の指示に入れる | 同左 |

- 届いた印 (`Order.Delivered`) は起動・再開を確かめたとき (settle) に付ける。付けるのは起動・再開の印 (`LaunchedAt`) までに積まれたものだけ
  (失敗と返った再開では付けず、後の Tick で一覧から取り込んだときは、その間に積まれたものを含めない)
- **別件**は元のカードの子 (`ParentID`) の新しい依頼として箱に置く (PM が分けて issue に紐づける)
- **btw は PG へ届けない** (指示の文面は「PG へ届ける」だったが、415 要件 9「作業中の PG は止めず、その文脈も汚さない」を優先した。
  dispatcher から PG へ届けるには止めて再開するしかない)。dispatcher が PG の出力の末尾とカードの記録を材料に haiku (`claude -p`) で答え、
  答えをカードの `Btws` と履歴に書く。1 本ずつ裏で作り、Tick を止めない。PG の出力が無ければ記録だけから答える
- **片付け**は画面が見ていた完了のカードの ID を箱に置き、dispatcher が今も完了のものだけを Archived にする (適用までに完了になったカードを巻き込まない)
- `live.Backend` は `Accepts` を持たない (すべて受ける)。断るのは見ているだけの画面 (`--view`) だけ

## 進捗

- [x] 受付の箱に `order` / `btw` / `clear` と、`add` の `ParentID` を足した (`store`)。完了のカードへの追記・方針変更は除ける。レビュー待ちは受ける
- [x] dispatcher: 追加オーダーを届ける (`orders.go`)・btw に答える (`btw.go`)。起動の指示と再開の文に未達のオーダーを添える
- [x] `live.Apply` が追加オーダー・btw・片付けを箱に置く。画面の断りの文言を「見ているだけの画面」向けに直した。README と docs/glogx-ui-guide.md §8
- [x] 検査: store 4 本 / dispatcher 9 本 (busy は待つ → idle で届く・問いで止まった / 落ちた PG には届けない・方針変更は busy でも止める・回答に添える・
  起動の指示に入れる・渡したものだけに印・レビュー待ちから戻す (箱の review を追い越さない)・btw は PG に触らず答える・材料が無ければ記録から) / live 2 本。
  偽の launcher と一覧で発火条件を作った (本物の claude・state dir は触らない)。`bin/mutate-verify` で変異 13 本がすべて red
  (途中で 1 本が緑 = レビュー待ちの分岐が idle の分岐と重複していたので、分岐を畳んだ)
- [ ] 本物の claude での確認 (idle の判定が turn の区切りと一致するか・再開の文が PG に読まれるか・haiku の答えの質)

## 残り・未確認のリスク

- 再開の直後に session がまだ idle に見える間に次のオーダーが来ると、立ち上がったばかりの PG を止めて再開し直しうる (未実測。無害に近いが turn の途中を切る)
- 415 論点 11 の「方針変更で止める前に、その時点の diff をカードに記録する」はしていない (worktree は `claude stop` で消えないので変更は残る)
- AskUserQuestion で止まった PG へ追記が届かないまま残る (規律違反の PG。見張りは watchdog 側の課題)
