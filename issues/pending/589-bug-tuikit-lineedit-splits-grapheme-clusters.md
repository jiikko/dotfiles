# 589 (bug): tuikit の lineedit が rune 単位で編集し、肌色・ZWJ・結合文字を割る

起票日: 2026-10-01

> **pending (2026-10-01 の見直しで凍結)**: 害は本物 (送る文字列が黙って変わる) だが、今の使い方では入力に現れていない。
> pro-con の状態 (cards.json・cards-archive.jsonl・events) に 4 バイトの絵文字・肌色・ZWJ・キーキャップは 0 件、
> PG の transcript で利用者が入力した文 7,496 件にも NFD の濁点 (U+3099 / U+309A) は 0 件。lineedit を使うのは pro-con の 3 ファイルだけ。
> `Window` の O(n²) は、実際のカード本文の長さ (n=48、最大 690 字) では 1 ms 未満なので、書記素の修正のついでに直す扱いにする。
> **再開の trigger**: カードの本文・回答に絵文字や Finder から貼ったファイル名 (NFD) を入れ始めたとき / lineedit の消費者が増えたとき /
> schedkeys の editor を lineedit へ寄せる作業が来たとき

## 概要

`tuikit/lineedit` の `Line` は `[]rune` とカーソル (rune の位置) を持ち、backspace / delete / ←→ /
`Window` の前切りを **1 rune ずつ**行う。見た目の 1 文字 (書記素クラスタ) が複数 rune から成る入力では、
1 回の操作でクラスタの一部だけが消え・動き、**見た目はほぼ同じまま別の文字列が送られる**。

同じ欠陥を schedkeys の入力欄は 2026-08-28 の敵対的レビューで踏んで直している
(`src/schedkeys/editor.go` の `prevBoundary` / `nextBoundary` の 🚨 コメント:
「👍🏽 の backspace 1 回で 👍 になる」)。tuikit へ切り出した lineedit にはその直しが入っていない。

## 詳細

該当: `src/tuikit/lineedit/lineedit.go`

- `Line.Key` の `backspace` / `ctrl+h`: `l.r[:l.cur-1]` で 1 rune 落とす
- `delete` / `ctrl+d`: `l.r[l.cur+1:]` で 1 rune 落とす
- `left` / `right` (`ctrl+b` / `ctrl+f`): カーソルを 1 rune 動かす → クラスタの途中にカーソルが入り、
  次の Insert がクラスタの中に文字を差し込む
- `Line.Window`: 欄に収めるため `before` の頭を `utf8.DecodeRuneInString` で 1 rune ずつ削る →
  欄の左端に肌色修飾子や VS16 だけが残った表示になりうる (表示だけの問題)
- `Line.Window` は前を 1 rune 削るたびに `termwidth.Of(before)` で先頭から測り直すので、入力の長さに対して O(n²)。
  実測 (2026-10-01、`"あa"` の繰り返し 10000 rune を `Window(40)`): **151 ms**。調査役の測定では 1000 字 2 ms、50000 字 3.7 秒。
  `Window` は欄を描くたびに呼ばれるので、長文を貼ると入力欄を描くたびにこれを払う
- `wordStart` / `wordEnd` は空白で区切るので、語の内側のクラスタは割らない (問題なし)

消費者: pro-con だけ (`grep -rl 'tuikit/lineedit' src` で `pro-con/ui/answerform.go` / `search.go` / `model.go`
の 3 ファイル。glogx・schedkeys・ratelimit・restartable は import していない)。answerform は PG の質問への回答、
model.go の入力欄はカードの本文なので、壊れた文字列はそのまま PG / カードへ渡る。

### 発火条件 (コードを読んだ結果。実機での入力は未実測)

- 入力 `👍🏽` (U+1F44D U+1F3FD) の末尾で backspace 1 回 → `👍` が残る (期待は空)
- `👨‍👩‍👧` (ZWJ 列) / `🇯🇵` (国旗 = 2 rune) / `é` (e + U+0301) / `1️⃣` (キーキャップ) でも同じ形
- silent: compile もテストも止まらない (`lineedit_test.go` に複数 rune のクラスタの入力が無い。
  `grep -n '🏽\|👍\|ZWJ\|\\u200d' src/tuikit/lineedit/*.go` で 0 件)

## 対応方針

- カーソルの移動と削除を書記素クラスタ単位にする。境界は `termwidth.FirstCluster`
  (tuikit の幅の単一情報源。実体は `ansi.FirstGraphemeCluster`) で求める。schedkeys の `eachBoundary` は `uniseg.StepString` を
  直接使っており、境界の求め方が 2 実装になっている。寄せる先は x/ansi 側: `termwidth/width_fast_test.go` のコメントに、
  uniseg (Unicode 15) と x/ansi (Unicode 16) で境界の判断が割れた実測 (2026-09-03、"a"+U+0897) がある
- `Window` の前切りもクラスタ単位にし、同時に 1 回の走査にする (末尾側から幅を足して `w-1` を超えたところで止める。境界は書記素で取る)
- カーソルの単位 (`Cursor()` の返り値) を rune 位置のまま保つか、クラスタ境界に限った rune 位置にするかを決める
  (呼び出し側 `pro-con/ui/answerform.go` の `fieldView` はカーソルの位置を `Window` の col 経由で使う)
- 直したら schedkeys の editor を lineedit へ寄せられるかを見る (寄せられれば境界処理の実装が 1 つになる。
  schedkeys は複数行の editor なので、寄せられない場合は理由をコードに残す)
- 回帰テスト: `👍🏽` の backspace / delete / ←→ / Insert をクラスタの途中で、ZWJ 列・国旗・結合文字でも。
  rune 単位の実装に戻す変異で red になることを確かめる

## 関連ファイル

- `src/tuikit/lineedit/lineedit.go` (`Line.Key` / `Line.Window`)
- `src/schedkeys/editor.go` (`prevBoundary` / `nextBoundary` / `eachBoundary` — 直した側の実装)
- `src/tuikit/termwidth/termwidth.go` (`FirstCluster`)
- `src/pro-con/ui/answerform.go` / `search.go` / `model.go` (消費者)

## 進捗

- [ ] 書記素単位の移動・削除
- [ ] `Window` の前切りを書記素単位かつ 1 回の走査に (確保・計測の回数で固定するテスト)
- [ ] 回帰テスト + 変異で red を確認
- [ ] schedkeys の editor と寄せられるかの判断を記録
