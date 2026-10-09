---
name: start-issues-ops
version: 1.0.0
description: repo の root に issues/ を作り、dotfiles の issues/README.md を持ち込んで issue 運用を始める。commit するかはユーザーに聞く。「issue運用を始めて」「issuesディレクトリを作って」「start-issues-ops」で発火。
---

# start-issues-ops — repo で issue 運用を始める

repo の root に `issues/` を作り、`~/dotfiles/issues/README.md` をコピーする。
`issues/` が在る repo では、SessionStart hook (`_claude/hooks/issue-rules-inject.sh`) が共通規約
(`~/dotfiles/_claude/issue-rules.md`) を注入するようになる (効くのは次のセッションから)。

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
4. **commit するかを AskUserQuestion で聞く** (repo ごとに運用が違うので既定を決めない)。選択肢は
   「commit する」「commit して push する」「commit しない」
   - commit するなら、先に `git check-ignore -v issues/README.md` で ignore されていないかを見る (されていたら commit せずに伝える)。
     `git add issues/README.md` してから pathspec と heredoc で commit する (`git commit -F - -- issues/README.md`)。
     終わったら `git log -1 --stat` で入ったことを確かめる
   - 「commit しない」なら untracked のまま置いて終える
5. **報告する**: 作ったパスと commit の有無に加えて、次を伝える
   - コピーした README は **dotfiles 固有の内容**を持っている (`scripts/issue_done.sh`・`tests/issues/`・glogx の viewer・
     `audit-log`・dotfiles の経緯)。その repo に無い道具の記述は、その repo の採番・完了の手順に書き換えるか消す必要がある
     (共通規約は hook が注入するので、README に写さない。`issue-rules.md` の冒頭)
   - `done/` などの状態ディレクトリは作っていない (空のディレクトリは git に載らない。最初に使うときに作る)
