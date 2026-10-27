# 934 (feat): dupscanの候補探索にdoc全文・テスト使用例・元ソースを補う

起票日: 2026-10-26

## 方向性の再評価（2026-10-27）

対象: a5b4196e の実装と既存測定。

**判断: 補助証拠の方向は妥当で、evidence最小版は実装済み。残りは品質評価を先に行う。** 現行はdoc全文・test/sourceのlexical hit、parse error・省略・次の問い合わせを返し、938のgrouping/scoped取得と928の能力案内もある。metadata-only走査のscanned.contentsを共用済みで、新しいReadSetがないから未着手という状態ではない。

次はdoc後半/テスト使用例が鍵の固定課題と、古い例・紛らわしいコメント・同名別定義の負例で、適切なrg+該当箇所Readに比べて正しい候補へ到達できるか、誤誘導と確認量を測る。source hitだけでhelper発見や再利用を確定しない。比較側を広いcatに固定してbytes差を効果と呼ばない。

Go testは比較から除いてもcallsを収集し、937のcallersはtest ownerを保持する。一方evidenceのGo test ownerは単位外になり、独立したGo test/type docもdoc枠には収集しない制約が残る。実課題で必要ならここを個別に直す。hash/version・共通byte予算・新ReadSet APIは、版の混在/大出力などの失敗を確認してから検討する条件付き案とする。

## 目的と不足

構文単位の索引から落ちる手掛かりも使い、名前不明の既存実装の見落としと、確認のための大量Readを減らす。[925](925-feat-dupscan-helper-search.md)は初期doc先頭行まで、[927](927-feat-dupscan-agent-context.md)は選択済み定義/近似refs/callersのcontext。本issueは、まだ候補が無い段階でdoc後半やテスト内の使用例から候補へ到達する補助経路を担当する。

## 現状の裏取り（2920a72a）

- Unit.Docはdoc comment先頭行。SwiftのdocCommentとGoのdocLineは索引に全文を保持しない。
- GoのfuncDeclはTest/Benchmark等を比較単位から外す。callsはテスト内のcallも収集するため、全テスト情報が失われるという意味ではない。テスト関数をそのまま重複比較対象へ戻す変更は不要。
- 現行はmetadata-only走査のscanned.contentsを共用する。公開ReadSetは過去の未実装設計案。元sourceのcontentsは存在するので、索引へ無制限に全文を複製する必要はない。[931](../../done/931-refactor-tuikit-domain-duplicates-and-discovery-check.md)では広いcatで出力予算を消費し、実際のモデル可視量までは測れていなかった。

## 元の設計案（1〜3の最小版は実装済み、残りは条件付き）

1. doc全文、テスト/使用例、元sourceのlexical一致を、定義検索と区別した証拠として返す。各hitにkind/file/位置/短いsnippet/一致理由を持たせる。コメントや文字列の一致を宣言や実callと偽らない。
2. 最初は明示的な補助検索modeかnext_actionsとして提供する。定義0件とsourceのhitは別の件数にし、sourceのhitがあるだけで対応するhelperを見つけたと扱わない。hitが非公開/別責務かは確認を残す。
3. 比較単位の除外を保ったまま、doc/テストはevidence-onlyとして解析する。型解決なしに使用例を特定定義へ紐づけられない場合は曖昧さを保持する。Goがdef/refsに対応したことにはしない。
4. 原文は924のReadSetを共用し、file/hashと結果scopeを返す。無制限の全文index/cacheを最初から追加せず、候補選択後の詳細は927へつなぐ。返却snippet・件数・bytesは926の予算を共用する。
5. 未対応言語/設定等の検索は能力範囲を偽らず、必要なら通常のrg/Readへつなぐ。grepを禁止しない。正解が減るほど出力を切り詰めてコンテキスト節約と報告しない。

## 検証・受け入れ条件

- [x] sourceコメント/doc後半からの正例2件と、用途を否定するdocコメントの負例1件を事前固定して確認する。（今回E1〜E3）
- [ ] テストの具体的使用例が唯一の手掛かりになる課題、古い例、同名の別定義の対照を追加する。E1のtestコメントだけで全条件を満たしたとはしない。
- [x] 旧比較単位・gateの違反/除外が変わらず、source hitと構文上の定義/callを分けられる。（最小版: doc の範囲はハッシュ・鍵に入れない。既存の report / index / def の出力の一致を実データで確認。make test の gate も通過）
- [x] 部分解析・不正UTF-8の扱い・長い行・省略を表示し、原文位置やsymbol解決を捏造しない。（最小版で達成）
- [ ] 928の同じ課題で、正しい候補への到達・誤誘導・再利用・不要な重複追加・実際に観測できたコンテキスト量を比較する。今は効果未測定。

## 進捗

- 2026-10-26: `issues: reserve dupscan behavior hints and source evidence search`で番号予約。925/927が担う範囲と補助検索の不足を区別して記録した。実装・性能・利用効果は未検証。

- 2026-10-26: 未実装ReadSetと現行contentsを区別し、補助証拠の取得範囲を整理。 読み取り専用Codex subagentで指摘の網羅性とcontext計測を反証レビューし、P3（現行/設計、着手条件、計測範囲）の3点を訂正した。文書の改善は、不足した指摘の担当と検証条件を明確にしたこと。実装・両効果の実証は未着手。

- 2026-10-27: **最小版を実装** (`dupscan evidence <語>`。ユーザー指示「これもお願いします」を受け、927 と同じく 924・926 に依存しない部分だけを作った)。
  - 作ったもの: 語 (大文字小文字を区別しない) を含むソースの行を、doc / テスト / その他のソースに分けて返す (同じ行を 2 つに数えない)。doc の行は doc が付いた宣言を 1 件にまとめ、テスト・ソースの行は囲む関数 (関数の外なら型) とその行の呼び出しを添える。テストとソースの行で呼ばれている名前のうち repo の定義と同じ名前を、行の数の多い順に先頭に出す。text だけ・0 件でも rc=0・節ごとの `-limit`・行は 160 文字、署名は 80 文字で切り、壊れた UTF-8 は置き換える
  - 設計案との対応: 1 (定義検索と区別した証拠・kind/file/位置/snippet・コメントや文字列を宣言や呼び出しと偽らない) と 2 (明示的な補助検索 mode・件数を分ける) と 3 (比較単位の除外は保ったまま doc・テストを evidence-only で読む。Go の def/refs 対応にはしない) を満たす。4 (924 の ReadSet・926 の予算) と 5 の能力表示 (928) は未実装の依存なので作っていない。走査は既存の `scanSourceOptions` の contents を使い、全文の索引や cache は作っていない
  - adapter の変更: `tree.Unit`・`lang.Definition`・`lang.TypeDeclaration` に doc comment の行の範囲 (`DocStart` / `DocEnd`) を足した。ハッシュ・鍵には入れない (report / gate の判定は変わらない。既存の `report -format json`・`index -doc` の出力が変更前と一致することを、敵対的レビューで実データの diff で確かめた)。Swift は宣言に近い側から続く `///` / `/** */`、Go は関数の doc
  - 限界 (README に書いた): 集計の「repo の定義と同じ名前」は名前だけで照合するので、同名の外の関数 (`t.Fatalf`・`strings.Join`・`append(contentsOf:)`) も数える。Go のテスト関数・型の doc は取れない (Go は定義を集めない。937 と同じ限界)。`/** … */ func f() { g() }` のように doc と宣言を 1 行に書くと、その行は doc に数え、行の呼び出しは添えない
  - 検査: `src/dupscan/evidence_cmd_test.go` の 10 本 (doc の 2 行目以降の語から単位へ辿る・`index -doc` では引けないことを前提として確かめる・doc でないコメントを doc にしない・テストとソースを分けて囲む関数と呼び出しを添える・集計は repo の定義と同じ名前だけ・doc を単位ごとにまとめる・protocol の要件・保存 property・型・テスト関数の doc・MARK の上の doc を入れない・長い署名と長い行を切る・0 件・引数の誤り)。変異 16 本がすべて想定の検査で red
  - opus の敵対的レビュー 1 周目: P1 なし・既存の退行なし。P2 3 件 (集計に同名の外の関数が混ざる・protocol の要件と保存 property の doc を拾わない) と P3 4 件 (MARK を挟んだ doc の範囲・テスト関数の doc・長い署名・コメントの位置) を再現して直すか README に書いた
  - opus の敵対的レビュー 2 周目 (直した差分): P1・P2 なし。既存の `index` / `def` / `report` の出力が変更前と一致し、evidence の区分の移動は「ソース→doc」「テスト→doc」だけで doc の行が本体の行を吸う例も無かった (swift-smbee・screen-share-app の実データ)。P3 のうち README の書き方 (MARK の上の doc の条件・対象の一覧) を直した。記録だけのもの: `index -doc` の最初の行は MARK を挟んだ上の doc も探すので、その形では evidence の doc の範囲と食い違う (index -doc の出力を変えないため揃えていない。再提起には、食い違いで候補を取りこぼした実例が要る)。署名を 80 文字で切ると同じファイルの overload が同じ文字列になる (行番号で区別できる)。`/// dupscan:allow` の形の印は doc の範囲に入る
  - 実測 (swift-smbee): `evidence credit` は 1.25 秒・15,137 bytes (既定の `-limit 20`)、同じ語の `rg -n -i -g '*.swift'` は 2,119 行・240,204 bytes。`cancellation` は 0.92 秒・12,540 bytes と 132,103 bytes。**evidence は節ごとに切っているので同じ情報量の比較ではない**。正しい候補への到達・誤誘導・再利用・重複追加への効果は未測定 (受け入れ条件の 1・4 は 928 の評価で測る)


- 2026-10-27: `docs(dupscan): reassess open epic 932 work against current behavior`で現実装と残設計を再照合し、上記の最小作業・着手条件へ更新。改善は誤った未実装扱いと一括実装の前提を減らしたこと。コード変更、開発効率/重複追加率の改善は今回の成果に含めない。

## 補助証拠の固定課題評価（2026-10-27）

09e983fcでE1〜E3を事前固定した。Go corpusは09e983fcのuserdirs、Swift corpusはObaket 386e287652125961d045fe565d0e18fe41495d89のCore。凍結条件とagent検証の制限は[928](928-feat-dupscan-agent-onboarding.md)を参照。課題文には正例の関数名を含めず、ソースから決めた正解集合で確認した。

| 課題 / 固定phrase | 人が決めた正解と観測 | rg / index-doc / search / evidenceのbytes | 中央値ms（同順） |
|---|---|---:|---:|
| E1 相対linkの移行 / `相対 symlink` | MoveIfMissingが正解。source枠は囲む関数付きで到達、test枠は単位の外。searchはConfigFileだけで正解なし | 404 / 48 / 1122 / 1291 | 6.25 / 29.89 / 28.40 / 27.98 |
| E2 既存keyの置換をdropに数えない / `as a drop` | BoundedFIFODictionary.updateValueのdoc2行目へ到達。index-docは0、search上位5に正解なし | 180 / 51 / 3755 / 1670 | 9.55 / 123.83 / 122.45 / 179.67 |
| E3 UI付帯情報だけで更新を通知したい / `UI 付帯情報` | 正解集合は空。MetadataDiff.computeはその更新を意図して除外するため負例 | 198 / 51 / 3298 / 1697 | 9.20 / 121.42 / 113.79 / 153.74 |

rgは各phraseを`rg -n -i -F <phrase> -g <言語拡張子> <同scope>`で検索した。正例2件ともrgで該当行へ既に到達し、evidenceの方が遅く大きかった。evidenceはfile/位置に加えdocの所属やsourceの囲む関数を返す。その追加情報を得たことと、同じ情報量で効率を改善したことを混同しない。各5回の中央値で、Swift source scopeは4個のparse errorを両evidenceで通知した。

正解の確認にはE1で547bytes/17行/2.20ms、E2で754bytes/20行/2.27msのtarget Readを別途要した。E3のsnippetは否定理由まで含まず、computeへのnext_actionも出た。source hitから再利用を即断すると誤る負例で、22〜84行のRead（3519bytes/63行/2.35ms）で除外の契約を確認して初めて却下できた。コメント一致を発見成功とは採点しない。

判断: metadataでは拾えない正例2件への補助経路は確認したが、強いrg+Read対照より少ない費用で正しい再利用に至る効果は示せなかった。新ReadSet、hash/version、共通JSON/cursor/cacheを今回の結果から採用しない。doc後半を必要時に見る既存fallbackは維持する。Go test owner不足はE1で再現したが、この正例ではsource ownerから到達でき、owner追加が必要な独立課題は未確認。実agentの見落とし・誤誘導・不要な重複追加への差は未検証で、934はopenを維持する。次はコメント一致を誤採用しやすい1課題で実際の候補確認を観測する。

改善の再評価: 2正例だけでなく、否定のコメントから誤った用途へ誘導されうる負例と確認の実費を記録した。出力節約だけを根拠に拡張する計画を避け、既存fallbackの役割と未実証の効果を分けた。コード・実token・開発効率の改善は今回の成果に含めない。

- 2026-10-28: `docs(dupscan): evaluate current discovery with fixed tasks and edit pilot`でdoc/sourceの正負例を評価。強いrg対照との差と確認Readの費用を残し、実agent効果はopenで追跡。
