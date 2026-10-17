# 812 (feat): zundamon-kaisetsu の build で、ラベルの意図しない改行を実際の描画で検出する

起票日: 2026-10-17

## 概要

画面に出すラベル (左上のタイトル・トピック名・区切りのカード・図解の欄) が、意図しない場所で改行していないかを、
mp4 の撮影と同じ Chrome の描画で測り、build が書き出す前に止める。

## 詳細

- 起点 (2026-10-17 のクリーンアーキテクチャの動画): 左上のタイトルが 2 行になってトピック名に詰まり、重要語「図はフォルダ構成ではない」が
  「図は / フォルダ構成ではな / い」と 3 行になった。SKILL.md には「2 行になる語は文節を 9 字以内に」と書いてあったが、守られたかを機械で
  見ていなかった。字数の上限 (show.go の KeywordTextMax 等) は「収まる長さ」の目安で、どこで折り返すかは描いてみないと分からない
- 同じ検査を当てると、シャボン玉の動画の比較の項目「断定には本人の記 / 録が要る」も拾った (mp4 の静止画でも実際にそう切れていた)

## 対応方針

- 測る: player.html に計測モード `#measure` を足す。mp4 と同じ 1280x720 の舞台に各ラベルを本物の CSS で描き、1 字ずつ Range で位置を取って
  行に分け、切れ目が `Intl.Segmenter` (word) の境目か・行ごとの書記素の数・箱か舞台からのはみ出しを JSON で返す
- 判定する: Go (`internal/build/label_wrap.go`)。1 行で出す欄 (タイトル・トピック名・見出し・箇条書きの項目) が折り返す / 語の途中の改行 /
  1 字だけの行 / はみ出し。期待するラベル (`ExpectedLabels`: 鍵・種類・台本の字) の欠け・食い違いも止める (測れなかったものを問題なしと数えない)
- build: 字幕の速さの検査の後・音声の圧縮の前に止める (`--allow-label-wraps` で続ける)。Chrome が無い・計測の Chrome が落ちたときは、
  確かめていないことを出して続ける (HTML だけの build を Chrome の不調で止めない)
- 比較の項目は `word-break: auto-phrase` で文節で折り返す (検出だけでなく、起きにくくする)

### 検出しないもの

- 語の境目 (Intl.Segmenter の word) では切れているが、文節の途中の改行 (Chrome の auto-phrase の文節は外から見えない)
- mp4 と違う窓の幅で HTML を開いたときの改行と、字幕の改行
- player の測り方 (行の分け方・はみ出しの量) そのものの退行: テストは本物の Chrome に触れないので、計測の JS はテストから実行されない。
  JS を変えたら、問題のある台本で本物の Chrome の build を通して止まることを確かめる (src の CLAUDE.md)

## 関連ファイル

- `_claude/skills/zundamon-kaisetsu/templates/player.html` (`#measure`)
- `src/zundamon-kaisetsu/internal/build/label_wrap.go` / `label_wrap_test.go`
- `src/zundamon-kaisetsu/CLAUDE.md` (Go と player.html の契約)

## 進捗

- [x] 計測モードと判定・build への配線・テスト (zundamon-kaisetsu: build がラベル (タイトル・トピック名・区切りのカード・図解) の意図しない改行・はみ出しを Chrome の描画で測って止める (812))
- [x] 本物の Chrome での確認: クリーンアーキテクチャの元の台本はタイトルと重要語の 2 件で止まり、直した台本は通る。シャボン玉の台本は比較の項目で止まり、
  CSS の auto-phrase で「断定には本人の / 記録が要る」になって通る
- [x] 本物の Chrome での変異: カードの max-height を 40% にする → はみ出しで止まる / player がトピック名を測らない → 「測れなかった」で止まる
- [x] 1 周目の敵対的レビュー (opus) と回帰のレビュー (sonnet) の指摘を直した (zundamon-kaisetsu: ラベルの改行の検査を敵対的レビュー・回帰レビューの指摘で直す (812))
  - P1 (再現あり): 改行の位置が無い長い英字のラベルが舞台の端で切れても通った → 文字のラベルもはみ出しを測る (`AbstractSingletonProxyFactoryBean` のカードで止まることを本物の Chrome で確認)
  - P2: 期待するのが図解の箱だけで、項目の欠け・字の取り違えが素通り → 項目単位の期待と字の一致を見る / `#measure` と `measure-out` の字句をテストで固定
  - P3: 1 字だけの行を rune で数えていた → 書記素 (JS の glen) / 空白の判定が JS の trim と違う → jsTrim / clip が読めないと問題なし → 問題にする / 中断が失敗に見える → 中断として返す
  - 回帰 P2: HTML だけの build が Chrome の不調で止まる → 警告して続ける / P3: SKILL.md の箇条書きの項目の扱い
  - 採らなかったもの: 本物の Chrome を使う opt-in のテスト (規約でテストは本物の Chrome に触れない。手順は src の CLAUDE.md に書いた)
- [x] 2 周目の敵対的レビュー (opus) の指摘を直した (zundamon-kaisetsu: ラベルの改行の検査を 2 周目の敵対的レビューの指摘で直す (812))
  - P2 (再現あり): 空白だけの字のラベルで、ありもしないはみ出しで止まった → 描かれた矩形が無ければ clip を測らない
  - P3: `--allow-label-wraps` が計測の契約違反まで通す → layout と broken に分け、broken は止める / Chrome の stderr が結果に割り込みうる →
    `extcmd.RunSplit` で stdout だけを読む (`RunQuietCtx` と共通の `run` に寄せた) / rc≠0 の結果を捨てる → 結果があれば判定に使う /
    「合成の濁点」のテストが結合文字を含んでいなかった → 本物の U+3099 で書き直す
  - メモの対応: トピック名の `max-width: 70%` が効かず 16 字ほどで折り返していた → `width: max-content`
  - 変異: 1 字だけの行を書記素でなく rune で数える → TestLabelProblems の「1 字だけの行」が red / broken を allow で通す →
    TestAllowLabelWrapsDoesNotPassBrokenMeasure が red (どちらも mutate-verify rc=0)
- [x] 3 周目の敵対的レビュー (opus) の指摘を直した (zundamon-kaisetsu: ラベルの改行の検査を 3 周目の敵対的レビューの指摘で直す (812))
  - 中 (再現あり): 2 周目の空白の判定が、描かれていないラベル (display: none) も黙って通した → 字があるのに矩形が無ければ clip を null にして止める
    (本物の Chrome で `.stage-topic { display: none }` を足した player が「はみ出しの量を読めない」で止まることを確認)
  - 低: rc≠0 で結果を使う経路のテスト (TestMeasureLabelsUsesResultDespiteChromeRC) / 空白だけの章名のトピック名の枠を出さない
  - 壊せなかった観点 (3 周目): RunQuietCtx の寄せ替えで返す rc・文字列は全経路で同じ / 途中で切れた DOM・measure-out の偽装 / 長いトピック名のはみ出し / broken と layout の振り分け
  - 4 周目は回していない: 3 周目の差分は判定を変えず (null を返す 1 分岐・trim の 1 条件・テスト)、本物の Chrome と変異のテストで確かめた範囲に収まるため
- `make test` rc=0 / `make lint` rc=0 (最後の編集の後)

## 結果

- 本物の Chrome で: クリーンアーキテクチャの元の台本はタイトルと重要語の 2 件、シャボン玉の台本は比較の項目 1 件 (CSS の auto-phrase で解消) を拾った。
  直した台本は両方とも通る
- 残る未確認リスク: player の計測の JS はテストから実行されない (規約で本物の Chrome に触れない)。JS を変えたら src の CLAUDE.md の手順で本物の Chrome の build を通す
