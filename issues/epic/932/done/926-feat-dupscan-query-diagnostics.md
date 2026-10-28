# 926 (feat): dupscan の問い合わせ形式と検索0件の診断を揃える

起票日: 2026-10-26

## 完了した範囲と採否（2026-10-27）

09e983fcから再評価節の3件を実装した。`index`/`callers`も既存interspersed parserを使い、後置flagを受理する。拒否案では正常な次の書き方への往復が残り、既存parserで受理できるため採らなかった。`--`はどの位置でもそこでflag解釈を止め、値欠落flagが区切り記号を値として消費しないようにした。report/gate等の引数規則を変えるCLI全面改訂はしない。

`refs Worker.run`のように識別子で始まるqualified queryは、text/jsonともrc=2・stdout空で拒否し、型を解決できないことをstderrへ表示する。元scopeを保持した単独名refsのargv配列を最大1個提示し、実行はしない。型/ラベルの絞り込みを外すことを明示する。Unicode識別子・backtick付き名・dot operatorは一律拒否しない。型絞り込みをしていた成功検索を変える変更ではなく、literal 0件だった非対応入力の診断を明示的に変えた。その他の不明な入力やラベルだけのqueryまで網羅したvalidatorは作らない。

従来のdef/refs/index/callersは、file-level parse errorの件数と先頭3pathをstderrへ出す（pathは引用し、残りは件数）。hit/0件/抽出単位0のすべてで通知する。stdoutのtext/JSON構造と正常なrc、def/refsの0件rc=1・index/callersの0件rc=0は保つ。Goを読まない案内とcontext/evidenceの既存表示も維持する。

共通json-v2 envelope・strict/require・fuzzy候補・selectors・全コマンドbyte予算は今回不採用。3件を直すためには不要で、既存JSONを変えずstderrを読む導線で診断できる。machine利用でstderrを扱えない実例、候補提示の失敗、大出力/版混在等の具体的な不足が出たら必要な部分だけ再起票する。下の当初設計は将来案の記録であり、本issueの未完了条件ではない。両agentの実課題による再試行回数・誤不存在判断・重複追加率の評価は928で継続し、本修正の効果と断定しない。

## 方向性の再評価（2026-10-27）

対象: a5b4196e の実装と既存測定。

**判断: 方向性は妥当。包括的CLI再設計より、再現する誤診断3件を先に直す。** 現行では `callers Of -limit 1 src/tuikit/markdown` がrc=2で「-limit は git の repo の中ではない」と返る一方、flagを前へ置いた形は成功する。`refs Worker.run` は末尾名runが存在してもliteral比較で0件になる。main.goはfile-level parse error（抽出単位0も含む）を収集済みでcontext/evidenceは表示するが、従来のdef/refs/index/callersは取り込まない。Go非対応の案内など既存診断まで「無い」と扱わない。

次の最小作業は、(1) 非対応qualified refs、(2) 後置flag、(3) 解析失敗/抽出単位0の通知。正当な0件・hit・部分解析をfixtureで区別し、案内した修正argvが実行できることを確認する。後置flagは受理の統一だけでなく、位置の誤りを正しく説明して拒否する案も比較する。型を黙って取り除いてtyped検索と偽らず、Unicode/backtick/operator等の有効な名前を一律に不正扱いしない。

正常な既存出力/rcの互換を保ち、新診断の契約を先に決める。共通envelope・strict/require・fuzzy候補・selectors・全コマンド予算は条件付き拡張で、3件の修正や928のA/Bの前提ではない。下の大規模設計は候補であり、既存registry/analysisPlanを捨てて作り直す計画ではない。

## 目的

Claude/Codexがコマンドの書き方の違いで失敗したり、非対応の問い合わせを「対象が存在しない」と解釈したりする往復を減らす。正しい次の問い合わせを少数返せるCLIにする。再評価節の最小改善は実装済み。一般的な開発効率の効果測定は928で継続する。

## 現状の裏取り（5a9a6a98時点）

- `main.go` の `parseInterspersedFlags` をdef/refsは使う。index/callers等は `parseFlags` を使い、位置引数以降のflagの扱いが異なる。
- `def` は `Type.member` と引数ラベルを解釈する。`refs` は問い合わせ文字列と識別子名の一致で探すので、`refs Worker.run` は `run` の呼び出しがあっても0件（直前の使い捨てSwift repoで再現済み）。
- def/refsは0件でrc=1、index/callersは0件でもrc=0。現在のJSONには、問い合わせ非対応・本当の0件・解析不完全を区別する共通の状態欄がない。
- main.goはfile-level errorを収集する。context/evidenceは通知済みだが、従来のdef/refs/index/callersは通知へ取り込まない。0件は全構文を調べた証明にならない。

## 当初の改善案（採否は冒頭）

1. 共通flagの位置と `--` の扱いを揃える。名前・パスの前後どちらでも受け付ける案を、各コマンドの曖昧な位置引数と衝突しないか確認する。file/scope/prod/testの絞り込みも意味を明示して揃える。
2. 非対応の `Type.member` / ラベル / 言語は、単なる0件と区別し、対応する問い合わせ例を返す。型の解決なしに末尾名へ黙って置換し、絞り込み済みと誤認させない。
3. 近い名前・別のラベル・絞り込み候補を少数返す。自動で再検索・実行せず、次に使えるargvを構造化して返す。再試行ループや際限のない候補列挙を避ける。
4. JSONにschemaの版、状態、部分的な解析であること、結果総数/返却数、省略、次の問い合わせ案を持たせる。案の状態名は `no_match` / `unsupported_query` / `unsupported_language` / `partial` 等。具体的schemaは実装時に確定する。
5. 既存の正常な呼び出しの終了コードとJSONの互換性を維持する。0件を止めたい用途は共通の明示flag（例: `-require-match`）を設計し、既存defaultを一律変更しない。解析不完全を止めるmodeも、通知とrcの契約を決めてから追加する。

## 完了範囲の受け入れ条件

- [x] index/callersの前置/後置flag、重複単値flag、空白/記号scope、`--`のliteral path、欠落flag値をCLIから検査した。
- [x] qualified refsは正当な0件と区別し、型を外した近似であると説明する。修正argvを再実行し、複数scope内の参照が返りscope外が混ざらないことを検査した。
- [x] bare Unicode/backtick名の参照取得とdot operator/正当な0件の旧rcを維持する。operator参照収集の拡張はしていない。
- [x] 4コマンド×hit/no_match/zero_units×text/jsonの24ケースで、file parse error・引用path・先頭3件上限を通知する。index JSONでzero_units fixtureの抽出単位0を確認した。
- [x] 同一fixtureの変更前後24条件を比較した。完全解析16条件はstdout/stderr/rc一致、部分解析8条件はstdout/rc一致と新stderr警告を確認した。全入力の互換性を証明するものではない。
- [x] 追加回帰を14種類の退行変異で検証した（scope欠落の予備変異は全repoへの拡大へ修正して再検証）。lint/race/gateとissue検査の結果は末尾に記録する。

関連: [900](../../890/900-research-dupscan-next-languages.md) の言語ごとの対応範囲を反映する。[928](928-feat-dupscan-agent-onboarding.md) の能力情報と整合させる。gateのbase/違反/除外の判定を変更する作業は、この探索診断と切り分ける。

## 不採用とした当初の詳細設計案（2026-10-26、未実装）

### 互換性と導入順

既存 `-format text|json|xcode` の正常な出力・終了コードは維持する。machine向けの共通契約は **明示的な `-format json-v2`** に追加し、既存JSONへ共通envelopeを黙って被せない。新しいsearch/context/doctor/capabilitiesもjson-v2を受け付ける。旧モードは現行goldenを固定し、正常ケースのbyte比較で退行を検査する。

現行の情報源は928で実装したmain packageの `command_registry.go:commandSpecs`。`internal/commandmeta` は存在しない。analysisPlan、search/context、doctor/guideも実装済みなので、将来のv2はこれらへ段階的に追加し、過去の実装順を前提に作り直さない。

### 引数の共通規則

- flagは位置引数の前後で受け付ける。既存flagの型・既定値は維持し、単値flagの繰り返しは現行Go flagと同じく最後を採る。複数値の-pathは例外として全指定を保持する。`--` 以降はflagとして解釈しない。
- `-repo DIR` は探索repoを指定。未指定はcwdからgit rootを解決する。`-path DIR` はrepo内の相対directory prefixで、複数指定は和集合。実在directoryを正規化し、repo外へ出る指定・symlink経由の指定はrc=2。directory境界で一致させ、重複prefixを除く。Gitにもliteral pathspecとして渡し、[]*?等をglobとして解釈させない。shell quotingだけで済ませない。legacyの位置引数directoryと新しいrepo/path指定の併用は、曖昧なためrc=2にして直し方を示す。
- v2の `-file` はrepo相対の結果selector。scopeのscan範囲を勝手に狭めない。legacy -fileのcwd相対解決は旧モードに残す。contextの `-definition-file` は定義選択だけに使う。
- `-scope all|prod|test` は返す宣言/出現の分類を絞り、既定all。呼び出し数が別scopeで集計される場合は `calls_scope` を明記する。
- `-lang auto|swift|go` はregistryから解決。明示swiftならGoは意図的な対象外、autoで既知のGoファイルをdef/refsが読めなければskipとしてcoverageに載せる。domainは列挙したrepo sourcesであり、scope内のtracked/未ignoreのvendor sourceも含む。外部SDK・repo外の依存取得・未知の拡張子は対象外で、module/targetの所属解決をしたとは表示しない。
- v2のrefs/callersは単独名（callersはlabel指定可）。`Worker.run` を型解決済みの絞り込みとして受理せず `unsupported_query` とし、defと、絞り込みなしのrefs/callersのargv案をreason付きで出す。当初はv2だけに拒否を追加する案だったが、今回の最小改善では識別子で始まるqualified refsの拒否を旧text/jsonにも明示的に追加した。その他のv2設計は採用していない。

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

9d2e801fでも `refs Worker.run` は0件でrc=1、同じ入力の `refs run` は2件となる例を再現した。これは旧仕様だが、書く前の探索では誤った不存在判断から重複実装につながりうる。今回の最小改善は旧text/jsonで非対応queryと再検索argvを返し、裸名へ変更すると型で絞れないことも表示する。

また、0件と解析不完全、省略された候補と本当の0件、同名候補と選択済み定義を分ける。parse errorを含むfixtureでは、結果がある場合もcoverageの不完全さが見えることを検査する。未知の入力形式を無条件に正常な検索として受理しない。

- [929](929-bug-dupscan-gate-lost-file-parse-errors.md) はgate側のfile parse error通知とstrict判定の担当。本issueだけを完了してgateの解析漏れまで直ったとは扱わない。
- [930](930-feat-dupscan-candidate-call-evidence.md) は呼び出し数の照合条件・省略引数・曖昧さの担当。approx=0を未使用と表示しない。
- このscopeで保証するのは、構文検索の結果と限界を正しく伝えること。型/target解決を実装せず「別moduleから使える」「影響は他に無い」を出力しない。

## 進捗

- 2026-10-26: 起票。CLIの不一致・0件と解析不完全の区別を裏取り。非対応形式の拒否は挙動変更なので、既存契約への影響を明示してから実装する。
- 2026-10-26: Codexの読み取り専用subagentで、CLIの事実・互換性とagent導線・効果測定の2観点から反証レビュー。P1/P2/P3の指摘なし。静的レビューであり、新機能の実装・性能・agentによる利用効果を検証したものではない。
- 2026-10-26: 相対リンク・diffと、Bash 5でのissue関連テストを検査して成功。提案の本文を記録し、機能の実装・効果測定はopenの作業として残す。

- 2026-10-26: grepによる既存実装の見落とし・類似メソッドの追加を減らす目的に照らし、出力の誤解と探索成功の評価条件を追記。実装・効果測定は未着手。

- 2026-10-26: 出力の正確性について読み取り専用Codex subagentで反証レビューし、P1/P2/P3の追加指摘なし。 issue内の相対リンク・JSON/Python構文・diffとBash 5のissue関連テストが成功。929の本文内手順と930の3問い合わせをCLIで再現した。文書の改善は、出力誤認の再現・責務と、既存実装発見/不要な重複追加を測る条件を具体化したこと。機能改善の効果は未検証。

- 2026-10-26: 929でgateのbase/head file parse error通知とstrict-analysisを実装した。本issueの探索コマンド側のcoverage/共通v2は継続して未実装。

- 2026-10-27: 938でcontext/evidenceの先頭状態・grouped text・scope付きnext_actionと任意summaryを実装/局所比較した。938の完了は本issueの全受け入れ条件の完了を意味しない。v2 envelope/serializerへ共通状態とaction modelを統合する作業は継続。

- 2026-10-27: `docs(dupscan): reassess open epic 932 work against current behavior`で現実装と残設計を再照合し、上記の最小作業・着手条件へ更新。改善は誤った未実装扱いと一括実装の前提を減らしたこと。コード変更、開発効率/重複追加率の改善は今回の成果に含めない。


- 2026-10-27: `fix(dupscan): diagnose unsupported refs and incomplete exploration`で再評価節の3件を実装。既存helperをindexで探し、parseInterspersedFlags・headList・queryNextAction/writeNextActionを再利用した。後置`--`と欠落flag値の退行もCLI回帰で固定した。

### 検証と改善の評価

改善は、qualified refsを「存在しない」で返さず同scopeの実行可能な次問い合わせへ進めること、後置flagをGit pathと誤診しないこと、抽出単位0を含む解析漏れを4探索コマンドで隠さないこと。代償は明示した非対応queryのrc変更と部分解析時stderrの追加、共通parserへの境界処理と回帰テストの増加。構文解析/型解決のcoverageそのものは変わらず、stderrを捨てる利用者には解析不完全の通知が見えない。一般的な時間/コンテキスト/不要な重複追加の削減は未検証。

14種類の退行は、index/callersの旧parser、後置`--`の保持、欠落flag値の見逃し、qualified guard無効化、dot operatorまでの拒否、修正argvの全repo化、path引用/件数上限の欠落、zero-unit errorの抑制、4コマンド各々の警告配線除去。隔離worktreeで構文検査とbaselineを先に行い、すべて狙ったCLI回帰でredを確認した。予備のscope変異はscope自体を消していたため、全repoへ広げる変異に置き換えてscope外の参照混入でredになることを確認し直した。

検査結果: `make -C src/dupscan lint test`（最終差分）でlintと全race testが成功。`dupscan gate -base 09e983fc -strict-analysis src/dupscan`は新しい重複/追加除外なし。全srcのgateは既存の意図的な構文不正fixture `src/subproc/boundarycheck/testdata/broken/broken.go`をbase/headで同じ1件報告し、全srcの完全解析は主張しない。Homebrew Bashでissue完了scriptを実行し、リンク/next/採番/裸path/claim/期限の6検査が成功した。system Bash3.2では完了scriptのcommand substitution構文で停止したためHomebrew Bashを使用（コード変更なし）。親の読み取り専用レビューでP1/P2なし。
