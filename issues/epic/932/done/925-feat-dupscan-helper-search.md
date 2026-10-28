# 925 (feat): dupscan で用途語・署名から既存ヘルパーの候補を探す

起票日: 2026-10-26

## 今回完了する機能範囲（2026-10-28）

textの字句候補検索と、その制約を保った確認/0件回復を本issueの機能範囲として完了する。複数語・型/file/prod/test・一致根拠・件数/bytes予算・原文確認は実装済み。型aliasや同義語の解決は行わない。実agentが検索を使ったことで再利用/重複追加率が改善するかは、未実証のまま928の評価に残す。この機能完了を開発効率の証明としない。

固定alias例で無益だった語数緩和を修正した。closure以外の索引単位について型/file/scope条件を通った数を語一致より前に数え、0なら語数を減らす案を飛ばす。元の必要語数は維持し、明示的なhard条件があれば1個の解除案へ進み、なければ既存のevidence案を使う。どちらも案内は予算内でのみ表示する。語数を緩めても増えない旨は要約とargvを優先した残余bytesに入る場合だけ表示する。解析不能や索引対象外を含む意味上の不存在とは区別する。

18673a39の固定userdirs.goで `search -query 'XDG 設定' -returns ConfigPath -min-matched-terms 2` の生成argvを順に実行した。旧版は3検索/3867 bytes（最後は語数1で3候補）、新版は2検索/2568 bytes（要求どおり語数2で2候補）。ConfigBaseへ到達し、aliasを解いたのではなく型filter解除を明示した結果である。複数hard条件が交差して0になる場合に2回で回復する保証はしない。

別のhit対照を各5回測定: Go4単位は中央値24.38→23.03ms/1589bytes、大きい固定Swift corpusは187.92→178.52ms/2630bytes。rc/stdout/stderrはbyte一致。非統制の小標本であり高速化の証明には使わず、少なくともこの2入力ではhit出力を変えなかった確認とする。rawはdotfiles/tmp/remaining-932-20261028/{alias-measure,scale}.json。

標準lint/raceとstrict module gateが成功（変わった単位2、新しい重複/除外なし）。同名の別Go moduleの2候補を潰さず、両方をreuse=not_provenとして返すCLI fixtureも追加した。5隔離変異で語retry/count/任意説明budget/action budget/候補欠落をそれぞれ検出。反証レビューで旧851bytesが追加説明だけで1025bytesへ溢れrc2になる問題を再現し、残余予算方式と境界回帰で訂正した。既存action予算テストは末尾の任意説明をactionへ数えていたfixture計算を直し、元のoverflow検出を変異で維持した。

## 方向性の再評価（2026-10-27）

対象: a5b4196e の実装と既存測定。

**判断: text最小版は維持し、次は検索品質の評価を優先する。** 現行searchはname/signature/doc先頭行のlexical検索と型/file条件、件数/bytes予算、一致根拠・未一致語・ソース確認argvを備える。固定doctor例では旧型索引の23番目のhelperへ1候補で到達したが、OR検索の `process identity` ではfilesystemのstatIdentityも先頭に出た。小さい出力だけでは正しい再利用を示せない。

次は名前を伏せた実課題と未使用の負例で、適切なrg+必要箇所のRead、index、search、併用を比較する。候補に到達したか、誤誘導、候補確認量、正しい再利用/不要な重複追加を別々に記録する。既知の2正例だけでrankを調整しない。現測定は選んだ例の探索的比較で、同情報量の比較・実token節約・一般的な高速化は未証明。

json-v2・同義語・追加rank featureは、現行textの具体的な失敗や機械処理上の不足を確認してから選ぶ。926の包括設計を一括実装することを本issueの次の前提にしない。930の近似callsは現在も順位に使わない。

## 目的と利用場面

Claude/Codexが新しいfixtureや値の組み立て関数を書く前に、名前が分からない既存ヘルパーを探せるようにする。利用回数を増やすだけでなく、正しい既存実装へ到達するまでの検索・Readの往復と、不要なヘルパー追加を減らしたい。固定課題の結果と限界は下記へ記録済み。text機能を実装し、json-v2は未採用の条件付き案、実agent効果は928の未完了評価とする。

例: 「接続とバケットからテスト用の保存先を組みたい」が、既存関数名は分からない。戻り値の型と用途語を一度に渡し、使えそうな候補と一致根拠を短く受け取る。

## 現状の裏取り（5a9a6a98時点）

- `src/dupscan/index_cmd.go` / `internal/report/index.go` に名前・戻り値・引数型・docのフィルターがある。指定した各条件はANDで、名前の同一/前方/部分一致、shared、test/prod、可視性、近似呼び出し数等で並べる。
- 名前以外のフィルターは型やdocの文字列の部分一致。docは宣言のdoc commentの先頭行で、複数の用途語をフィールド横断で当てる検索・一致根拠の一覧はない。
- `near` は既にある本体から正規化後の一致を探す。書く前の用途語検索とは入力が違う。

## コマンド（text最小版を実装済み）

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

- [x] 既存indexのfilter・並び順・終了コードを維持する。（実装時の旧出力10条件に加え、今回も型filter/ファイル指定×text/jsonの4条件で旧binaryとrc/stdout/stderr一致）
- [x] 名前を伏せた用途語+型の正例、型filter不一致、既存なしを、コードから決めた正解と固定queryで確認する。（今回S1〜S4）
- [x] 同名の2Go module fixtureを追加し、2候補と未証明表示を確認する。実プロジェクトのtarget解決や全パターンの網羅とは扱わない。
- [x] 固定課題のtop-k到達/誤候補/false negativeと、現行textの一致根拠・省略・0件の限定説明を確認する。（今回S1〜S4/E1〜E3と実装時の予算検査）
### 928へ残す未実証の評価（本機能の達成扱いにしない）

- [ ] searchを実利用したagentの誤った再利用・不存在断定を比較する。JSONは機械処理の具体的不足を確認し採用した場合だけの追加条件とする。
- [ ] ClaudeとCodexを分けて、既存indexによる探索との比較を行う。対象が同じ課題で、正しい再利用・最初の適切な候補までの往復・返したbytes・elapsed timeを測る。利用回数だけで成功扱いにしない。
- [ ] 既存がない課題では正しく新規実装できることを対照に入れる。小さなrepoで対照群も常に既存を見つけるなら、効果を測れない条件として扱う。
- [x] 固定Goと大きいSwift corpusでhit互換とlatency/bytesを測定する。一般的な高速化は主張しない。

関連: [924](924-perf-dupscan-exploration-latency.md) は処理速度、[918](918-feat-dupscan-target-and-literal-table.md) は所属ターゲット、[917](../../890/pending/917-test-dupscan-usefulness-verification.md) は効果検証。917の保留条件は維持する。895/918の実装はepic 932の別担当であり、本issueに混ぜない。

## 詳細設計案（2026-10-26）

以下は全体案。現行はtext最小版で、未実装のjson-v2/-repo/-path/共通serializer/効果評価と区別する。実装差分は末尾に記録する。

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

2026-10-26のユーザー確認: grepを使っていても既存実装を見つけられず、似たメソッドを量産しているためdupscanを作った。これを解くことが目的であり、「既知の名前を1回引く速度」やCLIの呼び出し回数だけで有効性を判断しない。[928](../928-feat-dupscan-agent-onboarding.md) の比較課題・採否をこの目的へ揃える。

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

- 2026-10-26: [931](../../../done/931-refactor-tuikit-domain-duplicates-and-discovery-check.md)の検証: 931のtuikit実例では、同じ表示幅ループがslice直渡し/struct.field経由の構文差と19-nodeの閾値で現行near/reportに一致しなかった。未知名からの型/用途検索の評価へ追加する。新searchの効果は未検証。

- 2026-10-26: 初期のname/signature/doc先頭行に加える呼出先/本体hintは[933](933-feat-dupscan-behavior-hint-search.md)、doc全文・テスト等の補助証拠は[934](934-feat-dupscan-source-evidence-search.md)へ起票した。段階ごとの仕様変更を分け、未実装の能力を初期searchに混ぜない。

- 2026-10-27: 924で探索共通経路の比較用本体変換/正規化を省く処理を実装した。925の用途語/署名検索・順位設計は未着手で継続する。

## text最小版の実装（2026-10-27）

- SearchTerms/BuildSearchをpureな検索規則へ分け、hard filterはindexのQuery.matchを共用。名前付きの同じUnitsからname/signature/doc先頭行へOR/指定語数を適用する。camel/acronym/snakeの境界は小文字化前に分割し、Unicodeの結合記号を勝手に落とさず、日本語は原文部分一致だけ。語の最強fieldだけを加点し、近似call countは順位に使わない。
- searchを実dispatch/能力表/agent-guideへ登録。CLIは-query/-returns/-param-type/-file/-lang/-scope/-min-matched-terms/-limit/-max-bytesを持つ。今回のscopeはall/prod/testと既存の位置引数dirで、926の-repo/-path等とは混同しない。textのみで新たな固定JSON契約を作らない。
- 各カードにfile/line/col・署名・型・既知のvisibility/shared/test・一致field/語と不足語・doc抜粋・再利用not_proven・最大80行のsource確認argvを表示する。doc160runes、候補上限5（1〜50）、全体予算8192（1024〜1048576bytes）。headerに候補数/表示数/parse失敗と省略を出し、候補があるのに表示0件でも不存在としない。
- 0件ではmin-matched-terms>1なら1へ下げる案、それ以外はfilterを1個だけ変える案、最後は最初の用途語をevidenceでdoc全文/テスト/sourceに当てる案。reasonに変更点を明記し、絶対directory scopeと残るfile/lang等を保つ。自動で条件を緩めて走査し直さず、argvも実行しない。通常ORの0件から1語だけへ再検索しても新しい候補が増えないため、その案は採用しない。
- 任意の案内で必須要約が予算超過しないよう、実際の残余bytesで収容を判定する。予算不足の0表示、doc切詰め、不完全parseを検査した。source確認argvも実際に実行し、80行上限を確かめた。

### 評価と制約

実ソースsrc/doctorを固定し、変更前の型だけの `index -returns bool`（既定20表示/全50候補）と、今回のquery+同じ型filterを各5回実行。探索的に選んだ正例/反例で、held-outやagent A/Bではない。

| query / min語数 | index候補数 / bytes | search候補数 / bytes | 中央値ms（index/search） |
|---|---:|---:|---:|
| 完全一致 プロセス / 2 | 50 / 4113 | 1 / 1186 | 33.54 / 31.24 |
| Lstat dev / 2 | 50 / 4113 | 1 / 1098 | 32.23 / 31.46 |
| process identity / 1 | 50 / 4113 | 4 / 2836 | 31.68 / 31.61 |

「完全一致 プロセス」はprocessRunningへ到達し、型だけの索引では23番目で既定表示から外れていた候補が1件になる。「Lstat dev」はstatIdentityへ到達する（旧8番目なので型だけの既定表示にも既にある）。`process identity`は4候補へ減るが、先頭のstatIdentityはfilesystemの識別であり、processの語意を理解していない。不足語・引数型を見て確認が必要。返す情報と絞り込み条件が違うため、bytes減少を同じ情報量や総context削減の証明にはしない。所要も小規模/準備済みの観測値で、速度改善の主張には使わない。

- 旧index/callersのtext/json、report/gateのtext/json/xcodeの10条件でrc/stdout/stderrを比較して一致。既存indexの条件/順位/数値は変更していない。help/能力表とguideにはsearchを追加した。
- lint/race全テスト、検索規則15種類とCLI4種類の意図的な変異を検査。語数/加点/可視性等の順位、結合記号、予算による偽0件、source確認範囲、進展しない再検索、案内の予算超過を想定assertで検出した。レビューでは「0件案内不足」「確認argv不足」「任意案内による予算超過」を修正した。
- 追加する複雑さはtoken化/rankingとbounded text出力。新cache・型解決・call hint・構造類似度・v2 envelopeは追加しない。実課題での正しい再利用・不要な重複追加・総context/費用の改善は928、共通JSON/診断は926で評価/統合する。925全体はopenで継続。

改善の再評価: 既知名の部分一致や型だけの一覧に加え、名前を与えずに用途語が異なるmetadata fieldへ現れる既存候補に1回で絞れる。固定の実例で候補の隠れた初期表示と不要な候補を減らし、source確認argvまで具体化した。一方、語彙/型alias/同義語の限界、ORの無関係候補、追加bytesと解析費用は残る。新しい検索が常にgrepより効率的とは主張しない。

- 2026-10-27: `feat(dupscan): rank lexical helper candidates across metadata`を統合し、`feat(dupscan): expose bounded metadata helper search`でtext CLI・source確認・再検索・guideを実装。`docs(dupscan): document lexical helper search and its limits`で実測/残作業を記録する。

- 最終strict gateはbase66699f2aから変わった14単位で新しい重複なし、追加除外なし。全srcの通常gateは既存の不正fixtureをbase/head各1件として通知する。今回のtext実装のclaimを解除し、残る統合/評価をopenで追跡する。

- 2026-10-27: `docs(dupscan): reassess open epic 932 work against current behavior`で現実装と残設計を再照合し、上記の最小作業・着手条件へ更新。改善は誤った未実装扱いと一括実装の前提を減らしたこと。コード変更、開発効率/重複追加率の改善は今回の成果に含めない。

- 2026-10-27: `docs(dupscan): correct evidence terminology and conditional acceptance`で反証レビューP2を訂正。JSONは未採用の条件付き案とし、text版の品質評価に必須とする旧条件を置き換えた。

## 現行版の固定課題評価（2026-10-27）

対象は09e983fc。旧doctorの2正例を再利用せず、ソースを人が読んでquery・期待候補・非同等候補をUTC 12:35:50に凍結した。名前を課題文に含めないS1/S2と、alias/不存在の対照を含む。今回選んだ小規模課題であり、blindな順位評価や一般化の証明ではない。共通の計測条件と実agent編集pilotは[928](../928-feat-dupscan-agent-onboarding.md)へ記録した。

Go corpusは09e983fcの`src/userdirs` / `src/safefs`。rgはqueryの各語を`rg -n -i -e <語> ... -g '*.go' <scope>`でOR検索し、全件のソース・テスト行を許す。indexは同じ型filter、searchは同じ用途語と指定語数。候補の意味上の正解は出力から生成せず、事前に読んだ実装の規則で判定した。

| 固定課題 / scope / query / min / returns | 期待と観測 | rg / index / searchのstdout bytes | 中央値ms（同順） |
|---|---|---:|---:|
| 与えたhomeから設定rootを決める / userdirs / `XDG 設定` / 2 / string | ConfigBaseが正解。searchは2候補の1番目、ConfigDirはapp用で入力契約が違う | 5011 / 728 / 1633 | 7.45 / 31.32 / 30.04 |
| 経路の全要素のsymlinkを拒否 / safefs / `symlink 経路` / 2 / error | NoSymlinkInPathが正解。searchは1候補。root内解決のResolveUnder・最後だけ開くopenDirNoFollowとは別規則 | 8558 / 3660 / 1044 | 5.77 / 29.71 / 29.08 |
| 要求側の型名だけConfigPathというalias / userdirs / `XDG 設定` / 2 / ConfigPath | 既存ConfigBaseがあるがindex/searchは0。型filterの文字列契約によるfalse negative | 5011 / 48 / 854 | 5.87 / 28.40 / 31.35 |
| 設定payloadのAES-GCM暗号化 / userdirs / `AES-GCM encrypt` / 1 / なし | 人が4関数を読み、該当なしと確定。searchの0件だけを不在証明にはしない | 0 / 987 / 779 | 7.38 / 31.71 / 30.58 |

S1/S2の正解確認に共通のtarget Readを追加すると、それぞれ534bytes/10行/2.70ms、336bytes/13行/2.21msが別途必要。searchが返す確認argvは各6行/12行で、同じ情報量のReadではない。S1のsearchはindexより出力が多い。rgは両正例で既に正しい行へ到達し、速かった。searchの候補整理は観測できたが、rgで見落とした既存実装を救ったという結果ではない。

alias課題のnext_actionを実行すると、必要語数2→1で0件のまま（820bytes追加）、次にreturns条件を明示解除して2候補へ到達（2277bytes追加）。初回から計3951bytes/3検索となる。黙って型を解決したことにはせず、不要な再問い合わせ費用も結果に含める。

doc/source課題でも失敗を残した。[934](934-feat-dupscan-source-evidence-search.md)のE1では`相対 symlink`のOR検索がConfigFileだけを返し、正解MoveIfMissingは出ない。E2の`as a drop`では短い`a`も一致語になるため上位5件に正解が入らず、evidenceでは到達した。既知の用例だけに合わせたstopword・rank調整は今回行わない。

判断: 現行textの候補整理と限界は評価できた。JSON、同義語、cache、追加rank featureをこの結果だけで採用しない。正しい再利用/不要な重複追加を検索そのものが改善したという実agent差は未実証で、925はopenを維持する。次は独立した実際の見落とし事例から新しい1課題を事前固定し、rg/target Readへsearchを追加した差を調べる。今回の成功/失敗例を入れ替えて効果を作らない。

改善の再評価: 2正例の上位到達だけでなく、型aliasのfalse negative、ORの非同等候補、再検索の実費用、強いrg対照を記録した。機能追加が必要と決めつける状態から、現行版を維持しつつ実証不足を具体的に追える状態へ進めた。コードや一般的な開発効率を改善したという主張ではない。

- 2026-10-28: `docs(dupscan): evaluate current discovery with fixed tasks and edit pilot`で固定評価・互換4条件・失敗と残作業を記録。実証したtext条件だけチェックし、未実証のagent効果を完了扱いにしない。

- 2026-10-28: `fix(dupscan): skip term retries that cannot pass hard filters`で既知alias例の不要な往復を除き、型/file/scopeと元minを維持。局所的な検索/確認機能は完了し、一般的な再利用効果は928に未実証として残す。

- 2026-10-28: 文書反証レビューP3に従い、hard0でも明示filterが無ければevidenceへ進むこと、案内は予算不足なら省くことを明記。実測値と完了scopeにP1/P2指摘なし。
