# 1109 (bug): issue_next_number.sh が repo 固有の採番の道具 (bin/next-issue-number) を無視して、数えていない issue dir と衝突する番号を返す

起票日: 2026-11-26

- 由来: obaket retro 1555 の 4 (2026-11-26 にユーザーが「片付けて」と指示し、retro の提案どおり起票)

## 概要

`scripts/issue_next_number.sh` は repo root の `issues/` だけを数える (`[ -d issues ]` を確かめて `issue_max_number` を呼ぶ)。
obaket は `issues/` と `macOS/issues/` で番号を共有し、採番は `bin/next-issue-number` が両方を数える (obaket の `issues/README.md` と、そのスクリプトの冒頭の「なぜ」)。
共通の道具を obaket で呼ぶと、`macOS/issues/` の番号を数えないまま、知らせずに狭い答えを返す。

## 起きたこと

- 2026-11-20 (obaket retro 1555): retro の番号を `issue_next_number.sh` で取り 1554 を得た。`macOS/issues/` に 1554 が既にあり、pre-push の gate (`check-issue-number-unique`) が止めた。`bin/next-issue-number` で 1555 に採り直した
- 2026-11-26: 同じ道具で obaket の 1588 を取った。今回は `macOS/issues/` に 1588 が無く衝突しなかった (`find issues macOS/issues -name '1588-*'` で 1 件)。1555 の retro を読む前の出来事で、同じ形を 2 回踏んだ
- どちらも、repo の README を読まずに共通の道具を呼んだ手順の誤り。共通の規約 (`_claude/issue-rules.md` の冒頭) は採番の道具を repo 固有の事項として README に置くと書いている。ただ、道具の側は repo 固有の道具があることを知らせない

## 対応方針

- `issue_next_number.sh` が repo root に `bin/next-issue-number` (実行可能) を見つけたら、それに委ねて出力をそのまま返す (引数の `<ref>` の扱いが合わないなら、委ねずに断ってそのコマンドを案内する)
- 委ねる・断るは stderr に 1 行出す (どちらが番号を出したかが分かるように)
- `issues/` 以外に issue の dir を持つが道具を持たない repo は、この変更の外 (見つけ方が無い)。今そういう repo があるかは未確認
- テスト: 一時の repo に偽の `bin/next-issue-number` を置いて委ねること、無ければ今までどおり数えること、の 2 本。変異で委ねる分岐を外して red を見る

## 進捗

- [ ] 委ねる分岐と stderr の 1 行
- [ ] テストと変異
