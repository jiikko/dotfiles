# 604 (research): src/tuikit の監査 (未実施の 7 タイプ、2026-10-01)

起票日: 2026-10-01

## 範囲とやり方

- 対象: `src/tuikit` の Go 62 ファイル (production 31・test 31、9,127 行)。消費者の照合は tuikit を go.mod に持つ 5 module (glogx・pro-con・ratelimit・restartable・schedkeys)
- 同じ日の 16:06 に 14 タイプを済ませている ([594](594-research-tuikit-audit-2026-10-01.md))。今回はその残りの 7 タイプ: leaky-abstraction / false-green / ux / general / dependency / issues-done / lint-from-done。594 の所見は再提出の対象外にした
- 直接実行。読み取り専用の調査役 (sonnet) を 3 本、1 本ずつ直列に回した (leaky-abstraction + false-green / ux + general / dependency + issues-done + lint-from-done)。主要な主張は Claude がコードとコマンドで確かめた (下の各項に書いた)
- 反証レビュー: 起票の前に read-only のサブエージェントで 1 回 (下の「反証レビュー」)

## 結果 (全数勘定)

調査役の所見は計 22 件 (第 1 波 8・第 2 波 6・第 3 波 8。0 件の範囲の報告を除く)。起票 2 本 ([602](602-test-tuikit-declared-guards-not-enforced.md) に 3 件・[603](603-chore-ansi-version-and-forbid-drift-across-modules.md) に 4 件)、ここに記録 13 項目 (14 件。restartable の tuikit と termsafe の固定の 2 件を 1 項目に)、見送り 1 件。

### ここに記録 (P3。必要になったら個別に起こす)

- [ ] **toast: 無関係な通知が進行中の info を消す** (ux): `push` が通知のたびに `dropInfo` する (`ShowInfo("build中")` の後の `Show("コピーした", true)` で info が消える。調査役の probe)。
  コメントに「結果が出たら用済み」と意図があり、Claude は設計の副作用と判断した (結果と無関係な通知を区別しない)。直すなら info にキーを持たせ、同じキーの結果でだけ退かせる
- [x] **confirm: 幅 38 桁未満で案内の「n/Esc: キャンセル」が先に切れる** (ux): 直した。既定の案内 (HintYesNo / HintYesOther) が入らない幅では「y: 実行  n: 取消」「y:実行 n:取消」へ替える (幅 20 以上で取り消しのキーが残る)
- [ ] **confirm / layout.Panel: 幅 10 桁未満で画面を超える** (ux): `PanelMinWidth` の doc は押し上げを明記しているが、呼び出し側へ知らせる口が無い (restartable は 601 で自前の 1 行表示にした)
- [ ] **markdown: colored=false で強調・コード・リンク先が消える** (ux): `` `code` `` の記号も URL も出ない。`RenderLinks` は links を別に返すので消費者が拾う余地はある
- [ ] **lineedit: Insert が書式文字 (unicode.Cf。U+202E 等) を落とさない** (ux): 制御文字だけを落とす。ZWJ は絵文字に要るので除外の範囲は要検討
- [x] **highlight.Code が chroma.Lexer を公開 API に出している** (leaky-abstraction L2): 直した。非公開の `codeLine` にした (外からの呼び出しは 0 件)
- [x] **restartable が confirm.IsYes と同じ y / Y / enter を手書きしている** (leaky-abstraction L6): 寄せない (理由を model.go にコメント)。confirm は y / Enter 以外をすべて取り消すが、restartable は n / N / Esc だけを取り消しにして、ほかのキーでは確認を開いたままにする。実行の集合だけを寄せると規則が混ざり、状態遷移の層が UI の部品を import する
- [x] **editor の doc の「glogx はまだこの package に寄せていない」が古い**: 直した (glogx と pro-con が使うと書いた)
- [x] **lineedit / listnav のキーの語彙の正本が、消費者側の docs/glogx-ui-guide.md を指す**: 依存の向きが逆 (部品の正本が消費者の文書)
  - 2026-10-02 決着: 向きは変えない。ガイドは glogx 専用の文書ではなく tuikit を使う TUI 全体の語彙の正本と位置づけ直した (題名を「TUI ガイド」にし、冒頭に「語彙の正本は本書、tuikit の部品は実装する側」と書いた)。ファイル名は参照が多いので据え置き。
    代わりに「部品を足す・決まりを変えたらガイドも直す」を tuikit の CLAUDE.md・README とガイド §10 に置き、ガイドに部品の地図と通知の語彙 (§9) を足した。
    ガイドの語彙表とコードの突き合わせを機械で止める検査は置いていない (`motion_vocabulary_test.go` / pro-con の `guide_test.go` は動作を固定するが、ガイドの文書は読まない)
- [ ] **restartable が tuikit を擬似バージョン 99b256e3 で固定し、HEAD より production 10 ファイル遅れている** (dependency): 固定は go install 経路のための意図 (restartable の CLAUDE.md)。589〜592 の修正が入っていない。
  `src_restartable.yml` の paths に tuikit が無く、追従の合図が出ない。restartable が import する tuikit は confirm・layout・termwidth だけで (反証レビュー)、
  固定以降にこの 3 つで変わったのは `layout/panel.go` の追加 (+36 行) と使っていない `termwidth/wrap.go` の新設だけ (`git diff --stat 99b256e3 HEAD`)。今は遅れの実害は無い。
  tuikit 自身も termsafe を擬似バージョン (20260930152350) で要求していて、go install 経路の restartable に termsafe の変更が届くかは未確認 (第 2 波)
- [ ] **termwidth の !arm64 の経路が CI で走らない** (false-green F4): CI は macos-15 (arm64)。`GOARCH=amd64 go vet` でコンパイルは通る。doc に明記済みで、範囲の申告として記録
- [x] **pending/049 の参照先が古い** (issues-done の副産物): 049 の冒頭に移動先を注記した (呼び出しは今 3 か所: glogx の gitlog.go・worktree_status.go と pro-con の ui/diffview.go。最初は 2 か所と数え違えた)。完了ではないので pending のまま: 本文の `src/glogx/highlight.go` / `HighlightDiff` は今 `src/tuikit/highlight` の `Diff` (ベンチは `BenchmarkDiff`)。完了ではない (trigger 待ち)
- [ ] **govulncheck が無く、依存の脆弱性は未確認** (dependency)

### 見送り

- 589 の `[]rune(` の禁止を lint に: forbidigo は型変換を捕まえられず偽陽性も多い。591 (正規表現の逆戻り)・484 (予算)・592 (OSC) は字句の lint にならない

## 0 件の範囲 (攻めて見つからなかった所。次の監査の起点)

- leaky-abstraction: production に `interface {` が 0 件で、L4 / L7 は対象なし。L3 (識別子分岐) 0 件。glogx/doctor_delete.go の独自のキー表は「破壊的確認で中止のキーを送りに化けさせない」と明記された意図。Border の字幅の暗黙契約は、消費者が `layout.Border{` を組む箇所が 0 件で発火しない
- false-green: Makefile と CI は `go test -race ./...` / `run ./...` で examples も含む。F1 / F3 は 0 件。termwidth の子プロセスの検査・fast 比率の下限・Panel の最小幅は、判定不能を自分で失敗にしている。他 package の `RUNEWIDTH_EASTASIAN=1` の TestMain の漏れは 0
- ux: markdown の幅超えは幅 1・2・3・4・8・20 × 色の有無で 0。キーの語彙の衝突は doc に明示済み。confirm.IsYes は y / Y / enter 以外を取り消しに倒す
- general: depguard / forbidigo と README の「不変条件の守り」の表は一致 (toast / lineedit の漏れは 602)。widthenv の主張 (x/ansi が RUNEWIDTH_EASTASIAN を ParseBool で読む) は x/ansi v0.11.7 で裏取り済み
- dependency: `go mod tidy -diff` 差分 0 (未使用の依存なし)。同じ機能の重複依存 0
- issues-done: done 以外の issue 153 本のうち tuikit に触れるのは 415 (epic の親) の 1 本で、完了ではない。完了と判定できたものは 0 件

## 反証レビュー

起票の前に read-only のサブエージェント (sonnet) で反証させた (codex は使っていない)。事実の誤りは 0 件。直したのは:
- 603: 「0.11.7 と 0.11.8 で挙動が変わるかは未確認」→ module cache の diff で、tuikit の経路 (GraphemeWidth) は変わらないと確定した。重要度を下げ、予防の揃えに書き直した。runewidth の分かれ方を module 別に書いた
- 604: restartable の固定の遅れは、使う 3 package では追加だけと確定した
- 602: parts-pure に足しても time の import は止まらない (forbidigo が担う) ことを書き足した。2・3 の変異はレビュー側でも緑を再現した

反証できなかった主張: Go の 62 / 31 / 9,127 行、x/ansi の版の表、forbidigo の有無、editor の doc の古さ、049 の参照先、src_restartable.yml の paths。594・589〜601 との重複・修正済みは 0 件

## 結果 (2026-10-01)

- 直した: 上の [x] の 5 項目 (confirm の案内・highlight.Code・restartable の判断の記録・editor の doc・049 の参照先)。confirm の案内は、差し替えを外す変異で
  `TestDialogHintKeepsCancelKeyWhenNarrow` が赤。`make test` rc=0 (glogx・pro-con の案内を見るテストも通る)
- 直さない (判断が要るもの・害が無いもの。[ ] のまま。必要になったら個別に起こす):
  - toast の info を無関係な通知が消す: info にキーを持たせる API の変更になる。コメントの意図 (結果が出たら用済み) とも衝突するので、困った事例が出たら
  - Panel が幅 10 未満で画面を超える: 下限は doc に明記した仕様。restartable は自前の 1 行表示で済ませている (601)
  - markdown の色なしで強調・URL が消える: 描画の契約を変える (消費者の表示が変わる)。links は RenderLinks が別に返す
  - lineedit の書式文字 (Cf): ZWJ・異体字セレクタを残す範囲の判断が要る
  - lineedit / listnav のキーの語彙の正本が消費者側の docs を指す: 文書の置き場の移動で、挙動に害は無い
  - restartable の tuikit の固定の遅れ: 使う 3 package は追加だけで害が無い (上の項)。termwidth の !arm64 の経路・govulncheck は記録のまま
