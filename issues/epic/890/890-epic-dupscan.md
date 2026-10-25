# 890 (epic): dupscan — 構文木で重複しているメソッドをあぶり出し、似たメソッドの量産を書く前に止める道具

起票日: 2026-10-24

> epic 890 の親 issue。要件と構想の正本は [891](891-design-dupscan-requirements.md)。作業は同じディレクトリのタスクの子 issue で進める。

## 概要

repo で実行すると、テスト / 製品コードのそれぞれで重複しているメソッドを組にして列挙し (重複のチェック)、
これからコードを書く人が似たメソッドを探さずに量産するのを書く前に止める索引を出す道具。Go のコア + 言語ごとの差分 (adapter)。最初の言語は Swift。
obaket の週次の Claude audit の PR 24 で、名前の違う同じ中身のヘルパーが 6 つ見つかったことが起点 (経緯は obaket issue 1298)。

**主な用途 (ユーザー 2026-10-24)**: Claude Code にコードを変更させている間に、重複したメソッドを作らせない・既存のメソッドを見つけやすくして実装を滑らかにする。
後からまとめて重複を洗う使い方 (週次の audit) より、実装の最中に効く使い方 (書く前に索引を引く [903](done/903-docs-dupscan-search-before-write-procedure.md) と、`make test` で落とす [905](905-feat-dupscan-ci-check-gate.md)) を先にする。
**既存の道具の調査 ([901](done/901-research-dupscan-existing-tools.md)) を先に済ませ、足りない部分だけを作る** (当初は既存の道具を調べていなかった)。

## 子 issue

| 番号 | 何をするか | 依存 |
|---|---|---|
| [891](891-design-dupscan-requirements.md) | 要件と構想の正本 (用途・コアと adapter の境界・正規化の段・出力・索引・実測・構想・決めること) | — |
| [901](done/901-research-dupscan-existing-tools.md) | **既存の道具の調査** (CPD・jscpd とその MCP・DRYwall・dupl・similarity・ast-grep・意味検索の MCP。LSP・Serena は使わないと決めた)。PR 24 の 6 つに当てて、自分で作る範囲を決める | — |
| [903](done/903-docs-dupscan-search-before-write-procedure.md) | **書く前に既存のメソッドを探す手順と、実装後のセルフレビューで `gate` を回す手順**を使う側の repo の入口に書く (索引ができるまでは grep の暫定の手順) | 暫定は無し (索引は 897・`gate` は 905) |
| [892](done/892-chore-dupscan-project-scaffold.md) | `src/dupscan` の骨組み (module・Makefile・bin・CI・README・PACKAGES.md、cgo の所要の実測) | 901 |
| [893](done/893-feat-dupscan-core-normalize-type2.md) | コア: 走査・test / prod の分類・単位・正規化 L1〜L3・ハッシュ・Type-2 の組 | 892 |
| [894](done/894-feat-dupscan-swift-adapter.md) | Swift adapter (文法の pin・対応表と網羅テスト・ERROR の除外・PR 24 の固定の入力・canary) | 893 |
| [895](pending/895-feat-dupscan-type3-candidates.md) | **保留** (2026-10-25)。Type-3 (少し違う写し) の類似度と候補の絞り込み (後回し。主な用途には Type-2 と L3 の一致で足りるかを先に見る) | 893 |
| [896](done/896-feat-dupscan-report-output.md) | 出力 (人が読む形・JSON・リテラルの違いの表・プロパティの値・ターゲット・可視性・呼び出し元の数・既存のヘルパーとの一致) | 893・894 |
| [897](done/897-feat-dupscan-index-subcommand.md) | 探索のための索引 (subcommand)。名前・署名の型・doc 文で引く (名前の検索も LSP でなくこれでまかなう) | 893・894 |
| [898](done/898-test-dupscan-obaket-calibration.md) | obaket と DualNoteApp に当てた実測・既定の閾値・PR 24 の前の木での肯定と否定の期待値・変異 (Type-3 の条件は 895 の着手時) | 894・896 |
| [899](pending/899-research-dupscan-lsp-integration.md) | **保留** (LSP は使わない)。構文木の近似で判断を誤る実例が出たら、LSP・言語の解析の道具で補う案を再開する | 896 |
| [900](900-research-dupscan-next-languages.md) | 次の言語 (Go・zsh) の読み取りを測る | 893 |
| [902](pending/902-feat-dupscan-claude-code-hook.md) | **保留**: 編集のたびの hook。止める網は `make test` (905) にした。`make test` まで待つと手戻りが大きい実例が続いたら再開 | — |
| [904](done/904-feat-dupscan-trial-subcommands.md) | 人が試す入口 (`near`・`explain`・`gate` を手で回す)。動作確認として何ができるか・なぜ同じと判定したかを手で確かめる | 893・894・896・897・898・905・906 |
| [905](905-feat-dupscan-ci-check-gate.md) | **セルフレビュー・`make test`・CI で新しく入った重複を落とす** (`dupscan gate`)。変わった単位を名指しし、その形の単位が base より増えたときだけ落とす。止めるかは所要と誤って止める割合を見て決める | 893・894・896・898・906 |
| [906](done/906-feat-dupscan-exclusion-config.md) | **誤検知を除外する設定** (組・単位・パス・段・下限。理由が必須。効いていない除外と、新しく足された除外を出す) | 893・896 |
| [907](done/907-feat-dupscan-exploration-queries.md) | **探索の問い合わせ** (`outline`・`find`・`callers`)。grep の代わりに、定義だけを 1 件 1 行で返して文脈を食わずに関連の実装を探す | 893・894・897 |
| [909](done/909-task-dupscan-integrate-into-obaket.md) | **obaket への組み込み**。2026-10-25 に obaket の issue 1301 へ移した (PR 24 の前の木での受け入れと obaket の規模の実測も 1301 で行い、結果を各 issue に書き戻す) | 903・905・906 |
| [917](pending/917-test-dupscan-usefulness-verification.md) | **保留** (obaket の 1 か月分の記録を待つ)。**有益さの検証**。書く前に引く効果を skill の有る無しの A/B で測る・起点の写しを拾えるか・組み込んだ後の効果 (この 2 つは obaket の issue 1301 へ移した) | 903・905・909 |
| [918](pending/918-feat-dupscan-target-and-literal-table.md) | **保留**。所属ターゲットの解決とリテラルの違いの表 (894・896・897 から移した)。obaket で使う中で欲しくなったら作る | — |

## 進捗

- 2026-10-24 起票 (issue 890)。同日 epic にし、本文を 891 へ、受け入れ条件をタスクの子 issue (892〜900) へ分けた (ユーザー指示)
- 2026-10-24 主な用途 (Claude Code の編集の最中の重複の防止と発見性) を明記し、既存の道具の調査 (901) と hook (902) を足した。892 は 901 の結論を待つ (ユーザー指示)
- 2026-10-24 反証レビュー (設計) の指摘で、書く前の手順を 903 に分けて先頭に出し、902 の依存に 898 を足した
- 2026-10-24 LSP は使わないことにした (ユーザー決定 2026-10-24: dupscan だけで依存も使い方もシンプルになるなら LSP は使わない)。名前の検索も索引 (897) でまかない、899 は pending へ
- 2026-10-24 人が試す入口 (904) を足した (ユーザー指示: 動作確認としてどういう使い方ができるのかを知りたい)
- 2026-10-24 検出できる種類の重複を hook (902) と CI (905) で止めることにした (ユーザー決定: 実行コスト次第)
- 2026-10-24 止める網を `make test` の中の `dupscan gate` (905) にし、編集のたびの hook (902) は保留にした (ユーザー指示 2026-10-24: hook というより make test の中に dupscan を含める感じの実行頻度)
- 2026-10-24 実装後のセルフレビューで `gate` を回すことを主な使い方に足し (903)、誤検知を除外する設定 (906) と検出の方法の文書 (`docs/dupscan-detection.md`) を足した (ユーザー指示)
- 2026-10-24 repo に依存しない作りを要件にし (891 の「汎用性」)、検証に DualNoteApp を使うことにした。SwiftLint のルールにしない理由を 891 に書いた (ユーザー 2026-10-24: repo に依存しない汎用的な作りにしたい。途中の検証には DualNoteApp の macOS を使ってよい)
- 2026-10-24 探索の問い合わせ (907) を足した。道具の名前は、重複の検出と探索の両方に使う道具として、実装が落ち着いてから決め直す (ユーザー指示)
- 2026-10-25 obaket への組み込み (909) を足した (ユーザー指示: obaket に組み込みたいが clone していないので sub issue として書いておく)
- 2026-10-25 有益さの検証 (917) を足した (ユーザー指示: 検証方法を sub issue に書いておく)
- 2026-10-25 893・903・906・907 を done にした (受け入れ条件が済んだ。残った確かめは 898・909・917 へ)
- 2026-10-25 obaket 側のタスク (909 と 917 の検証 2・3) を obaket の issue 1301 へ移した (ユーザー指示: ソースは dotfiles に残し、obaket 側のタスクは obaket の issue へ)。go・zsh (900) を除いて epic を done にするまで進める (ユーザー指示)
- 2026-10-25 まだ作っていない機能 (Type-3 = 895・所属ターゲット・リテラルの違いの表) を保留にした (ユーザー 2026-10-25: 必要性を評価できないので、obaket の開発やコードスキャンを進める中で欲しい機能があったらその時に実装する)。所属ターゲットとリテラルの表は 918 にまとめた。obaket での受け入れ (obaket の issue 1301 の段階 1・2) の結果を 897・904・905 に書き戻した
