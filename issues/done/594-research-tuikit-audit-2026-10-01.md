# 594 (research): src/tuikit の監査 (設計・品質・テスト 14 タイプ、2026-10-01)

起票日: 2026-10-01

## 概要

`/audit` で src/tuikit を、設計 (design / responsibility / duplication / polymorphism / encapsulation / ui-components)・
品質 (security / resource-leaks / broken-code / dead-code / error-handling / performance)・テスト (test-cleanup / test-helpers)
の 14 タイプで監査した。直接実行 (forge なし)。読み取り専用の調査役 (sonnet) を 3 波に分けて 1 体ずつ直列に回し、
main が報告をコードと probe で裏取りしてから起票・修正した。

- 母集合: `src/tuikit` の Go 59 ファイル (production 30・test 29、合計 8,686 行)。消費者の照合は `src/` の 14 ディレクトリ全体
- 着手時点: `make -C src/tuikit lint` 0 件 / `make -C src/tuikit test` 全 green (termwidth だけ 248 秒。593)

## 結果 (全数勘定)

調査役の報告は計 26 件 (第 1 波 9・第 2 波 9・第 3 波 8)。起票 5 本 (589〜593) に 9 件を集約、その場で直した 6 件、却下・記録・未着手 11 件。

### 起票

| issue | 中身 | 出典 |
|---|---|---|
| 589 (pending) | lineedit が rune 単位で編集し書記素を割る + `Window` が O(n²) | 設計 #1・品質 F4 |
| 590 (pending) | 右寄せ overlay の 3 重実装 / `PanelInnerWidth(w-1)` の再計算 / wrap が無く x/ansi を直接呼ぶ (キーキャップで幅超えを実測) / markdown の `clipToWidth` の複製 | 設計 #2・#3・#4・#6 |
| 591 (閉じた) | highlight の正規表現が特定の行で数百 ms 止まり、markdown・diff の描画が数秒固まる | 品質 F1 |
| 592 (閉じた・416 の重複) | `StripSGR` / `DropColumns` が OSC を SGR として扱い断片と BEL を残す (実害の経路は見つかっていない) | 品質 F3 |
| 593 | termwidth のテストが -race で手元 248 秒・CI 388 秒 (3 本の総当たり) | 品質 F6 |

### この監査の commit で直したもの (テスト・文書)

- toast に `TestMain` (`widthenv.ExitIfUnsupported`) が無かった。`RUNEWIDTH_EASTASIAN=1 go test ./toast/` が理由の無い `--- FAIL` 3 件
  → 追加後は理由付きの 1 行で止まる (実測。layout / markdown / confirm と同じ形)
- `termwidth/truncate_width_test.go` の `TestTruncatePropertyWidth` 末尾の `checked != 20000` は恒真 (ループに continue が無い) → 削除
- `highlight` の `lexerForDiffPath` の `/dev/null` 分岐と `TestDiffDeletedFileDevNull`: 分岐を消しても全テストが green
  (`lexers.Match("/dev/null")` が元から nil。調査役が変異で確認) → 分岐とテストを削除し、理由を doc に残した
- `highlight_test.go` の `BenchmarkDiff` の前に残っていた旧 Test (閾値 5s) の説明 5 行を削除。案内の `go test -bench=HighlightDiff` は
  関数名と合わず何も走らないので `-bench=Diff ./highlight/` に直した (実行して benchmark が走ることを確認)
- `toast_test.go` の `for range SlideFrames + 2 { s.Advance() }` 10 か所を `settle(&s)` に (タイマーを集める 1 か所は対象外)
- `src/tuikit/CLAUDE.md` の消費者一覧に restartable が無かった。restartable は `go install …@<版>` のため replace を使わず
  擬似バージョンで固定する (意図的。`src/restartable/README.md`)。その旨を 1 行足した

### 却下・記録のみ (再提出するなら、ここに書いた条件を崩す根拠を添えること)

- **toast の種別を bool 2 つ (`ok` / `info`) で持つ** (polymorphism): 4 つ目の種別を足すと、`important()` が新しい種別を
  「重要な警告」と扱ってしまう形は本物。ただし種別を増やす予定が無い。trigger: 種別を足すとき、先に `Kind` enum へ寄せる
- **`listnav.Pager.Move` は 1 行送りで glide を止めない (`List.Move` は止める)**: 見た目が数フレーム遅れるだけで操作のずれは出ない。
  確度低・未実測
- **pro-con の終了ダイアログが `confirm.Dialog` に空の hint を渡し、空行 2 つが付く**: 意図した見た目かは未確認 (pro-con 側の判断)
- **Prepend 文字 (U+0600 等) を行末に置くと `Panel` / toast の右罫線が 1 桁ずれる** (`Panel("", {"ab؀"}, 20, …)` の行が幅 19。main で再現):
  書記素の規則で Prepend が直後の空白を取り込むため。入力に現れるのはアラビア語の数字記号などに限られ実害は小さい。
  trigger: 実際の表示で罫線のずれが報告されたとき。直すなら termsafe で Prepend を落とす (入口 1 か所)
- **markdown のインライン解析が病的入力で二次** (`"**a "`×32000 = 128KB で 5.8 秒。調査役の実測): 自然な本文 (数 KB) では起きない。
  trigger: 外部の長文を Render する消費者が増えたとき
- **dead-code**: `listnav.List` (と `Move` / `Fit` / `DrawCursor` 等) は消費者の production から 0 件で、使うのは `examples/listdetail` だけ
  (`grep -rEn 'listnav\.List\b' src | grep -v '^src/tuikit/'` → examples の 1 件)。pro-con の picker は `listnav.WindowOffset` / `Half` だけを使う。
  README は一覧の動かし方の正本として `List` を説明しており、demo の対象でもあるので削除は提案しない。
  `toast.Stack.Entries` / `Clear` は消費者のテスト (glogx の 3 ファイル) からだけ使われる test seam で、違反に数えない
- **`editor.Command` がパスを `--` 無しで渡す** (先頭が `-` のパスがエディタのオプションになる): 呼び出し側は絶対パスを渡すので発火しない想定。
  呼び出し側の path の由来は未確認
- **`markdown/wrap_test.go` の `TestWrapSpansKeepsStyleBoundaries` の前半 (スパンに ESC が無い) は恒真** という指摘: 入力に ESC が無いのはそのとおりだが、
  守っているのは「スパンは素の文字で持ち、色は `paintLine` が付ける」という層の分け方で、`wrapSpans` に色付けを入れる退行で落ちる。恒真ではないので残した
- 調査役の「lineedit の消費者に schedkeys が入る」は誤り (`grep -rl 'tuikit/lineedit' src` は pro-con の 3 ファイルだけ)。589 には入れていない
- テストの小さな重複 (toast の予算テストの末尾、termwidth の `TestDropColumns` の境界、markdown の非空行を集めるループ) は、
  issue 057 の再現の意図がコメントにある・まとめても数行しか減らない、のどちらかなので触らない

### 未着手の残り (起票はしていない)

- ~~markdown の行末禁則 (`noLineEnd`) をどのテストも見ていない~~ → 2026-10-01 にテストを足した (下の進捗)
- **「幅を assert するパッケージは TestMain で widthenv を呼ぶ」を機械で検査していない** (toast の漏れはこの監査で見つかった)。
  4 つ目の漏れが出たら、`tests/` に検査を足すかを決める

## 起票後の見直し (2026-10-01、「本当に意味があるか」を 2 回)

事実が正しいかではなく「実際の使い方で害が起きるか」(1 回目) と「手間に見合うか・既存の判断と重なっていないか」(2 回目。1 回目が害を
軽く見すぎていないかも逆向きに攻めた) を、read-only のサブエージェント 1 体ずつで見た。母集合は pro-con の状態 (`~/.local/state/pro-con/live`)、
`~/.claude/projects` の transcript、dotfiles の `issues/` と git 履歴、`~/src` の 34 repo。結果:

| issue | 判定 | 理由 |
|---|---|---|
| 589 | pending | 害は本物だが、複数 rune のクラスタも NFD も実際の入力に 0 件。`Window` の O(n²) は実際の長さで 1 ms 未満 |
| 590 | pending | A・B は今は壊れていない (4 か所目を足すときに寄せる)。C は害が行末の `…` だけで格下げ。`clipToWidth` は markdown を触るときについでに |
| 591 | 閉じた | 発火する形 (バックスラッシュの長い連続) が実データに 0 件。diff は開いたときに 1 回払うだけ。実際に起きうるのは minified の長い行 (50 KB で 94 ms) |
| 592 | 閉じた | 416 の P3-2 と同じ問題で、そこで「記録のみ」と決着済み (起票時に見落とした) |
| 593 | 残す | 前提を訂正: 手元はテストキャッシュが効く (2 回目は `(cached)` を確認)。毎回払うのは CI の 388 秒 |

**教訓**: 起票の前に「同じ問題が done に無いか」を、症状の語 (`StripSGR` / `OSC`) で `issues/done/` まで grep していれば 592 は起票しなかった。
監査の段階では「事実が正しいか」しか反証させておらず、「実際の入力に現れるか」は見直しで初めて測った。

## 起票した 589〜594 の反証レビュー (read-only のサブエージェント 1 体。codex は使っていない)

指摘 6 件、すべて main で裏を取って反映した。

- P1 (592): 「glogx の job 名は `dropEmojiVS16` しか通らない」は誤り。`detailsOf` が `sanitizeDetailLine` を通していた → 592 の実害経路の節を
  「見つかっていない」に書き直し、glogx 側の対応を外した。代わりの候補とされた pro-con の質問文も `card/choices.go` で無害化済みだった
- P2 (593): 子プロセスで同じ総当たりを回すのは「重複」ではない (`ansi.StringWidth` が env で値を変えるので別の検査) → 外す案を採らないと書いた。
  後ろに置く相手の日本語は既に間引き済みなので、方針 2 を「さらに間引く」に直した
- P2 (591): 発火には行に lexer が付くこと (フェンスの言語名・diff のパス) が要る → 発火条件に足した
- P3 (589): `FirstCluster` の実体は `ansi.FirstGraphemeCluster` で、uniseg と版が違って判断が割れた実測が既にある → 寄せる先を x/ansi と書いた
- P3 (590): A の書き方の弱さ / glogx の `wrapToWidth` のコメントがキーキャップを見ていない → 反映
- 反証できなかった主張: 589 の再現 (`👍🏽` の backspace・`Window` 150 ms)、590 の母集合と実測、591 の実測、593 の構造、594 の却下理由の事実面

## 攻めたが見つからなかった範囲

- design: 依存方向 (anim → listnav → layout → confirm / toast) は一方向で循環なし。bubbletea を import するのは `caret` だけ (depguard が守る)
- responsibility: toast.go (600 行)・termwidth.go (592 行) は大きいが、状態は `Stack` 1 つに閉じ、fast-path は差分 fuzz で固定されている。分割で複雑性が下がる見込みなし
- resource-leaks: tuikit の production に goroutine・Timer・Ticker・ファイル・exec の実行は無い (`go func` / `NewTimer` / `NewTicker` / `time.After` / `os.Open` / `cmd.Start|Run` の grep で 0 件)。toast は Timer を値で返すだけ
- broken-code: 調査役が 20 万回のランダム列で `Clip` / `Cut` / `TruncateLeft` / `DropColumns` / `Scrollbar` の幅の契約を見て、Prepend 以外は違反 0。markdown は 2 万入力 × 幅 7 通り × 色 2 通りで panic・幅超え 0
- security: markdown と toast の入口は termsafe を通り、調査役の 2 万件のファジングで ESC / BEL / C1 の漏れ 0。layout / confirm は無害化しないが、
  そこへ無害化していない外部の文字列が届く経路は見つかっていない (glogx の job 名は `sanitizeDetailLine`、pro-con の質問文は `card/choices.go` の `clean` を通る)。
  残るのは 592 の「`StripSGR` が SGR 以外の ESC 列を半端に削る」という tuikit 内の頑健性だけ

## 進捗

- [x] 14 タイプの調査 (3 波)
- [x] 起票 589〜593
- [x] テスト・文書の修正 (上の「直したもの」)
- [x] 行末禁則のテスト — `markdown/wrap_test.go` の `TestWrapSpansKinsokuKeepsBracketsAndMarksOffLineEdges` (全角の文を幅 4〜24 の全部で折り、
  開き括弧 `「（『【` が行末に、`」）』】ー…` が行頭に来ないことを見る。字は production の表から作らずテストに直接書く)。
  変異 2 本で red を確認: `noLineEnd` を空にする (幅 4 で「 が行末) / `noLineStart` を「、。」だけに縮める (幅 4 で 」 が行頭)。
  調査役の報告ではどちらも既存のテストでは green のままだった
