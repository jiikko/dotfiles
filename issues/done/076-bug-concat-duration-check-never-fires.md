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

## 対応 (2026-08-25)

`tests/zshrc/concat/test_concat_duration_drift.sh` を新設し、乖離検査の 2 方向を pin した。

**tolerance (100) は触っていない** — issue の「崩れた側」のとおり、あれはモック環境で実時間の
duration を作れないための意図的な test seam であり、5 へ戻すと他の 14 ファイルが false failure に
なる。代わりに **`__concat_get_duration` を上書きする関数 seam** を使い、出力側の duration だけを
制御した (この repo の既存パターン。`test_concat_verify_order.sh` が同じ形)。tolerance の上書きも
新テストの中だけに閉じている。

### pin した契約 (9 + 2 観点)

- 乖離なし (20.0/20) は通る / 短い側 (25%) と長い側 (30%) は発火し、理由と割合が REPLY に入る
- 帯の境界: ratio がちょうど下限 (0.95) なら通る / 1 つ割る (0.945) と発火する
- 入力合計が 10 秒以下なら検査そのものをスキップする
- **production の既定 tolerance (5%) が生きていること** (6% で発火 / 2.5% は通る)

### 変異検証 (6 本すべて red)

短い側の検査を潰す / 長い側の検査を潰す / 10 秒スキップの閾値を外す / **tolerance の既定を
`:-100` にする** / 境界を `lt` → `le` にする / ratio の分母子を入れ替える。

🚨 **最初は「tolerance の既定を 100 にする」変異が green のまま通った**。テストが毎回
`CONCAT_DURATION_TOLERANCE` を明示していたため、production の既定 (`:-5`) が一度も評価されて
いなかった。`test_helper.sh` が 100 を export している以上、既定を見るテストは自分で `unset`
する必要がある — **seam が本番の既定を隠す**形で、これも issue が指摘した構図と同じ。観点を
足して red 化した。
