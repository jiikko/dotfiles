# 934 (feat): dupscanの候補探索にdoc全文・テスト使用例・元ソースを補う

起票日: 2026-10-26

## 目的と不足

構文単位の索引から落ちる手掛かりも使い、名前不明の既存実装の見落としと、確認のための大量Readを減らす。[925](925-feat-dupscan-helper-search.md)は初期doc先頭行まで、[927](927-feat-dupscan-agent-context.md)は選択済み定義/近似refs/callersのcontext。本issueは、まだ候補が無い段階でdoc後半やテスト内の使用例から候補へ到達する補助経路を担当する。

## 現状の裏取り（2920a72a）

- Unit.Docはdoc comment先頭行。SwiftのdocCommentとGoのdocLineは索引に全文を保持しない。
- GoのfuncDeclはTest/Benchmark等を比較単位から外す。callsはテスト内のcallも収集するため、全テスト情報が失われるという意味ではない。テスト関数をそのまま重複比較対象へ戻す変更は不要。
- 現行にあるのはscanned.contentsで、ReadSetは924の未実装設計。元sourceのcontentsは存在するので、索引へ無制限に全文を複製する必要はない。[931](../../done/931-refactor-tuikit-domain-duplicates-and-discovery-check.md)では広いcatで出力予算を消費し、実際のモデル可視量までは測れていなかった。

## 設計案（未実装）

1. doc全文、テスト/使用例、元sourceのlexical一致を、定義検索と区別した証拠として返す。各hitにkind/file/位置/短いsnippet/一致理由を持たせる。コメントや文字列の一致を宣言や実callと偽らない。
2. 最初は明示的な補助検索modeかnext_actionsとして提供する。定義0件とsourceのhitは別の件数にし、sourceのhitがあるだけで対応するhelperを見つけたと扱わない。hitが非公開/別責務かは確認を残す。
3. 比較単位の除外を保ったまま、doc/テストはevidence-onlyとして解析する。型解決なしに使用例を特定定義へ紐づけられない場合は曖昧さを保持する。Goがdef/refsに対応したことにはしない。
4. 原文は924のReadSetを共用し、file/hashと結果scopeを返す。無制限の全文index/cacheを最初から追加せず、候補選択後の詳細は927へつなぐ。返却snippet・件数・bytesは926の予算を共用する。
5. 未対応言語/設定等の検索は能力範囲を偽らず、必要なら通常のrg/Readへつなぐ。grepを禁止しない。正解が減るほど出力を切り詰めてコンテキスト節約と報告しない。

## 検証・受け入れ条件

- [ ] 名前/署名/doc先頭には無い語がdoc後半・テスト・使用例にある課題を用意する。コメントだけの言及、古い例、同名の別定義も対照に入れる。
- [ ] 旧比較単位・gateの違反/除外が変わらず、source hitと構文上の定義/callを分けられる。
- [ ] 部分解析・不正UTF-8の扱い・長い行・省略を表示し、原文位置やsymbol解決を捏造しない。
- [ ] 928の同じ課題で、正しい候補への到達・誤誘導・再利用・不要な重複追加・実際に観測できたコンテキスト量を比較する。今は効果未測定。

## 進捗

- 2026-10-26: `issues: reserve dupscan behavior hints and source evidence search`で番号予約。925/927が担う範囲と補助検索の不足を区別して記録した。実装・性能・利用効果は未検証。

- 2026-10-26: 未実装ReadSetと現行contentsを区別し、補助証拠の取得範囲を整理。 読み取り専用Codex subagentで指摘の網羅性とcontext計測を反証レビューし、P3（現行/設計、着手条件、計測範囲）の3点を訂正した。文書の改善は、不足した指摘の担当と検証条件を明確にしたこと。実装・両効果の実証は未着手。
