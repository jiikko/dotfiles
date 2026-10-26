# 931 (refactor): tuikitの同一ドメインの重複を共通化しdupscanの探索効果を検証する

起票日: 2026-10-26

## 目的と今回の範囲

ユーザーの問題は、grepで既存実装を見落とし似たメソッドを増やしてしまうこと。本検証は実際のdotfiles/srcのうちtuikitで既存の重複を探し、同じ責務だけを共通化する。grepの検索1回の速度だけでdupscanの価値を判定しない。

提案925〜930の機能は未実装のため、それらを実施した後の改善効果は今回測れない。現行dupscanを併用した探索とgrep中心の探索を比較し、実際の共通化を1組行う。単一module・各arm1 agentの探索実験であり、書く前のhelper再利用率、不要な新規重複追加率、agent全体の時間/費用の因果効果は未検証。これらを測る追試のtriggerは、925〜928実装と928のpilot条件が整ったとき。917/918/921/895の保留は変更しない。

## 固定入力と手順

- 入力: dotfiles `e7ddfd9fc107a6a02fb0dff73d99d352f48ea8bb` の `src/tuikit`、Go 92 files / 抽出333 units（test関数は単位から除外され、test helperは含む）。探索終了までGo sourceは無変更。issue skeletonの予約commitだけを追加した。
- CLI: 9d2e801fからbuildしたdupscan。e7ddfd9fまでsrc/dupscanは無変更。binary SHA-256 `87cec7d3d2bc0e2b0ccb645fe46de5b93a11dbed99eaaf85971f92973f2aee73`。build時間を探索時間に含めない。
- 方法: 周辺の会話・他armの発見を渡さないfresh subagentを各armに割り当て、「同一domain/packageで共通化すべき重複を探す」という同じ課題を渡す。grep armはrg/grep/cat/sed/wc、併用armはそれにdupscanを追加する。名前のseedは与えない。model/推論条件は親から継承し、違うmodelを指定しない。
- 指示/READMEの初回setup読取は探索計測の外。以後のretrievalは共通recorderでsubprocess argv、rc、wall time、stdout/stderrのbytesを記録する。外側のtool制限/serializationの後にモデルが実際に受け取ったbytesは独立検証しておらず、deliveredをモデル入力token量とは扱わない。下の固定予算runでは、両armがREADMEを再読した27,250 bytesも予算・時間に含む。
- 上限: 各arm12 retrieval / 60,000 recorder転送bytes（表のdelivered）。固定予算runは超過部分をUTF-8を壊さないprefixへ制限し、markerを含めて上限内に収める。モデル自身の推論時間・setup・tool呼出し往復時間はcommand wall timeに含めない。

## 予備runで判明した測定上の不備

初回は上限をagentへ指示しただけで、機械的に止めていなかった。grepは7 calls / stdout 60,896 bytes、併用は11 calls / stdout 84,307 bytes（stderr込み87,023 bytes）となり上限を超過した。このrunを同じ出力量での効率比較には使わない。別のfresh agentとhard capで取り直し、両方の結果を残す。

予備runのgrepはmarkdownのmergeCells/mergeSpansを候補にした。併用armはこれとspansWidth/lineWidthをsource読取で見つけたが、後者はdupscanの機械検出による発見ではない。この知見は本体が確認して共通化対象を選ぶために使用した。後続runのagentには候補を渡していない。

## 固定予算runの結果

| arm | retrieval数 | raw stdout+stderr bytes | delivered bytes | command wall合計（秒） | 共通化を推奨できた組 |
|---|---:|---:|---:|---:|---|
| grep中心 | 4 | 65552 | 60000 | 0.018578 | 0 |
| dupscan併用 | 7 | 79266 | 59999 | 0.163516 | 0 |

grepはScrollGlide.Offset/CursorGlide.Cursor等の小さな同形部分を見つけたが、異なる開始/中断/進捗ポリシーを結び付けるコストに対して利益が小さく、今回は抽出しない。併用armはreport 0組、Advanceのnearも0件となり、その後のsource読取でも確実な共通化対象を確定しなかった。

**今回の比較ではdupscanによる効率向上を確認できなかった。** 共通化推奨0組は「本当に重複が無い」という結果ではなく、この予算と探索経路では候補を確定できなかったという結果。command合計が小さい方をagent全体の効率が高いと断定しない。探索判断の個体差・対象選択・read量の配分を統制できず、反復/無作為割付/完了までの時間を測っていない。

### 固定予算runの全retrieval

各timeは単発観測で中央値ではない。rawはstdout+stderr、deliveredはrecorderが標準出力へ転送したbytes。末尾の大きなreadは一部しか渡していない。

| arm | # | argv（dupscanは固定binary） | rc | 秒 | raw bytes | delivered bytes |
|---|---:|---|---:|---:|---:|---:|
| grep | 1 | `cat src/tuikit/README.md` | 0 | 0.002347 | 27250 | 27250 |
| grep | 2 | `rg --files src/tuikit -g '*.go'` | 0 | 0.006870 | 3082 | 3082 |
| grep | 3 | `rg -n '^func ' src/tuikit/anim src/tuikit/layout src/tuikit/termwidth src/tuikit/markdown src/tuikit/listnav -g '*.go' -g '!**/*_test.go'` | 0 | 0.006941 | 13716 | 13716 |
| grep | 4 | `cat src/tuikit/anim/glide.go src/tuikit/layout/panel.go src/tuikit/termwidth/fasttrunc.go` | 0 | 0.002421 | 21504 | 15952 |
| 併用 | 1 | `cat src/tuikit/README.md` | 0 | 0.002613 | 27250 | 27250 |
| 併用 | 2 | `dupscan report -limit 20 src/tuikit` | 0 | 0.054988 | 53 | 53 |
| 併用 | 3 | `rg --files src/tuikit` | 0 | 0.006814 | 3371 | 3371 |
| 併用 | 4 | `cat src/tuikit/anim/glide.go src/tuikit/listnav/list.go src/tuikit/listnav/pager.go src/tuikit/termwidth/wrap.go src/tuikit/markdown/wrap.go` | 0 | 0.002057 | 26208 | 26208 |
| 併用 | 5 | `dupscan near src/tuikit/anim/glide.go:Advance src/tuikit` | 2 | 0.047081 | 184 | 184 |
| 併用 | 6 | `dupscan near src/tuikit/anim/glide.go:43 src/tuikit` | 0 | 0.046655 | 97 | 97 |
| 併用 | 7 | `cat src/tuikit/layout/drawer.go src/tuikit/layout/panel.go src/tuikit/termwidth/fasttrunc.go` | 0 | 0.003309 | 22103 | 2836 |

## 再現した現行検出の限界

以下は本体の追加確認であり、上のarm比較の検索回数へ混ぜない。入力commit e7ddfd9fに固定した別worktreeのrepo rootで同じbinaryを使う。修正後のworktreeではlineWidthは削除されており、そのままではlineWidthのexplainを再現できない。

```sh
dupscan report -format json src/tuikit
dupscan report -format json -min-nodes 8 -levels L1,L2,L3 src/tuikit
dupscan index -format json -name Width src/tuikit/markdown
dupscan explain -format json src/tuikit/markdown/wrap.go:spansWidth src/tuikit
dupscan explain -format json src/tuikit/markdown/links.go:lineWidth src/tuikit
dupscan near -format json -min-nodes 1 -min-closure-nodes 1 -levels L1,L2,L3 src/tuikit/markdown/wrap.go:spansWidth src/tuikit
```

- default reportは92 files / 333 units / parse error filesなし / groups 0。min-nodes=8かつL3追加では5組（Open/Close、Offset/Cursor、Dialog/WideDialog、List/PagerのAnimatingとStop）。数値下限の変更だけでなくL3追加も同時にしており、どちらの寄与かはこの観測だけで分けない。
- 確認したspansWidthは19 nodesで既定20未満、lineWidthは21 nodes。加えて、range対象が引数 `spans` か `l.spans` かという構文差でL1/L2/L3のhashが全て異なる。下限1のnearでもこの2つは一致しない。
- これはType-2の一致・閾値の限界で、parse failureや「閾値を変えるだけで解消するバグ」とは扱わない。reportが0件でも、同じ責務の既存実装を探し終えた証拠にならない。
- name Widthのindexなら両方が候補に出るが、これは発見後に作ったqueryであり、未知名の探索で有利だったという証拠にはしない。

## 共通化した1組と残した候補

**対象: markdownの表示幅計算。** links.goのlineWidthはline.spansをrangeし、wrap.goのspansWidthは渡されたspansをrangeする。どちらも各Textのtermwidth.Ofの総和。同じpackage・同じspan・同じ幅モデルであり、リンク末尾の切り詰めと表の列幅が異なる幅規則を持つ理由が無い。

- collectLinksは既存 `spansWidth(l.spans)` を呼ぶ。privateなlineWidthを削除し、既存のclip境界テストも同じ既存入口を使う。
- 新しいhelper/type/package/依存は追加しない。exported API・表示・字幅規則を変えない。2つの幅計算loopを1つへ寄せ、2 filesで2行追加/11行削除（差引9行減）。実行時間の高速化は主張しない。
- mergeCells/mergeSpansは同じ(style, link)の連結という候補だが、空入力/空Textの扱い・入力表現が異なる。共有accumulatorや中間sliceを足すコストを今回正当化しておらず、現状のまま残す。
- Open/Closeの対称dispatch、Scroll/Cursorのglide、List/Pagerの小さなdelegation、既にdialogへ委譲するDialog/WideDialogは寄せない。packageが同じというだけで政策や動作の違いを潰さない。

## 次の改善案へ戻す知見

[925](../925-feat-dupscan-helper-search.md): 名前が不明な状態の型/用途検索と、入力がslice直渡し/struct.field経由で違う実例を評価課題に入れる。今回のnode閾値/構文差を新しいlexical searchが必ず解消するとは言わない。Type-3等は既存の保留範囲。

[928](../928-feat-dupscan-agent-onboarding.md): default reportだけで探索を終えず、候補が無い場合に型/用途/outlineへ移る導線を検証する。同じstdout予算でも広いcatで読取を使い切れるため、検証harnessではbudgetを機械的に制御する。未実装featureの効果と現行ツールの結果を分ける。

## 検証と受け入れ条件

- [x] 共通化前に `make -C src/tuikit test` 成功（raceとANSI深さ4の総当たりを含む）。
- [x] 共通化後に `make -C src/tuikit lint test` 成功、lint 0 issues、既存のリンク折返し・隣接リンク・全角clip境界・表幅のテストを維持。
- [x] `dupscan gate -base e7ddfd9f -format json src` 成功。changed_units=1、failed=false、unreadable_changed_units=[]。gateだけでparse完全性を保証せず、別途reportでもparse errorなしを確認。
- [x] after reportは92 files / 332 units / parse errorなし / groups 0。削除した1 private functionに対応するunit減であり、groups 0→0を重複検出率の改善とは呼ばない。
- [x] 最終diffと同一domainの判断、測定結果の主張を読み取り専用レビューし、指摘を反映した。
- [x] issue整合検査を完了し、実装と検証記録を別commitで残す。検証結果が否定的でも削除・差し替えず残した。pushの成否は完了報告で確認する。

## 進捗

- 2026-10-26: `issues: reserve tuikit duplicate discovery verification` で番号予約。各armの予備run/固定予算runを実施し、同じ入力でも現行dupscanの優位を実証できなかったことを記録した。
- 2026-10-26: markdownの幅計算だけを既存実装へ寄せ、既存テスト・lint・gateが成功。改善は同じ幅規則の変更箇所を2実装から1実装へ減らしたこと。新規抽象化を増やさず、将来の幅規則変更の不整合を避ける。性能・一般的な開発効率・提案featureの効果は未検証で、別責務の類似実装と未評価候補は残る。

- 2026-10-26: `refactor(tuikit): share markdown span width calculation` に実装を分離。実装/domainの反証レビューは追加指摘なし。測定レビューのP3（修正前入力の固定、recorder bytesとモデル可視bytesの境界）を訂正した。Bash 5でissue関連テストとrepo標準make test-dupscan-gateも成功。931は今回の限定比較と1組の共通化として完了し、一般的な優位性と未実装featureの検証は925/928へ残す。

- 2026-10-26: この実例を拾うための部分木類似検索・短い関数の別条件・差分付き候補出力の設計を[895](../epic/890/pending/895-feat-dupscan-type3-candidates.md)へ追記。931の実装済み範囲は幅計算の共通化までで、Type-3は未実装。
