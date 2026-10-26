# 900 (research): dupscan の次の言語 (Go・zsh) の読み取りを測る

起票日: 2026-10-24

> **zsh / shell script は対応しない** (ユーザー決定 2026-10-26。理由と、対応するなら要る改修は `docs/dupscan-overview.md` §4 の言語の表)。この issue に残るのは Go だけ。
>
> **再開** (2026-10-26。ユーザー指示: Go を go/ast の adapter で実装する。手順は「対応方針」)。以下は保留していたときの記述: **保留** (2026-10-25)。ユーザー指示 (2026-10-25: epic 890 は Go・zsh 対応を除いて done にする)。**再開の条件**: dupscan を Go の repo でも使いたくなったとき。

## 概要

[891 (要件の正本)](done/891-design-dupscan-requirements.md) の「構想: 対応する言語」節。Swift の次は Go と zsh が候補 (ユーザー 2026-10-24)。足す前に、Swift と同じく
「対象の repo の全ファイルを読ませて、読めない構文 (ERROR / MISSING) が出るファイルを数える」を測ってから決める。
zsh は tree-sitter-bash を使う案で、zsh だけの構文 (`${(f)...}`・glob の修飾子など) を読めるかが分かれ目。Go は tree-sitter-go か標準の `go/ast`。

Go の重複のチェックは dupl (golangci-lint) が dotfiles の Go の 7 module (doctor・glogx・mac-lan-manager・pro-con・ratelimit・treefiler・tuikit) で有効。閾値はどれも 120 トークンで、tuikit 以外はテストを除外 (`src/*/.golangci.local.yml`)。
Go を足す価値は、索引と `gate` ([905](done/905-feat-dupscan-ci-check-gate.md)) の側と、dupl の閾値より小さいヘルパーの写しにある。

## 対応方針 (2026-10-26。ユーザーと合意)

- dupl・gopls のラッパーにはしない。価値は `index` と `gate` (dupl に無い) にあり、gate の判定は単位ごとのハッシュで base と head を比べる形なので、dupl の出力 (一致した区間) には乗らない。dupl は lint に残して並べて比べる
- 構文は標準の `go/ast` で読む。コアは tree-sitter の木ではなく `internal/tree` の中立の `Unit` / `Node` だけを見るので、adapter が go/ast をそこへ写せばよい
- `def` / `refs` / `callers` は Go では後回し (Claude Code の LSP ツールで gopls が使える)
- 手順: ① コマンド層が `lang/swift` を直に呼んでいる所を、拡張子で adapter を選ぶ口に寄せる (挙動は変えない) ② Go の adapter ③ dotfiles の Go の 7 module で較正 (dupl の閾値より小さい写しが拾えるか) ④ Go の module の make test に gate を入れるかを決める

## 受け入れ条件

- [ ] dotfiles の Go のファイルで、読めない構文があるファイルの数と所要を測って書く
- [ ] 足すかと、足すならどの文法・道具かを決める (足すときは、言語ごとに adapter と網羅テストの子 issue を起こす)

## 進捗

- 2026-10-24 起票 (epic 890 の分解)
- 2026-10-24 Go は dupl が既に回っていることを書いた (901。反証レビューで 3 → 7 module に直した)
- 2026-10-26 zsh / shell script は対応しないと決めた (ユーザー決定)。overview §4 の表に理由・要る改修・再評価の条件を書いた。受け入れ条件を Go だけに絞った
