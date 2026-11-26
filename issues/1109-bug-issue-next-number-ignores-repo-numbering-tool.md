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

- `issue_next_number.sh` が repo root に `bin/next-issue-number` を見つけたら、番号を出さずに断り、そのコマンドを案内して rc≠0 で終わる。委ねて実行はしない。理由は 2 つ:
  - 約束が違う。共通の道具は位置引数で `<ref>` を取り、省略すると `origin/HEAD` の参照先を使い、ref が読めなければ失敗する (`scripts/issue_next_number.sh`・`scripts/lib/issue_numbers.sh`)。
    obaket の道具は位置引数で ref を取らず `NEXT_ISSUE_ORIGIN_REF` で受け、何も渡さないと `origin/master` を数え、ref が読めなくても手元だけで採番して成功する
    (obaket の `bin/next-issue-number` と `bin/next-issue-number-test` が固定している)。素通しで委ねると、呼び出し側の期待が知らせずに変わる
  - 実行可能なファイルがあるだけで委ねると、採番を頼んだだけで checkout の中のコード (untracked のものも) を実行し、その stdout が番号になる。案内して止めれば、実行は呼び出し側が明示して打つ
- `bin/next-issue-number` が無くても、root 直下でない issue dir の候補 (`<root>/*/issues`) があれば stderr で警告する。候補の列挙は `_claude/hooks/lib/issue-hooks.sh` が既に持つ
  (`issues/`・`issue/` と入れ子の 1 段。依存ディレクトリは名前で除く) ので、それを使う。番号を共有しているかまでは分からないので、止めずに知らせるだけにする
- テスト: 一時の repo に偽の `bin/next-issue-number` を置いて、番号を出さず案内して rc≠0 になること (偽のコマンドが実行されないこと) / 入れ子の `x/issues/` があれば警告が出ること / どちらも無ければ今までどおり数えること、の 3 本。
  変異で断る分岐を外して red を見る
- 関連して見つけたもの (この issue の外): obaket の `.claude/rules/issue-placement.md` に、採番の説明として「3 桁ちょうど」「自分の作業ツリーだけ」が残っている。
  `bin/next-issue-number` は 3 桁以上を数え、origin の ref も数える。2026-11-26 に obaket の commit「docs(rules): issue-placement の採番の説明を bin/next-issue-number の今の挙動に合わせる」で直した

## 進捗

- [ ] 委ねる分岐と stderr の 1 行
- [ ] テストと変異
