# 1066 (research): dupscan の dogfood — slack-bbs の監査 (2026-11-11)

起票日: 2026-11-11

## 概要

`/all-audit-on-diy slack-bbs` (監査の記録は [1065](../../done/1065-research-slack-bbs-audit-2026-11-11.md)) を dupscan を使って回した所見。
摩擦 2 件・役に立った場面 2 件。既存の issue との重複は epic 932 の本文と done の dogfood を「起点の外」「呼び出しの数」で grep して確かめた (見つからなかった)。
観察の記録だけなので、反証レビューは省いた (skill dupscan-dogfood の 5 節)。dupscan は直していない: epic 932 の `next/` に別の担当 (1034) がいるため (skill の 0 節の 4)。

- dupscan を使った回数 7 (`capabilities` 1・`sweep` 2・`index -name` 3・`callers -limit 0` 1)。grep / rg に戻した回数 3 (理由: F1 の呼び出しの数を確かめる 1・module をまたいで `func …(ctx context.Context, d time.Duration) error` の形を探す 1 (dupscan の `-param-type` を思い出せなかった)・`IsGroup` 系の判定の場所を字面で集める 1 (`len(p.Members)` のような式は dupscan の単位にならない))
- 出力の大きさ: `sweep src/slack-bbs` 2,303 B (0.17 秒)、`sweep -focus src/slack-bbs src` 3,610 B

## F1 index の「呼び出し ~N」が、走査の起点の外からの呼び出しを数えない (使い方が分かりにくい)

- 場面: `dupscan index -name IsTemp src/atomicfile` (slack-bbs の cache が使う `atomicfile.IsTemp` を寄せる先として確かめた)
- 期待: 呼び出しの数が「生きている道具か」の目安になる
- 実際: `src/atomicfile/atomicfile.go:17 IsTemp(name string) bool (呼び出し ~0)`。実際は別 module の `src/slack-bbs/cache/cache.go` の `expireGroup` が呼ぶ。rc=0
- その後: rg で確かめた
- 提案: 起点を module に絞ったときは数の横に範囲を出す (`呼び出し ~0 (起点 src/atomicfile の中)`)。または、公開名 (大文字で始まる Go の名前) は repo root から数える。確かめ方: 上のコマンドで slack-bbs の呼び出しが数に入るか、範囲が出るか
- 失うもの: repo root から数えると、起点を絞って速く引く使い方が遅くなる (未計測)

## F2 sweep の節 5 が、前置きの文だけ同じで、結果を決める return が違う関数を「呼べる候補」に出す (雑音)

- 場面: `dupscan sweep src/slack-bbs`
- 実際: `src/slack-bbs/view/highlight.go:176 numberStart の中に boundaryBefore (…highlight.go:220) の本体の文 2/3 個 (節 20)`。同じなのは `if i == 0 { return true }` と `r, _ := utf8.DecodeLastRuneInString(s[:i])` の 2 文で、残りの return の式 (英字の後を許すか) が違うので、numberStart は boundaryBefore を呼べない
- 提案: 呼ぶ側の関数の最後の文 (return) が一致しないときは節 5 に出さない、または「一致しない文が return」と印を付ける。確かめ方: 上の組が消え、treefiler・tuikit の既知の節 5 の候補 (1047) が残ること
- 失うもの: return の前までを寄せて残りを呼び出し側に書く候補 (前置きの helper を作る案) が見えなくなる

## 役に立った場面

- `sweep -focus src/slack-bbs src` で module をまたいだ写し (`slack-bbs/source/throttle.go` の `sleepCtx` ~ `doctor/health/check.go` の `SleepCtx`、Dice 0.698) が 1 回で出た。寄せないと決めた (10 行が 2 か所・共有の module を作ると replace が増えるだけ。1065 に記録)
- `callers -limit 0 pageLabel` で呼び出し 5 か所 (2 段の実行の show が手で組んだ `render.Page` を渡す 1 か所を含む) を 1 回で得て、`render.Page.Label` への置き換えで取りこぼさなかった (1065 の encapsulation の修正)
