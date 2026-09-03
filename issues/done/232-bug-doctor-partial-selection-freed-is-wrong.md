# doctor: ディレクトリ単位で選んで削除すると、成功しても必ず「未完了」になり解放量が別物になる

起票日: 2026-09-04
種別: bug (broken-code / 数え方のスコープ違い)
優先度: **P1** (正規の使い方で日常的に発火し、UI が「解放された容量はありません」と嘘をつく)
出典: audit (broken-code / design) 2026-09-04 / forge-Standard。指摘は main agent がコードで裏取り済み

## 該当

- `src/doctor/disk/delete.go: verifyEntry` の `out.AfterSize = after.Size` と最終判定 `len(after.Items) > 0`
- `src/doctor/disk/delete.go: planDelete` の `out.BeforeSize += it.Size` (渡された Item の分だけ足す)
- 呼び出し側: `src/glogx/doctor_delete.go: selectedResults` (Items を**部分集合**にした Result を渡す)

## 症状

2 つの量のスコープが違う:

| 量 | 範囲 |
|---|---|
| `BeforeSize` | **渡された Item だけ**の合計 (部分選択なら選んだ分だけ) |
| `AfterSize` = `after.Size` | 削除後に**エントリ全体**を再走査した値 |

`selectedResults` は「Enter で開いて Space で個別に選んだ」ケースで Items を部分集合にして渡す
(`doctor_delete.go` のコメントが明記する正規の使い方)。このとき兄弟ディレクトリ b を残して a だけ
消すと:

- `Freed = min(BeforeSize - AfterSize, touchedSize) = min(size(a) - size(b), size(a))`
  → **b が a より大きければ負になり 0 に落ちる**。b が小さければ「a のサイズ」ではなく
  「a - b」が解放量として出る
- 最終判定が `len(after.Items) > 0` を「実行したのに残っている」と読むため、
  **残った兄弟 b の存在が「未完了」の根拠になる** → 常に `OutcomeIncomplete`
  「削除を要求しましたが 1 件が残っています」

UI ではエントリ行が「🚨 未完了」、サイズ列が `---`、合計行が「解放された容量はありません」になる
(`doctorDeleteResultLines` / `deleteResultSize` は incomplete に数字を出さない設計なので、
engine の数え方が狂うとそのまま画面に出る)。

## 発火条件

`Xcode DerivedData` / `Containers` のように **1 エントリに複数の対象パスがあるエントリで、
一部だけを選んで削除する**とき (2026-09-03 に足した「ディレクトリ単位の選択」の主経路)。
エントリ全体を選んだ場合は BeforeSize と AfterSize のスコープが一致するので発火しない。

## silent か

**silent。** 削除自体は正しく行われ、error も返らない。壊れるのは結末語と解放量だけ。
既存テストは全 Item を渡す fixture しか作らないので、この経路を構造的に踏まない。

## 反証の試み

- 「`min` があるから安全側では」→ `min` は**過大計上**は防ぐが、`AfterSize` が別スコープだと
  引き算そのものが意味を失う (他人が消した量ではなく「選ばなかった兄弟の量」が混ざる)
- 「incomplete は安全側の丸めでは」→ `verifyEntry` の doc は incomplete を
  「実行したのに残っている」と定義している。ここでは**残っているのは触っていない対象**なので、
  定義に反する第 3 の状態の誤用。ユーザーには「再スキャンしてください」と出るが、何度やっても同じ

## 最小の修正方向

`AfterSize` と残存判定を「**触ろうとした対象の集合**」に閉じる:
削除後の再走査結果を `out.Items` のパス / Ref で絞り込んでから `AfterSize` と `after.Items` を数える
(エントリ全体を渡した場合は現状と同じ値になる = 既存の振る舞いは保たれる)。

## 変異検証の形

同一エントリに a (小) と b (大) を作り、a だけを Items に入れた Result を渡す。
`Outcome == OutcomeDeleted` かつ `Freed == size(a)` を assert する。
変異 = `AfterSize` の絞り込みを外す → incomplete / freed=0 で red。
**b > a と b < a の両方**をケースに入れる (片方だけだと符号の効果が見えない)。

## 対応 (2026-09-04)

`verifyEntry` の `AfterSize` と `Remaining` を「触ろうとした対象」の集合 (`out.Items` の
`itemKey`) に閉じ、残存判定も `len(after.Items)` から `len(out.Remaining)` へ移した。
エントリ全体を渡した場合は集合が一致するので既存の値は変わらない。

### 敵対的レビューの全数勘定 (opus / red team)

| 指摘 | 判定 | 対応 |
|---|---|---|
| P2-1 絞り込みが rm / trash では何も守っておらず、壊しても既存テストが 1 本も落ちない | **本物** (変異が緑と実測) | `TestDeleteRmResidueIsIncomplete` を追加。粗い防御 (`len(after.Items) > 0`) を外した以上、代わりの検知を固定する必要があった |
| P3-3 `AfterSize` の doc が実態とずれた (履歴 JSON に落ちるフィールド) | **本物** | struct のコメントを実態へ合わせた |
| P3-4 `touchedKeys` が `unmatchedItem` 由来 (BeforeSize 非寄与) も含む非対称 | **本物だが退行ではない**。旧実装も同じ向きにずれており、Freed を過小に見せる安全側 | コードにコメントで残した (直すには plan 側で寄与の印を持つ必要があり、テストできない変更を入れない) |
| C: 絞り込みで残存を見落とす具体的入力 | **壊せなかった**。両側のパスは `validateTarget` を通った同じ正規形で、symlink / 末尾スラッシュ / Clean / 大文字小文字はそこで吸収される。cli ref は両側 Ref を持ち、cli 非 ref は両側 Ref 空 | なし (ただし P2-1 のテストで固定した) |
| D: `AfterSize +=` の二重加算 | **発火経路なし**。`verifyEntry` の呼び出し元は 1 箇所、`out` は毎回新規、`dedupeTargets` が同一 ID を畳む、履歴復元は `planDelete` 冒頭で fail | なし |

### 変異検証

- `AfterSize` の絞り込みを外す → `TestDeletePartialSelectionCountsOnlySelected` が両ケース red
  (兄弟が大きい: freed 0 / 兄弟が小さい: freed 114688、want 131072。どちらも incomplete)
- 残存の記録を cli 経路だけに絞る → `TestDeleteRmResidueIsIncomplete` が red
  (outcome=deleted / Remaining 空 / freed 65536 = 「消えていないのに消えたと全額申告」)

### 未確認

ディスク上に既にある過去の run の履歴 JSON (`after_size` / `remaining`) は、旧い意味
(エントリ全体) で書かれている。repo 内に読む側は 0 件 (grep 済み) だが、混在の影響は未確認。
