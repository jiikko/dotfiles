# 1029 (retro): issues viewer の u・all-audit-on-diy・issues viewer の監査・serialrun の flake (2026-11-04)

起票日: 2026-11-04

## 概要

1 セッションで 4 つ: issues viewer の一覧の `u` を pull に (5385ff22f)、skill `all-audit-on-diy` の新設 (12ffc38aa)、
それを使った issues viewer の全観点の監査 (1026)、root の `make test` で出た serialrun の flake の対応 (1028)。踏んだ所だけを書く。

## どこで踏んだか

1. **挙動を変えたのに、古い挙動を assert するテストを 1 本見落とした**。`u` の変更で、影響するテストを
   `grep -rn '"u"' --include='*_test.go' . | head` で探したが、`head` の 10 行で打ち切られ、`internal/issuesview` の
   `TestIssuesViewerListModeUReturnsReason` が出力から落ちていた。root の `make test` で初めて赤になり、往復が 1 回増えた
2. **flake の並列の再現で、自分の起こし方が 48 回全部を偽の赤にした**。`( sr.test … ) &` で非対話のシェルから起こしたので、
   テストは SIGINT を無視した状態で始まり、子の `sh` は INT を trap できなかった。`verify-execution-not-just-exit-code.md` の
   「プロセス・シグナルの観測」に同じ形が既に書いてあったが、思い出さずに走らせた。失敗時の子孫の一覧の形 (serial-run が 10 秒後も生きている)
   が元の失敗 (serial-run は猶予の後に終わっている) と違うことで気づいた
3. **最初に書いた回帰テストが、変異の 1 本に対して緑だった**。`loadAllMeta` の警告は「件数 + 先頭の 1 件」の 1 行に畳むので、
   警告にしてはいけないファイル (ENOENT) を一覧の後ろに置いたテストでは、誤って数えても文に出なかった。変異 (ENOENT も警告にする) で気づき、
   そのファイルを先頭に置いた
4. **1 つのファイルに独立した 2 つの修正を入れ、pathspec の commit で両方が 1 commit に入った** (警告の追加と、親の選び方の寄せ)。
   commit message は片方しか書いておらず、自分の未 push の commit の message を直した
5. **監査の記録 (1026) に「食い違わない」と書いた論証が誤りだった**。viewer の親行が段階・タブで変わる経路を見落とし、反証レビューで直した
   (見た目だけで実害なし)

## 次に効きそうな改善

- **A (1 から)**: 挙動を変えるときに、その挙動を assert しているテストを探す検索は、`head` で切らずに件数を先に出す。
  切ると「無い」と「見えていない」が区別できない。→ 切り出し先: 既存ルール `verify-execution-not-just-exit-code.md` の
  「検証コマンドの出力を `tail` / `head` / `grep` で削って読まない」の項に、影響範囲の検索 (呼び出し元・古い挙動を assert するテスト) も同じ、と 1 行足す
- **B (3 から)**: 複数の要素を 1 つに畳む出力 (件数 + 先頭の 1 件・上位 N 件) を assert するテストでは、退行で増える要素を、畳んだ後にも見える位置に置く。
  後ろに置くと、誤って数えても出力が変わらない。→ 切り出し先: 既存ルール `mutation-verify-new-tests.md` のチェックリスト
  (`rules-checklists/mutation-verify-new-tests.md` の「assert と期待値」) に 1 項足す
- **C (4 から)**: 1 つのファイルに独立した 2 つの変更を入れるなら、1 つ目を commit してから 2 つ目を書く (pathspec はファイル単位で、混ざった変更を分けられない)。
  → 切り出し先: 既存ルール `commit-with-pathspec.md` に 1 行足す
- 却下 (2 から): 規範は既に `verify-execution-not-just-exit-code.md` にある。失敗の形を元の失敗と比べて気づけたので、足す規範が無い
- 却下 (5 から): 「不在・一致の主張は反証にかける」は既存の規範 (`~/.claude/CLAUDE.md`「レビュー方針」) が働いて捕まえた

## 進捗

- [x] A・B・C をユーザーの指示 (2026-11-04「全部入れて」) で既存の rule に足した (rules: retro 1029 の A・B・C を既存の rule に足す)
