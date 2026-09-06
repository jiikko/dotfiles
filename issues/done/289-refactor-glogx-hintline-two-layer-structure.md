# hintLine の「2 層構造」を 1 本のパイプへ寄せる（trigger 待ち）

- 種別: refactor
- 対象: `src/glogx/tui.go:hintLine` / `hintLineText`、`src/glogx/status_view.go:fitHintItems`
- 優先度: medium（**trigger 待ち**だが、下記のとおりゲートは新分岐を検出できない）

## 何が問題か

`hintLine` は 147 行あり、**幅の予算を通る経路と通らない経路が混在している**。

1. 冒頭で `activeFullScreen()` の全画面ビューアを 5 本、**早期 return** で返す
   （viewer 自身が幅ぴったりに詰めた hint をそのまま返す）
2. その後の大きな switch で通常画面の hint を組む
3. さらにその後、CI 取得中スピナーと gh 警告の **2 つの前置を後付け**する

行数が問題なのではない（[`verify-design-intent-before-refactor.md`](../../_claude/rules/verify-design-intent-before-refactor.md)）。
問題は **「幅に収まる」という不変条件を、経路ごとに別の人が守っている**こと。

## 発火条件（このセッションで実際に 2 回出た P1）

どちらも敵対的レビューが検出した:

- **P1-a（issue 279）**: 前置（スピナー / gh 警告）を `fitHintItems` の**後**に足していたため、
  予算計算に前置ぶんが入らず、末尾の `hintLineText` が溢れたぶんを切り落としていた。
  切り落とされるのは行末 = **抜ける手段（`q` / `esc`）の案内**。
  `m.ghErr` が立つ環境（gh 未インストール / 未認証）では **幅 60〜200 の全域で exit が消えていた**
- **P1-b（issue 281）**: 固定文字列の hint が 3 本だと思って直したが、**4 本目**（job panel の
  `panelCursor == -1` 経路）が switch の別の枝にあり、63 桁で溢れていた

どちらも「経路が分かれていて、片方だけ直した」形。**構造がこの間違いを誘発している。**

## 現状（このセッションで入れた手当て）

- 前置は switch の**前**で計算して予算から引くようにした（`hw = max(hw-dispWidth(prefix), 0)`）
- 通常画面の hint は `fitHintItems(hw, []hintItem{...})` に寄せ、**exit を優先度 1** にして
  最後まで残るようにした（`shortestPrio1`）
- 幅ゲート `hint_width_test.go` の**全画面ビューアぶん**をレジストリ駆動にした
  （`fullScreenCases` 由来。`TestFullScreenCasesCoverEveryID` が ID の追加を強制するので、
  新しい全画面ビューアは自動で幅検査の対象になる）

つまり**今出ている症状は塞がっている**。

### 🚨 ただしゲートは「新しい分岐」を検出できない（実測 2026-09-06）

`hintSurfaces()` のうち**全画面ビューア以外**（基底一覧 / PR 状態 / job パネル ×2 / 前置あり ×2）は
**手書きのリテラル列**で、レジストリではない。下限チェックは
`len(surfaces) < 2+len(fullScreenCases)` なので、**登録の削除**は落とすが**分岐の追加**は見えない。

変異で確認した:

```
# hintLine の switch の手前に、幅に収まらない固定文字列を返す分岐を 1 本足す
go build ./...   # rc=0
go test -run 'TestHintsFitPopupWidth|TestEveryHintKeepsExitWithinWidth|TestFitHintItemsReservesExit|TestDiffHintUsesRenderBudget|TestFullScreenCases' .
# → ok glogx (全部緑)
```

これは issue 281 の P1-b（4 本目の固定文字列 hint を取りこぼした）と**同じ形**が今も再現しうる、
ということ。次に分岐を足す人は `hintSurfaces()` への手動登録を忘れれば静かに素通りする。

## 提案（trigger が来たら）

`hintLine` を **「hintItem の列を作る」→「1 箇所で予算に詰める」** の 1 本のパイプにする。
全画面ビューアの早期 return も `hintItem` を返す形に揃え、`fitHintItems` を必ず通す。

- 効果の測り方: 「幅の不変条件を守っているコード」の箇所数（現在 3 = viewer 側 / fitHintItems /
  hintLineText の clip）が 1 に減るか。行数では測らない
- 落とす前に [`list-masked-failure-modes-before-removing-guard.md`](../../_claude/rules/list-masked-failure-modes-before-removing-guard.md)
  を通すこと: 早期 return は「viewer の hint に CI 前置を混ぜない」という**別の意図**（本文冒頭の
  コメント）も担っている。パイプへ寄せるときにその意図が落ちないか先に列挙する

## Trigger（これが来るまで着手しない）

**次に hint の分岐を 1 本足す / 既存の分岐の中身を変える変更が来たとき。**
そのとき「登録し忘れると幅ゲートが素通しするか」を確かめ、素通しするなら本 issue を実施する。

投機的に今やらない理由: 症状は塞がっており、実需要が来る前に構造を変えると、見た目のテストを
張り替えるコストだけ先払いになる（[`verify-design-intent-before-refactor.md`](../../_claude/rules/verify-design-intent-before-refactor.md)）。

**ただし「ゲートが守っているから安全」を理由にはできない**（上の実測のとおり守っていない）。
パイプへ寄せる本改修が重いなら、**先に軽い方**を入れてもよい:
`hintLine` の分岐を走査して `hintSurfaces()` の登録と突き合わせる走査型ゲート
（issue 201 が本来指定していた形）。ただしこれは「守りを新設する」変更なので、
着手時は [`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md)
の §0-B（既に答えを出している経路はないか）と §8（脅威モデルと「検出しない形」を先に書く）を通すこと。

## 決着（2026-09-06）

範囲は「**非全画面の分岐をレジストリへ寄せる**」で合意し、全画面ビューア 4 枚の早期 return は
意図どおり残した（viewer の hint に前置を混ぜると末尾の抜ける手段が切れる、という文書化された判断）。

| 指標（この issue が定めた測り方） | 前 | 後 |
|---|---|---|
| hint を決める分岐のうち予算 (`fitHintItems`) を通る割合 | 8 / 13 | **13 / 13** |
| 幅ゲートが覆う面の数 | 4 | **13**（+ 前置 2 + 全画面 4） |
| `hintLine` の行数 | 147 | 49 |

**この issue の起点だった穴は塞がった**: 「`hintLine` に溢れる分岐を直接足す」変異は、
変更前は幅ゲート 4 本とも緑だったが、今は `TestHintLineHasNoInlineHintText` が落とす。

敵対的レビューを 2 周通し、**1 周目 P1×1 / P2×1 / P3×3、2 周目 P1×2 / P3×2** を反映した。
2 周目で出た本質は「1 周目の pin が `hint` の代入数と `hintLineText` の呼び出し数しか見ておらず、
**279 / 281 の実際の根本原因である『予算を通さずに前置を積む』を 1 mm も見ていなかった**」こと。
前置の組み立てと頭打ちを `hintPrefix(maxWidth)` へ出し、予算を引いた後で `prefix` に触る形を
**構造で残さない**ようにして閉じた。

🚨 自分の修正が 2 回恒真だった（幅 200 の全項目チェックは期待値を builder から作っていた /
最初の同一性チェックは出口の部分一致のままだった）。どちらも変異を当てて初めて分かった。

残り: 全画面ビューア側の hint の中身は [296](296-bug-glogx-issues-hint-badge-claim-false-for-epic-children.md) で別途。
