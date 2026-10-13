# 745 (feat): zundamon-kaisetsu を 1 本作って出た、コマンドと skill の手直し 9 件

起票日: 2026-10-13

## 概要

ubiregi の esa 記事 (ADR。リンクの無い 8.5KB の記事) から mp4 を 1 本作った (93 行・522 秒・6 チャプター)。
その途中で引っかかった点・手間が増えた点を、コマンド (`src/zundamon-kaisetsu`) と skill (`_claude/skills/zundamon-kaisetsu`) に返す。

## 詳細と対応方針

### コマンド

1. **`kana --check` が、1 文字ずつ読むのが正しい略語でも rc=1 になる**。ADR・OIDC・JWKS・mTLS・QRO で 5 件の警告が出た。
   外す手段は共有の `acronyms.json` (skill のディレクトリ。`app.Env.AcronymsFile`) に足すことだけで、社内の語 (QRO) は置けない。
   → 台本のトップに `"acronyms": ["QRO"]` を書けるようにし、`acronyms.json` と合わせて `SpelledWords` に渡す。形の検査は `readings` と同じく `script.LoadScript` で行う
2. **字幕の速さの検査が build で初めて走る**。音声の長さは synth の時点で分かるのに、build で止まって `pause_after` を足して build し直す往復が 1 回出た。
   → 行の開始・終了秒を wav の長さと間 (`lead_in` / `gap` / `pause_after`) から計算する部分を `Assemble` から関数に切り出し、synth の最後に同じ `WarnFastCaptions` を回す (synth では警告だけで rc は変えない。止めるのは今どおり build)
3. **区切りのカードの長さ (`chapterCardSeconds = 1.5`) が短い** (ユーザーの感想)。決定 (2026-10-13): **各チャプターの 1 行目の音声が終わるまで出す。最短 1.5 秒・最長 4 秒。1 行目に図解 (`show`) があるチャプターは 1.5 秒のまま** (カードが 1 行目の図解を隠し続けないように。`chapter_card.go` のコメントの懸念)
4. **行の開始秒が分からない**。mp4 の図解を目で確かめるとき、図解の出る秒を当て推量で切り出した。
   → 2 の関数を使い、行ごとの 開始秒 / 終了秒 / 話者 / 字幕 を TSV で出すサブコマンド `times` を足す (合成済みの wav だけを読む。エンジンも Chrome も要らない)

### 読み辞書

5. **`readings.json` に一般の語の読みが足りない**。`HMAC-SHA256` (辞書の `SHA-256` はハイフンの位置が違って当たらない)・`HMAC`・`JSON`・`Ed25519` を台本の `readings` で直した。
   検出は `kana --check` が 1 文字読みとしてできていた (検出漏れではない)。→ 4 語を辞書に足す。足す前に、語のままでは誤読し読みにすれば正しく読まれることを `kana` で確かめる (SKILL.md 手順 5 の 7)

### skill (SKILL.md)

6. **手順 3-1 の裏取りが重い**: リンクの無い記事でもサブエージェント 1 体に約 19 万トークン使った (主張の裏取りは数件)。
   → 「資料にリンクが無く、裏取りする主張が数件なら main で行ってよい」を足す
7. **サブエージェントを起動できないとき (5h 枠など) の代わりの手順が無い**。今回は手順 3-3 の確認役を起こせず、main が自分で見た。
   → main が最低限見るもの (lint・`kana --check`・読み分け候補の grep・図解と資料の突き合わせ) と、飛ばした工程をユーザーへ報告することを書く
8. **コードの図解の幅 44 と、確認の観点「資料のコードと 1 行ずつ同じか」が両立しない**。資料のコードは 1 行が長く、改行し直さないと載らなかった。
   → 「幅に収めるための改行と字下げはよい (字句は変えない)」を図解の節と確認の観点に書く
9. **片付け (手順 8) に container のサービスが無い**。手順 1 で `container system start` を打つことがあるのに、片付けはエンジンだけを扱う。
   → 自分で起動したサービスを止めるかをユーザーに聞く、を 1 行足す

## 関連ファイル

- `src/zundamon-kaisetsu/internal/synth/reading_check.go` (`LoadAcronyms` / `SpelledWords`)
- `src/zundamon-kaisetsu/internal/build/build.go` (`Assemble`) / `caption_speed.go` / `chapter_card.go`
- `src/zundamon-kaisetsu/internal/synth/synth.go` (`CmdSynth`) / `main.go`
- `src/zundamon-kaisetsu/internal/script/script.go` (`LoadScript` のキー検査)
- `_claude/skills/zundamon-kaisetsu/SKILL.md` / `readings.json` / `README.md`

## 反証レビュー (issue) の結果 — 2026-10-13、sonnet 1 体

採った指摘 (方針に反映した):
- **P1** 時刻の計算を `Assemble` から「切り出す」と書いたが、`Assemble` の頭は立ち絵の読み込みと図の埋め込み (mermaid は Chrome で描く) をするので、
  そのまま呼ぶと `times` が mermaid を描く → 時刻は wav の長さと間だけを読む別の関数 `LineTimes` にし、間の丸め (`leadInSamples` / `pauseSamples`) を
  `Assemble` と共有して、両者が同じ値を出すことをテストで固定した
- **P2** 1 の前提の訂正: 外す手段は `acronyms.json` だけではなく、台本の `readings` で語をカタカナにしても警告の対象から外れる (今回の台本もそうした)。
  ADR・OIDC・JWKS・mTLS は一般の技術語なので `acronyms.json` に足し、台本の `acronyms` はその台本だけの語 (QRO のような社内の略語) 用と整理した
- **P2** 3 の「1 行目に図解がある」は、行に `show` を書いたかではなく、その行で図解が出ているか (前の行から持ち越した図解を含む) で判定する
- **P2** 5 の `JSON` は部分一致で `JSONL` などにも当たる → 派生語 (`JSONL` / `JSONB` / `JSON5`) を一緒に足した
- **P2** SKILL.md の「区切りのカードで状態が増える」の実測と、台本キーの表の「1.5 秒」が古くなる → 直した (実測は「1.5 秒のときの計測、以後は未実測」と書いた)

採らなかった指摘:
- **P3** 2 と 4 は効果が限定的 (build の検査は撮影より前に走る / HTML の `D.lines` を jq で読めば時刻は取れる) → 優先度の指摘で、誤りの指摘ではない。
  2 は往復が 1 回減り、4 は mp4 だけを作るときに HTML を作らずに済むので、そのまま入れた

## 敵対的レビュー (実装) の結果 — 2026-10-13、sonnet 1 体

壊せなかったもの: `LineTimes` と `Assemble` の食い違い (`pause_after` の負数・文字列は `LoadScript` が弾く)、`acronyms.json` の並べ替えでの欠落・重複 (なし)、
台本の `acronyms` が共有の map に残ること、カードが次のチャプターに食い込む形。

直したもの:
- **P2** 合成済みの wav が壊れていると、新しく足した字幕の速さの検査が失敗し、synth が rc=1 になっていた (前は rc=0 で、build が止めていた) →
  synth は「確かめられない」と知らせて rc=0 で返す。壊れた wav は今までどおり build が止める
- **P2** `times` が字幕のタブ・改行をそのまま出し、TSV が崩れていた → 空白にした
- **P3** `LineTimes` は wav だけを見て、`Assemble` は wav と query の両方を要る → 揃えた
- **P3** synth は丸める前の長さで、build は丸めた後の長さで字幕の速さを測っていた (境界の行で判定が割れうる) → synth も丸める

採らなかったもの:
- **P3** 前のチャプターから持ち越した図解でもカードが 1.5 秒になる → 意図どおり (カードは出ている図解を隠すので、持ち越しでも同じ)。
  持ち越し自体は lint が警告する
- **P3** `GeoJSON` が「Geoジェイソン」、`JSONLines` が「ジェイソンエルines」になる → 部分一致の辞書の既知の性質 (SKILL.md 手順 5 の 7)。
  これらの語が出る台本では `read` で直す。`SHA-256` が未登録という指摘は誤り (既にある)
- **P3** `ChapterCardEnds` の単体テストが無い → 誤り。`TestChapterCardRuns` が最短・最長・図解の分岐を見ている (下の変異で確認)

直した差分への 2 周目 (sonnet 1 体): 壊せなかった。指摘のうち、`times` のテストがチャプター名のタブの置き換えを見ていない点を塞いだ
(話者は台本の検査で `metan` / `zundamon` に限られるので見ない。図解の欄は種類の語だけで、台本の自由な文字列は入らない)。

## 進捗

- [x] 1. 台本の `acronyms` (`script.LoadScript` で形を検査・`script.Acronyms`・`checkReadings` で合流) と `acronyms.json` に ADR / JWKS / OIDC / mTLS
- [x] 2. synth の字幕の速さの警告 (`build.LineTimes` / `build.CaptionLines`。rc は変えない)
- [x] 3. 区切りのカードを 1 行目の終わりまで (`build.ChapterCardEnds`)
- [x] 4. `times` サブコマンド (`build.CmdTimes`)
- [x] 5. 読み辞書に HMAC-SHA256 / SHA256 / HMAC / JSON / JSONL / JSONB / JSON5 / Ed25519 (どれも語のままでは誤読し、読みにすると正しく読まれることを `kana` で確認)
- [x] 6〜9. SKILL.md (3-1 を main で済ませてよい条件 / サブエージェントを起こせないとき / コードの図解の改行 / container のサービスの片付け)。
  あわせて `times`・台本の `acronyms`・synth の警告・カードの長さを SKILL.md と README に書いた
- [x] 反証レビュー (issue) / 敵対的レビュー (実装。指摘を直した差分にもう 1 周) / `bin/skill-eval zundamon-kaisetsu`
  (`PASS zundamon-kaisetsu score=1 delta=1 cases=2/2 cost=$0.63`)
- 変異で red を確認したもの (`bin/mutate-verify-list`、9 本すべて想定のテストが落ちた): `LineTimes` の間を落とす / カードの図解の分岐を外す /
  カードの最長を外す / `Assemble` の配線を旧に戻す (`ChapterCardEnds(chapters, nil)`) / synth の警告を外す / 台本の acronyms を合流しない /
  acronyms の形の検査を外す / `times` のタブの置き換えを外す / synth が壊れた wav で失敗に戻る
- 未検証: カードを長くしたことによる mp4 のまとめ撮りの状態数の増え方 (実測していない。次に mp4 を作るときに `build` の「見た目の状態 N 種類」で見る)
