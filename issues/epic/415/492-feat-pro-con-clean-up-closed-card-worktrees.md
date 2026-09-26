# 492 (feat): 閉じたカードの PG の worktree を片付ける (カードを閉じても消さないので増え続ける)

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-26): ディスクの使用量を測ったら (456)、PG の worktree が増え続けていたので issue にする。
447 の決まりで、カードを閉じても PG の worktree とブランチは消さない (取り込む前の作業が入っているかもしれないため)。
消す仕組みは無いので、閉じたカードの分が溜まる一方になっている。

## 実測 (2026-09-26 13:55)

`~/dotfiles/.claude/worktrees/pc-*` は 48 個で 1.5GB (1 個あたり 24〜38MB)。内訳:

| 状態 | 個数 | 消してよいか |
|---|---|---|
| カードが完了・未 commit の変更なし・ブランチの先端が origin/master の祖先 | 20 | よい |
| カードが完了・未 commit の変更なし・祖先ではないが、中身は全部 master にある (`git cherry` が全部 `-`。rebase / cherry-pick で入った) | 18 | よい |
| カードが完了・**master に無い commit が残っている** (C-004 1 本 / C-009 1 本 / C-020 3 本) | 3 | 人が見る |
| session がまだ動いている (`claude agents` の cwd) | 5 | 消さない |
| 記録に無いカードの worktree | 1 | 人が見る |
| PM と取り込みの係の worktree (`pc-pm-*` / `pc-int-*`) | 2 | 消さない (役が次の知らせをそこで再開する) |

消してよい 38 個で約 1.2GB。

## 考えること

- **worktree を消すことと、ブランチを消すことを分ける**。`git worktree remove` で消えるのは作業ツリー (未 commit の変更) だけで、
  ブランチと commit は repo に残る。未 commit の変更が無ければ worktree を消しても何も失わない。ブランチを消すのは、中身が master にあると示せたときだけ
- 「取り込み済み」の判定は祖先かどうかだけでは足りない (上の 18 個のように、rebase / cherry-pick で入ったものは祖先にならない)。`git cherry` の patch の同一性でも見る
- いつ消すか: カードを片付けた (Archived。完了から 24 時間で自動) ときに dispatcher が消す / `pro-con` のコマンドで手で消す / 設定画面 (456) から消す。
  一度に全部を消すのではなく、条件を満たしたものだけを 1 個ずつ確かめて消す
- 消せないもの (master に無い commit・記録に無い・session が動いている) は、消さずに一覧で出す (456 の設定画面のディスクの内訳と同じ出どころ)
- 🚨 **破壊的な操作を新設する**ので、`~/.claude/rules/sandbox-real-destructive-test-apis.md` (テストでは状態の置き場の外を実行前に拒否する) と
  `adversarial-review-own-safeguards.md` (敵対的レビューを最終ゲートにする。判定から実行までの間を 1 個単位に縮める) に従う。
  消す直前に、未 commit の変更・動いている session・master との比較を取り直す (検査と実行を離さない)
- pro-con が起動した session の transcript (`~/.claude/projects/*worktrees-pc-*`。51 個・408MB) も増え続けるが、これは Claude Code の持ち物で、
  再開 (`--resume`) と活動の表示 (467) が読む。消すかはこの issue では決めない (記録だけ)

## 関連

- 447 (カードを閉じたら PG の session を止める。worktree とブランチは残す) / 456 (設定画面のディスクの使用量と内訳) / 465 (同じ名前の worktree を黙って再利用しない)
