# 1048 (research): src/tuikit を sweep -focus で見直したときの dupscan の所見 (2026-11-09)

起票日: 2026-11-09

## 概要

dupscan-dogfood の skill で src/tuikit をもう一度掃除した (同じ日の [1047](done/1047-research-dupscan-dogfood-treefiler-tuikit-2026-11-09.md) は起点を tuikit に絞った sweep。今回は 1047 の P1 で入った `sweep -focus src/tuikit src` で module をまたぐ相手も見た)。
摩擦 1 件・提案 1 件。dupscan は直していない (src/dupscan は別のセッションが 1047 の F2 を直している最中で、触るのは 1 セッションだけ)。
既存の issue との重複照合はしていない (skill の 3 節)。所見の記録だけなので反証レビューは省いた (skill の 5 節)。

## 開発タスクの結果

`dupscan sweep -focus src/tuikit src` (29 行・2,615 B) が出した 3 組:

| 組 | 対応 |
|---|---|
| [L1] glogx の doctorview と tuikit の termwidth のテストの `childContext` (節 1・58 節) | `testwait.ChildContext` に寄せた。1047 が寄せなかった理由 (tuikit にテストだけの依存が増える) は、tuikit が 10-30 から testwait を取り込んでいて成り立たなかった (1047 の担当のセッションが本文を訂正すると返事をくれた)。2 つのコメントが指していた `internal/testenv.CommandContext` は実在しなかった (subproc へ移った後の古い参照) |
| [L1] pro-con の `orName` と tuikit/markdown の `linkText` (節 2・16 節) | 寄せない。空の session 名の代わりの値と、markdown のリンクの表示文字で意味が違い、寄せても module をまたぐ依存が増えるだけ。両方に `dupscan:allow intentional` の印を付けた |
| [L2] dupscan/internal/report の `hidden` と tuikit/confirm の `IsYes` (節 2・13 節) | 誤検知 (`x == a \|\| x == b \|\| x == c` の形が同じだけで、文字列のリテラルが違う)。印は付けていない (src/dupscan は別のセッションが触っているため)。次に src/dupscan を触るときに `hidden` の側へ `dupscan:allow false-positive` を付ける |

直した後の `sweep -focus src/tuikit src` は、節 1 が無し、節 2 が hidden / IsYes の 1 組 (印・除外で静かにした組は 10 → 11)。`dupscan gate src` は「新しい重複なし」。

ChildContext のテストは変異 3 本で赤を確かめた (猶予を外して deadline ちょうどで止める / 最低 1 秒の床を外す / deadline を読まない)。

## 摩擦

### F1 `index -returns context.Context` が、context.Context を引数に取る関数を返す関数まで拾う
- 場面: childContext の写しを戻り型で探した。`dupscan index -returns context.Context -limit 0 src`
- 期待: 戻り型が context.Context の関数 (childContext 2 件・`fakeSpeed.started` など)
- 実際: 19 件・4,706 B。うち 15 件は戻り型が `func(ctx context.Context, …) …` の関数 (`FakeRunner`・`HaikuAsk`・`ExecRunner` など)。
  欲しかった 2 件は 18・19 件目 (呼び出しの数の順で、呼び出し 1 回のテストの helper が後ろに来る)
- その後: 結果を目で読み分けた。grep `t.Deadline()` で手書きの写しを突き合わせ (2 件で一致)
- 種類: 雑音 (戻り型の部分文字列の照合が、関数型の引数の中に当たる)
- 提案: `-returns` は戻り型の最上位の型 (関数型なら関数型そのもの) で照合し、関数型の中まで見るのは別の指定にする。失うもの: 今は `-returns context.Context` で「context を受ける関数を返す factory」も引ける (それを探す使い方があるかは未確認)

## 提案

### P1 index の text の末尾に件数の 1 行
- 場面: F1 の出力を自分で `| head` で切り、childContext が「出ない」と読みかけた (保存した全文には在った。dupscan の見逃しではない)
- 今の回避: `-format json` を jq で数えた (2 回)
- 提案: 出力の中身。text の最後に「N 件 (全部)」か「N 件中 M 件 (打ち切り)」を 1 行出す (打ち切りのときに今何を出しているかは未確認)
- 効果の理由と確かめ方: 全件のときも、末尾の行が見えていなければ切って読んでいると分かる。出力を切った読み違いを数えて減るかで測る (今回 1 回)
- 失うもの: 1 行ぶんの出力 (約 40 B)。狭い検索の出力の大きさは 942 で削った経緯がある

## 使った回数

- dupscan: capabilities 1・sweep 2・index 5 (name 1・returns 3・-h 1)・gate 1
- grep に戻した: 2 (`t.Deadline()` の手書きの写しを探す = skill の 1 節の指示どおり / `testenv.CommandContext` の実在を確かめる = 文字列)
- 役に立った場面: `sweep -focus` が module をまたぐ写し (childContext) を 1 回で出し、寄せる先 (testwait) を両方の go.mod から決められた。`index -name childContext` は 2 件・635 B で grep と一致
