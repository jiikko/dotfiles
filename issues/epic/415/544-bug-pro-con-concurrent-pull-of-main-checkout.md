# 544 (bug): 取り込みの係と外の session が本体の checkout (~/dotfiles) を同時に pull してぶつかる

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

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

- [ ] まず取り込みの係の transcript で、同じ時刻に pull していたかを確かめ、本文に書く
- [ ] 本体の checkout を pull する手順 (integrator-guide.md・dotfiles の `.claude/rules/worktree-per-session.md`) が同じ lock を取る

## 関連

- `src/pro-con/integrator-guide.md` (役目 3) / `.claude/rules/worktree-per-session.md` (push の後に本体を pull する) / 471 (lock で直列にする仕組み)

## 進捗

(まだ無い)
