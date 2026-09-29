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
- baseline は変異ごとに測り直すか 1 回で済ませるかを実装で決める。済ませるなら、同じ木の状態で測っている根拠を書く
- 入口のドキュメント (スクリプト冒頭の usage と `mutation-verify-new-tests.md` の `bin/mutate-verify` の段落) を同じ変更で更新する

## 関連ファイル

- `bin/mutate-verify`
- `_claude/rules/mutation-verify-new-tests.md` (`bin/mutate-verify` を案内している段落)
- issue 579 (起点の retro。提案 C)

## 進捗

- 2026-09-29 起票 (issue 579 の提案 C。ユーザー指示)。未着手
