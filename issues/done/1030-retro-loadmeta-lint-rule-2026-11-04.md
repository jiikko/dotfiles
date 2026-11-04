# 1030 (retro): issue の読み込みの失敗を捨てる形を止める lint の規則 (2026-11-04)

起票日: 2026-11-04

## 概要

[1029](1029-retro-issues-viewer-u-audit-serialrun-flake-2026-11-04.md) の後の作業: glogx の ruleguard に `issueReadErrorNotDiscarded` を足した
(20b77f6a6・3f848d130)。踏んだ所だけを書く。

## どこで踏んだか

1. **root の `make test` を、Bash の 1 回の呼び出しの中で素の `&` で背景に回した**。呼び出しが返った後も走り続けたが、ハーネスの完了の通知が来ず、
   ログに結果の行が出るまで待つ見張りを別に立てる羽目になった。止めようとした kill は自動承認に拒否され、最後まで走らせた。
   しかも素の `&` の子は INT を無視して始まる (同じセッションで 1028 の調べ中に確かめた形)
2. **規則のコメントに issue 番号を書き、root の `make test` の `test-comment-provenance` で初めて落ちた**。書いた直後に知らせる hook
   (`comment-provenance-on-edit.sh`) は matcher が `Write|Edit` で、規則を `cat >>` (Bash) で書いたので動かなかった。往復が `make test` 1 回分増えた
3. 規則の最初の版が `m.Import` を欠いて読み込みに失敗した (全ファイルに execution error)。lint が赤になって即座に気づいたので損は小さい
4. 変異の 1 本がコンパイルできない形 (戻り値のある関数の中の素の `return`) だった。lint の出力の compile error で気づいた (規範は `mutation-verify-new-tests.md` の手順 1.5 にある)
5. red team が `body, _ := iss.ReadBody()` (`:=`) の取りこぼしを見つけた

## 次に効きそうな改善

- **A (1 から)**: 長い検証は、道具 1 回の中の素の `&` で背景に回さず、ハーネスの背景実行で起こす。→ 既存ルール `verify-execution-not-just-exit-code.md` の
  「ツールの実行時間上限を超えうるコマンド」の項に 1 行足した
- **B (2 から)**: Bash (`cat >>`・python) で書いたコメントには Write / Edit の hook が掛からないので、commit の前に検査を自分で打つ。→ dotfiles の
  `CLAUDE.md` の「何をどこに書くか」のコメントの経緯の語の項に 1 行足した (`~/.claude/CLAUDE.md` の「Bash で編集するなら先に CLAUDE.md を Read する」と
  同じ根だが、こちらは hook の話なので repo の CLAUDE.md に置く。global の常時読み込みの予算が残り約 660 字のため)
- 却下 (3 から): `make lint` が赤で即座に知らせた。足す規範が無い
- 却下 (4 から): 規範は既にある (手順 1.5)。今回も compile error を red と読まずに当て直せた
- 却下 (5 から): 自作の検査の red team (`adversarial-review-own-safeguards.md`) が働いた結果

## 進捗

- [x] A・B をユーザーの指示 (2026-11-04「retro を書いて対応やって」) で反映した (この issue と同じ commit)
