# 867 (cleanup): ruby-refs-index の measure.rb のコメントを今の production の rg の引数に合わせる

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-22〜）

起票日: 2026-10-22

## 概要

`nvim/ruby-refs-index/measure.rb` の `ripgrep_hits` の冒頭コメントは「production (telescope.grep_string) は **ファイルタイプを絞らない**。
ここで --type ruby を付けると … 過小評価する」と書いている。しかし今の production の `<C-k>` は
`nvim/lua/dotfiles/lsp.lua` の `M.ripgrep_reference_args` で ruby (rake・jbuilder を含む)・erb・haml・slim に絞っている
(issue 344)。コメントは逆のことを根拠に「絞るな」と言っている。同じ前提の記述が
同じファイルの上の方にもう 1 か所ある (「下の ripgrep_hits は production と土俵を合わせるため type を絞らない。」)。

`nvim/lua/dotfiles/lsp.lua` の 2026-09-09 の追試値の注記 (「production と同条件 = ファイルタイプを絞らない」) は、日付つきの当時の実測で、
すぐ下で今の絞り込みを説明しているので対象外とする。

同じディレクトリの README は既にこのずれを書いている (「`measure.rb` の rg の基準線はこれに合わせていない」「sidecar は作らないと
決めた (issue 344) ので、`measure.rb` は直さずに残している」)。直さない判断は README 側にあり、コード側のコメントだけが古い前提のまま。

## 対応方針

- 2 か所のコメントを「当時の production は絞っていなかった。今は `M.ripgrep_reference_args` で絞っているが、measure.rb は合わせていない
  (README の該当節)」に書き換える。測り直すなら基準線を `M.ripgrep_reference_args` と同じ引数にする、の一文を添える
- 引数そのものを合わせるかは README の判断 (直さずに残す) に従い、この issue ではやらない

## 関連ファイル

- `nvim/ruby-refs-index/measure.rb` の `ripgrep_hits` と、その呼び出し元の「type を絞らない」のコメント
- `nvim/lua/dotfiles/lsp.lua` の `M.ripgrep_reference_args`
- `nvim/ruby-refs-index/README.md` の「production の `<C-k>` はその後 Ruby 系のファイル … に絞った」の段落

## 進捗

- [ ] コメントを直す
