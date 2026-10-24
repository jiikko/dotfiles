# 900 (research): dupscan の次の言語 (Go・zsh) の読み取りを測る

起票日: 2026-10-24

## 概要

[891 (要件の正本)](891-design-dupscan-requirements.md) の「構想: 対応する言語」節。Swift の次は Go と zsh が候補 (ユーザー 2026-10-24)。足す前に、Swift と同じく
「対象の repo の全ファイルを読ませて、読めない構文 (ERROR / MISSING) が出るファイルを数える」を測ってから決める。
zsh は tree-sitter-bash を使う案で、zsh だけの構文 (`${(f)...}`・glob の修飾子など) を読めるかが分かれ目。Go は tree-sitter-go か標準の `go/ast`。

## 受け入れ条件

- [ ] dotfiles の Go と zsh のファイルで、読めない構文があるファイルの数と所要を測って書く
- [ ] 足すかと、足すならどの文法・道具かを決める (足すときは、言語ごとに adapter と網羅テストの子 issue を起こす)

## 進捗

- 2026-10-24 起票 (epic 890 の分解)
