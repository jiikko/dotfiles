---
name: start-issues-ops
version: 1.1.0
description: repo の root に issues/ を作り、dotfiles の issues/README.md を持ち込んで issue 運用を始める。claim (next/) を使うかと commit するかはユーザーに聞く。「issue運用を始めて」「issuesディレクトリを作って」「start-issues-ops」で発火。
---

# start-issues-ops — repo で issue 運用を始める

repo の root に `issues/` を作り、`~/dotfiles/issues/README.md` をコピーする。
`issues/` が在る repo では、dotfiles の issue 系の hook が次のセッションから効く (手順 5 の一覧)。
共通規約 (命名・type・状態ディレクトリ・`epic/`・`期限:`・`human`・`retro`) は `~/dotfiles/_claude/issue-rules.md` が正本で、
hook が注入する。README には写さない。

## 手順

1. **root を決める**: `git rev-parse --show-toplevel`。git repo でなければ、その旨を報告して止まる
2. **既に在れば止まる**: root に `issues/` か `issue/` が在るなら、何も作らずに「既に在る」と報告する
   (README を上書きしない。その repo の README は repo 固有の事項を持っている)
3. **コピーする**: 元の `~/dotfiles/issues/README.md` が無ければ止まる。在れば

   ```sh
   root=$(git rev-parse --show-toplevel)
   mkdir "$root/issues" && cp ~/dotfiles/issues/README.md "$root/issues/README.md" &&
     cmp ~/dotfiles/issues/README.md "$root/issues/README.md" && echo copied
   ```

   `copied` が出たことを確かめる
4. **AskUserQuestion で 2 つを 1 回に聞く** (repo ごとに運用が違うので既定を決めない)
   - **claim (`next/`) を使うか**。claim の規約 (`issue-rules.d/claim-issue-in-next-and-push.md`) と hook は、`issues/next/`
     (か `issues/epic/<name>/next/`) が実在する repo でだけ効く
     - 「使う」: `mkdir "$root/issues/next" && touch "$root/issues/next/.keep"` (空のディレクトリは git に載らず、別の checkout では opt-in が消える)。
       着手のたびに目印と担当者バナーを push する手間が増える
     - 「使わない」: 何も作らない。複数のマシン・セッションが同じ issue に同時に着手しても、どこにも出ない。単一マシンの運用ならこちら
   - **commit するか**: 「commit する」「commit して push する」「commit しない」
     - commit するなら、先に `git check-ignore -v issues/README.md` で ignore されていないかを見る (されていたら commit せずに伝える)。
       作ったファイルだけ (`issues/README.md`、作ったなら `issues/next/.keep`) を `git add` してから、同じものを pathspec に並べて heredoc で commit する
       (`git commit -F - -- issues/README.md [issues/next/.keep]`。作っていないパスを並べると pathspec が外れて commit されない)。終わったら `git log -1 --stat` で入ったことを確かめる
     - 「commit しない」なら untracked のまま置いて終える
5. **報告する**: 作ったパスと commit の有無に加えて、次を伝える
   - コピーした README は **dotfiles 向けに書かれている**。その repo に合わせて書き換える必要がある
     - **他の repo からも呼べる道具** (パスを `~/dotfiles/scripts/…` に書き換えて残す): 採番の `issue_next_number.sh`、
       claim の `issue_claim.sh` (Go で dotfiles の `issue-claim` を建てる)、完了の `issue_done.sh` (`epic/<name>/done/` も扱う)。
       glogx の issues viewer (`i` キー) もその repo の `issues/` を読む
     - **dotfiles にしか無いもの** (消すか、その repo の同等物に置き換える): `tests/issues/` の検査、`githooks/pre-push`、
       pro-con 用の `issue_number_drafts.sh`、`audit-log`、経緯の節
   - 次のセッションから効く hook: 規約の注入 (`issue-rules-inject.sh`。本体・claim・反証レビュー・検証レポートの移し方)、
     `human` の期限 (`human-tasks-due.sh`)、未決着の `retro` (`retro-open.sh`)、本文の更新漏れ (`issue-progress-check.sh`)、
     claim を使うなら未 push の claim (`next-claim-push.sh` / `next-claim-unshared.sh`)
   - 状態ディレクトリ (`done/`・`pending/`・`waiting/`) と `epic/<name>/` は作っていない。最初に使うときに作る
     (epic は group 名と同じ番号の親 issue と同時に作る。規約は注入される)
