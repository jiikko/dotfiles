# 580 (feat): `bin/mutate-verify` に、一覧から複数の変異を順に当てて結果を表にする形を足す

起票日: 2026-09-29

## 概要

`bin/mutate-verify` は 1 回の起動で 1 つの変異を当てる。1 つの変更に変異を何本も当てるときは、呼び出し側が並べ役の
スクリプトを毎回書いている。issue 579 のセッションでは約 50 本を当て、並べ役を `tmp/` に 4 回書き直した
(bash 3.2 で `source <(...)` が関数を読まず、1 回は並べ役が 1 本も走らないまま終わった)。
`new-tool-requires-entrypoint-docs.md` の「使い捨てを 2 回目に書いたら道具にする」に当たる。

## 詳細 (今の使い方)

並べ役が毎回持っていたもの:

- 1 変異 = (名前, `--file`, perl の置換式, 検証を走らせるディレクトリ, `-run` の絞り込み, `--expect`)
- 変異ごとに `--apply` / `--syntax` / `--verify` / `--baseline-expect` / `--expect` を組んで呼び、ログを `tmp/mut-<名前>.log` に残す
- 終わったら各ログから rc と `--- FAIL:` の行を集めて 1 行ずつ出す (狙ったテスト以外が落ちていないかを目で見るため)

## 対応方針

- 一覧 (1 行 1 変異。区切りと引用の規則は実装で決める) を受けて順に当て、変異ごとに `名前 / rc / 落ちた検査` の表を出す
- 変異ごとの guard (baseline green・当たったか・構文・誤ファイル・`--expect` の帰属) は今の 1 本ずつの判定をそのまま通す
  (一覧の形のために判定を別実装しない)
- 🚨 **既定は「外側のループが変異ごとに今のスクリプトを 1 回ずつ起動する」形にする**。今の `bin/mutate-verify` は
  worktree の作成と破棄が 1 プロセスに 1 回で、後始末は単一の EXIT trap (`WTS_ON_CLEANUP`)、終了は `exit N` で
  判定を返す構造になっている。1 プロセスの中で複数の変異を回すと、この終了の流れと trap を組み替えることになる
  (反証レビューの指摘)
- baseline を 1 回で済ませて使い回す案は、上の組み替えを伴うので別の判断にする。採るなら、前の変異を戻してから
  次を当てる手順と、同じ木の状態で測っている根拠を書く
- 入口のドキュメント (スクリプト冒頭の usage と `mutation-verify-new-tests.md` の `bin/mutate-verify` の段落) を同じ変更で更新する

## 関連ファイル

- `bin/mutate-verify`
- `_claude/rules/mutation-verify-new-tests.md` (`bin/mutate-verify` を案内している段落)
- issue 579 (起点の retro。提案 C)

## 進捗

- 2026-09-29 起票 (issue 579 の提案 C。ユーザー指示)。未着手
- 2026-09-29 反証レビュー (sonnet 1 本): 1 起動 1 変異・入口の段落・引用・重複 issue の無さは反証できず。採用 (P2): 今の
  スクリプトが 1 プロセス 1 worktree・単一 EXIT trap・exit で判定を返す構造であることを対応方針に書き、既定を
  「変異ごとに今のスクリプトを起動する」にした
- 2026-09-29 実装 (commit「feat(bin): mutate-verify-list — 一覧の変異を 1 本ずつ mutate-verify に渡して表にする (issue 580)」):
  - `bin/mutate-verify-list [--log-dir DIR] LIST`。LIST は `defaults` / `mutant <名前> <mutate-verify の引数>` の 2 関数で書く
    bash の断片。変異ごとに `mutate-verify --name <名前> …` を 1 回ずつ起動し、名前 / rc / 意味の表を出す (判定は mutate-verify のまま)
  - LIST は `set -e` の**隔離したサブシェル**で読み、完了の目印と記録ファイル (名前 + printf %q した引数) で受け取る。検査
    (名前の字・重複・mutant 側の `--name`) は親が持つ。途中の書き損じ・途中の exit・LIST の set / cd / export は当てる側に漏れない
  - ログの置き場が repo の中で ignore されていなければ拒否 (中だと全部 rc=9)。rc=9 が出たら残りを当てずに「未実行」と出す
  - 入口: `mutation-verify-new-tests.md` の `bin/mutate-verify` の段落と、`bin/mutate-verify` の「使い方」に使い分けを 1 行
- 敵対レビュー (opus 2 周):
  - 1 周目: P1 3 件 (途中の書き損じを見落とす / LIST の `exit 0` が偽の緑 / LIST の `set -e` で表ごと死ぬ)・P2 2 件 (`--name` で
    名前が食い違う / repo の中のログの置き場で全部 rc=9)・P3 (引数 0 個で空の引数・cd・names の直接操作)。親での source を
    サブシェルへ隔離して P1 をまとめて塞ぎ、P2 は拒否、P3 の空の引数は直した。names の直接操作・die の再定義は「LIST は
    信頼する」脅威モデルの内側として受容
  - 2 周目: P1 (`( set -e; … ) || die` は `||` の左なので bash 5 では set -e が無効。self-test も道具を shebang の bash 3.2 でしか
    走らせていなかった) → rc を後で見る形に直し、self-test は道具を `/bin/bash` と PATH の bash の両方で起動する。P2 (内部の
    変数名を LIST が上書きすると後ろが黙って消える) → `readonly`。P3 (大文字小文字違いのパスで repo の中の判定をすり抜ける) →
    git に聞く。P3 (LIST の export が効かない) → doc に書いた。3 周目は省略: 2 周目の修正はどれも変異で直接確かめた
- 検証: self-test は bash 3.2.57 / 5.3.20 の両方で green。既存の `tests/bin/test_mutate_verify.sh` (41 ケース) も green。
  変異 15 本をこの道具自身で当て、15 本とも想定どおり red (1 本はパターンが古くて当たらず、道具が rc=4 と報告したので当て直した)
- 残タスク: なし

