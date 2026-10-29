# 972 (research): glogx の tui.go の分割 (971) で dupscan を使った所見

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-29〜）

起票日: 2026-10-29

## 概要

skill dupscan-dogfood で、src/glogx/tui.go (4241 行) を責務ごとの browse_*.go 9 本へ分ける作業 (issue 971) を行った。
dupscan の修正は、同じ時間帯に別のセッションが src/dupscan を触っていた (半日以内の commit が 5 本・970 の claim) ので行わず、記録だけにした。
既存の issue との重複照合はしていない (skill の 3 節)。所見の記録だけの issue なので反証レビューは省いた (skill の 5 節)。

## 集計

- 摩擦 3 件 (雑音 3)
- dupscan を使った回数: 4 (`capabilities` 1・`index -file` 1・`gate` 1・Write の hook の候補の注入 1 (自動))
- grep に戻した回数: 約 8。理由の内訳: 文字列・コメントの中の `tui.go` の名指しを探す (6。dupscan の対象外で、grep が正しい) /
  式の形 (`statusOv.Visible() {`・`"ctrl+c"`) で処理の置き場所を探す (2。dupscan は式を引かない)
- 役に立った場面 2:
  - `dupscan index -file src/glogx/tui.go -limit 0` で、関数 145 個の行範囲と呼び出しの数が 1 回で取れた (26394 B)。分割の対応表 (名前 → ファイル) の元にした
  - 移設の後の `dupscan gate` (repo root) が rc=0。移した 187 単位 (`変わった単位 187`) を「写しが増えた」と数えなかった (移設で偽の赤が出なかった)

## 摩擦

### F1 Go のファイルの index -file に Swift の注意書きが出る
- 場面: `dupscan index -file src/glogx/tui.go -limit 0`
- 期待: Go のファイルなので Go に関する注意だけ
- 実際: 先頭行に「Swiftの既定引数を省略した呼び出しは取りこぼす。」が出る。出力 26394 B / 146 行、rc=0
- その後: 読み飛ばした
- 提案: 対象の言語が 1 つに決まるとき (-file の拡張子・走査した単位の言語) は、その言語の注意だけを出す

### F2 1 ファイルを指定した index に、無関係なファイルの「解析不完全」が出る
- 場面: 同上
- 期待: 指定した 1 ファイルの解析の可否だけ
- 実際: stderr に「解析不完全: 読めない構文を含むファイル 1 個: src/subproc/boundarycheck/testdata/broken/broken.go」(tui.go と無関係の testdata)。
  `dupscan gate` にも同じ行が毎回出る
- その後: 読み飛ばした
- 提案: -file のときは、指定したファイルの解析の可否だけを言う。testdata の壊れた fixture は既定で数えないか、除外の一覧に載せる

### F3 tmp/ の使い捨ての道具を書いたときにも、候補の注入が出る
- 場面: Write で `tmp/971-2026-10-29/tool/main.go` (repo の module の外の使い捨ての main) に `apply` という関数を書いた
- 期待: module の外・tmp/ の下は対象外 (寄せる先にならない)
- 実際: PreToolUse の hook が「`apply` に近い既存の定義」として pro-con の `Apply` 3 件 (store / fake / live) を注入した。どれも別の役割
- その後: 無視した
- 提案: hook は、書くファイルが `src/` の module の中にあるときだけ注入する

## 見送り

- 無し
