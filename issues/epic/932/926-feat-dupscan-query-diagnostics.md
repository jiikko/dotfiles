# 926 (feat): dupscan の問い合わせ形式と検索0件の診断を揃える

起票日: 2026-10-26

## 目的

Claude/Codexがコマンドの書き方の違いで失敗したり、非対応の問い合わせを「対象が存在しない」と解釈したりする往復を減らす。正しい次の問い合わせを少数返せるCLIにする。今回は提案の記録であり、実装・効果測定は未着手。

## 現状の裏取り（5a9a6a98時点）

- `main.go` の `parseInterspersedFlags` をdef/refsは使う。index/callers等は `parseFlags` を使い、位置引数以降のflagの扱いが異なる。
- `def` は `Type.member` と引数ラベルを解釈する。`refs` は問い合わせ文字列と識別子名の一致で探すので、`refs Worker.run` は `run` の呼び出しがあっても0件（直前の使い捨てSwift repoで再現済み）。
- def/refsは0件でrc=1、index/callersは0件でもrc=0。現在のJSONには、問い合わせ非対応・本当の0件・解析不完全を区別する共通の状態欄がない。
- parserが読めなかったファイルの情報は探索コマンドへ返されていない。0件は全構文を調べた証明にならない。

## 改善案

1. 共通flagの位置と `--` の扱いを揃える。名前・パスの前後どちらでも受け付ける案を、各コマンドの曖昧な位置引数と衝突しないか確認する。file/scope/prod/testの絞り込みも意味を明示して揃える。
2. 非対応の `Type.member` / ラベル / 言語は、単なる0件と区別し、対応する問い合わせ例を返す。型の解決なしに末尾名へ黙って置換し、絞り込み済みと誤認させない。
3. 近い名前・別のラベル・絞り込み候補を少数返す。自動で再検索・実行せず、次に使えるargvを構造化して返す。再試行ループや際限のない候補列挙を避ける。
4. JSONにschemaの版、状態、部分的な解析であること、結果総数/返却数、省略、次の問い合わせ案を持たせる。案の状態名は `no_match` / `unsupported_query` / `unsupported_language` / `partial` 等。具体的schemaは実装時に確定する。
5. 既存の正常な呼び出しの終了コードとJSONの互換性を維持する。0件を止めたい用途は共通の明示flag（例: `-require-match`）を設計し、既存defaultを一律変更しない。解析不完全を止めるmodeも、通知とrcの契約を決めてから追加する。

## 検証・受け入れ条件

- [ ] まず現在のflag/位置引数/終了コードを表とテストで固定する。
- [ ] 全コマンドでflag位置、空白を含むパス、`--`、同名/多重定義、非対応言語、読めない構文、正当な0件、誤った問い合わせ形式を検査する。
- [ ] 同名の別型を含むrefs/callersは近似であると分かり、型を絞れたという誤った表示をしない。
- [ ] parser errorに関数が飲み込まれて単位を抽出できない場合も、解析不完全を通知できることを確認する。
- [ ] 次の問い合わせ案は安全にquote可能なargvとして返し、表示内容を自動実行しない。候補数と出力量に上限を設ける。
- [ ] Claude/Codexで、誤った問い合わせから目的の結果までの再試行回数と、誤った不存在判断が減るかを比較する。改善量は実測する。

関連: [900](../890/900-research-dupscan-next-languages.md) の言語ごとの対応範囲を反映する。[928](928-feat-dupscan-agent-onboarding.md) の能力情報と整合させる。gateのbase/違反/除外の判定を変更する作業は、この探索診断と切り分ける。

## 詳細設計案（2026-10-26、未実装）

### 互換性と導入順

既存 `-format text|json|xcode` の正常な出力・終了コードは維持する。machine向けの共通契約は **明示的な `-format json-v2`** に追加し、既存JSONへ共通envelopeを黙って被せない。新しいsearch/context/doctor/capabilitiesもjson-v2を受け付ける。旧モードは現行goldenを固定し、正常ケースのbyte比較で退行を検査する。

CLI定義は928の `internal/commandmeta` を共通の情報源にする。実装順は「command/adapter registry→924の解析Plan/View→本v2診断→search/context→doctor/guideの詳細診断」。循環依存は作らず、共通packageは各commandのRun実装をimportしない。

### 引数の共通規則

- flagは位置引数の前後で受け付ける。既存flagの型・既定値は維持し、単値flagの繰り返しは現行Go flagと同じく最後を採る。複数値の-pathは例外として全指定を保持する。`--` 以降はflagとして解釈しない。
- `-repo DIR` は探索repoを指定。未指定はcwdからgit rootを解決する。`-path DIR` はrepo内の相対directory prefixで、複数指定は和集合。実在directoryを正規化し、repo外へ出る指定・symlink経由の指定はrc=2。directory境界で一致させ、重複prefixを除く。Gitにもliteral pathspecとして渡し、[]*?等をglobとして解釈させない。shell quotingだけで済ませない。legacyの位置引数directoryと新しいrepo/path指定の併用は、曖昧なためrc=2にして直し方を示す。
- v2の `-file` はrepo相対の結果selector。scopeのscan範囲を勝手に狭めない。legacy -fileのcwd相対解決は旧モードに残す。contextの `-definition-file` は定義選択だけに使う。
- `-scope all|prod|test` は返す宣言/出現の分類を絞り、既定all。呼び出し数が別scopeで集計される場合は `calls_scope` を明記する。
- `-lang auto|swift|go` はregistryから解決。明示swiftならGoは意図的な対象外、autoで既知のGoファイルをdef/refsが読めなければskipとしてcoverageに載せる。domainは列挙したrepo sourcesであり、scope内のtracked/未ignoreのvendor sourceも含む。外部SDK・repo外の依存取得・未知の拡張子は対象外で、module/targetの所属解決をしたとは表示しない。
- v2のrefs/callersは単独名（callersはlabel指定可）。`Worker.run` を型解決済みの絞り込みとして受理せず `unsupported_query` とし、defと、絞り込みなしのrefs/callersのargv案をreason付きで出す。この拒否はv2だけに追加し、旧モードの扱いは維持する。

### v2 envelopeと状態の直交化

```json
{
  "schema_version": 2,
  "command": "refs",
  "query": {"validity": "valid", "effective_name": "run"},
  "result": {"state": "found", "total": 8, "returned": 3},
  "analysis": {
    "state": "partial", "domain": "repo_sources",
    "searched_languages": ["swift"],
    "skipped_languages": [{"language": "go", "reason": "definitions_references_unsupported"}],
    "parse_error_files": ["Sources/B.swift"], "count_scope": "parsed_supported_files",
    "resolution": "syntactic"
  },
  "output": {"truncated": true, "byte_budget": 12000},
  "data": {}, "next_actions": []
}
```

- query.validity: valid / invalid / unsupported。result.state: found / no_match / ambiguous / not_evaluated。analysis.state: complete / partial / not_run / failed。matchとcoverageを1つの状態に潰さない。
- totalは解析できた対象内の正確な件数で、プログラム全体での件数ではない。未評価ならnull。意図したscope除外と、非対応言語/parse errorによるskipを分ける。
- file単位のHasErrorは、Unitsが0件でもcoverageへ伝える。parse errorの位置情報が無いadapterはfile名だけを返し、位置を捏造しない。I/O失敗はanalysis.failed、dataを確定結果として返さない。
- formatが確定した後の入力/実行エラーはv2で返す。format指定自体が不正・確定不能の場合はstdoutを空にしてstderrとrc=2を返す。生成argvはformatを先頭に置く。
- next_actionsは最大3個、各要素はkind/reason/argvのデータ。shell文字列のevalは使わない。提案のquery変更・絞り込みの有無を明示する。

| v2条件 | 通常rc | 明示mode |
|---|---|---|
| valid / found / complete | 0 | 同じ |
| valid / no_match / complete | 0 | -require-matchなら1 |
| valid / ambiguous / complete | 0（候補を返す） | -require-uniqueなら1 |
| valid / found・no_match・ambiguous / partial | 対応するcomplete行と同じ | -strict-analysisなら2（他のrequire条件より優先） |
| invalid / unsupported、対象言語を全く評価できない | 2 | 同じ |
| I/O失敗、stale selector/cursor、内部処理失敗 | 2 | 同じ |

終了コードは「入力不正・実行不能・analysis.failed→2、strictかつpartial→2、require-matchかつtotal=0→1、require-uniqueかつtotal≠1→1、その他→0」の順で決める。require-uniqueは定義を1つ選ぶdef/contextだけに提供し、0件も失敗とする。require条件はpartialでも有効だが、0件は解析できた範囲内での結果に限る。complete/partial×0/1/複数×require/strictの全組合せを検査する。

autoの混在repoでは「Swiftのhit＋Go skip」「Swift 0件＋Go skip」「いずれか＋parse error」を全て検査する。明示swiftでの正常な対象外はpartialにしない。Goだけのdef/refsはunsupportedでありno_matchにしない。Go callersは対応済みとして登録する。

### 出力の上限と回帰検査

新commandのjson-v2とtextはmax-bytesを持つ。serializerは総件数を別に数え、mandatory metadataと継続案の領域を確保してから、制限済みsnippet/itemを予算内のprefixだけencodeする。全件JSONを作って1件ずつ削除・全体再encodeする方式は採らない。診断配列にもtotal/returned/truncatedを持たせ、予算を無制限に消費させない。最終UTF-8文書のbytesを検証し、byte列の途中では切らない。legacy serializerは変更しない。mandatory metadataさえ収まらなければ短い `output_budget_too_small` エラー（rc=2）を返す。許されるbudget最小値は1,024 bytesとし、このエラー自体はその内側に収める。

legacy mode、v2通常/require/strict、flag前後、--、quotes、空白・Unicode・[]*?を含むパス、..・symlink・repo外・重複prefix、vendor/外部依存、混在言語、I/O、完全に抽出できない構文、同名別型、0件、予算不足をmatrixで固定する。共通状態型は `internal/report` か専用protocol packageに置き、検索・context・capabilitiesが別定義を持たないようにする。

## 今回の3回のセルフレビュー（2026-10-26）

各回で読み取り専用Codex subagentの反証レビューと本体の自己点検を行い、指摘を次の回の前に反映した。

1. **CLI仕様・互換性**: matchとcoverageを分離し、混在言語/parse errorの組合せとGo callers対応を修正した。
2. **agentの使いやすさ・効果測定**: partialでもrequire条件が有効になる終了コード優先順位、require-uniqueの0件判定を追加した。
3. **実装構造・運用コスト**: vendorも含む探索domain、literal pathspec、予算内のprefixだけencodeする方式へ修正した。

改善の評価: 起票時の機能案から、引数・状態・依存関係・異常系・検査手順を実装/レビュー可能な契約へ具体化した。共通仕様の正本は926、registry/評価手順は928、解析経路は924とし、重複した実装を避ける。これは設計文書の改善であり、実装・性能・開発効率の向上は未検証。受け入れ条件は未完了のままopenに残す。

## 誤った「存在しない」「使えない」を防ぐ出力条件

9d2e801fでも `refs Worker.run` は0件でrc=1、同じ入力の `refs run` は2件となる例を再現した。これは旧仕様だが、書く前の探索では誤った不存在判断から重複実装につながりうる。v2はunsupported queryと合法な再検索argvを返し、裸名への変更で型絞り込みが失われることも表示する。

また、0件と解析不完全、省略された候補と本当の0件、同名候補と選択済み定義を分ける。parse errorを含むfixtureでは、結果がある場合もcoverageの不完全さが見えることを検査する。未知の入力形式を無条件に正常な検索として受理しない。

- [929](done/929-bug-dupscan-gate-lost-file-parse-errors.md) はgate側のfile parse error通知とstrict判定の担当。本issueだけを完了してgateの解析漏れまで直ったとは扱わない。
- [930](930-feat-dupscan-candidate-call-evidence.md) は呼び出し数の照合条件・省略引数・曖昧さの担当。approx=0を未使用と表示しない。
- このscopeで保証するのは、構文検索の結果と限界を正しく伝えること。型/target解決を実装せず「別moduleから使える」「影響は他に無い」を出力しない。

## 進捗

- 2026-10-26: 起票。CLIの不一致・0件と解析不完全の区別を裏取り。非対応形式の拒否は挙動変更なので、既存契約への影響を明示してから実装する。
- 2026-10-26: Codexの読み取り専用subagentで、CLIの事実・互換性とagent導線・効果測定の2観点から反証レビュー。P1/P2/P3の指摘なし。静的レビューであり、新機能の実装・性能・agentによる利用効果を検証したものではない。
- 2026-10-26: 相対リンク・diffと、Bash 5でのissue関連テストを検査して成功。提案の本文を記録し、機能の実装・効果測定はopenの作業として残す。

- 2026-10-26: grepによる既存実装の見落とし・類似メソッドの追加を減らす目的に照らし、出力の誤解と探索成功の評価条件を追記。実装・効果測定は未着手。

- 2026-10-26: 出力の正確性について読み取り専用Codex subagentで反証レビューし、P1/P2/P3の追加指摘なし。 issue内の相対リンク・JSON/Python構文・diffとBash 5のissue関連テストが成功。929の本文内手順と930の3問い合わせをCLIで再現した。文書の改善は、出力誤認の再現・責務と、既存実装発見/不要な重複追加を測る条件を具体化したこと。機能改善の効果は未検証。

- 2026-10-26: 929でgateのbase/head file parse error通知とstrict-analysisを実装した。本issueの探索コマンド側のcoverage/共通v2は継続して未実装。
