# 874 (chore): `.golangci.yml` の 23 個の写しのずれを止める (検査か生成)

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-22〜）

起票日: 2026-10-22

## 概要

`src/*/.golangci.yml` が 23 個あり (2026-10-22 に `find src -name .golangci.yml` で数えた)、共通の塊 (`allow-parallel-runners` とその理由のコメント・
衛生系とバグ検出系の linter の一覧・govet の analyzer) を写しで持つ。linter を足す・版を上げるたびに 23 回同じ編集をしている
(872 のセッション: ba03b908 で linter の追加、0fa4cdaa で v2.5.0 → v2.15.0)。写しがずれても検査は 1 か所しか見ていない。872 の残課題から切り出した。

## 詳細

- 行数は 60〜248 行に散る (2026-10-22 時点。最小は process_supervisor、最大は glogx の 248・treefiler の 215。共通の塊に、モジュールごとの除外と
  depguard などが足されている)
- `.golangci.yml` の中身を見る既存の検査は 3 本で、どれも共通の塊の一部だけを見る:
  `tests/scripts/test_golangci_parallel_runners.sh` (全部の `allow-parallel-runners: true`)・`test_tuikit_consumers_aligned.sh` (tuikit を使う module の
  forbidigo の折り返しの禁止)・`test_tuikit_depguard_covers_parts.sh` (depguard の 2 つのリスト)。linter の一覧・govet の設定の一致は誰も見ていない
  (`test_golangci_lint_pinned.sh` は `scripts/golangci_lint.sh` のビルドと置き場所の検査で、設定の中身は見ない)
- golangci-lint v2 の設定に別ファイルを読み込む仕組みがあるかは未確認 (着手の時に v2.15.0 の文書と実物で確かめる)

## 対応方針 (案。決めるのは着手の時)

- A. 検査: 共通の塊を 1 つの正本に置き、各 `.golangci.yml` がそれを逐語で含むことを test で見る (ずれたら赤)。編集は 23 回のまま。
  上の 3 本の検査との役割分担 (どれを残し、どれを正本の逐語検査に吸わせるか) も決める
- B. 生成: 正本 + モジュールごとの差分から `.golangci.yml` を作るスクリプトを置き、生成物が最新かを test で見る。編集は 1 回になるが、
  手で直したモジュールごとの事情 (除外のコメント) を差分の側へ移す手間がある
- どちらにするかは、モジュールごとの差分の量を数えてから決める

## 受け入れ条件

- [ ] 共通の塊が 23 個でずれたら、`make test` が赤になる
- [ ] 入口のドキュメント (`src/README.md` の `.golangci.yml` は必須、の項。今は `allow-parallel-runners` と tuikit の forbidigo だけを書いている) に、
  共通の塊の正本と直し方を書く

## 関連

- 872 (切り出し元)
