# doctor: 確認画面の「N 件を削除」とパス一覧が、触らない対象まで数えて並べる

起票日: 2026-09-04
種別: bug (表示と実際の食い違い)
優先度: **P2** (破壊の範囲は広がらないが、確認画面の doc が主張する契約が成立していない)
出典: audit (broken-code / design) 2026-09-04 / forge-Standard。main agent がコードで裏取り済み

## 該当

- `src/glogx/doctor_delete.go: confirmLines` の `len(e.Items)` (件数)
- `src/glogx/doctor_delete.go: deletePathLines` (全 Item を並べる)
- 生成元: `src/doctor/disk/delete.go: planDelete` → `unmatchedItem` / `planItem` が
  `Skipped` (既に存在しません / 差し替わっています) の Item も `out.Items` に積む

## 症状

`confirmLines` はエントリ単位の Outcome しか見ないため、下見で `Skipped` / `Failed` になった Item も
「N 件を削除」に数え、`deletePathLines` がそのパスも並べる。同じ行のサイズは `BeforeSize` =
**照合が取れた分だけ**なので、**件数とサイズが食い違う**
(例: 「3 件を削除」と出ているのにサイズは 1 件分)。

`deletePathLines` の doc は「パスは engine が走査し直して正規化したもの = **実際に触る対象そのもの**」
と書いているが、成立していない。

## 発火条件

下見の時点で 1 件でも「既に存在しません」/「走査時と別の実体」になっているエントリを選んで `d` を押す。
キャッシュ相手なので珍しくない。

## silent か

**silent** (表示だけの問題で、実際に触る対象は増えない)。

## 最小の修正方向

`confirmLines` / `deletePathLines` を `Outcome == OutcomePlanned` の Item に絞る。
省いた分は「他 N 件は対象外」と件数で伝える (0 件なら `planHasWork` が false になり
「消せるものがありません」へ落ちる = 既存の分岐がそのまま効く)。

## 変異検証の形

下見の結果に Planned 2 件 + Skipped 1 件を持つ `DeleteReport` を作って `confirmLines` を呼び、
出力に「2 件を削除」が出ることと、Skipped のパスが**出ないこと**を assert する。
変異 = 絞り込みを外す → 「3 件」で red。
