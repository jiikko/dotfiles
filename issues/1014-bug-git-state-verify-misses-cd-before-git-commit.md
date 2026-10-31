# 1014 (bug): git-state-verify が `cd <dir> && git commit` の dir を見ず、hook の cwd の repo の state を出す

> 🚨 **担当中: air5 (glogx)**（2026-10-31〜）

起票日: 2026-10-31

## 概要

PostToolUse の `_claude/hooks/git-state-verify.sh` は、git commit / push の直後に「実際の git state」を
「成功報告の前にこれで検証すること」として注入する。ところが `cd <dir> && git commit` の形では `<dir>` の repo を見ず、
hook のプロセスの cwd (Claude の session の cwd) の repo の state だけを出していた。worktree で作業する規約
(`.claude/rules/worktree-per-session.md`・obaket の同名 rule) どおりに打つと、検証用に出る state が別の repo のものになる。

## 詳細

### 再現 (2026-10-31)

session の cwd が `~/src/my-products/apps/obaket/macOS` のセッションで、次を打った:

```sh
cd /Users/koji/dotfiles-wt-1014-git-state-cd-202928 && ... git commit ... && git push origin HEAD:master
```

注入された state の先頭は `検査した repo: /Users/koji/src/my-products/apps/obaket` で、last commit も obaket の commit だった。
dotfiles の commit と push の結果はどこにも出ていない。同じ日の obaket の worktree (`apps/obaket-wt-…`) での commit・push でも、
毎回共有 checkout (`apps/obaket`) の state が出た。

### 原因

- hook のプロセスは、コマンドの中で cd した先ではなく session の cwd で起動される。Bash ツールの cwd も、working directory の外へ cd すると
  次の呼び出しで戻る (worktree は repo の外の兄弟ディレクトリに作る)
- 検出側の `_claude/hooks/lib/git_cmd_detect.sh` は「`cd X && git commit` は行き先として拾わない (cwd を見る)」と決めていた。
  この判断は「hook の cwd = cd した後の shell の cwd」を前提にしていたが、その前提が成り立たない

## 対応方針

- git より前に出る `cd <dir>` を追い、`-C` と合わせた行き先を返す。cwd のブロックは置き換えず、追加のブロックとして出す
  (行き先で cwd を置き換えると、本文に書いた `cd` / `-C` が「どの repo を検証するか」を乗っ取れる。lib の冒頭の脅威モデル)
- 同じコマンドの中の単純な代入 (`W=/x` / `export W=/x`) と `$HOME`・`~` だけを展開する。展開できない行き先は字面のまま積み、
  hook が「判定不能: 行き先が見つからない」と出す (空に畳むと cwd を見たことになる)
- `( … )`・`$( … )`・バッククォートの中の cd は外の git に効かないので、閉じで開く前の行き先へ戻す

## 関連ファイル

- `_claude/hooks/git-state-verify.sh`
- `_claude/hooks/lib/git_cmd_detect.sh`
- `tests/claude/test_git_state_verify.sh`

## 進捗

- [x] cd の行き先を追う実装とテスト (commit「fix(hooks): git-state-verify が cd の行き先の repo も見るようにする (1014)」)
  - テスト 55 件緑。変異 3 本で red を確認: cd を追わない / サブシェルの閉じで戻さない / 知らない変数を空に展開する
- [x] 敵対的レビュー (codex は 2026-11-04 まで使用上限のため opus のサブエージェント)。cwd のブロックが消える形と偽の全クリアは作れなかった。
  再現した指摘への対応 (commit「fix(hooks): git-state-verify の cd 追跡を敵対的レビューの指摘で直す (1014)」):
  - 直した: `if cd /a; then …` / `builtin cd` / `command cd` を拾う、`cd -- /a` の `--` を読み飛ばす、サブシェルの閉じで代入も戻す、
    `${W}` を分割器の `{` で割らない、引用の中の `~` と単一引用の `$HOME` を展開しない (`-C` の値も同じ関数で剥がす)、
    代入と参照のたびの fork / awk をやめる
  - テストが見逃した変異 7 本 (`~` / `$HOME` / 引数なしの cd / `cd -` / 入れ子の閉じ / バッククォートの開き / `export`) を、lib を直接呼ぶ表のテストで捕まえる。
    今回の修正分を含む変異 13 本で red を確認 (`/bin/bash` 3.2 と bash 5 で 77 件緑)
  - 直さずに冒頭コメントへ書いた (どれも行き先が 1 つ増えるか減るだけで、cwd のブロックは必ず出る): パイプの左の cd を持ち越す、
    `cd a || cd b` は b だけを残す、`case` の `)` をサブシェルの閉じと読む
  - 性能 (`/bin/bash` 3.2、2 回ずつ): 代入と参照 100 組 (2.2 KB) は 0.13〜0.20 秒 → 0.23〜0.24 秒。git を含む 30 KB (echo 1000 行) は
    12.4〜12.6 秒 → 12.9〜13.0 秒。12 秒の大半は修正前からある分割器の文字単位のループ
- [ ] push して `~/dotfiles` へ pull し、本物の session で注入される state を見る
