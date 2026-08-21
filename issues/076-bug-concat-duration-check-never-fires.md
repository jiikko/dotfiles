# 076 bug: concat の duration 乖離検査が全テストで発火しない (tolerance 上書き + モックの固定値)

起票日: 2026-08-21
種別: bug
優先度: **P3** (production の検査は生きている。テスト側のカバレッジ欠落)

監査 072 の `072-concat-tolerance-drift` を敵対的反証にかけた結果、**指摘の半分は崩れた**が
生き残った部分がある。素朴に直すと false failure を作るので、経緯ごと残す。

## 崩れた側 (やってはいけない修正)

- 指摘は「`tests/zshrc/concat/test_helper.sh:194` の `CONCAT_DURATION_TOLERANCE=100` が
  production 既定 (`_concat_helpers.zsh` の `:-5`) を上書きし、**duration 乖離検査とサイズ乖離
  検査が concat テスト 14 ファイルのどれでも発火しない**」としていた
- **サイズ乖離検査は tolerance と無関係**で、この主張は成立しない。また 100 という値は
  「drift (放置された腐り)」ではなく**意図的な test seam** (モック環境で実時間の duration を
  作れないため)
- したがって **`tolerance` を 5 へ戻す修正は誤り**。モックの ffprobe が duration を 20.0 に
  固定している状態で閾値を締めると、正常なテストが乖離扱いで落ちる (false failure)

## 生き残った側 (本当の負債)

**duration 乖離の 2 分岐 (乖離あり / なし) に回帰カバレッジが無い。** production の検査は
生きているが、その検査が壊れても concat テストは緑のまま通る。

真の原因はモック側: `ffprobe` スタブが duration を固定値で返すため、「入力の合計 duration と
出力の duration がずれた」状況をテストが作れない。tolerance を触るのは対症療法で、
**モックが duration を制御できる形にする**のが構造的な解。

## 対応方針 (着手時に再確認すること)

1. モック `ffprobe` に「出力の duration だけ意図的にずらす」入口を足す (環境変数など)
2. その入口を使って **乖離あり → 検査が発火して失敗する / 乖離なし → 通る** の 2 方向を pin する
3. `CONCAT_DURATION_TOLERANCE=100` は 1〜2 が入るまで**触らない**。触ると false failure になる
4. 変異検証: 乖離検査の条件式を潰して red になることを確認してから閉じる

## trigger

concat の duration 検査そのものを触るとき、またはモック ffprobe を改修するときに同時に着手する。
単独で着手する価値は低い (production の検査は生きている)。

## 関連

- `_claude/rules/mutation-verify-new-tests.md` — 「差が出ない状況しか作っていないか」の実例。
  モックが 1 つの値しか返さないと、その値に依存する分岐は永久に片側だけが走る
- issues/072 (テストコード監査) の該当項目。反証の結論はこの issue が正本
