# 550 (feat): 決まった時刻に決まったコマンドを pro-con が回すスケジューラ (設定画面で「何時に何が呼ばれるか」を見られる)

> 🚨 **担当中: dotfiles の issue 550 を実装しているセッション (worktree wt-550-sched)**（2026-09-27〜）

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

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

## 対応方針 (案。設計で詰める)

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

## 🚨 先に決めること: 自動で消してよいか (447 の決定との関係)

447 では「カードを閉じても worktree とブランチは消さない (取り込む前の作業が入っているかもしれない)」と決め、492 は消す判断を人が打つ一手に預けた。
自動で回すと、その **人の一手** が無くなる。`wtclean` の判定 (master に無い commit・未 commit の変更・動いている session・中を cwd にするプロセス・
消すと戻せない無視されたファイル・記録に無いカード・PM と取り込みの係の worktree は残す。消す直前に 1 個ずつ取り直す。
reflog にしか無い版は `refs/pro-con/removed/` に退避) が、人が一覧を見ずに回しても足りるかを、設計で 1 度問う。
問い: 人は一覧を見て何を止めていたか。止めていたものが無いなら、人の一手は判定を足していない。

## 受け入れ条件

- [ ] 設定画面に予定の一覧が出て、`pro-con worktree clean --yes` の行に、コマンドの字面そのままと、いつ呼ばれるかが出る
- [ ] 画面に出る字面と、dispatcher が実際に起こすコマンドの argv が同じ表から来ている (片方だけ変えられない)
- [ ] 決めた時刻に dispatcher が worktree clean を回し、結果 (消した / 残した / 失敗) がログのタブと予定の行に出る
- [ ] dispatcher が止まっていた間に過ぎた予定の扱いが決まっていて、画面に出ている
- [ ] 人の手の実行と重ならない
- [ ] 「自動で消してよいか」の結論と理由を本文に書く

## 関連ファイル

- `src/pro-con/worktreecmd.go` (`runWorktree`。今の worktree clean の入口) / `src/pro-con/wtclean/`
- `src/pro-con/dispatcher/runner.go` (Tick) / `src/pro-con/dispatcher/forget.go` (`purge`。今ある唯一の時間で回す処理)
- `src/pro-con/ui/settings.go` / `src/pro-con/ui/settingslog.go` (設定画面とログのタブ)
- `src/pro-con/configcmd.go` (`pro-con config`)

## 関連

- 492 (worktree clean 本体。これを自動で回したい) / 447 (カードを閉じても worktree を消さない) / 497 (1 週間たったカードの記録を消す。今ある暗黙の定期処理)
- 456 (設定画面) / 471 (重い処理の直列化)

## 進捗

- 起票 (2026-09-27)
- 反証レビュー (sonnet 1 体、2026-09-27):
  - [P1] 採用: 「経過時間で回す処理は purge だけ」は誤り。5 つある (今の形の表と対応方針 3 を直した)
  - 反証できなかった: 時刻で回す仕組みは無い / worktree clean を呼ぶのは CLI の入口 (`worktreecmd.go`) だけ / 設定画面のタブ構成 / 関連 issue の要約 / 同種の issue は無い
  - 自己参照の罠 (dispatcher が子で worktree clean を起こすと、自分や PM が「動いている」と判定されて全部残る): 判定のコード上は見当たらない
    (dispatcher は claude の session ではなく、cwd も PG の worktree の外)。ただし dispatcher の cwd が worktree の中になる場合が無いかは、実装のときに実測する
