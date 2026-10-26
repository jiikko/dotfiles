# 927 (feat): dupscan がagent向けに定義・参照・呼び出しをまとめて返す

起票日: 2026-10-26

## 目的と利用場面

「このメソッドを変更する前に、定義と利用箇所を知りたい」というClaude/Codexの探索を、複数コマンドとファイルReadの往復から短くする。byte上限のあるcontextを返し、取りこぼしや近似が見える形にする。実装・効率改善の測定は未着手。

## 現状の裏取り（5a9a6a98時点）

- defは定義本体/型のoutline、refsは識別子の出現、callersは呼び出しを別々に返す。コマンドごとに走査し、出力をagent側でつなぐ。
- defの `-max-lines`、各コマンドのtextの `-limit` はある。JSONは全件を返すが、全セクション共通の出力byte予算・継続取得の契約はない。
- refs/callersは型解決をしておらず、同名の別メソッドも含みうる。「呼び出しグラフの正確な影響範囲」とは扱えない。

## コマンド案（未実装）

```sh
dupscan context -symbol StorageClient.upload -definition-file Sources/StorageClient.swift -max-bytes 12000
```

- まず定義をdisambiguateする。複数なら候補を返して選ばせる。definition-fileは定義の選択条件で、refs/callersをそのファイル内に制限する条件ではない。
- 選んだ定義、短い本体/outline、関連するrefs/callers、近似・総件数・省略・次の取得方法をまとめる。対象の言語・scopeを明示する。
- 同じ出現を複数セクションで扱う場合は関連を示し、独立した呼び出しや役割を誤って1件に潰さない。
- 一度読み込んだソース内容を共有して構成する。worktree全体の原子的snapshotを保証せず、参照したファイルの版/hashを表示し、次回取得で入力が変わった場合の扱いを決める。
- token数はモデルごとに違うため、CLIはbytes/行等の測れる予算を持つ。UTF-8とJSONを途中で壊さず、総件数・返却件数・省略箇所を示す。上限が最小のmetadataも収められないときの扱いを定義する。
- 近似のrefs/callersしか得られない場合は、その範囲と限界を示す。部分的な情報から「他に影響なし」と断定させない。

## 検証・受け入れ条件

- [ ] 選択の曖昧さと、定義選択/参照検索scopeの違いをtext/JSONに表す。
- [ ] 同じ入力で、既存def/refs/callersとの対応と、省略していない結果の一致を確認する。結果の網羅性は既存コマンドの限界も引き継ぐと明示する。
- [ ] 大きい型、多重定義、同名の別モジュール、1行に複数の呼び出し、source変更、読めない構文、0件を検査する。
- [ ] 小さいbudgetでもJSON/UTF-8が有効で、省略と継続取得が検証できる。継続時の入力変更は検知/明示し、以前の結果と混ぜない。
- [ ] Claude/Codexで同じ変更課題を既存コマンド列と比較し、必要な情報への到達、往復数、返したbytes、elapsed time、見落としを測る。少ない出力だけを成功指標にしない。
- [ ] [924](924-perf-dupscan-exploration-latency.md) の1走査/batch案と処理を共有できるか確認し、別の解析cacheや常駐プロセスを重複して作らない。

関連: [926](926-feat-dupscan-query-diagnostics.md) の曖昧さ・解析不完全の契約を使う。[918](epic/890/pending/918-feat-dupscan-target-and-literal-table.md) の所属情報が無い段階では、利用可能なモジュールを推測で断定しない。literal差分表やType-3は既存issueの範囲を維持する。

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

924の1つのReadSet/AnalysisViewから定義・refs・callersを構成する。選んだ定義に合わせ、refsは末尾名、callersは名前/labelに基づく近似だとsectionごとに明記する。selected definitionと完全に同じ対象へ型解決した結果とは表示しない。

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

初期版はSwiftのみ。contextの近似refs/callersを選択定義に型解決した使用例と表示しない。出力上限による省略や[929](929-bug-dupscan-gate-lost-file-parse-errors.md)と共通する解析不完全を、再利用先/影響箇所が存在しない根拠にしない。JSONのmetadataが単純なrgより大きい場合もあるため、候補の確認に必要なsectionとsnippetを予算へ収め、実際の総tool出力量で評価する。

## 進捗

- 2026-10-26: 起票。既存コマンドの組み合わせで手動では可能な探索を、上限付きの1問い合わせへまとめる提案。効果は未測定。
- 2026-10-26: Codexの読み取り専用subagentで、CLIの事実・互換性とagent導線・効果測定の2観点から反証レビュー。P1/P2/P3の指摘なし。静的レビューであり、新機能の実装・性能・agentによる利用効果を検証したものではない。
- 2026-10-26: 相対リンク・diffと、Bash 5でのissue関連テストを検査して成功。提案の本文を記録し、機能の実装・効果測定はopenの作業として残す。

- 2026-10-26: grepによる既存実装の見落とし・類似メソッドの追加を減らす目的に照らし、出力の誤解と探索成功の評価条件を追記。実装・効果測定は未着手。

- 2026-10-26: 既存実装発見・比較評価について読み取り専用Codex subagentで反証レビュー。925の「候補の適切性をCLIが判断するように読める」P3を訂正し、候補抽出とagentによる採用判断を分けた。 issue内の相対リンク・JSON/Python構文・diffとBash 5のissue関連テストが成功。929の本文内手順と930の3問い合わせをCLIで再現した。文書の改善は、出力誤認の再現・責務と、既存実装発見/不要な重複追加を測る条件を具体化したこと。機能改善の効果は未検証。
