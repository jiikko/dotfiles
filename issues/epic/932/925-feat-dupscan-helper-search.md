# 925 (feat): dupscan で用途語・署名から既存ヘルパーの候補を探す

> 🚨 **担当中: air5 (glogx)**（2026-10-27〜）

起票日: 2026-10-26

## 目的と利用場面

Claude/Codexが新しいfixtureや値の組み立て関数を書く前に、名前が分からない既存ヘルパーを探せるようにする。利用回数を増やすだけでなく、正しい既存実装へ到達するまでの検索・Readの往復と、不要なヘルパー追加を減らしたい。効果は未測定。今回は機能提案で、実装は未着手。

例: 「接続とバケットからテスト用の保存先を組みたい」が、既存関数名は分からない。戻り値の型と用途語を一度に渡し、使えそうな候補と一致根拠を短く受け取る。

## 現状の裏取り（5a9a6a98時点）

- `src/dupscan/index_cmd.go` / `internal/report/index.go` に名前・戻り値・引数型・docのフィルターがある。指定した各条件はANDで、名前の同一/前方/部分一致、shared、test/prod、可視性、近似呼び出し数等で並べる。
- 名前以外のフィルターは型やdocの文字列の部分一致。docは宣言のdoc commentの先頭行で、複数の用途語をフィールド横断で当てる検索・一致根拠の一覧はない。
- `near` は既にある本体から正規化後の一致を探す。書く前の用途語検索とは入力が違う。

## コマンド案（現時点では未実装）

```sh
dupscan search -query 'connection bucket fixture' -returns StorageLocation -limit 5
```

名前・署名・docの語を横断するlexicalな候補検索を追加する。詳細設計では新しいsearch subcommandに分離し、既存indexの意味を維持する。

- 識別子のcamelCase / snake_case分割、大文字小文字、複数の語の一致を扱う。単なる語の部分一致として仕様を明示し、意味理解を保証しない。
- `-returns` 等の明示した型フィルターは制約として扱い、用途語の部分一致とは分ける。どのフィールドに何語が一致したかを表示する。
- 先頭は少数候補。file:line、署名、可視性、shared、一致根拠、総件数と省略を返す。scoreは順位のための値で、正しさの確率にはしない。
- 0件なら勝手に条件を緩めず、再検索案を出す。候補を探すことと、その候補を使えると判断することを分ける。
- 同じ形でも別々に変わるべきドメインを自動統合しない。既知の非公開範囲を表示し、別モジュールからの利用可否が不明なら不明とする。

## 検証・受け入れ条件

- [ ] 既存indexのfilter・並び順・終了コードを維持する。
- [ ] 名前が不明でも用途語と型で見つかる / 型が合わない / 同名が複数 / 別ターゲットの候補 / 本当に既存がない、を含むgolden queryを用意する。候補の正解はコードを読んで決め、dupscan自身の判定だけで採点しない。
- [ ] top-k内の適切な候補、誤った再利用、0件時の誤った不存在断定を検査し、一致根拠と省略がtext/JSONの両方に出る。
- [ ] ClaudeとCodexを分けて、既存indexによる探索との比較を行う。対象が同じ課題で、正しい再利用・最初の適切な候補までの往復・返したbytes・elapsed timeを測る。利用回数だけで成功扱いにしない。
- [ ] 既存がない課題では正しく新規実装できることを対照に入れる。小さなrepoで対照群も常に既存を見つけるなら、効果を測れない条件として扱う。
- [ ] 924の代表的規模でlatencyと出力量を測り、検索の便利さと追加コストを評価する。

関連: [924](done/924-perf-dupscan-exploration-latency.md) は処理速度、[918](918-feat-dupscan-target-and-literal-table.md) は所属ターゲット、[917](../890/pending/917-test-dupscan-usefulness-verification.md) は効果検証。917の保留条件は維持する。895/918の実装はepic 932の別担当であり、本issueに混ぜない。

## 詳細設計案（2026-10-26、未実装）

### 初期版のCLI

新規 `search` subcommandにする。既存indexの挙動を変えず、署名・docを横断する順位付けを分離する。

```sh
dupscan search -format json-v2 -query 'connection bucket fixture' -returns StorageLocation -lang swift -limit 5
```

`-query` 必須、1〜8語、全体512 UTF-8 bytes以内・各語64 rune以内。`-lang auto|swift|go`、`-scope all|prod|test`、`-repo` / `-path`、`-file`、`-returns`、`-param-type`、`-limit`、`-max-bytes` は926の契約に従う。limitは既定5、1〜50。出力予算は既定8,192 bytes、指定範囲は1,024〜1,048,576 bytes。無制限出力は初期版に設けない。

### 検索の規則

1. 924のmetadata用AnalysisViewを取得し、現在のindexと同じく名前付きUnitsを候補にする。Swift/Goのindexに出る単位が対象で、各言語の除外条件を引き継ぐ。
2. 明示filterはAND。`-returns` / `-param-type` は現行indexの字面の部分一致（大文字小文字を区別）を保つ。型別名の解決や型推論はしない。型metadata不明の候補は、指定された型filterを満たしたとはみなさない。
3. queryは空白で分け、Unicode小文字化後に重複語を除く。この空白区切りの語を加点単位とする。対象はname / signature / 現行doc先頭行。各fieldの小文字化した原文に語が部分一致するか、語をcamelCase/acronym/snake_caseの境界で分けた全成分が同じfieldの分割tokenへそれぞれ部分一致すれば、そのfieldの一致とする（境界分割は小文字化前に行う）。例: HTTPClientはhttp/client、http_clientもhttp/client。日本語は原文の部分一致を用い、形態素解析・同義語展開・Unicode正規化は行わない。根拠には元の語と一致field/成分を残す。
4. 初期版はOR候補検索で、少なくとも1語が一致した候補を返す。`-min-matched-terms N` を指定したらN語以上を要求する。範囲は1〜queryの語数。自動で条件を緩めない。
5. 各語について最も強い一致fieldだけを加点（name=3、signature=2、doc=1）。順位は「一致語数→加点合計→query全体との名前完全一致→shared→非hidden→prod→file/line/col/署名」の順。前の比較項目が同じときだけ次を見る。近似呼び出し数は順位に使わない。

scoreは確率ではない。各候補に `matched_terms` とfield別根拠、`missing_terms`、可視性、shared、型metadataを返す。snippetはdocの短い抜粋とし、長さ制限・省略を示す。利用可能性は `not_proven` と、既知の可視性を分ける。sharedをpublic/他moduleから利用可能という意味にしない。

### 出力と0件

926のv2 envelopeに `data.candidates` を持たせる。候補IDは同じReadSet内のfile/位置/署名から作る参照であり、型解決済みのsymbol IDではない。件数はfilterと検索規則を満たした全候補、returnedは予算内の件数。budget不足は `output.truncated` で示し、hitがあるのに返却0件でもno_matchにしない。

0件なら現在のfilterを保持したまま、1語への再検索・既存indexでの型検索等を最大3個のargvで提案する。提案を自動実行しない。語・filterを変える案は、何が変わるかをreasonへ明記する。

### 実装の分割と検査

- `internal/report` の既存index queryからhard filter判定を共通化し、旧indexのgolden出力を保持する。新しいtokenize/rankは独立したpure functionとして検査する。
- camel/acronym、日本語、重複語、同点の順序、AND filter、OR/min-matched、alias不一致、doc欠落、scope、private/unexported、同名別file、partial解析をtable testにする。
- 精度のgolden queryは925の受け入れ条件にある正解/不正解対照で作る。ranking規則を変えた際はtop-kの変化を評価し、都合のよいqueryだけで採用しない。
- 初期版は永続検索indexを作らず、924の実測を用いて必要性を判断する。詳細設計の数値は初期仕様案で、効果確認後の変更は本文とテストで追跡する。

## 今回の3回のセルフレビュー（2026-10-26）

各回で読み取り専用Codex subagentの反証レビューと本体の自己点検を行い、指摘を次の回の前に反映した。

1. **CLI仕様・互換性**: 既存indexから独立したsearchを選び、型filterの現行部分一致を維持する仕様を明文化した。
2. **agentの使いやすさ・効果測定**: 順位評価とagent作業評価を分離し、正解あり/なし・別targetの対照を928の評価手順へ接続した。
3. **実装構造・運用コスト**: 語の分割/部分一致/加点単位の曖昧さを自己点検で修正し、sourceにあるmetadataだけを利用する境界を維持した。

改善の評価: 起票時の機能案から、引数・状態・依存関係・異常系・検査手順を実装/レビュー可能な契約へ具体化した。共通仕様の正本は926、registry/評価手順は928、解析経路は924とし、重複した実装を避ける。これは設計文書の改善であり、実装・性能・開発効率の向上は未検証。受け入れ条件は未完了のままopenに残す。

## grepで見落とした既存実装へ到達するための追加条件

2026-10-26のユーザー確認: grepを使っていても既存実装を見つけられず、似たメソッドを量産しているためdupscanを作った。これを解くことが目的であり、「既知の名前を1回引く速度」やCLIの呼び出し回数だけで有効性を判断しない。[928](928-feat-dupscan-agent-onboarding.md) の比較課題・採否をこの目的へ揃える。

- 名前を知らない段階では、現行 `index -returns` / `-param-type` から始められる。新searchは複数用途語を名前/署名/docへ横断して当て、一致根拠を返す役割。名前を既に知っている課題だけで検索性能を評価しない。
- 別名・docなし・型が容器に包まれる・helperがtest supportや別directoryにある・同名の無関係な関数が多いケースを入れる。字面が全く一致しない用途/型別名は初期lexical検索の限界として失敗数へ残す。意味検索ができると宣伝しない。
- 0件ならCLIが、型だけのindex検索、1語検索、明示filterを1つ外す、scopeを広げる等の次のargvを最大3個返す。候補がある場合も一般的な絞り込み/再検索案は出せるが、候補の関連性や再利用の適否はagentがコードを読んで判断し、必要なら明示的に再検索する。変更する条件をreasonへ明示し、元のfilterを黙って緩めない。0件を新規実装の許可とみなさない。
- 候補カードには署名・可視性・一致根拠・file位置と確認用argvを返す。可視性は実際の利用可否の証明ではなく、module/target確認は未解決と表示する。candidateの数や[930](930-feat-dupscan-candidate-call-evidence.md)の近似呼び出し数で正しさを代用しない。
- 正解集合から、top-5に適切な既存候補が入る率・既存ありなのに候補0となる割合を測る。agent評価では、最終diffの不要な重複追加率、正しい再利用率、誤った統合率を928で計測する。候補の網羅性と最終再利用を別に採点する。

受け入れ条件への追加: grepを適切に使っても見落としが起こる代表課題で、dupscanが適切な候補への到達と重複追加の削減に寄与するか比較する。結果が改善しなければ、語の規則・順位・案内のどこで失敗したかを残す。Type-3と所属target解析の実装は895/918の担当であり、本issueの範囲外。

## 進捗

- 2026-10-26: 起票・既存indexとの違いを裏取り。実装・効果測定は未着手。
- 2026-10-26: Codexの読み取り専用subagentで、CLIの事実・互換性とagent導線・効果測定の2観点から反証レビュー。P1/P2/P3の指摘なし。静的レビューであり、新機能の実装・性能・agentによる利用効果を検証したものではない。
- 2026-10-26: 相対リンク・diffと、Bash 5でのissue関連テストを検査して成功。提案の本文を記録し、機能の実装・効果測定はopenの作業として残す。

- 2026-10-26: grepによる既存実装の見落とし・類似メソッドの追加を減らす目的に照らし、出力の誤解と探索成功の評価条件を追記。実装・効果測定は未着手。

- 2026-10-26: 既存実装発見・比較評価について読み取り専用Codex subagentで反証レビュー。925の「候補の適切性をCLIが判断するように読める」P3を訂正し、候補抽出とagentによる採用判断を分けた。 issue内の相対リンク・JSON/Python構文・diffとBash 5のissue関連テストが成功。929の本文内手順と930の3問い合わせをCLIで再現した。文書の改善は、出力誤認の再現・責務と、既存実装発見/不要な重複追加を測る条件を具体化したこと。機能改善の効果は未検証。

- 2026-10-26: [931](../../done/931-refactor-tuikit-domain-duplicates-and-discovery-check.md)の検証: 931のtuikit実例では、同じ表示幅ループがslice直渡し/struct.field経由の構文差と19-nodeの閾値で現行near/reportに一致しなかった。未知名からの型/用途検索の評価へ追加する。新searchの効果は未検証。

- 2026-10-26: 初期のname/signature/doc先頭行に加える呼出先/本体hintは[933](933-feat-dupscan-behavior-hint-search.md)、doc全文・テスト等の補助証拠は[934](934-feat-dupscan-source-evidence-search.md)へ起票した。段階ごとの仕様変更を分け、未実装の能力を初期searchに混ぜない。

- 2026-10-27: 924で探索共通経路の比較用本体変換/正規化を省く処理を実装した。925の用途語/署名検索・順位設計は未着手で継続する。

- 2026-10-27: `feat(dupscan): expose bounded metadata helper search`でtext search、source確認argv、0件再検索、registry/guideへの接続を実装。pure検索規則の先行commitを統合し、lint/raceと予算・再検索の各変異を検証。JSON-v2と実課題評価は未完了でopenを維持する。
