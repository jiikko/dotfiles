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

## 関連

- 492 (worktree の片付け。`pro-con worktree clean`) / 478 (書庫) / 438 (記録から消さない) / 456 (設定画面のプロセスの一覧) / 467 (前の session を辿る活動の表示)
