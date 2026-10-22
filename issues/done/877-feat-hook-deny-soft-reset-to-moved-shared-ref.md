# 877 (feat): 自分の起点より先へ進んだ共有の ref への `git reset --soft` を hook で止める

起票日: 2026-10-22

## 概要

`git reset --soft origin/master` を、自分の commit の起点が `origin/master` より古いまま打つと、index が「新しい origin/master + 自分の木
(古い起点)」になり、他のセッションの commit を打ち消す差分が入る。気づかずに commit すると上流の変更を巻き戻す。
2026-10-22 に 2 つのセッションで踏んだ (retro 875 の踏んだ所 1・retro 876 の踏んだ所 4。どちらも commit の前に気づいた)。

規範は `_claude/rules-checklists/commit-with-pathspec.md` (「squash / 付け替えの soft reset の起点は…先に新しい起点の hash へ rebase」) に既にあり、
`commit-with-pathspec.md` は soft reset の前にこのチェックリストを Read せよと言うが、2 回とも読まずに打った。読むことに頼らず、PreToolUse(Bash) の
hook で機械的に止める。retro 875 は「字面の判定で正当な使い方を止めないか未確認」として hook の提案を保留し、876 は規範の追記を
「読まなければ効かない」として却下した。この issue はその保留を引き取り、字面でなく実行時の祖先の判定で止める。

## 対応方針

- `_claude/hooks/` に PreToolUse(Bash) の hook を 1 本足す (形は `deny-piped-push-then-destroy.sh` に倣う: 冒頭に脅威モデルと検出しない形を書く、
  `_claude/settings.json` の command は実体パス、テストは `tests/claude/`)
- 字面だけで止めない: soft reset の行き先について hook の中で `git merge-base --is-ancestor <行き先> HEAD` を打ち、祖先なら通す
  (rebase を済ませた正しい手順を止めない)。判定できない形は止める側に倒す (詳細は次節)
- deny の文で「先に `git rebase <行き先>` してから、その行き先へ soft reset する (`commit-with-pathspec.md` のチェックリスト)」を案内する
- 変異で red: 祖先の判定を外す (正しい手順まで止める) / 判定を常に真にする (事故の形を通す)。opus の敵対的レビューを最終ゲートにする
  (自作の安全機構。`adversarial-review-own-safeguards.md`)

## 実装で決めたこと

- 対象は remote-tracking ref に限らず、`--soft` (省略形 `--s` / `--so` / `--sof` を含む) の行き先すべて。HEAD からの相対 (`HEAD~N` / `@~N`) は必ず祖先なので見ない。
  行き先が commit に解決できない・dir が repo でないときは通す (`git reset` 自体が失敗する)
- 祖先でない行き先のほかに、判定できない形も止める: 行き先が変数・コマンド置換 (`$(git merge-base … HEAD)` を除く)、引用符が閉じないまま終わった
  所から後ろの `reset … --soft`、下の 2 つ
- dir が読めない形 (`cd $wt` / `cd -` / `popd` / 括弧・パイプの中の cd / `--git-dir` / `GIT_DIR=`) は**止める** (判定不能を通すと、`cd $wt && git reset --soft origin/master` という
  一番ありそうな形が素通りする)。絶対パスの cd / `-C` か、直前の `git merge-base --is-ancestor <行き先> HEAD &&` で通る
- 同じコマンドの中の fetch / pull / `remote update` / `pull_main_checkout.sh` の後ろの reset は止める (hook の時点の ref は実行時と違う)
- && で繋いだ直前の `git rebase <行き先>` / `git merge-base --is-ancestor <行き先> HEAD` は「実行時に確かめている」として通す (`;` / `||` を越えたら忘れる)
- 区切りは awk で 1 回だけ走って分ける (bash の 1 文字ずつの切り出しは 2 乗の時間で、2 万字で 10 秒の timeout を越えて素通りした)。heredoc の本文は
  `_claude/hooks/lib/hook_io.sh` の `hook_strip_heredocs` で落とす (`deny-piped-push-then-destroy.sh` から lib へ移して共有)
- 判定規則・検出しない形・受ける偽陽性の正本は hook 冒頭のコメント

## 未確認

- 敵対的レビューの各周の指摘数は、セッション内のレビュー担当の報告から書いた (報告そのものはログに残していない)

## 関連ファイル

- `_claude/hooks/deny-soft-reset-to-non-ancestor.sh`・`tests/claude/test_deny_soft_reset_to_non_ancestor.sh`
- `_claude/hooks/lib/hook_io.sh` (`hook_strip_heredocs`)・`_claude/hooks/deny-piped-push-then-destroy.sh`
- `_claude/settings.json` の PreToolUse (timeout 10)
- `_claude/rules-checklists/commit-with-pathspec.md` (規範の正本。hook が止めることを書き足した。2 行)
- issues/done/875・876 (起点の retro)

## 進捗

- [x] hook の実装とテスト — 96 ケース × (PATH の bash 5 と /bin/bash 3.2) = 192 件が緑。`make test-lint` rc=0、`test_deny_piped_push_then_destroy.sh` 94 件が緑 (heredoc の除去を lib へ移した後)。
  性能: 8000 行の入力の判定が /bin/bash で 2.3 秒 (区切りごとの `$( )` の fork を除く前は 4000 行で 19 秒、1 文字ずつの切り出しのころは 2 万字で 10 秒超)
- [x] 変異で red を確認 (`mutate-verify`、`--expect` で想定のケースへ帰属)。計 31 本がすべて red:
  - 2 周目まで (9 本): 祖先の判定を常に真 / 常に偽、`;` を越えて確かめを持ち越す、`!` の反転を無視、括弧の中の cd を無視、分からない dir の確かめを覚える、
    heredoc を落とさない、末尾の `}` を落とす、`rebase -X` の値を行き先と読む
    (heredoc のケースは初回は緑で、本文の確かめが行末の `;` で消えて変異と区別できていなかった。本文を `cd /` に替えて red にした)
  - 3 周目 (8 本): 変数の行き先を通す、merge-base の例外を外す、`--keep-base` を覚える、`||` の後を覚える、wrapper の後ろの fetch を見ない、`env -C` を無視、
    `command -p` を語と読む、コメントを捨てない
  - 4 周目 (9 本): merge-base の例外の dir の条件を外す、HEAD の条件を外す、awk のコメントの切り捨てを外す、空の区切りを出す、引用符を追わない、
    `env -C<dir>` を読まない、パイプの中の cd を追う、`--keep-b` の省略形を外す、`$( )` の中の fetch を見ない
  - 5・6 周目 (5 本): 閉じない引用符の判定を外す / 中身を見ずに止める / 字面をばらばらに探す、merge-base の相手の数の条件を戻す、行末の `\` の継続を外す
- [x] 敵対的レビュー (opus) — 6 周。6 周とも P1 (普通に書く形で事故が通る) は 0 件。各周の指摘は再現してから直した (3 周目: P2 3・P3 4・偽陽性 1、4 周目: P2 1・P3 4、
  5 周目: P2 2・P3 3 (4 周目の「行をまたいで引用符を追う」が bash と読みのずれる形で後ろを飲む → 閉じない引用符の中の soft reset を判定不能として止める)、
  6 周目: P2 1 (その判定の誤 deny) ・P3 2)。直さず hook 冒頭の「検出しない形」「受ける偽陽性」に書いたもの: fetch 以外で ref を動かす形 (`git branch -f` / `gh repo sync`)、
  symlink を含む `..`、読み違いが 2 回で打ち消し合う引用符、継続の後ろの heredoc、取り切れない heredoc (識別子でない区切り語・1 行に 2 つ) の本文の誤 deny。
  6 周目で P1 が出ず、脅威モデル (Claude の書き間違い) と検出しない形を冒頭に書いたので、ここで閉じる (adversarial-review-own-safeguards の §8)
- [x] 反証レビュー (この issue の本文、sonnet) — 件数・名前・retro の参照・「実装で決めたこと」と hook の一致は反証できず。指摘 7 件のうち
  6 件 (対応方針に残っていた起票時の記述 3 件、未確認節の古い書き方、チェックリストの行数、875 の保留を引き取った経緯) を直した。
  残る 1 件 (各周の指摘数の出典がログに無い) は未確認節に出典を書いた
- [x] push と反映 — `feat(hooks): HEAD の祖先でない行き先への git reset --soft を PreToolUse で止める` を master へ push し、`pull_main_checkout.sh` で ~/dotfiles へ取り込んだ。
  push の前の `make test` は `test-go-lint` の src/glogx だけが赤だった (go のビルドキャッシュに、消えた worktree `/Users/koji/wt-874` のパスが残っていて gofmt が開けない)。
  GOCACHE と GOLANGCI_LINT_CACHE を新しい dir にした `make lint` は rc=0 で通った (この変更は Go を触っていない)。ほかは `[ok]` 140 本 (新しいテストを含む)
- [x] headless の新規セッション (`claude -p --model haiku`、Bash を許可) での確認 (2026-10-23)。どちらも使い捨ての repo で `git reset --soft origin/master` を打たせた:
  - origin/master が HEAD の祖先でない repo: hook が deny した (「行き先が HEAD の祖先ではありません (merge-base --is-ancestor rc=1 …」)。HEAD は動いていない
  - 祖先の repo: reset が通り、HEAD が origin/master へ動いた
