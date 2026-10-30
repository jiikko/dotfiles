# 1004 (retro): dupscan・treefiler の監査と 981〜997 の対応の振り返り

起票日: 2026-10-30

## 概要

`/audit` を src/dupscan と src/treefiler に 22 観点の直接実行で回し (984・992)、起こした issue (981・982・991・996・997。983 は取り下げ) を同じセッションで直した。
ここには、どこで踏んだかと、他の作業でも効く改善だけを書く。

## どこで踏んだか

1. **push の後に CI を 2 回赤くした** (どちらも直した commit で緑に戻した)
   - `8449137e9` (991): テストのコメントの「(issue 991)」を `test_check_comment_provenance` が落とした
   - `a84449964` (997 の横展開): テストからしか呼ばれない包みの関数を `test-unused-excluding-tests` が落とした
   - どちらも push の前に回したのは `make -C src/<module> lint test` だけで、repo 全体の検査 (root の `make test` の `test-lint`・`test-unused-excluding-tests`) を回していなかった。
     メモリ `make-lint-test-before-commit` が「変更が Go だけなら module の lint / test でも可」と許していたのに従った。repo 全体の検査は module の Makefile から呼ばれないので、この許しは成り立たない
2. **rebase が衝突で止まったのに push が走った** (981・982 のコードの 2 commit だけが先に載った。中身は検証済みで壊れてはいない)。`git rebase …; echo; git push` を `;` で繋いでいた。
   その後は `r=$?; [ $r -eq 0 ] || exit 1` で止めた
3. **監査で、既に決めて README に書いた仕様を bug として起票した** (983。paths の除外の levels が L1m を粗い側に数えない形は、973 で決めて README の「除外の設定」に書いてあった)。反証レビューで見つかり取り下げた
4. **同じ形を横に洗わずに起票した** (991 は確認の板だけを書き、同じ「描けない大きさで描画を捨てるのにキーは取る」形の設定の板を反証レビューが見つけた)
5. **Go の変異を「文を消す」形で当て、ビルドの失敗 (第 3 の結果) を 2 回出した** (消した呼び出しの `bytes` の import・`contains` の関数が未使用になる)。`false &&` で包む・`if false { … }` で囲む形に当て直した (ほかに 1 回は自分の置換の式の誤り)
6. 番号の採番を 3 桁前提の `sort | tail -1` でした (今回は 1000 未満で衝突しなかった。手順の直しは dotfiles-9a の 1001)

## 提案

| # | 気づき (一般形) | 切り出し先 | 状態 |
|---|---|---|---|
| A | push の前の検査は、触った module の Makefile ではなく、**その変更で CI が起こす workflow の検査の和**で選ぶ (repo 全体の lint・未使用の検査は module の lint に入らない)。許しの例外は「その検査が module の target から呼ばれる」と確かめたときだけ | メモリ `make-lint-test-before-commit` の例外を直す (ルールは既にある: `verify-execution-not-just-exit-code.md` の「関係しそうな suite だけを選んで回すのも代用」。踏んだのはメモリの許しの方) | 対応済み |
| B | 監査の発見を起票する前に、その挙動が**既に決めた仕様でないか**を、README・done の issue・コードの「採らない / 見送り」のコメントで、同じ symbol・挙動の語で grep する | audit skill の「監査の姿勢」に 1 項 (`verify-design-intent-before-refactor.md` の 1 は refactor の提案向けで、不具合の起票には発動しない) | 対応済み |
| C | Go (未使用の import・変数・関数をコンパイルが拒む言語) の変異は、文を消すのでなく**評価を止める形** (`false && …`・`if false { … }`) で当てる。消すと参照が消えた import・関数でビルドが落ち、第 3 の結果になる | `_claude/rules-checklists/mutation-verify-new-tests.md` (手で当てる変異のチェックリスト) | 対応済み |
| — | rebase と push を `;` で繋がない | 却下: 既存の `commit-with-pathspec.md`「push を再試行するなら、合間の pull --rebase の rc を見る」で足りる。守れなかっただけ | — |
| — | 起票の前に同じ形を横に洗う | 却下: 既存の CLAUDE.md「直したバグは同じ間違いが別の場所にもある前提で grep する」(失敗の仕組みで書く) で足りる | — |
| — | 採番の 3 桁前提 | 却下: 1001 (dotfiles-9a) で扱っている | — |

## 進捗

- A: メモリ `make-lint-test-before-commit` の「Go だけなら module の lint / test でも可」を外し、少なくとも `make test-lint test-unused-excluding-tests` を回すと書いた (メモリは repo の外。MEMORY.md の索引の行も直した)
- B: commit「docs(audit): 起票の前に、既に決めた仕様でないかを README・完了した issue・採らないのコメントで確かめる (retro 1004)」。
  skill の整合チェック 3 観点 (①skill の中: 前後の箇条と矛盾しない ②外: 973 は `issues/epic/932/done/` にあり、`issues/done/` だけでは漏れる → epic の done を足した ③使う人: どこを探すかの例が無い → grep の例を足した)。
  直した版で再確認。`bin/skill-eval audit` → `PASS audit score=1 delta=1 cases=1/1 cost=$0.2635382`
- C: 同じ commit で `_claude/rules-checklists/mutation-verify-new-tests.md` の「red の読み方」に 1 項
- 残課題なし (提案は全部対応か却下)
