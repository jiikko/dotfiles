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
  | 落ちている (一覧に無い / pid 無し) | 待つ (自動の再開の後の turn で届く。戻らなければ 458 の requeueVanished の再開に、落ち続けて止めたら回答の再開に添える) | prepare の待ち (restartWait) の後、止めずに再開 |
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
- [x] 検査: store 5 本 / dispatcher 12 本 (busy は待つ → idle で届く・問いで止まった / 落ちた PG には届けない・方針変更は busy でも止める・回答に添える・
  起動の指示に入れる・渡したものだけに印・レビュー待ちから戻す (箱の review を追い越さない)・btw は PG に触らず答える・材料が無ければ記録から・削除を待つカード (451) は再開しない・消えた PG (458) の再開に添える) / live 2 本。
  偽の launcher と一覧で発火条件を作った (本物の claude・state dir は触らない)。`bin/mutate-verify` で変異 15 本がすべて red (差し戻しの後、458 / 461 / 445 の入った master に rebase した木で当て直した)
  (途中で 1 本が緑 = レビュー待ちの分岐が idle の分岐と重複していたので、分岐を畳んだ)
- [x] 敵対的レビュー (sonnet 1 体・読み取りだけ): 採用 2 件 = ①同じ Apply でオーダーの後に PM の close が来ると、未達のまま完了に埋まる →
  未達が残るカードの close を除ける (dispatcher がレビュー待ちから PG へ戻して届ける) ②方針変更は箱の適用待ちを見ずに再開し、PG の
  `card review` / `run` を追い越して除けさせうる → 方針変更も箱が空くまで待つ。どちらも直す前に red を見た検査を足した
  (store `TestCloseRefusedWithPendingOrder` / dispatcher `TestRedirectWaitsForInbox`)。記録だけ 1 件 = 下の残り (テストの係の実行)。
  btw の `At` の重複・`order` の omitempty はレビューが追って実害なしと確かめた
- [x] make test rc=0 (テストの係・所要 5m19s。origin/master (472 まで) を merge した後の `cca9a8c3`)
- [x] 差し戻し (取り込みで 458 / 461 とぶつかった) に対応: origin/master (445 まで) に rebase し、ブランチ `pc-c-012-r2` へ push (自分のブランチへ force push しない)。
  461 の `ExecLauncher{UserSettings}` と btw の `Ask` を両方残し、445 の `store.Pending` (画面の出来事を数えない) に合わせた。分解済みへ戻す処理は 458 の `requeue` に寄せた。
  ガイド §8 は 445 の「--view では断る操作を出さない」を正にした
- [x] rebase 後の make test rc=0 (テストの係・repo の root で・所要 4m15s)
- 本物の claude での確認 (idle の判定が turn の区切りと一致するか・再開の文が PG に読まれるか・haiku の答えの質)
  - [x] 追加オーダー: 本番で PG に届いた (下の「本番で見たこと」)
  - [x] 片付け (2026-09-27、C-008): 隔離した置き場の本物のモード (下の「隔離した本物のモードで見たこと」) で `x` → `y`。完了の 2 枚がボードから外れ、
    書庫 (`cards-archive.jsonl`) に `Archived: true`・履歴「完了のレーンから片付けた」で残り、`card list --all` に出た。依頼の列のカードは残った
  - [x] btw の受付から答えまで (同じ置き場): 画面の `w` → 確認 `y` → dispatcher が同じ Tick で答えを履歴と詳細の画面に書いた (PG の出力が無いので記録から答える経路。1 秒以内)
  - [x] haiku の答えの質: 本物の `HaikuAsk` + `btwPrompt` に、この PG が実際に出した出力 8 行を渡して 3 問 (1 本 7〜10 秒)。
    「あと何分?」には「材料からは分からない」と答えて推測しなかった。「確かめ終わった?」は材料にある事実とまだ分からない部分を分けた。
    「今どうなってる?」は要点は合っていたが「btw（追加オーダー）」と取り違えた (題名の並びから混ぜた。下の残り)
  - [ ] 本物の PG の transcript を dispatcher が読んで haiku へ渡す通しの経路 (`pgOutputs` → `Ask`)。隔離した置き場で PG を起こせなかった (下の残り)。
    配線 (`dispatchercmd.go` の `Ask: HaikuAsk` / `Transcript: transcriptReader`) と、偽の transcript での検査はある

## 残り・未確認のリスク

- 再開の直後に session がまだ idle に見える間に次のオーダーが来ると、立ち上がったばかりの PG を止めて再開し直しうる (未実測。無害に近いが turn の途中を切る)
- 415 論点 11 の「方針変更で止める前に、その時点の diff をカードに記録する」はしていない (worktree は `claude stop` で消えないので変更は残る)
- 方針変更でテストの係の実行中のカードを戻すと、実行を止めるのは次の Tick (tickRuns が deliverOrders より先に回る)。その間の結果は捨てる (カードの履歴には「取り下げた」が残る)
- AskUserQuestion で止まった PG へ追記が届かないまま残る (規律違反の PG。見張りは watchdog 側の課題)
- haiku の btw の答えが、カードの題名に並んだ語を取り違えうる (実測 1/3 問: 「btw（追加オーダー）」)。答えは記録と履歴に残るだけで操作は起こさないので、今は直さない
- btw の答えを本物の PG の出力から作る通しの経路は未実測 (上の進捗の最後の項)。確かめるには、隔離した置き場で PG を起こす必要がある。
  🚨 `claude --bg` の session が起動した側の環境変数 (`XDG_STATE_HOME`) を受け継ぐかは**未実測**。受け継がないなら、隔離した dispatcher が起こした PG の
  `pro-con card review <ID>` は本番の箱に届く (隔離した置き場の ID が本番のカードと重なる)。先にこれを測る (測りかけたが、probe の session が権限の確認で止まり、打ち直しは PG の session の guard に止められた)

## 本番で見たこと (2026-09-27、ユーザーと話す Claude が本番の記録 `~/.local/state/pro-con/live/events.jsonl` で確かめた)

- 追加オーダーは本番で PG に届いている (C-080 の履歴「追加オーダーを 1 件 PG へ届けた」、C-078・C-081 にも届けた)
- btw は本番で 1 度も使われていない (kind `btw` が 0 件)。「本物の claude での確認」は btw と片付けが残る

## 残りの確認の進め方 (2026-09-27、PM。pro-con カード C-008)

- 残りは「本物の claude での確認」のうち **btw (haiku の答えの質) と片付け** の 2 つ (追加オーダーは上の節で本番で届いたと確かめ済み)。
  どちらも `pro-con card` の CLI に口が無く、画面の `w` / `x` からしか出せない
- 🚨 **本番の記録 (`~/.local/state/pro-con/live/`) で片付け (`x`) を試さない**。人のボードの完了のカードが画面から外れる。
  状態の置き場を分けた dispatcher と画面 (隔離した state dir) で、本物の claude を使って確かめる。本番でしか確かめられないと分かったら、そこで人に聞く
- 確かめ方 (pty での画面の操作・隔離の仕方) は PG が決めてよい。不具合が見つかったら直して、ここの「残り・未確認のリスク」を更新する

## 隔離した本物のモードで見たこと (2026-09-27、C-008 の PG)

- 置き場: `XDG_STATE_HOME=<worktree>/tmp/e2e438/state` (本番の `~/.local/state/pro-con` に触らない)。`XDG_CONFIG_HOME` は git の設定にも効くので
  変えず、dispatcher を手で `pro-con dispatcher --pm off --integrator off` と起こし、画面は `--join` で加えた (隔離した tmux `-L pc438 -f /dev/null`)。
  カードは確認のカード (`--purpose question`) だけにして、PG を起こさない
- 🚨 **空の置き場で dispatcher を起こすと、予定 `worktree-clean` (毎日 04:00) がその場で「今日の枠は未実行」として走る** (`schedule.Job.Due` は記録が無いと真。コードで読んだ。走らせてはいない)。
  隔離した記録を正として本番の repo (`~/dotfiles`・`~/src/*`) の worktree を片付けにいくので、起こす前に `live/schedule.json` へ直前の時刻の実行を書いて止めた。
  隔離した置き場で dispatcher を起こすときは同じ手当てが要る
- 画面の幅 170 桁では、案内の行から `d 削除`・`s 設定`・`x 完了を片付け` が落ちて見えなかった (削る仕組みは未確認)。押せば効いた
