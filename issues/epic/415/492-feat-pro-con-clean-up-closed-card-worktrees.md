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

## 決定 (2026-09-26、C-053 で人間が推奨を選んだ)

- いつ消すか: まず手のコマンド `pro-con worktree clean` だけ (既定は一覧、`--yes` で 1 個ずつ取り直して消す)。dispatcher の自動は別カード
- ブランチ: 中身が master にある (祖先か `git cherry` が全部 `-`) ときだけ消す。master に無い commit があるものは worktree もブランチも消さない
- 消せないもの: 出力に理由つきで並べる。判定は 1 つの関数 (`wtclean.Judge`)。設定画面 (456) への表示はこのカードではやらない

## 実装で分かったこと (2026-09-26 の実物で一覧だけ回した)

- **完了のカード 44 枚のどれも Archived になっていない** (書庫 `cards-archive.jsonl` も無い)。条件を「片付けた」にすると何も消えないので、
  合意どおり「完了・PG を止め終えた (StopAfterClose が無い)・削除の途中でない」にした。書庫へ移らない理由はこの issue では追わない
- **Claude Code は `claude -w` の worktree に `git worktree lock` を掛け、session が終わっても外さない** (理由は
  `claude session pc-c-025 (pid 5442 start …)`)。23 個が lock 付きで、pid は動いている session のものとも一致しない
  (動いている pc-c-053 の pid も既に居なかった)。session が動いているかは lock ではなく `claude agents` の cwd で見る。
  Claude の形の lock で、その pid に claude が居なければ外して消す (消せなければ同じ理由で掛け直す)。人が掛けた lock は消さない
- 無視された `tmp/` は 40 個中 40 個にあったが、34 個は空のディレクトリ (git は無視パターンに当たる空のディレクトリも `!!` で出す)。
  中にファイルがあるときだけ残す (5 個。変異のスクリプトやサンプル)
- ブランチの名前が `worktree-pc-<カード>` でないもの (`pc-c-012-r2` 等 3 個) は worktree だけ消し、ブランチは残す
- 一覧の結果: 消してよい 36 個 / 消さない 14 個 (master に無い commit: C-004・C-009・C-020。記録に無い: pc-c-043。tmp/ にファイル 5 個。
  動いているカード・役の worktree 等)。issue 本文の実測と同じ 3 個 + 1 個を残す

## 進捗

- [x] `pro-con worktree clean [--yes]` (package `wtclean`。git の呼び出しは `gitx` にまとめ、継承した `GIT_DIR` 等を外す。monitor も同じ口を使う)
- [x] テスト: テストの二進 (`testing.Testing`) では登録した sandbox の外を消す前に拒否する。変異 16 本を 1 本ずつ当てて全部 red を確認
- [x] 敵対的レビュー (最終ゲート。2026-09-26、使い捨ての repo で消えるのを再現させた)。直したもの:
  - [P0] `git worktree remove` は --force なしでも無視されたファイルを消す (tmp/ 以外の .env・settings.local.json・入れ子の repo・下の worktree)
    → 無視されたファイルは空のディレクトリと go_autobuild の産物だけ通す。下に登録された worktree があれば残す
  - [P1] `git cherry` の patch-id は空白を無視する → `git patch-id --verbatim` で比べ直す (merge した tree と比べる形は、
    取り込んだ後に master が同じ行を変えていると衝突し、実物で 36 個中 17 個を残したのでやめた)
  - [P1] 取り込む先を origin/HEAD から読んでいた → origin/master (無ければ origin/main) だけ
  - [P1] session の cwd の大文字小文字違い (APFS) を見落とす → 大文字小文字を無視して比べる (消さない側に倒れる)
  - [P2] skip-worktree / assume-unchanged の変更は git status に出ない → `git ls-files -v` の印で残す
  - [P2] reflog にしか無い版を失う → 取り込む先から辿れないものを refs/pro-con/removed/<名前>/<sha> に残してから消す (実物で 29 個にあった)
  - [P2] claude 以外のプロセス (人の shell・テスト) の cwd → lsof の cwd で残す
  - [P2] rc 1 を成功と読む → 消す側の git は rc 0 以外を失敗にする (run0)
  - 記録だけ: revert された commit も - になる (commit は master の歴史に残るので失わない)
  - 変異 28 本を 1 本ずつ当てて全部 red。直した後の実物の一覧も 36 個 / 14 個
- [ ] 取り込み後、本物で `pro-con worktree clean --yes` を人が回して、消した 36 個 / 残した 14 個を確かめる

## 関連

- 447 (カードを閉じたら PG の session を止める。worktree とブランチは残す) / 456 (設定画面のディスクの使用量と内訳) / 465 (同じ名前の worktree を黙って再利用しない)
