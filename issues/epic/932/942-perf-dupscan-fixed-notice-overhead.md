# 942 (perf): 狭い検索で dupscan が grep より大きくなる — 毎回の固定の注意書き

> 🚨 **担当中: air5 (glogx)**（2026-10-28〜）

起票日: 2026-10-28

## 概要

dupscan は「grep より context を食わない」前提で勧めているが、この前提が成り立つのは**広い名前**の検索だけで、目的の定義 1 件を引くような**狭い検索**では dupscan のほうが大きい。原因は、結果の件数に関係なく毎回出る注意書き (`index` で 400〜470 B・`callers` で 427 B) である。0 件・結果が空のときの注意書きは保ったまま、狭い検索の固定費を下げる。

## 実測 (2026-10-28、dotfiles の `src` の Go 1258 個を repo root から検索。byte は stdout + stderr)

| 検索 | grep 系 | dupscan |
|---|---|---|
| 定義を 1 件 (`ReadCookies`) | `rg -n 'func .*ReadCookies' src -g '*.go'` **340 B** / 3 行 | `index -name ReadCookies` **738 B** (stdout 492 + stderr 246) |
| 呼び出し元 (`ReadCookies`) | `rg -n ReadCookies src -g '*.go'` 1,954 B / 17 行 (定義・コメント・テストが混ざる) | `callers ReadCookies` 1,811 B (1,563 + 248) |
| 広い名前 (`Join`) | `rg -n Join src -g '*.go'` **約 43〜45 万 B** / 約 4,000 行 (レビューの再測定は 433,246 B / 4,014 行。path の接頭辞の差) | `index -name Join` 2,900 B / `callers Join` 3,293 B |
| 大小が違う名前 (`ProcessRunning`。実体は `processRunning`) | `rg -n ProcessRunning` 0 B (当たらない) | `index -name ProcessRunning` 738 B (1 件に到達) |

- 名前 3 つ・repo 1 つだけの測定で、一般の効果ではない。rg は大小を区別する既定で比べている。dupscan は repo root からの実行で、`src` の中から実行すると `index` は 669 B になる (次の Swift の文の差)。
- **固定費**: 結果の件数に関係なく、stdout の先頭に「構文近似…」の 1 行 (`index` 225 B・`callers` 179 B)、stderr に解析不完全の通知 (246〜248 B。`src/subproc/boundarycheck/testdata/broken/broken.go` は構文エラーを意図した fixture で、毎回読めない扱いになる) が出る。root 実行の合計は `index` 471 B・`callers` 427 B で、狭い検索の出力の過半 (定義 1 件で 471 / 738 B = 64%) を占める。
- `index` の「構文近似」行の「Swiftの既定引数を省略した呼び出しは取りこぼす」は、**読んだ scope に `.swift` があるときだけ**出る実装 (`src/dupscan/query_ux.go` の `callApproximation`)。root 実行では dotfiles が追跡する `scripts/lib/terminal_profile_appearance.swift` が scope に入って 225 B になり、`src` の中から実行すると出ず 156 B になる。この 225 B は dotfiles に Swift が 1 つあることの副産物で、Go だけの scope の固定費は **`index` 402 B (156 + 246)**・`callers` 427 B である。

## 守ること (この issue の不変条件)

- **0 件のとき「0 件は不存在の証明ではない」を省かない**。結果が空のときの解析不完全の通知も現行と同じ全文を保つ。
- 🚨 **[926](done/926-feat-dupscan-query-diagnostics.md) は「hit・0 件・抽出単位 0 のすべてで」解析不完全を通知する契約を、4 コマンド × hit / no_match / zero_units × text / json の 24 ケースで固定している**。下の b・c は、その **hit 側を弱める提案**である。採るなら 926 の hit 側のケースを明示的に改定し、理由 (本 issue の実測) を 926 に書く。黙って省かない。まず省かずに**短くする**案 (b') を先に評価する。929 の gate の通知は変えない。
- gate の解析不完全通知 (929) は変えない。変えるのは探索のコマンド (`index` / `callers` / `search` 等) だけ。

## 方針 (候補。全部は実装しない)

a. (実装済み。変更しない) Swift の既定引数の文は scope に `.swift` があるときだけ出る。確認のみ。
b'. **省かずに短くする**: 解析不完全の通知を 1 行 (件数と先頭 1 path) にする。hit でも通知は残るので 926 の契約の範囲に収まる。
b. **結果が 1 件以上のときは短縮形**(1 行・60 B 前後を目標。実装時に決める)、0 件のときだけ全文。926 の hit 側の改定を伴う。
c. **解析不完全の通知は、読めなかったファイルに問い合わせの名前が出現するときだけ全文にする** (926 の hit 側の改定を伴う)。条件: ①**dupscan 自身の名前照合と同じ規則を共有する** (照合は大小を無視する。`index -name processrunning` が `processRunning` に当たる。大小を区別する文字列包含で判定すると本物の候補を含みうるファイルの通知を消す。規則を二重に持たない) ②名前を取らない問い合わせ (`callers -selector`・`near`・`search`・gate) と、`Type.member` の qualified 形・ラベル付き照合は対象外にする ③I/O で読めず中身を走査できないファイル・非 UTF-8・BOM 付きは「出現する」側に倒す。別名 import・cgo・埋め込みで昇格したメソッドは名前の文字列がどこかに残るので、c では壊れないと見ている (未検証。fixture で確かめる)。
d. **`testdata` などを既定の対象から外す** (go tool は `testdata`・`_`・`.` 始まりの dir を無視する)。report / gate の意味が変わり (fixture の写しを見なくなる)、docs の既存の決定との照合が要るので、a〜c で足りない場合の別判断とする。
e. **不採用候補**: 同一 session の 2 回目以降は注意書きを省く。会話が compaction されると注意書きの無い結果だけが残り、「0 件 = 不存在」の誤認に戻る恐れがある (推測。根拠は実測していない)。

## 受け入れ条件

- [ ] 上の 4 クエリを同じ手順で測り直し、変更前後を表で記録する。目標は「定義 1 件の検索が rg の目的検索より小さい」こと。未達でも差を書く。
- [ ] 0 件・結果が空の出力は現行と同一 (golden で固定し、変異で崩して赤になることを確認する)。b・c を採る場合は、926 の 24 ケースのうち hit 側の期待値を改定した差分をレビューに出す。
- [ ] Swift の repo を 1 つ以上加え、同じ表を作る (dotfiles の Go だけで一般化しない)。
- [ ] 注意書きを減らしても、0 件を不存在と読む誤りが増えていない: [925](925-feat-dupscan-helper-search.md) / [927](927-feat-dupscan-agent-context.md) の固定課題で確認する。確認できなければ未確認と書く。
