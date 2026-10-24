# 903 (docs): 書く前に既存のメソッドを探す手順を、dupscan を待たずに使う側の repo の入口に書く

起票日: 2026-10-24

## 概要

epic 890 の主な用途 (Claude Code にコードを変更させている間の重複の防止と発見性) に、いちばん安く効く手当て。
名前で引く手段 (Claude Code の LSP ツールの `workspaceSymbol`・Serena) は今日から使えるので ([901](901-research-dupscan-existing-tools.md))、
dupscan の実装 (892〜897) を待たずに、新しいメソッドを書く前に探す手順を使う側の repo の入口 (CLAUDE.md か skill) に書く。
dupscan の索引 ([897](897-feat-dupscan-index-subcommand.md)) ができたら、署名の型と doc 文で引く手順を足す。

## 受け入れ条件

- [ ] 手順を書く: 新しいヘルパーを足す前に、名前の候補を `workspaceSymbol` で引く / 共通の置き場所 (obaket なら `SharedTestSupport`) を先に見る / 似たものがあれば寄せる
- [ ] LSP の落とし穴も書く: 初回は索引を作り終えるまで 0 件を返す (0 件を「無い」と読まない) / gopls はあいまい一致で 100 件で打ち切る (901 の実行)。obaket の手順は obaket issue 1299 と合わせる
- [ ] 書いた手順が読まれる場所 (毎回読まれる CLAUDE.md か、発火する skill か) を選んだ理由を書く

## 進捗

- 2026-10-24 起票 (反証レビュー (設計の観点) の指摘: 一番安く効く手当てが実装の全体の後ろに回っていた。902 から分けた)
