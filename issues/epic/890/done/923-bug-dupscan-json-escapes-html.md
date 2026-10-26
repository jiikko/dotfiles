# 923 (bug): dupscan の JSON の出力が `<` `>` `&` を `\u003c` `\u003e` `\u0026` に escape する

起票日: 2026-10-26

## 概要

dupscan の JSON の出力 (report / gate / near / def / refs / callers / index の `-format json`、`-v` の「除外の 1 行」、効いていない除外の行、gate の記録) が、
Go の `encoding/json` の既定で `<` `>` `&` を escape している。JSON として読めば同じ値だが、人が読む・貼る・grep するときに困る。

## 詳細 (再現)

- `dupscan report -v` の「除外の 1 行」: `static location(_ connection: StorageConnection, path: String) -\u003e StorageLocation@b2522f088f64`。
  この 1 行は `.dupscan.json` に貼るためのもの。obaket の `.dupscan.json` は `->` が 323 か所・`\u003e` が 0 か所 (2026-10-26 に数えた) で、
  貼る側が毎回手で戻している (obaket issue 1305 でも python で書き直した)。戻さずに貼ると、`->` で grep しても当たらない除外が混ざる
- `def -format json` の本体の `&&` は `\u0026\u0026`、`Array<Int>` は `Array\u003cInt\u003e` になる

## 対応方針

JSON を書く 4 か所 (`report.WriteJSON`・`exclude.GroupSnippet`・`report.ruleJSON`・`report.AppendRecord`) を、escape しない 1 つの helper に通す (`json.Encoder.SetEscapeHTML(false)`)。
JSON として読んだ値は変わらない。

## 受け入れ条件

- [x] 4 か所とも `<` `>` `&` をそのまま出す。テストで固定し、helper の `SetEscapeHTML(false)` を外す変異で赤を確かめる
- [x] JSON として読んだ値が変わらない。obaket の `report -format json -limit 0 .` を変更前と比べ、`exclude` (除外の 1 行) 以外は外側を読んだ値が一致、`exclude` は内側の JSON まで読むと一致 (外側を読んだ後の文字列は escape の表記の分だけ変わる)
- [x] 1 行で出す所 (除外の 1 行・gate の記録) は改行を含まない (Encoder が足す末尾の改行を落とす)

## 進捗

- 2026-10-26: `internal/jsonout` (`Write` / `Line`。どちらも `SetEscapeHTML(false)`) を足し、4 か所をそこへ通した (commit「fix(dupscan): JSON の出力で < > & を escape しない (issue 923)」)
  - テスト: `internal/jsonout/jsonout_test.go` (Write と Line が `Array<Int>`・`-> Bool`・`a && b` をそのまま出し、読み戻すと同じ値・Line は改行を含まない)、
    `TestJSONOutputsKeepSwiftOperators` (report の `-v` の除外の 1 行・report の json・def の json)
  - 変異: `Write` の `SetEscapeHTML(false)` を外す → 両方のテストが赤 (report の json と Write の assert) / `Line` のを外す → 両方が赤 (除外の 1 行と Line の assert)
  - obaket の `report -format json -limit 0 .` を変更前 (PATH の dupscan) と比べた: 違いは `exclude` (除外の 1 行) の 122 か所だけで、どれも内側の JSON を読むと同じ値。
    変更前は内側の `\u003e` が文字のまま残っていた (外側を読んだ後も `\u003e` の 6 文字)。この欄を機械で読むのは dupscan のテスト (内側を読み直す) だけ
  - README の除外の節に「escape しないので、そのまま貼れる」を 1 行足した
- 2026-10-26 codex review (gpt-6.1-sol・high、読み取りのみ): P2 1 件。helper の設定を外す変異は捕まえるが、`ruleJSON` (効いていない除外の行) と `AppendRecord` (gate の記録) を
  個別に `json.Marshal` へ戻す退行はどのテストも見ていなかった → `TestStaleExclusionLineKeepsSwiftOperators` と `TestGateRecordKeepsSwiftOperators` を足した。
  ほかの的 (1 行の出力の末尾・記録の 1 回の write・HTML / shell の消費者・U+2028 / U+2029 (escape はそのまま残る)・helper を通らない残り) は反証できなかった
- 起票時の本文 (タイトルを含む) と jsonout.go のコメントで `\u003e` などの表記が `>` に化けていた (Claude が送るコマンドの中で `\uXXXX` が文字に戻される)。直して、書いた後に表記を grep で確かめた
- 2026-10-26 codex review を観点を変えて 3 本 (gpt-6.1-sol・high、読み取りのみ): ①JSON を読む側の互換性 ②テストの偽の緑 ③設計と文書。
  ①は壊れる読み手なし (dotfiles の wrapper・hook・scripts、obaket の lint gate・CI・skill の案内を調べた)。②が P2 3 件、③が P3 1 件。4 件とも採った
  (commit「test(dupscan): JSON の書き手の境界と、除外の行の形をテストで固定する (issue 923)」)
  - P2 効いていない除外の行が `{}` に退行しても、出力全体に別の `->` があるので緑だった → 行そのものを取り出し、貼った除外と同じ釘を持つかを見る
  - P2 `GroupSnippet` / `ruleJSON` が末尾に改行を付ける退行を見ていなかった → report の json の除外の 1 行に改行が無いこと、`ruleJSON` の出力を文字列で丸ごと比べる (report package の中のテスト)
  - P2 near / callers / index / refs のどれかが jsonout を通らずに書く退行を見ていなかった → コマンドごとのテストではなく、非テストの .go が jsonout の外で
    `json.Marshal` / `MarshalIndent` / `NewEncoder` を呼んだら落とす検査 (`TestJSONIsWrittenOnlyByJSONOut`、go/ast) にした。新しい書き手も捕まえる。走査が空振りしたら落ちる
  - P3 受け入れ条件の「JSON で比べて一致」が、除外の 1 行の 122 か所の差と食い違っていた → 比べた範囲どおりに書き直した
  - 変異: `ruleJSON` が `{}` を返す → 3 本赤 / `GroupSnippet` が改行を付ける → `TestJSONOutputsKeepSwiftOperators` 赤 / `ruleJSON` が改行を付ける →
    `TestRuleJSONIsOneLineWithoutHTMLEscapes` 赤 / refs が自前の `json.NewEncoder` で書く → `TestJSONIsWrittenOnlyByJSONOut` 赤 / 境界の検査の走査を空にする → 同じテストが「空振り」で赤

