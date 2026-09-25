# 489 (bug): attach の間の指示が、履歴を時刻の順に並ばなくしうる

起票日: 2026-09-26

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

440 の 1 回目の dogfooding で、428 のレビューのときに見つけた小さな点 (未起票だった)。attach から戻ったときに残す「attach の間に人間が PG へ打った指示」は、
**打った時刻**を付けたまま履歴の**末尾**に足される。その間に dispatcher が別の出来事 (テストの係の結果・PG の質問 など) を履歴へ足していると、
履歴が時刻の順に並ばなくなる。

## 詳細 (2026-09-26 に読んだ)

- `src/pro-con/store/store.go` の `case "attach"`: `c.History = append(c.History, card.Event{At: e.At, ...})`。`e.At` は打った時刻で、適用した時刻ではない
- 履歴を読む側 (`cardview.go` の履歴・画面の引き出し) は、並びが時刻の順だと前提にしているかを確かめる (並べ直していなければ、表示の順と時刻が食い違う)

## 対応方針 (候補)

- 足すときに時刻の位置へ差し込む、または読む側で時刻で安定に並べ直す。どちらにするかは、履歴を「起きた順の記録」と「適用した順の記録」のどちらとして扱うかで決める
- 🚨 受け側のガードを先に洗う: 履歴の末尾を「最新」として読む箇所 (末尾の出来事で状態を判定している所) を grep してから変える

## 関連ファイル

- `src/pro-con/store/store.go` の `case "attach"`
- `src/pro-con/cardview.go` の履歴の表示、`src/pro-con/ui/drawer.go`

## 関連

- 440 (元の未起票の項目) / 428 (attach の間の指示を残す)

## 対応 (2026-09-26 C-045)

- **履歴は「起きた順の記録」として扱う**。attach の間の指示だけが過去の時刻で後から届くので、足す側 (`store.go` の `insertByTime`) で
  打った時刻の位置へ差し込む (同じ時刻なら先に在る出来事の後ろ)。読む側で並べ直す形にしなかったのは、読む側が複数 (`cardview.go` / `ui/drawer.go` /
  `card.go` の `HandedOff` / dispatcher の末尾を見る所) あり、1 か所でも並べ直し忘れると同じ食い違いが残るため
- 受け側のガード (末尾を「最新」として読む所) を洗った: `dispatcher.go` の `AutoClearText` と同じ文の重複の検査・`afterWaitPrefix` の検索・
  `btw.go` の直近の出来事。どれも attach の文 (`AttachPrefix` 付き) を判定に使わず、差し込みで末尾が attach でなくなっても判定は変わらない。
  `btw.go` の「直近の出来事」は時刻の順になった分だけ正しくなる
- 時刻の無い指示 (`At` が零) は受けない (差し込むと履歴の先頭へ行く)。live の `HumanPrompts` は transcript の時刻を付けて渡す
- 🚨 `apply` はカードを浅くコピーするので、`slices.Insert` がその場でずらすと適用前の state の履歴を書き換える。`slices.Clip` で新しい配列へ差し込む
  (`archive.go` と同じ形)
- テスト: `store_test.go` の `TestAttachInsertsSaidVerbatimByTime` / `TestAttachInsertKeepsPreviousState`。変異 3 本 (末尾へ足す / 時刻なしの検査を外す /
  Clip を外す) でそれぞれ落ちることを確かめた
