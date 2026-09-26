# 497 (feat): 完了から 1 週間たったカードを自動で消し、session と worktree は人が明示したときだけ消す

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの決定 (2026-09-26): 「カードが完了になって 1 週間経過したカードは自動削除、ユーザーが明示したときに session および worktree を消す」。
設定画面のプロセスの一覧に、終わったカードの止まった session が 49 本並んでいた (意味の無い行) ことと、終わった物が何も消えずに残り続けることから。

## 今の形 (2026-09-26)

| 残る物 | 量 (2026-09-26) | 今の扱い |
|---|---|---|
| カードの記録 (`cards.json`) | 動いているカードと完了して間もないカード | 完了から 24 時間 (`store.AutoClearAfter`) で書庫へ移す |
| カードの書庫 (`cards-archive.jsonl`) | 足していくだけ | 消さない (438 の「記録からは消さない」を書庫で守っている) |
| pro-con の起動の記録 (`sessions.json` / `sessions-retired.json`) | 206 行・約 60KB | 消さない。終了で止める対象と、活動の表示 (467) が前の session を辿るのに使う |
| PG / 役の session の transcript (`~/.claude/projects/*worktrees-pc-*`) | 約 408MB | 消さない (Claude Code の持ち物) |
| PG の worktree とブランチ | 約 1.5GB | 人が `pro-con worktree clean --yes` を打ったときに消す (492 / C-053。中身が master にあるときだけブランチも) |

## 期待する動作

1. **カードの自動削除**: 完了にした時刻から 1 週間たったカードを、dispatcher が書庫から消す (記録から書庫へ移すのは今どおり 24 時間)。
   消すのは「完了」のカードだけ (削除の途中・PG を止め終えていない・人の番のカードは消さない)。1 週間は定数にして、設定で変えるかは決める
2. **session と worktree は人が明示したときだけ消す**: `pro-con worktree clean` (492) を広げ、`--yes` のときに、消せる条件を満たすカードの
   worktree とブランチ (今どおり) に加えて、**そのカードの起動の記録の行と session の transcript も消す**。既定 (一覧だけ) では消す物を全部並べる
   - transcript は Claude Code の持ち物で消すと戻せない。消すのは、pro-con が起動したと記録で示せる session のものだけ (外の session の transcript に触らない)
3. 🚨 **1 と 2 の食い違いを埋める**: 今の `worktree clean` は、カードが記録にも書庫にも無い worktree を「記録に無い」として消さない。
   カードを 1 週間で消すと、その後の明示の片付けで、そのカードの worktree と session を消せなくなる。
   カードを消すときに、片付けに要る最小限の印 (カード ID・完了の時刻・session の id・worktree のパス・ブランチ) を別のファイルに残し、
   `worktree clean` の判定 (`wtclean`) がそれを読んで「pro-con が作った・完了した」と示せるようにする。印は片付けが済んだら消す
4. 設定画面 (456) のプロセスの一覧は、動いている物と、食い違い (カードは作業中なのに session が止まっている / カードは完了なのに session が動いている) だけを出す。
   終わったカードの止まった session は出さない (数も出さない)

## 考えること

- 🚨 自動で消すのはカードの記録だけ。**session・transcript・worktree・ブランチは自動では消さない** (ユーザーの決定)
- 破壊的な操作を足す: 書庫の書き直しは書き手を dispatcher だけにし (426)、途中で落ちても書庫を壊さない (今の Archive の失敗モードの表に倣う)。
  transcript の削除は `sandbox-real-destructive-test-apis` (テストは置き場の外を実行前に拒否) と敵対的レビューを通す
- 消したカードの ID は使い回さない (`nextId` は減らさない)。issue の本文や commit からカード ID で辿れなくなることは受け入れる (issue に要点が残る)

## 実装で決めたこと・分かったこと (2026-09-26、C-057)

- 1 週間は定数 (`store.PurgeAfter`)。設定では変えない (印があれば、消した後も片付けられるので急ぐ理由が無い)。dispatcher は起動して最初の Tick と
  その後 1 時間ごとに書庫を読んで消す (Tick ごとに書庫を読まない)。書庫の読めない行は消さずに残す
- 印は `…/live/cards-purged.jsonl` (カード ID・repo・完了の時刻・消した時刻・worktree・ブランチ・起動の記録の今の分と退いた分の session)。
  印を書いてから書庫を書き直す。印を作れなければそのカードは消さない
- 起動の記録と印の書き手は dispatcher のままにした: `worktree clean --yes` は受付の箱に `forget` (カード ID と消した session id) を置き、
  dispatcher が挙げた session の行だけを消してから印を消す (片付けの後に再開した session の行・ほかのカードの行は残す。完了していないカードは除ける)
- **Claude Code の session の一覧から消す口はある**: `claude rm <短い id>` (2.1.283 で使い捨ての session で実測)。
  `claude agents --all` の行と `~/.claude/jobs/<id>` を消し、**transcript は残す**。**worktree とブランチも自分の判断で消す**ので、
  pro-con が worktree とブランチを消し終えた後にだけ呼ぶ。job の state.json は `-w` の起動なら cwd が起動した repo で worktree は
  `worktreePath`、再開なら `worktreePath` が空で cwd が worktree (実物で実測)。session id・worktree・ブランチがカードのものでなければ呼ばない
- transcript は、起動の記録か印にある session のものだけで、中の cwd の記録が全部その worktree の下のときだけ消す
  (人が同じ session を別の場所で続けた transcript を消さない)。テストの二進は sandbox の外の transcript・本物の `claude rm` を実行前に拒否する
- 設定画面のプロセスの一覧 (456 が master に入った後に合わせた): 食い違いは「作業中なのに止まっている」「完了 (か片付け済み) なのに動いている」の 2 つだけ
  (`backend.Proc.Mismatch`。`pro-con ps` も「食い違い: 」で出す)。止まった PG は、それ以外は出さず数えない。止まっている役 (PM・取り込み・テストの係) は畳んだまま
- 本物の状態で一覧だけ回した結果 (2026-09-26): worktree 消してよい 37 / 消さない 18、session を消してよいカード 34 / 消さない 19
  (消さない 14 枚は worktree が残るカード。worktree を残すカードの session は残す)

## 進捗

- [x] 完了から 1 週間のカードを書庫から消す・片付けの印 (store/purge.go・dispatcher/forget.go)
- [x] `worktree clean --yes` で session (transcript・claude の job・起動の記録の行) も消す (wtclean/sessions.go)
- [x] 設定画面のプロセスの一覧を動いている物と食い違いだけにする
- [x] テスト: 変異 28 本を 1 本ずつ当てて 27 本が red (残った 1 本は到達しても意味の無い分岐だったので、分岐ごと消した)
- [x] 敵対的レビューと設計のレビュー (最終ゲート)。直したもの: transcript の cwd は全部で確かめる / `claude rm` は中立な dir で走らせる /
  symlink の置き場を rmdir しない / 食い違いを仕様の 2 つに絞る / 記録を読めないときは食い違いを付けない / 書庫を 1 時間に 1 回だけ読む /
  一覧に消す transcript と job を全部並べる / 取り直して残したカードは理由を出す
  - 記録だけ (未確認リスク): 動いているかは `claude agents --json` だけで見る (pid では見ない)。worktree とブランチが消えた後にしか届かない
- [ ] 取り込み後、本物で `pro-con worktree clean --yes` を人が回して、消えた物と残った物を確かめる

## 関連

- 492 (worktree の片付け。`pro-con worktree clean`) / 478 (書庫) / 438 (記録から消さない) / 456 (設定画面のプロセスの一覧) / 467 (前の session を辿る活動の表示)
