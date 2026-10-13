# 745 (feat): zundamon-kaisetsu を 1 本作って出た、コマンドと skill の手直し 9 件

> 🚨 **担当中: ubiregi-server のセッション (esa 33082 の動画を作ったもの)**（2026-10-13〜）

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

## 進捗

- [ ] 1. 台本の `acronyms`
- [ ] 2. synth の字幕の速さの警告
- [ ] 3. 区切りのカードを 1 行目の終わりまで
- [ ] 4. `times` サブコマンド
- [ ] 5. 読み辞書に 4 語
- [ ] 6〜9. SKILL.md
- [ ] 反証レビュー (issue) / 敵対的レビュー (実装) / `bin/skill-eval zundamon-kaisetsu`
