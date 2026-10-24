# 890 (epic): dupscan — 構文木で重複しているメソッドをあぶり出し、似たメソッドの量産を書く前に止める道具

起票日: 2026-10-24

> epic 890 の親 issue。要件と構想の正本は [891](891-design-dupscan-requirements.md)。作業は同じディレクトリのタスクの子 issue で進める。

## 概要

repo で実行すると、テスト / 製品コードのそれぞれで重複しているメソッドを組にして列挙し (重複のチェック)、
これからコードを書く人が似たメソッドを探さずに量産するのを書く前に止める索引を出す道具。Go のコア + 言語ごとの差分 (adapter)。最初の言語は Swift。
obaket の週次の Claude audit の PR 24 で、名前の違う同じ中身のヘルパーが 6 つ見つかったことが起点 (経緯は obaket issue 1298)。

**主な用途 (ユーザー 2026-10-24)**: Claude Code にコードを変更させている間に、重複したメソッドを作らせない・既存のメソッドを見つけやすくして実装を滑らかにする。
後からまとめて重複を洗う使い方 (週次の audit) より、編集の最中に効く使い方 ([902](902-feat-dupscan-claude-code-hook.md)) を先にする。
**既存の道具の調査 ([901](901-research-dupscan-existing-tools.md)) を先に済ませ、足りない部分だけを作る** (当初は既存の道具を調べていなかった)。

## 子 issue

| 番号 | 何をするか | 依存 |
|---|---|---|
| [891](891-design-dupscan-requirements.md) | 要件と構想の正本 (用途・コアと adapter の境界・正規化の段・出力・索引・実測・構想・決めること) | — |
| [901](901-research-dupscan-existing-tools.md) | **既存の道具の調査** (CPD・jscpd とその MCP・DRYwall・dupl・similarity・ast-grep・LSP・Serena・意味検索の MCP)。PR 24 の 6 つに当てて、自分で作る範囲を決める | — |
| [903](903-docs-dupscan-search-before-write-procedure.md) | **書く前に既存のメソッドを探す手順**を、dupscan を待たずに使う側の repo の入口に書く (LSP の名前検索から。索引ができたら足す) | — |
| [892](892-chore-dupscan-project-scaffold.md) | `src/dupscan` の骨組み (module・Makefile・bin・CI・README・PACKAGES.md、cgo の所要の実測) | 901 |
| [893](893-feat-dupscan-core-normalize-type2.md) | コア: 走査・test / prod の分類・単位・正規化 L1〜L3・ハッシュ・Type-2 の組 | 892 |
| [894](894-feat-dupscan-swift-adapter.md) | Swift adapter (文法の pin・対応表と網羅テスト・ERROR の除外・PR 24 の固定の入力・canary) | 893 |
| [895](895-feat-dupscan-type3-candidates.md) | Type-3 (少し違う写し) の類似度と候補の絞り込み (後回し。主な用途には Type-2 と L3 の一致で足りるかを先に見る) | 893 |
| [896](896-feat-dupscan-report-output.md) | 出力 (人が読む形・JSON・リテラルの違いの表・プロパティの値・ターゲット・可視性・呼び出し元の数・既存のヘルパーとの一致) | 893・894 |
| [897](897-feat-dupscan-index-subcommand.md) | 探索のための索引 (subcommand)。名前・署名の型・doc 文で引く。初版を既存の道具で作れるなら 893 を待たない | 893・894 (901 で既存の道具で足りれば不要) |
| [898](898-test-dupscan-obaket-calibration.md) | obaket に当てた実測・既定の閾値・PR 24 の前の木での肯定と否定の期待値・変異 (Type-3 の条件は 895 の着手時) | 894・896 |
| [899](899-research-dupscan-lsp-integration.md) | LSP・言語の解析の道具と組み合わせる (呼び出し元・型・値の解決) | 896 |
| [900](900-research-dupscan-next-languages.md) | 次の言語 (Go・zsh) の読み取りを測る | 893 |
| [902](902-feat-dupscan-claude-code-hook.md) | **Claude Code の編集の直後に効かせる**: Edit / Write の直後に、近い既存のメソッドを知らせる hook | 893・894・897・898・901 |

## 進捗

- 2026-10-24 起票 (issue 890)。同日 epic にし、本文を 891 へ、受け入れ条件をタスクの子 issue (892〜900) へ分けた (ユーザー指示)
- 2026-10-24 主な用途 (Claude Code の編集の最中の重複の防止と発見性) を明記し、既存の道具の調査 (901) と hook (902) を足した。892 は 901 の結論を待つ (ユーザー指示)
- 2026-10-24 反証レビュー (設計) の指摘で、書く前の手順を 903 に分けて先頭に出し、902 の依存に 898 を足した
