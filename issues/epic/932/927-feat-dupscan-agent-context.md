# 927 (feat): dupscan がagent向けに定義・参照・呼び出しをまとめて返す

起票日: 2026-10-26

## 方向性の再評価（2026-10-27）

対象: a5b4196e の実装と既存測定。

**判断: 集約の方向は妥当で、context最小版と段階取得は既にある。追加設計より現行の評価を先に行う。** context_cmd.goは1回のscanSourceでcontentsと解析結果を共用し、定義・近似refs/callers・曖昧さ・非対応・parse error・省略を表示する。938でsummary、節選択`-sections`、元scopeを保持した再取得案内も実装済み。byte予算、snapshot/hash、cursor、definition IDは未実装である。

次は現在のsummary→section/ソース確認を、個別コマンドと適切なReadに対して固定課題で比較する。同名定義の選択失敗や大きすぎる出力が確認されたら、selectorまたはbyte上限を個別に設計する。snapshot/cursorは繰り返しの部分取得や版混在による失敗が実際に問題になる場合に限る。機能の数や出力短縮だけで効率改善としない。

1走査の確認は現行scanSourceの実呼び出しで行える。公開ReadSet/AnalysisView APIの新設は必要条件ではない。以下のコマンド/詳細設計は最小版に追加する場合の条件付き案とし、既実装機能を未着手へ戻さない。

## 目的と利用場面

「このメソッドを変更する前に、定義と利用箇所を知りたい」というClaude/Codexの探索を、複数コマンドとファイルReadの往復から短くする。byte上限のあるcontextを返し、取りこぼしや近似が見える形にする。最小版と段階取得は実装済み。一般的な効率改善の測定は未完了。

## 現状の裏取り（5a9a6a98時点）

- defは定義本体/型のoutline、refsは識別子の出現、callersは呼び出しを別々に返す。コマンドごとに走査し、出力をagent側でつなぐ。
- defの `-max-lines`、各コマンドのtextの `-limit` はある。JSONは全件を返すが、全セクション共通の出力byte予算・継続取得の契約はない。
- refs/callersは型解決をしておらず、同名の別メソッドも含みうる。「呼び出しグラフの正確な影響範囲」とは扱えない。

## 最小版への拡張コマンド案（以下のflag構成は未実装）

```sh
dupscan context -symbol StorageClient.upload -definition-file Sources/StorageClient.swift -max-bytes 12000
```

- まず定義をdisambiguateする。複数なら候補を返して選ばせる。definition-fileは定義の選択条件で、refs/callersをそのファイル内に制限する条件ではない。
- 選んだ定義、短い本体/outline、関連するrefs/callers、近似・総件数・省略・次の取得方法をまとめる。対象の言語・scopeを明示する。
- 同じ出現を複数セクションで扱う場合は関連を示し、独立した呼び出しや役割を誤って1件に潰さない。
- 一度読み込んだソース内容を共有して構成する。worktree全体の原子的snapshotを保証せず、参照したファイルの版/hashを表示し、次回取得で入力が変わった場合の扱いを決める。
- token数はモデルごとに違うため、CLIはbytes/行等の測れる予算を持つ。UTF-8とJSONを途中で壊さず、総件数・返却件数・省略箇所を示す。上限が最小のmetadataも収められないときの扱いを定義する。
- 近似のrefs/callersしか得られない場合は、その範囲と限界を示す。部分的な情報から「他に影響なし」と断定させない。

## 現行版を評価する受け入れ条件

- [x] 同じ入力で、既存def/refs/callersとの対応と、省略していない結果の一致を確認する。網羅性は既存コマンドの限界も引き継ぐ。（最小版で達成。下の進捗）
- [x] context_cmd.goの1回のscanSourceでcontents/解析結果を共用する。別の解析cacheや常駐プロセスを作らない。（a5b4196eでコード確認。呼び出し回数を壊す変異の検出強度は今回未検証）
- [ ] Claude/Codexで同じ変更課題を既存コマンド列と比較し、必要な情報への到達、往復数、返したbytes、elapsed time、見落としを測る。少ない出力だけを成功指標にしない。
- [x] 固定実ソースで1定義/18呼出しの集約、同名461近似と省略、12ファイルのparse失敗通知を確認する。（今回C1/C2）
- [ ] 大きい型、多重定義、同名の別モジュール、1行に複数call、0件で、現行summary/節選択/再取得案内の不足を記録する。今回の2条件で全matrixの完了にはしない。

### 追加機能を採用する場合だけの受け入れ条件

- [ ] selector/JSONを採用する場合、選択の曖昧さと定義選択/参照検索scopeの違いを表す。現行textの多重定義警告を「未実装」と扱わない。
- [ ] byte予算/継続取得を採用する場合、小さいbudgetでもJSON/UTF-8が有効で、省略と継続取得を検証できる。
- [ ] snapshot/hashを採用する場合、継続時の入力変更を検知/明示し、以前の結果と混ぜない。

関連: [926](done/926-feat-dupscan-query-diagnostics.md) の曖昧さ・解析不完全の契約を使う。[918](918-feat-dupscan-target-and-literal-table.md) の所属情報が無い段階では、利用可能なモジュールを推測で断定しない。literal差分表やType-3は既存issueの範囲を維持する。

## 詳細設計案（2026-10-26、未実装）

### 定義の選択

初期版のcontextはDefinitions/References/Callsを全て提供する言語だけを対象にする。9369b0beではSwiftが対象で、Goはcallersは使えるがdef/refsが無いためcontextはunsupported。928のregistryから案内を生成する。

```sh
dupscan context -format json-v2 -symbol StorageClient.upload -definition-file Sources/StorageClient.swift -lang swift -max-bytes 12000
```

同一fileにも、同じlabelで引数型が違うoverloadや条件付き宣言がある。複数候補ならresult.ambiguousを返し、refs/callersを選択済みの依存sectionとして出さない。各候補に `definition_id` と次のargvを付ける。

`definition_id` は `v1:SHA256(snapshot_id + file + start_byte + end_byte + kind + signature)`。lang.Definition/TypeDeclarationにsource spanを追加し、Swiftのnode byte範囲から取る。行番号だけを識別子にしない。同じsnapshot内で一意に選ぶためのIDで、改名・編集を跨ぐ永続symbol IDではない。

```sh
dupscan context -format json-v2 -definition-id v1:... -snapshot-id ... -repo /absolute/original-repo -path shared -lang swift -scope all
```

上記のrepo/pathは例で、生成argvは最初の要求の解決済みrepo/path/lang/scopeを全て保持する。ID指定とsymbol/definition-file指定は排他。選択後も探索scopeを変えず、表示sectionsと出力予算だけを変更できる。設定が変わればsnapshot不一致として再選択する。

IDはcurrent snapshotから列挙した定義に完全一致するときだけ受理する。無効/複数一致/別snapshotならrc=2。任意のcursor/IDからパスを組み立ててfileを読まない。

### 共有するReadSetと参照範囲

現行は1回のscanSourceのcontents/解析結果から定義・refs・callersを構成する。公開ReadSet/AnalysisViewを設ける場合も、この共用を維持する。選んだ定義に合わせ、refsは末尾名、callersは名前/labelに基づく近似だとsectionごとに明記する。selected definitionと完全に同じ対象へ型解決した結果とは表示しない。

definition-fileは選択条件だけ。refs/callersの検索は明示されたrepo/path/lang/scopeの範囲全体を使う。scopeがrootでなければ範囲を表示する。1行の別colにある複数callを1件にまとめず、refs/callersで同じ位置を示す場合は役割を保持して関連付ける。

### snapshotと継続取得

snapshot_idは、canonical repo root（worktreeを区別）、tool/adapter版、解決済みscope/言語、関連設定bytes、**列挙した対象ファイル集合と全内容hash**を順序固定でhashする。出力したfileだけのhashではない。読み込んだ内容の識別子であり、全repoを原子的に読んだ証明ではない。

初期版の継続はstateless。cursorは版・snapshot_id・canonical query hash・section・次のindexをbase64urlで持ち、最大4,096 bytes。署名付き認証tokenではない。再入力時にquery・現在の入力集合・内容を照合し、変更があれば `stale_cursor`（rc=2）と先頭からの再取得案を返す。途中の別版の結果を混ぜない。継続で再走査するコストは924と測定し、永続cacheを先に作らない。

cursorのquery hashは定義IDと探索条件を束縛する。表示上のsections/budgetはhashから分け、cursorが指す1つのsectionだけを再取得する。継続時は定義の固定metadataと対象sectionを返し、本体や取得済みsectionを再送しない。返却itemがある成功ページではnext indexが必ず増える。末尾ならcursorを返さない。snippetを短縮しても最小itemが収まらない場合は、同じcursorを成功扱いで返さず、予算エラーと増額案を返す。

以前hitしなかったfileの編集、新しい参照file、untracked追加、削除、改名、設定・adapter版の変更も無効化対象。cursorの不正形式、範囲外index、別repo、query改変、長すぎる入力を検査する。

### 出力予算

既定12,000 bytes、指定範囲1,024〜1,048,576 bytes。definition metadata→本体の先頭（既定最大80行）→refs→callersの順に割り当てる。section内はfile/line/colの固定順。JSON/text共に926の予算付きserializerを使い、source snippetやitem単位で予算内のprefixを構成して完成後のbytesを検証する。全件encode後の逐次削除や全文の途中での切断はしない。sectionごとのtotal/returned/truncatedと次の取得argvを返す。省略でtotalを減らさない。

本体が予算を使い切ってrefs/callersが0件返却でも、それらをno_matchとは表現しない。`-sections definition,refs,callers` で必要なsectionだけを再取得できる。定義本体の80行制限もtruncatedとして示し、必要ならsnapshot-bound file/range/hashを使うRead案を返す。本体全文のページ取得は初期版の範囲外。小さすぎて選択metadataも返せない場合は926の予算エラー。入力とASTのメモリ量は出力量とは別に測る。

### 実装と検査の単位

1. source span・snapshot builder・selectorを先に実装し、同一file/同labelの曖昧さを解消できることを検査する。
2. 既存のreport builderのfilter/metadataを共用してcontext modelを作る。型の参照/式の参照等の役割を保持する。
3. budget allocator・cursor codecをpure functionとして検査する。UTF-8、極端に長いsource/署名、空section、数千の参照、metadata超過、入力変更のmatrixを固定する。
4. 既存3コマンド列との結果比較と、代表repoでのwall time・peak RSS・総出力量・往復・見落としを測る。少ない出力だけで採用しない。

## 今回の3回のセルフレビュー（2026-10-26）

各回で読み取り専用Codex subagentの反証レビューと本体の自己点検を行い、指摘を次の回の前に反映した。

1. **CLI仕様・互換性**: 同一fileの多重定義を選べるsnapshot-bound IDと、検索対象全体の内容/集合によるcursor無効化を追加した。
2. **agentの使いやすさ・効果測定**: 再実行argvに元scopeを保持し、section別継続・index進行条件・巨大itemの予算エラーを追加した。
3. **実装構造・運用コスト**: 予算付きserializerを926へ共通化し、全件の繰り返しencodeを避ける設計へ修正した。

改善の評価: 起票時の機能案から、引数・状態・依存関係・異常系・検査手順を実装/レビュー可能な契約へ具体化した。共通仕様の正本は926、registry/評価手順は928、解析経路は924とし、重複した実装を避ける。これは設計文書の改善であり、実装・性能・開発効率の向上は未検証。受け入れ条件は未完了のままopenに残す。

## 既存候補を採用するまでの確認を短くする

[925](925-feat-dupscan-helper-search.md)で得た候補の定義・本体・利用例を、再利用を判断する根拠としてまとめる。候補の名前だけ分かっても、別々のRead/refs/callersで取りこぼして新設へ戻る往復を減らす。CLIの速度・bytesだけで効果を決めず、最終的な不要な重複追加を928で採点する。

初期版はSwiftのみ。contextの近似refs/callersを選択定義に型解決した使用例と表示しない。出力上限による省略や[929](done/929-bug-dupscan-gate-lost-file-parse-errors.md)と共通する解析不完全を、再利用先/影響箇所が存在しない根拠にしない。JSONのmetadataが単純なrgより大きい場合もあるため、候補の確認に必要なsectionとsnippetを予算へ収め、実際の総tool出力量で評価する。

## コンテキスト節約の確認条件

初期表示は少数候補と一致根拠に絞り、選択した定義の本体/使用例を必要なsectionだけ追加取得する導線を[925](925-feat-dupscan-helper-search.md)/928と揃える。JSON metadataや同じsnippetの再送が増えた分も計上する。1ページの上限を守ったことと、実装完了までにagentが読む総量やwindow占有が減ったことを区別する。品質を保った総context量と不要な重複追加の同時評価は928が担当する。

## 進捗

- 2026-10-26: 起票。既存コマンドの組み合わせで手動では可能な探索を、上限付きの1問い合わせへまとめる提案。効果は未測定。
- 2026-10-26: Codexの読み取り専用subagentで、CLIの事実・互換性とagent導線・効果測定の2観点から反証レビュー。P1/P2/P3の指摘なし。静的レビューであり、新機能の実装・性能・agentによる利用効果を検証したものではない。
- 2026-10-26: 相対リンク・diffと、Bash 5でのissue関連テストを検査して成功。提案の本文を記録し、機能の実装・効果測定はopenの作業として残す。

- 2026-10-26: grepによる既存実装の見落とし・類似メソッドの追加を減らす目的に照らし、出力の誤解と探索成功の評価条件を追記。実装・効果測定は未着手。

- 2026-10-26: 既存実装発見・比較評価について読み取り専用Codex subagentで反証レビュー。925の「候補の適切性をCLIが判断するように読める」P3を訂正し、候補抽出とagentによる採用判断を分けた。 issue内の相対リンク・JSON/Python構文・diffとBash 5のissue関連テストが成功。929の本文内手順と930の3問い合わせをCLIで再現した。文書の改善は、出力誤認の再現・責務と、既存実装発見/不要な重複追加を測る条件を具体化したこと。機能改善の効果は未検証。

- 2026-10-26: 段階取得と総context量の評価を928へ接続。 読み取り専用Codex subagentで指摘の網羅性とcontext計測を反証レビューし、P3（現行/設計、着手条件、計測範囲）の3点を訂正した。文書の改善は、不足した指摘の担当と検証条件を明確にしたこと。実装・両効果の実証は未着手。

- 2026-10-26: **最小版を実装** (`dupscan context`。ユーザー指示「最小版で進めて」)。上の詳細設計のうち、924 (ReadSet)・926 (json-v2・予算付き serializer・状態)・928 (registry) に依存しない部分だけを作った。
  - 作ったもの: def と同じ問い合わせを受け、1 回の走査 (`scanSourceOptions` の calls / defs / refs を全部オン) から定義・参照・呼び出しの節を text で並べる。1 行目に 3 つの件数、続けて近似の範囲・定義が 0 件か 1 つに決まっていないこと・読めない構文のファイル (先頭 3 個 + 件数)・読まなかった言語を書く。`Type.init` は型の名前で refs / callers を引く。0 件でも rc=0 (926 の v2 の方針に合わせた)
  - 作らなかったもの: JSON (926 が既存の JSON の形を固定するので、v2 より前に固定すべき形を増やさない)・`definition_id` / snapshot / cursor・byte 予算・`-sections`。理由は 926 / 924 / 928 が未実装で、先取りすると二重に作るため
  - def / refs / callers の text 出力は関数を分けただけで、既存の出力は変えていない (省略の案内の文言だけを context 用に差し替える `*TextMore` を足した)。def と refs の走査結果からの組み立ては helper (`buildDefinitions` / `buildReferences`) に寄せて context と共用した
  - 検査: `src/dupscan/context_cmd_test.go` の 7 本。節ごとに単独コマンドの `-limit 0` の text と一致すること・`Type.member` で参照を絞らないこと・省略の案内に `-format` を出さないこと (def の案内は不変)・0 件・`Type.init`・読めないファイルと Go の注記・引数の誤り。変異 10 本 (参照を問い合わせ全体で引く・ラベルを落とす・曖昧の行を消す・読めないファイルの行を消す・既存の案内を使う・注記を消す・init を名前で引く・0 件の行を消す・要約の注記を消す・節の見出しを問い合わせの名前にする) がすべて想定の検査で red
  - opus の敵対的レビュー 1 周目: 既存コマンドの退行は壊せなかった。P1 1 件 (`SMB2Header.init` が callers を名前 init で引き、self.init など無関係の 10 件を出して `SMB2Header(…)` の 188 件を落としていた) と P2 3 件 (定義 0 件の注記なし・Swift と Go の混在で呼び出しの節が Go を落とすのを書いていない・README との食い違い)・P3 (ラベルで絞るのに「名前だけ」と書いていた) を再現して直した
  - opus の敵対的レビュー 2 周目 (直した差分): P1 なし。P2 2 件 (ラベルで引くと既定値のある引数を省いた呼び出しを落とす・`Foo<T>()` の呼び出しを数えない。どちらも callers の限界で、要約・README に書いていなかった) と P3 (`Type..init` が空の名前で通る・`Box<T>.init` が `Box<T>` のまま引く・`Self(…)` を数えないのを書いていない・`Type.init` の参照は型の名前の全部の出現になる) を、再現して直すか要約・README に書いた。変異 3 本 (空の名前を通す・generic の引数を残す・既定値の注記を消す) も想定の検査で red
  - 記録だけのもの (直していない): `dupscan context` は `context` という名前の dir の report ではなくサブコマンドになる (def / refs / callers と同じ扱い。`dupscan report context` で引ける)。定義のソースに列 0 の `## ` で始まる行があると節の見出しと紛れる (swift-smbee・screen-share-app には無かった。再提起には実例が要る)
  - 実測 (swift-smbee、`POSIXSocketTransport.connect`、既定の `-limit 20`、3 回): def・refs・callers を別々に 3 回で 2.34〜2.56 秒・9,731 bytes、context 1 回で 1.10〜1.12 秒・10,316 bytes。**時間は半分以下になるが、出力は見出しと要約の分だけ 6% 増える**。agent の往復・見落とし・総 context 量への効果は未測定 (928 の評価で測る)


- 2026-10-27: 924でcontextを含む探索共通経路をmetadata-onlyへ変更し、既存の出力一致を確認した。927の最小版は維持し、予算/継続取得など詳細設計の残りは継続する。

- 2026-10-27: 938でcontext/evidenceの先頭状態・grouped text・scope付きnext_actionと任意summaryを実装/局所比較した。938の完了は本issueの全受け入れ条件の完了を意味しない。既定は変更せず、bytes予算/cursorなど残りのcontext設計を継続。

- 2026-10-27: `docs(dupscan): reassess open epic 932 work against current behavior`で現実装と残設計を再照合し、上記の最小作業・着手条件へ更新。改善は誤った未実装扱いと一括実装の前提を減らしたこと。コード変更、開発効率/重複追加率の改善は今回の成果に含めない。

- 2026-10-27: `docs(dupscan): correct evidence terminology and conditional acceptance`で反証レビューのP3を訂正。行CallsとCalled集約、節選択と定義選択、label指定の有無を区別し、現行評価と追加機能の受け入れ条件を分けた。P1/P2指摘なし。

## 現行contextの固定課題評価（2026-10-27）

09e983fcのbinaryで、Obaketの386e287652125961d045fe565d0e18fe41495d89から`shared/Sources/ObaketCore`と`shared/Tests/ObaketCoreTests`をgit archiveして固定した。query/正解をUTC 12:35:50に凍結し、測定後に調整しなかった。snapshot corpusには12個のparse error fileがあり、contextが不完全性を通知した。元repoの編集中ファイルや設定は使用・変更していない。

| 課題 | 個別def+refs+callers（3回） | context（1回） | summary→definition,callers（2回） |
|---|---:|---:|---:|
| C1 MetadataDiff.compute | 8538bytes / 1054.82ms | 8149bytes / 520.31ms | 6315bytes / 1103.93ms |
| C2 BoundedFIFOArray.append | 10320bytes / 1023.76ms | 9679bytes / 485.70ms | 6605bytes / 970.44ms |

各5回の中央値。C1は人が対象ソースと18個のqualified call行を照合し、定義1・refs18・calls18との対応を確認した。個別比較は`def MetadataDiff.compute`、`refs compute`、`callers compute`。refsへ型名を渡して0件にする弱い対照は使わない。contextは同じ近似結果を1回で集め、個別3回に対する往復と所要の減少をこの条件で観測した。一方、適切な`rg -n -F MetadataDiff.compute`と定義43〜77行のReadは3400+1723bytes、14.14+2.55msだった。型位置のrefs等を含む同情報量ではなく、contextがrgより速いとは主張しない。

C2は明確な失敗例である。定義は正しく選べてもrefs/callsはappendという名前で461件になり、最初の20件に目的のFIFOテストが無い。人が型の使用箇所を確認すると、この固定corpusでBoundedFIFOArrayを構築するのはテスト2箇所で、対応するappendは5回だった。461を選択した型の使用数と扱うと誤る。出力の近似・省略表示はあるが、summaryだけから影響範囲を確定できない。

当初凍結したC2のqualified rgは0件で、メンバー変数経由の呼び出しを探す対照として不適切だった。この失敗を隠してcontext優位とは採点しない。結果確認後の別probeとして、`rg -n -F BoundedFIFOArray shared -g '*.swift'`→該当テスト1〜31行と定義32〜42行のReadを実行した。3コマンド2050bytes/中央値13.15ms（11.72〜20.98ms）で型を構築する箇所と5 callを確認できた。これは事後の回復戦略で、本番の独立比較には数えない。

判断: C1の既知名の集約は有効だったが、C2では型を特定した影響調査が未完になる。JSON/cursor/snapshot/hashを追加してもこの近似は解決しないため、今回の評価からは不採用とする。既存の警告と型名rg/target Readへのfallbackを維持する。一般の変更課題でagentが見落とさず作業できる差は未測定で、927はopenを維持する。次はC2のような同名メソッドで、summaryを受けたagentが実際に必要な型使用箇所へ回復できるかを、source開示の承認範囲内で確認する。型解決を一式実装することを前提にしない。

改善の再評価: 3問い合わせから1問い合わせへまとめた局所的利点と、同名461件に埋もれる失敗を分けた。段階取得は今回、bytesを減らす代わりに再走査時間を増やした。小出力を成功扱いせず、次の検証対象を明確にした文書の改善である。

- 2026-10-28: `docs(dupscan): evaluate current discovery with fixed tasks and edit pilot`で現行contextの局所的な集約効果と同名近似の失敗を記録。実agent変更課題は未完了としてopen継続。
