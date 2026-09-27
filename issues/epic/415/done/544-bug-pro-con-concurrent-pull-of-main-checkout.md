# 544 (bug): 取り込みの係と外の session が本体の checkout (~/dotfiles) を同時に pull してぶつかる

起票日: 2026-09-27

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

2026-09-27 01:4x、外の Claude が `~/dotfiles` で `git pull --rebase` を打つと、「fetch updated the current branch head」の後に
「untracked working tree files would be overwritten by merge」(`src/pro-con/card/purpose.go` ほか 531 のファイル) で止まった。
直後に見ると HEAD は origin/master と同じで `git status` はきれい、ぶつかったファイルの中身も master と同じだった。
取り込みの係の `git -C ~/dotfiles pull --rebase` (`integrator-guide.md` の役目 3) と同時に走ったと見ている (未確認。取り込みの係の transcript で時刻を突き合わせていない)

- 本体の checkout を pull するのは、取り込みの係 (push のたび)・外の Claude (worktree の後始末で `git -C ~/dotfiles pull --rebase`)・人
- git は index.lock で 1 つずつにするが、ファイルを書き出す途中を別の pull が見ると、上のように片方が止まる

## 期待する動作

- 本体の checkout の pull を 1 つずつにする (決まった lock を取ってから pull する。取り込みの係の手順と、dotfiles の worktree の後始末の手順の両方)
- 止まったら、何もせず報告して終わる (`git reset --hard` を案内しない。今回 git 自身がそう案内した)

## 受け入れ条件

- [x] まず取り込みの係の transcript で、同じ時刻に pull していたかを確かめ、本文に書く
- [x] 本体の checkout を pull する手順 (integrator-guide.md・dotfiles の `.claude/rules/worktree-per-session.md`) が同じ lock を取る

## 関連

- `src/pro-con/integrator-guide.md` (役目 3) / `.claude/rules/worktree-per-session.md` (push の後に本体を pull する) / 471 (lock で直列にする仕組み)

## 進捗

- 2026-09-27 (ユーザーと話す Claude):
  - 経路 (transcript で時刻を突き合わせた): 失敗した pull は 16:43:46.75Z (外の Claude の session 9ebbcf5d)。その **1.2 秒前の 16:43:45.51Z に、別の外の Claude の session (c69e30bd) が
    `git -C /Users/koji/dotfiles pull --rebase`** を打っていた。取り込みの係の pull はこの時間帯に無く、PM の pull は 5 分後 (16:48:54Z)。
    → 相手は取り込みの係ではなく、本体を pull する session どうし (取り込みの係・PM・外の session のどれでも起きる)
  - 直し: `scripts/pull_main_checkout.sh` を足した。本体の git の共通ディレクトリの `dotfiles-locks/pull-main` を `lockman with` で取ってから `git -C ~/dotfiles pull --rebase` する
    (ほかが持っていれば 2 分まで待ち、取れなければ rc 121 で何もしない。止まったら rc と理由を出し、`git reset --hard` はしない)
  - 手順書: `src/pro-con/integrator-guide.md` の役目 3 と `.claude/rules/worktree-per-session.md` を、このスクリプトを通す形に書き換えた
    (PM は pm-guide に pull の手順が無く、dotfiles の session として worktree-per-session.md に従う)
  - テスト: `tests/scripts/test_pull_main_checkout.sh` (一時的な origin と clone の上。lock を持たれている間は pull せず rc 121 / 空いていれば pull / checkout でない所は rc 2)。
    変異: lock を取らずに直接 pull する → 1 つ目が red / checkout でない所の断りを外す → 3 つ目が red
  - 敵対的レビューは省いた (lock を取ってから 1 コマンドを走らせるだけの薄いラッパーで、排他そのものは lockman が持つ)

## 取り込み後 (2026-09-27)

- CI の rest の runner には Go が無く、`bin/lockman` を初回にビルドできず `tests/scripts/test_pull_main_checkout.sh` が
  「lock を持つ側が始まらない」で落ちていた (run 36287059671)。lockman が動かず go も無いときだけ exit 77 (skip) にした
  (「fix(ci): Go の無い runner で pull_main_checkout のテストを skip にする」)。go があるのに lockman が動かないときは従来どおり赤
  (LM=/usr/bin/false の変異で go 無し = 77 / go あり = ✗ を確認)。CI 上の結果は未確認
