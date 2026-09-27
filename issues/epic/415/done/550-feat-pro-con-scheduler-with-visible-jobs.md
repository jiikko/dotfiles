# 550 (feat): 決まった時刻に決まったコマンドを pro-con が回すスケジューラ (設定画面で「何時に何が呼ばれるか」を見られる)


起票日: 2026-09-27

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-27): 「人間が打つのはめんどいので。スケジューラーみたいな機能欲しいね。何時に何が呼ばれるかを設定で見れて、
そこを見ると `...--yes` が呼ばれることがわかる感じにして」。

きっかけは 492 の `pro-con worktree clean --yes`。これは **人が手で打ったときだけ動く** 作り
(`card/flow.go` の案内文「消すのは人が pro-con worktree clean --yes を打ったときだけ」/ `dispatcher/forget.go` 冒頭の 🚨)。
打つ人がいないので溜まり続ける: 492 の起票時 (2026-09-26 13:55) は 48 個 1.5GB、2026-09-27 に数えると 92 個 2.9GB
(一覧だけ出すモードでは「消してよい」66 個 / 「消さない」26 個)。

やりたいこと: **pro-con が決まった時刻にコマンドを回す口を作り、設定画面を見れば「毎日 HH:MM に `pro-con worktree clean --yes`」
のように、何時に何が呼ばれるかが字面どおり分かる**ようにする。最初に載せるのは worktree clean。

## 今の形 (2026-09-27 にコードで確かめた)

- 時刻 (毎日 HH:MM) で回す仕組みは無い。dispatcher の Tick (`dispatcher/runner.go` の `time.NewTicker(poll)`) の中で、
  **起動からの経過時間**を見て回している処理が 5 つある。どれも `xxxEvery` の定数と `now.Sub(d.xxxAt) < xxxEvery` の同じ形:

  | 処理 | 間隔 | 何をするか |
  |---|---|---|
  | `purge` (`dispatcher/forget.go`) | 1 時間 | 完了から 1 週間たったカードを書庫から消す (497) |
  | `refreshUsage` (`dispatcher/usage.go`) | 5 分 | `/usage` を読み直す (子プロセスを起こす。1 回 約 4 秒) |
  | `announce` (`dispatcher/notify.go`) | 1 分 | tmux へ出し直す |
  | `collectProgress` (`dispatcher/progress.go`) | 30 秒 | PG の git の進捗を集める |
  | `collectDoing` (`dispatcher/doing.go`) | 10 秒 | PG の実行中のコマンドを集める |

  **どれも設定画面に予定としては出ていない** (purge は eventlog に `KindArchive` で残るが、ログのタブの既定の表示には出ない。
  `c` で全部を出したときに見えるかは未確認)
- 設定画面 (`ui/settings.go`。`s` で開閉) のタブは「設定 / プロセス / ディスク / ログ」
- `src/schedkeys` は tmux のペインへ指定の時刻にキーを送る別の道具で、pro-con とは関係しない (名前が似ているだけ)

## 対応方針 (起票時の案。実装した形は「設計」の節)

1. **予定の表を 1 つだけ持ち、実行と表示の両方をそこから引く**。表の 1 行 = いつ (毎日 HH:MM / N 時間ごと) + 何を (コマンドの argv)。
   設定画面に出す文字列は、実行する argv をそのまま並べたものにする (表示用の説明文を別に持たない。
   持つと、表示と実際に呼ぶものがずれても誰も気づかない)
   - 推奨: dispatcher が同じ `pro-con` を子プロセスとして argv どおりに起こす。こうすると、画面の字面と人が打つコマンドが完全に同じになる。
     関数を直接呼ぶ形にすると、字面は飾りのラベルになる
2. **表示**: 設定画面に「予定」のタブ (か、設定のタブの節) を足す。行ごとに、いつ・コマンドの字面・前回の実行時刻と結果・次回の時刻。
   CLI からも同じものを見られるようにする (`pro-con config` の出力に足すか、`pro-con schedule`)
   - 見た目は先に試作 (`src/pro-con/samples/`) で決める (`decide-layout-in-sample-renderer-first.md`)
3. **既にある経過時間の処理 (上の表の 5 つ) のうち、どれを同じ表に載せるか**決める。
   少なくとも purge (消す) は載せる候補: 載せれば「pro-con が勝手に何をいつ消すか」が 1 か所で見える。
   10 秒〜1 分の収集・出し直しは内部の見張りで、人が知る必要は薄い (載せると一覧が埋もれる)。
   `refreshUsage` は「決まった間隔で子プロセスを起こす」先例なので、実行の作り (子プロセスの起こし方・timeout・失敗の扱い) はここに揃えられるか見る
4. **時刻の変更**: 最初は既定の時刻で固定してよい (例: 毎日 04:00)。変える口は `pro-con config set` と設定のタブに後から足す
5. **dispatcher が止まっていた間の予定**: 次に起動したとき、遅れた予定を 1 回だけ回す (溜まった回数ぶんは回さない)。
   どちらにするかを決めて画面に出す
6. **結果を残す**: 回した結果 (rc・消した個数・残した個数・失敗の理由) を eventlog に書き、設定画面のログのタブと予定の行の「前回」に出す。
   失敗は人の番に上げるか決める (黙って失敗し続けない)
7. **人が同時に打ったときの排他**: 予定の実行と、人の手の `pro-con worktree clean --yes` が同時に走らないようにする (471 の直列化と同じ口を使えるか見る)

## 🚨 先に決めること: 自動で消してよいか (447 の決定との関係) → 結論: 自動で消す (2026-09-27 のユーザーの決定)

447 では「カードを閉じても worktree とブランチは消さない (取り込む前の作業が入っているかもしれない)」と決め、492 は消す判断を人が打つ一手に預けた。
自動で回すと、その **人の一手** が無くなる。`wtclean` の判定 (master に無い commit・未 commit の変更・動いている session・中を cwd にするプロセス・
消すと戻せない無視されたファイル・記録に無いカード・PM と取り込みの係の worktree は残す。消す直前に 1 個ずつ取り直す。
reflog にしか無い版は `refs/pro-con/removed/` に退避) が、人が一覧を見ずに回しても足りるかを、設計で 1 度問う。
問い: 人は一覧を見て何を止めていたか。止めていたものが無いなら、人の一手は判定を足していない。

**結論 (2026-09-27)**: 自動で消す。消える対象 (完了して中身が全部 master にある worktree と、そのカードの PG の session の transcript・claude の job・ブランチ) と
残る条件をユーザーに説明した後、「とりまスケジューラーから削除する実装やって」の指示を受けた。人は一覧を見ても、`--yes` がする判定
(1 個ずつ消す直前に取り直す) 以上のものを足していなかった (492 の実物の一覧で、消してよい側に消してはいけないものは無かった)。
止める口は `pro-con config set schedule off`。transcript と job には退避が無い (worktree の側は `refs/pro-con/removed/` に退避する) ことは変わらない。

## 設計 (2026-09-27。実装した形。README の「予定」節が正本)

ユーザーの指示「とりまスケジューラーから削除する実装やって」「s を押したら表示される画面にもタブとして追加してね」。最初は worktree clean だけを予定に載せる。

- **予定の表** (package `schedule`): `Job{Name, Hour, Min, Args, Lock}` の配列 `schedule.Jobs` が唯一の出典。
  最初の 1 行は「毎日 04:00 に `pro-con worktree clean --yes`」。画面・`pro-con config show` の字面 (`Job.Command()`) と dispatcher が起こす argv は同じ `Args` から作る
- **いつ回すか**: 前回に始めた時刻がその日の予定の時刻より前 (か未来 = 時計が戻った) なら、次の Tick で回す (`Job.Due`)。
  止まっていた・寝ていた間の予定は **1 回だけ**。記録が無い初回は、起動した最初の Tick で回る
- **回し方** (`dispatcher/schedule.go`): Tick の中で busy の印を立ててから裏の goroutine で子を起こす。子は `os.Executable()` の pro-con を argv どおりに、
  cwd = 状態の置き場・自分のプロセスグループ (Setpgid)・env から TMUX と入れ替えの lock の fd の変数を外し・stdout / stderr はファイル
  (`schedule/<名前>.out` / `.err`) へ直接。🚨 子は殺さない (timeout も取り消しも無し。worktree remove とブランチの削除の間で殺すと、ブランチだけが残って二度と片付かない)
- **記録** (`schedule.json`。書き手は dispatcher だけ): 起こす前に始めた時刻を書く (書けない・読めないなら回さない)。終わったら終了時刻・rc・結果の 1 行。
  失敗の判定は `store.ScheduleRun.Failed` の 1 か所 (dispatcher の出来事と画面の赤が同じ判定)
- **結果の 1 行**: `worktree clean --yes` が最後に `結果: worktree 消した N・残した N・失敗 N / session 消した N 枚・失敗 N 枚 (rc=N)` を出す
  (`schedule.ResultLine`)。rc≠0 なら stderr の最後の行をつなげて記録する
- **待てなかった実行** (dispatcher が先に抜けた・30 分を過ぎて入れ替わった): 次の dispatcher が、子の lock (`worktree-clean.lock`) が空いたことで終わりを知り、
  出力のファイルの結果の行から成否を読んで締める (rc = `schedule.RCUnknown`)。lock が持たれている間は次の予定を起こさず、6 時間続いたら error
- **排他**: `worktree clean --yes` が `worktree-clean.lock` を、設定の読み込みと claude の実体の解決より前に取る。取れなければ rc 3 (`schedule.ExitLocked`) で何もしない。
  予定の側は「重なったので何もしなかった」として扱い、2 回続いたら失敗として出す
- **止める**: `pro-con config set schedule off` / 設定画面の「予定を回す」のチェックボックス。設定を読めないときは回さない
- **見る所**: 設定画面 (s) の「予定」のタブ (設定の次) と `pro-con config show` (いつ・コマンド・前回・次回・出力の置き場。行は `backend.ScheduleRows` の 1 か所で作る)。
  「次回」は dispatcher の判定と同じ材料で出す (すぐ回る・前回が終わってから・回さない、を時刻と分けて出す)。出来事は `schedule` / `error` (ログのタブにも出る)
- 入れ替え (505) は予定の子を 30 分まで待つ (`Busy`)。e2e モード・`--once` では予定を持たせない
- **範囲外**: 時刻の変更 / 既存の経過時間の処理 (purge 等) を表に載せる

## 受け入れ条件

- [x] 設定画面に予定の一覧が出て、`pro-con worktree clean --yes` の行に、コマンドの字面そのままと、いつ呼ばれるかが出る (予定のタブ。`TestSettingsScheduleTab`)
- [x] 画面に出る字面と、dispatcher が実際に起こすコマンドの argv が同じ表から来ている (`schedule.Jobs` の `Args`。`TestJobsCommandMatchesArgs` / `TestExecScheduled`)
- [x] 決めた時刻に dispatcher が worktree clean を回し、結果 (消した / 残した / 失敗) がログのタブと予定の行に出る
- [x] dispatcher が止まっていた間に過ぎた予定の扱いが決まっていて、画面に出ている (1 回だけ回す。「次回」に出す)
- [x] 人の手の実行と重ならない (`worktree-clean.lock`。`TestWorktreeCleanYesRefusesWhileLocked`)
- [x] 「自動で消してよいか」の結論と理由を本文に書く (上の節)
- [x] 取り込み後、本物の dispatcher で初回の予定が回ったことを確かめる (下の「本物での確認」)

## 関連ファイル

- `src/pro-con/schedule/` (予定の表) / `src/pro-con/dispatcher/schedule.go` (回し方・待てなかった実行の締め方) / `src/pro-con/store/schedule.go` (記録・失敗の判定) /
  `src/pro-con/backend/schedule.go` (画面と config show の行) / `src/pro-con/ui/settings.go` (予定のタブ・チェックボックス)
- `src/pro-con/worktreecmd.go` (`runWorktree`。今の worktree clean の入口) / `src/pro-con/wtclean/`
- `src/pro-con/dispatcher/runner.go` (Tick) / `src/pro-con/dispatcher/forget.go` (`purge`。今ある唯一の時間で回す処理)
- `src/pro-con/ui/settings.go` / `src/pro-con/ui/settingslog.go` (設定画面とログのタブ)
- `src/pro-con/configcmd.go` (`pro-con config`)

## 関連

- 492 (worktree clean 本体。これを自動で回したい) / 447 (カードを閉じても worktree を消さない) / 497 (1 週間たったカードの記録を消す。今ある暗黙の定期処理)
- 456 (設定画面) / 471 (重い処理の直列化)

## 進捗

- 起票 (2026-09-27)
- 実装 (2026-09-27): 「pro-con: dispatcher が毎日 04:00 に worktree clean --yes を回す予定と、設定画面の予定のタブ (issue 550)」
  - 設計の敵対的レビュー (opus 1 本): P1 1 / P2 4 / P3 5。採用: 子を殺さない (timeout と取り消しを外し、Setpgid)・待てなかった実行を lock で締める・
    lock が取れないときの rc を分ける・時計が戻ったら回す・止める設定を最初から入れる。P1 (自動では消さないという決定とぶつかる) はユーザーの決定の更新として扱った (上の結論)。
    既に手当て済み: 記録を読めない・書けないなら回さない / busy の印を Tick の中で立てる
  - 実装の敵対的レビュー 1 周目 (opus 2 本。「壊す・並行」と「素通り・表示の嘘」): データを消す経路は見つからず。採用: 置き去りの子が lock を持つ間は起こさない・
    lock を realWorktreeEnv より前で取る・自分の実行を lock で締めない (busy を先に見る)・「次回」を dispatcher と同じ判定で出す・設定を読めないなら回さない (fail-open だった)・
    rc≠0 のとき stderr をつなげる・重なったが 2 回続いたら失敗・テストが守っていない 5 つの形 (残した数・session の数・WithoutCancel / Setpgid・off の描画・Failed と live の配線)
  - 2 周目 (opus 1 本): P1 なし。採用: 待てなかった実行を結果の行の rc で判定 (行の末尾に rc を入れた)・自分の子が 6 時間戻らなければ知らせる・
    error の重複抑止を直ったら外す・待てなかった実行を締めるとき Locked を 0 に・「前回が終わってから」の表示・6 時間の文面
  - 3 周目 (opus 1 本。2 周目の修正だけ): 採用 1 件: 1 つの Tick に回せない理由が 2 つあると、重複の抑止 (最後の 1 つだけを覚えていた) が効かず Tick ごとに両方出る
    → 理由を集合で覚え、その Tick に出なかった理由だけ外す。ResultRC の誤読・戻らない子の知らせ・Locked のリセット・表示と判定の食い違いは壊せなかった。
    この周の修正は抑止の覚え方の置き換えだけで新しい判定を足しておらず、テストと変異 2 本で直接確かめたので、4 周目は回さない
  - 記録だけ (3 周目): 置き去りの子が居るまま 6 時間を超えてスリープすると、起きた直後に 1 回だけ「6 時間以上居る」を誤って出しうる (記録の Start は壁時計)
  - make test: pro-con の gofmt 1 件 (改行だけ) で test-go-lint が落ち、直して test-go-lint を回し直して rc=0。ほかのターゲットは rc=0
  - 記録だけ (直さない): lock を確かめる一瞬 (settleOrphan の lockAs) に人の手の --yes が rc 3 で弾かれうる / 時計が子の所要より大きく戻ると同じ枠で 2 回回りうる /
    30 分を過ぎて入れ替わると子がゾンビとして残る (lock は外れる) / 子を起こしてから lock を取るまでの間に dispatcher が落ちると、走っている子を終わったと読む (何も消さない) /
    字面は「pro-con …」だが起こすのは os.Executable() の実体
  - 変異 (bin/mutate-verify): 実装 13 本・1 周目の修正 18 本 (うち 3 本はコンパイルできない形だったので当て直し)・2 周目の修正 6 本 (うち 2 本は当て直し。1 本は結果の行が rc を持つことを見るテストが無く、足してから red)・3 周目の修正 2 本、すべて狙ったテストが red。
    1 周目で緑だった「記録を読めないなら回さない」は、次の書き込みの失敗が同じ結果を出していたため。テストに理由の文面まで見させて red にした
- 本物での確認 (2026-09-27): push と ~/dotfiles の pull の後、動いていた dispatcher が新版へ入れ替わり、最初の Tick (13:39:02) で初回の予定を起こした
  (記録が無いので即回る = 設計どおり)。13:43:16 に rc=0 で終わった: 「結果: worktree 消した 66・残した 26・失敗 0 / session 消した 63 枚・失敗 0 枚 (rc=0)」
  (492 の一覧だけのモードで同じ日に数えた 66 / 26 と一致)。`.claude/worktrees` は 2.9GB → 936MB。reflog にしか無い版を `refs/pro-con/removed/` に 190 個退避。
  `pro-con config show` の前回・次回 (09-28 04:00) と、出来事 (`pro-con log` の schedule: 起こした / 終わった) も出た。
  設定画面の予定のタブは実画面では見ていない (描画はテストと試しの描画で確かめた)
- 残り: なし (時刻の変更・既存の経過時間の処理を表に載せる、は範囲外として起票しない。要るときに起票する)
