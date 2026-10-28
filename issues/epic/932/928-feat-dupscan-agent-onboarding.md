# 928 (feat): dupscan のClaude/Codex共通の導入案内・能力診断を用意する

起票日: 2026-10-26

## 方向性の再評価（2026-10-27）

対象: a5b4196e の実装と既存測定。

**判断: 導入支援の実装は十分進んだ。次は残った環境確認と効果評価に集中する。** registry/capabilities/doctor/agent-guide、先頭 `-no-build`、Go通知からの能力照会は実装済み。flags_helpは実helpの文字列で、型付きflag schemaではない。Codex実CLIのread-only smokeは成功した。Claude実CLIの当初smokeはBash許可で止まった（末尾の今回評価ではpwd-onlyに限って成功、source編集実験は別途未承認）。未build/staleの拒否とcold buildを区別する。

次の最小作業は、既存権限の範囲でClaudeの導線を確認し、未確認なら理由を残したまま、両agentの同じ探索課題で品質と費用を測ること。grep探索/能力照会/実ソース確認を別集計し、正しい再利用、不要な重複追加、誤誘導、再試行、観測可能なcontext量を記録する。通知が増えただけで利用・効率が上がったとしない。

typed metadata/json-v2・cache・新hookを一式足すのは次の既定作業にしない。既存導線で解けない具体例が出た機能だけを926等と調整する。既存のローカル測定は一般的な効果の証明ではなく、環境差と追加照会の費用も評価へ含める。

## 目的

CLIを使える環境でもagentが存在・適切なコマンド・対応言語を知らず、普段の探索へ戻る摩擦を減らす。Claude/Codexで共通のCLI契約と短い導入案内を持ち、環境依存の導線は別々に検証する。利用増加と開発効率の改善は未測定。2026-10-27にno-build、registry/capabilities/doctor/agent-guideとGo通知接続を実装した。各環境の実動確認と開発全体の効果評価は下記の状態で継続する。

## 現状の裏取り（5a9a6a98時点）

- `_claude/skills/dupscan/SKILL.md` と `_claude/CLAUDE.md` に使い方がある。
- `_claude/settings.json` は `suggest-dupscan-for-symbol-grep.sh` を呼ぶ。hookはClaude CodeのPreToolUseのBash/Grep入力を読む。grepらしい探索に案内を返し、拒否せず、既定900秒で通知を抑える。cwdのrepoでSwiftを追跡しているかを判定する。
- [921](../890/pending/921-feat-dupscan-explore-def-refs-and-grep-nudge.md) の案内hookは実装・push済みで、残りは1か月後の効果測定。新規のhookが必要だと一括して扱わない。
- `main.go` のusageは人向けで、対応コマンド・問い合わせ形式・言語別対応をmachine-readableに返す専用コマンドはない。Codexの各実行環境でこのClaude用hookが使えるかは未検証で、共通に動く前提にしない。

## 実装した共通CLI（2026-10-27）

```sh
dupscan capabilities -format json
dupscan doctor
dupscan agent-guide -target claude -print
dupscan agent-guide -target codex -print
```

### 共通のCLI契約

- capabilitiesは実装済みの言語×コマンド、問い合わせ形式、schema/版、近似・制約を短く返す。900でGoが加わっても、Goのdef/refsまで使えると誤案内しない（Go callersは9369b0beで対応済み）。実際のdispatchと同じ情報源を使い、手書きの対応表を別に増やさない。
- doctorはPATH、repoの認識、検査対象の言語、設定の読取、gateのbase候補等を診断する。設定ファイルが無くても動く現行契約を保ち、「`.dupscan.json` がない」だけで使えない扱いにしない。
- agent-guideは「既存helper探し→index/search」「定義→def」「参照→refs/callers」「実装後→gate」の短い例とfallbackを、その版が対応する範囲で出す。新機能の925〜927が無い版でも、使える入口を案内する。
- 既定では案内/差分を表示する。AGENTS.md / CLAUDE.md / skillや設定を書き換える導入処理を設ける場合は明示opt-inとし、既存指示との重複・競合を扱う。

### Agent別の接続

- Claudeは既存921のhookを活用し、無差別に別hookを重ねない。Codexはまず通常のshell経路とrepository instructionsでCLIを発見・実行できるか検証する。hook等の固有機構を使うなら、その環境の仕様と実動作を確認して別のadapterにする。
- セッション開始やtool呼び出しのたびに全repoを走査しない。初回buildも通常の対話的な導入経路で扱い、軽い通知hookから自動buildを起こさない。wrapperが初回buildする現行仕様を踏まえ、未buildを軽く診断する方法を設計する。
- baseは信頼するCLI引数/upstream等から決める現行の契約を保ち、生成した設定で `base=HEAD` を固定してgateを弱めない。
- 通知の回数・長さ・失敗時の挙動を設け、未対応言語やSDKの検索、文字列/設定の検索を誤って誘導しない。repoのソース/docは解析対象データとして扱い、命令やshell文字列として自動実行しない。

## 検証・受け入れ条件

- [x] CodexのGo編集1対でshell/guideの導線と実diffを記録し、Claudeはpwd-only smokeの実行まで確認する。（今回。CLIのsource編集・Swift課題と区別）
- [ ] Claudeのsource編集、両agentのSwift課題など、未検証の接続を既存権限/明示承認の範囲で確認する。動作確認できた組だけ成功として記録する。
- [ ] CLI不存在/未build/設定なし/壊れた設定/別cwd・worktree/対応言語が混在/非git repo/SDKのシンボルを含むケースを検査する。
- [ ] capabilities・guide・実際のCLIの対応表が一致し、初回buildと探索そのものの時間を分けて測る。導入の繰り返しで指示を重複させない。
- [x] hookの通知は少数・短文で、追加の待ち時間を実測する。全repo解析やbuildが通知だけで起きないことを確認する。
- [ ] [917](../890/pending/917-test-dupscan-usefulness-verification.md) / 921の計測とつなぎ、両agentを別集計する。適切な呼び出し、実際の再利用、誤誘導、往復数・elapsed timeを比較し、利用回数の増加だけで採用しない。
- [x] 共通CLIを手動で呼べる導入経路を確認する。両targetのguide6条件とfixtureの生成argv実行を確認した。固有AI連携の成功と同一視しない。

## 詳細設計案（2026-10-26）と実装差分

以下の当初案に対し、現行実装はtext/jsonの能力表・診断と表示専用guideを提供する。json-v2と型付きflag metadata/予算serializerは具体的な不足を確認した場合の未採用候補で、926と調整する。ここでは作ったことにしない。実装済み範囲と差分は末尾の記録を正本とする。

### Command/adapter registryを情報源にする

当初のinternal/commandmeta分離案に対し、実装はmain package内のcommandSpecへRun/対応出力/問い合わせ形/説明/SymbolsOnlyを登録した。dispatch・capabilities・guideが同じ一覧を使い、import循環を作らない。flags_helpは同じRunの-h結果を得て実flag定義と同期する。型付きflag schemaは、機械処理上の不足を確認して採用する場合に926と設計する。共通usageにも新3コマンドを載せ、正常な旧分析出力の互換性とは分けて検証する。

言語の能力は `internal/langs.All` / `lang.Adapter` から取得する。既存SymbolsをDefs/Refsの能力へ対応させ、Units/Calls等はadapterの実動作に合わせて登録する。手書きの別の言語表をguideへ埋め込まない。

9369b0be時点の初期表は、Swift=Units/Calls/Defs/Refsあり、Go=Units/Callsあり・Defs/Refsなし。従ってGoのindex/report/gate/near/explain/callersは案内し、Goのdef/refsと初期contextは案内しない。925/927等が未登録なら提案段階のcommandを広告しない。

### capabilitiesとdoctor

```sh
dupscan capabilities -format json
dupscan doctor -format json -repo /path/to/repo
```

- capabilitiesはrepo無しでも実行でき、tool/build情報、schema、commands/flags/出力mode、言語ごとの対応、近似・外部SDK非対応等の制約を返す。検査対象sourceの走査やgit fetchは行わない。現在は上限65,536 bytesで、超過時はrc=2・stdout空とする。926の分析envelope/予算serializerは未統合。
- doctorはcommand-vで起動した実体、git root/worktree、利用できるadapter、config読取、base候補の解決結果を返す。baseは現行resolveBaseの読取だけを使い、fetchや書込みはしない。HEADとbaseが同じなら注意情報にし、gate合格とは表示しない。
- doctorのsource調査は拡張子の列挙まで。AST解析・重複検出は起動しない。`.dupscan.json` 不在はoptional configとして正常、壊れた設定は明確な診断にする。
- 各checkは `ok / info / warning / error / not_checked` とreasonを持つ。診断が完了すればrc=0、実行・設定エラーなら2。対応sourceが無いこと等は用途別のreadinessに分け、個別のwarningを「ツール全体が使えない」と一括判定しない。

### buildを起こさない入口（2026-10-27実装済み）

現在のbin/dupscanはgo_autobuild経由で起動するため、binary内のdoctorだけでは未build時の自動buildを防げない。

wrapper専用の先頭option `-no-build` を追加した。既存go_autobuild helperのfingerprint/stale判定を共用し、規則を写さない。指紋が取得できてビルド記録と一致すればoptionを取り除いてexecし、missing/stale/指紋取得不能ならstdoutを空にしてrc=2と短いstderrを返す。当該入力のbuild失敗で古いbinaryだけがある場合もstaleとして拒否し、再試行やbackoff記録を更新しない。無関係な過去の失敗記録が残っているだけならcurrent binaryは拒否しない。binary不存在時はv2 envelopeを返せないwrapperエラー。古いbinaryで新しいcapabilityを答えたことにしない。

```sh
dupscan -no-build capabilities -format json
```

capabilitiesのno-build起動も実装済み。Go向けの既存通知hookは同梱wrapperのこの経路を使い、失敗時は1回の手動確認案内に留める。Swiftの通常案内は921の観測を維持する。通常の手動導入ではno-buildを指定せず、既存のbuild経路を利用できる。shell fixtureではbuild/SDK/git起動がないこと、未readyではbinaryも起動しないこと、projectファイルの内容/metadataを変更しないことを直接assertする。

### guideとagent別導入

`agent-guide -target claude|codex -print` は対応capabilityと固定templateから、利用場面→argv、scope/近似の説明、文字列検索等のfallbackを最大4,096 bytesで返す。初期版は表示だけにし、自動でinstruction/設定を書き換える機能は追加しない。ユーザーが内容を確認し、repository instructionsへ転記する入口にする。

Claudeは既存921の案内を共用する。Codexはshell+repository instructionsでの導入を先に検証し、未確認の固有hook機構をあるものとして実装しない。実装時に利用環境の版・公式仕様と実動作を確認し、確認できた接続だけをtemplateへ加える。

通知の初期上限は既存と同じsession当たり15分に1回、最大1,024 bytes。SDK名・文字列・設定探索・非対応言語には具体的なcommandを勧めない。通知が失敗しても元の探索を拒否せず、loopして再実行しない。追加のログ収集や外部送信は初期実装に含めず、効果検証は明示的に用意した評価データで行う。

### 最小実装の順と検査

1. registryとrepo不要capabilities、legacy dispatch/helpのgoldenを固定する。
2. wrapper no-buildをshellで検査し、未build/stale/異常config/別worktreeのmatrixを作る。
3. doctorの各checkをfake repoとread-only gitで検査する。実user設定を書き換えない。
4. guideの出力をlanguage/command能力別に固定し、Claude/Codexで実際に実行できるargvだけを示す。
5. agent効率のA/Bは以下の共通評価手順に従う。CLIが起動した回数だけを採用基準にしない。

### 925〜928の共通評価手順

実装時の評価計画であり、今回はagent実験を実行しない。917/921の保留条件を変更しない。

- 925の順位評価はCLI単体で先行する。候補がある/無い、型filter不一致、別target、同名等の正解を人がコードから確定し、queryと期待候補を固定する。top-5到達率と誤候補の根拠を保存する。agentが候補を使っただけで正解にしない。
- agent作業評価はClaude/Codexを別集計する。まず各agent4課題で実行可能性を確認する。ユーザー設定・skills・hookが対照群へ漏れない起動方法、ログの取得項目、対照群の失敗や余分な探索が観測できる難度を確認できなければ本番へ進まない。小さなrepoで基準率が0なら条件を修正し、効果ありとは報告しない。
- 本番用はpilotと重ならない12課題（helperあり、helperなし、同名/別target、影響範囲調査を各3）を、固定commit・設定・期待結果とともに事前確定する。各agent×各課題で旧導線/新導線の2群を独立したfresh sessionで実行し、順序を均等に振り分ける。同一agent内でmodel/版、prompt、tool権限、時間/出力上限、使える入力を固定する。群間で会話・作業diffを引き継がない。
- 主比較は通常のrg/grep/Glob/Readの導線に対してdupscanを加えた導線。既存index/def/refs/callersと現行の案内に対して新CLI/guideを比べる比較は、既存dupscanからの増分を見る別実験にする。925/927/928個別の寄与は一括比較から断定せず、必要なら同じ条件で1機能ずつ追加する追試を設計する。
- 評価者は群名を伏せた最終diff/回答と事前の正解を照合し、課題成功、正しい再利用、誤った再利用/不存在断定、影響箇所の見落としを判定する。最初の適切な候補までのtool往復、全tool出力bytes、wall time、取得可能なusage/費用も記録する。取得不能な値は欠測とし推定で埋めない。
- 生データ・失敗例・中央値/ばらつき・成功件数を群別/agent別に残す。正しさの悪化や重大な誤誘導があれば採用を保留する。正しさを保ち、往復/時間の差がばらつきを超えた範囲だけを改善と呼ぶ。12課題は一般化の証明ではなく、差が不明なら追加検証する。実装未完・未計測の現在は効率改善済みとしない。

## 今回の3回のセルフレビュー（2026-10-26）

各回で読み取り専用Codex subagentの反証レビューと本体の自己点検を行い、指摘を次の回の前に反映した。

1. **CLI仕様・互換性**: Go callersは対応済み、Go def/refsは非対応として能力表と案内を修正した。
2. **agentの使いやすさ・効果測定**: pilotで設定漏れ/基準率を確認し、固定12課題・fresh session・agent別集計・独立採点の評価手順を追加した。
3. **実装構造・運用コスト**: registryの依存方向とread-only doctor/no-buildを確認し、共通契約を926へ集約する境界を明確にした。

改善の評価: 起票時の機能案から、引数・状態・依存関係・異常系・検査手順を実装/レビュー可能な契約へ具体化した。共通仕様の正本は926、registry/評価手順は928、解析経路は924とし、重複した実装を避ける。これは設計文書の改善であり、実装・性能・開発効率の向上は未検証。受け入れ条件は未完了のままopenに残す。

## 開発上の見落としを減らすための比較と案内

ユーザーが実際に困っているのは、grepで既存実装を見落として似たメソッドを増やしていること（2026-10-26確認）。この観測を、小規模fixtureでgrepが成功した917の試走で否定しない。試走の設定漏れ・基準率0は、その実験で効果を測れなかった理由であり、大きなrepoでも問題が無いという証拠ではない。

### 比較課題と採否の補足

- 対照群はrg/grep/Glob/Readを通常どおり適切に使えることを保証する。新導線はそれにdupscanを追加する。既存index/def/refs/callersだけとの比較は、925/927等の個別改善を見る別の比較として区別する。対照群をわざと弱くしない。
- pilot/評価用に、可能なら実際の重複追加の事例から追加前commitと依頼内容を復元する。課題文へ答えの関数名を漏らさず、名前の違い・所在の違い・doc欠落等の見落とし理由を記録する。正解は既存実装と依存境界を読んで決め、過去に似ていたというだけで同一責務としない。
- 一度確定した本番課題で対照群が全件成功した場合は、その結果をそのまま報告する。差を出すために本番データを差し替えない。必要な次の評価は別datasetとして事前固定する。成功するまで課題を選び直す評価にしない。
- 主指標は「再利用すべき既存実装がある課題での不要な重複追加率」「正しい再利用率」。既存なし/別責務では正しく新設できることと誤統合率を同時に評価する。所要時間は検索1回でなく候補の発見→確認→実装/レビューまで、bytesは全tool出力で数える。呼び出し回数や単純検索の速度だけで優劣を決めない。
- 総時間/費用の増加と正しさの改善が両方ある場合はtrade-offをそのまま報告し、「効率改善」を自動的に断定しない。採用できる用途と残る失敗条件を明記する。

### 書く前と書いた後の案内

「用途/入出力は分かるが名前は不明→indexの型検索/925のsearch→候補の定義と利用例を確認→同じ責務なら再利用、違えば分ける→実装後にgate/nearで名前を変えた写しを検査」をguideの主導線とする。新機能未実装時は現行で実行可能な入口だけを案内する。

全grepを禁止せず、文字列・設定・未対応言語では継続利用する。grepで0件だった時やhelperを書き始める時にdupscanの型/用途検索が役立つことを説明する。軽量hookの契約を保ち、通知だけで全repo解析やbuildを始めない。[929](done/929-bug-dupscan-gate-lost-file-parse-errors.md)の未修正中はgate成功を解析完了の証明と案内しない。

## コンテキスト消費と重複追加を同時に減らす評価

2026-10-26のユーザー確認: 狙う効果は「agentのコンテキストウィンドウ消費を減らす」と「不要な重複メソッド定義を減らす」の両方。候補を絞って必要な定義/使用例だけ確認できれば達成の可能性はあるが、現状は未実証。少ない出力で見落としを増やす方式は成功としない。

- 同じ固定課題で、通常のrg/grep/Glob/Readに対してdupscanを追加する比較を維持する。課題終了までの全tool検索・Read・再問い合わせを数え、dupscan単体の短い出力だけで評価しない。
- 構文解析の除外・候補省略で読まなかった正解がないか確認し、正しい候補の到達/課題成功を維持した上で、正しい再利用率と不要な重複追加率、別責務の誤統合率を同時に採点する。
- 計測層を区別する: (a)toolのraw stdout/stderr bytes、(b)recorder/tool転送後のbytes、(c)観測できる場合のmodel入力側のtool/source text token数、(d)各turnの入力context token数とその最大値。bytesや費用をcontext token数へ固定倍率で換算しない。取得できない層は欠測とする。
- 各turnの入力context長の最大値と、全turnを通した入力tokenの累積を別に示す。入力側だけ観測できる場合は「最大入力context」と表記し、生成中・出力を含むwindow全体の最大占有は、その範囲を実測できた場合だけ報告する。会話の再送・共通instructions・compaction/要約・出力制限・cacheの扱いを環境ごとに記録し、累積量の減少だけでwindow最大占有が減ったとは言わない。provider固有usageの定義は実装時の一次仕様と実動作で確認する。
- 初期instructions/agent/model/版/権限/予算/compaction方針を同一agent内で揃え、対照群への設定漏れをpilotで検査する。[931](../../done/931-refactor-tuikit-domain-duplicates-and-discovery-check.md)のrecorder転送量は、モデルが実際に見た全bytesの証明ではなかった点を踏まえる。
- 比較表には課題成功・正しい再利用・不要な重複追加・誤統合・context量・総時間/費用を併記する。一方だけ改善した場合はtrade-offを残す。両効果を観測できた課題/agent/条件の範囲だけを報告し、未実装featureや他repoに一般化しない。

追加受け入れ条件: 上記の計測層と欠測を区別した報告形式を用意し、同じ品質を維持しながらコンテキスト消費と不要な重複追加を減らせたか判定する。12課題の既存案だけで一般的な非劣性や有効性が証明できるとは扱わない。

## 進捗

- 2026-10-26: 起票。既存Claude hookと新たな共通入口を分けた。CLI・設定・agent連携の実装は未着手。
- 2026-10-26: Codexの読み取り専用subagentで、CLIの事実・互換性とagent導線・効果測定の2観点から反証レビュー。P1/P2/P3の指摘なし。静的レビューであり、新機能の実装・性能・agentによる利用効果を検証したものではない。
- 2026-10-26: 相対リンク・diffと、Bash 5でのissue関連テストを検査して成功。提案の本文を記録し、機能の実装・効果測定はopenの作業として残す。

- 2026-10-26: grepによる既存実装の見落とし・類似メソッドの追加を減らす目的に照らし、出力の誤解と探索成功の評価条件を追記。実装・効果測定は未着手。

- 2026-10-26: 既存実装発見・比較評価について読み取り専用Codex subagentで反証レビュー。925の「候補の適切性をCLIが判断するように読める」P3を訂正し、候補抽出とagentによる採用判断を分けた。 issue内の相対リンク・JSON/Python構文・diffとBash 5のissue関連テストが成功。929の本文内手順と930の3問い合わせをCLIで再現した。文書の改善は、出力誤認の再現・責務と、既存実装発見/不要な重複追加を測る条件を具体化したこと。機能改善の効果は未検証。

- 2026-10-26: [931](../../done/931-refactor-tuikit-domain-duplicates-and-discovery-check.md)の検証: 931で現行dupscan併用とgrep中心を同一入力/固定出力予算で各1 agent比較したが、共通化候補を確定する効率の向上は確認できなかった。予備runの予算超過も記録し、hard capで再確認した。提案feature実装後の再利用/重複追加率の検証は引き続き未着手。

- 2026-10-26: context消費と重複追加の同時評価を追加し、最大入力contextとwindow全体の占有を区別。 読み取り専用Codex subagentで指摘の網羅性とcontext計測を反証レビューし、P3（現行/設計、着手条件、計測範囲）の3点を訂正した。文書の改善は、不足した指摘の担当と検証条件を明確にしたこと。実装・両効果の実証は未着手。

- 2026-10-26: [935](done/935-test-doctor-duplicate-discovery-effect.md)でdoctor探索を各方式2回比較した。Dは各run1組、探索recorder転送bytesは平均54,783対28,116で併用が48.7%少なかった。ただし候補は異なり、品質非劣性・model context token/window占有・新規重複追加率は未実証。本issueの実装課題A/Bを置き換えない。

- 2026-10-27: 938でcontext/evidenceの先頭状態・grouped text・scope付きnext_actionと任意summaryを実装/局所比較した。938の完了は本issueの全受け入れ条件の完了を意味しない。出力bytes減少を記録したが、実課題の総tokens/重複追加/時間の評価とregistry/capabilitiesは継続。

## no-build実装時点の境界（2026-10-27前半）

この節の「未実装」は前半時点の履歴。後半で進めた機能の現状は末尾の記録を参照する。

- `bin/dupscan` は先頭の `-no-build` だけを共通helperの `--no-build` へ渡す。残る引数の空文字/空白/記号/改行/`--`と終了コードはそのまま保つ。通常wrapperの起動方法は維持する。
- helperはasync/sync/build処理へ入る前にstrictなreadinessを判定し、既存のmtime fallbackへは降りない。入力指紋は既存のパス・mtime・サイズであり、内容hashやtoolchain/SDK/Cのassetまで検証する契約には広げない。no-build自身はASTやgitを起動しないが、exec後のcommandの解析を禁止するoptionではない。
- 独立した新規issueの作成は自動承認レビューで拒否されたため、既存928の一項目として対応した。registry/capabilities/doctor/guide、hook側の対応表の置換、Codex固有の接続、両agentの効果評価は未実装のまま。このissue全体はopenに残す。
- `feat(dupscan): add an explicit no-build wrapper path`でwrapper/helperと回帰を記録し、`docs(dupscan): record no-build contract and verification`で検証・残作業を更新する。

改善の評価: 軽い問い合わせでも未build/staleなら自動buildが走る従来の起動しかなかったところへ、最新指紋に合う成果物だけを実行し、準備不足ではbuildや旧版execをしない選択肢を追加した。通知hookへ接続するための前提が一つ整った。追加複雑さはwrapperの先頭optionと共通helperのearly guardで、入力集合の二重実装はない。実際の通知/agentの探索効率の向上は未検証。

### 今回の検証結果

- 専用回帰24結果項目（--pkgやasyncは各項目内で複数状態を検査）: current/missing/non-executable/stale/ビルド記録欠落/空指紋、ソース追加・削除・編集、go.mod/go.sum、相対replace先、test-only編集、失敗記録、async/syncとTTL、--pkgの独立成果物、別worktree、先頭以外のflagとargv/rc保持。projectの内容/metadataと外部コマンドmarkerで副作用を検査した。
- 変異2本: 変更前wrapper/helperではargv保持のassertが失敗し、空指紋guardだけを削除すると本来rc=2のケースがrc=0となって失敗した。期待したassertで検出できた。
- `make test-dir DIR=tests/bin/test_go_autobuild.sh` が成功し、既存のbuild・lock・失敗回復・--pkg/async・SDKの扱いを維持した。
- 実バイナリを非gitの `/private/tmp` から `bin/dupscan -no-build -help` で15回実行。rc/stdout/stderrは直接binary実行とすべて一致し、binary/.autobuild成果物の内容・mtime・サイズは変化なし。所要中央値11.72ms（10.57〜13.03ms、1環境の観測値）。速度改善やhookの実運用効果を証明する比較ではない。
- 実dupscanの `-no-build gate -base 2a6c8691 src` も実行した。Goの変更単位は0、新しい除外なし。既存の意図的な不正Go fixtureをbase/head各1個として解析不完全を通知する（このgateはshellの重複を検査しない）。
- subagentでテスト検知力と実装/API・文書の反証を分けて確認。--pkgと先頭以外のflagの回帰を補い、fingerprint対象の「ローカルreplace」を「相対パスのreplace」へ正確にした。

- 2026-10-27: `docs(dupscan): record no-build contract and verification`で実装済みの境界・検証・残作業を記録。今回の部分対応を終えてclaimを解除する。928全体はopenで継続する。

- 2026-10-27: `fix(dupscan): preserve static wrapper discovery for warmup checks`で、flag配列を静的なwrapper列挙がsrc_dirと誤読する回帰を修正。明示if/elseへ変更し、専用24結果項目と全17ツールのwarmupが成功した。専用検査の結果項目数を当初の26という報告から24へ訂正した。

### 広域検査の結果と既存環境の制約

`make test-changed` のshell構文/ShellCheck/zsh構文/test-lint-tests/zshrcとbin/claudeの適用検査を最後まで実行した。途中のwarmupはwrapperのflag配列を静的に読めず失敗したため、本対応内で修正し、専用検査と全17ツールのwarmup再実行で成功を確認した。README/issueの適用検査も成功（dupscanのGo全テストとissue整合検査を含む）。

広域検査全体は緑ではない。残る `test_dangling_symlinks.sh` は変更前からある `~/.config/nvim/coc-settings.json -> ~/dotfiles/_coc-settings.json` のリンク先欠落で、単独再実行でも再現する。このテストはautobuild helperを呼ばず、今回変更していない。ユーザーの設定は変更していない。`test_tmux_pane_state_bell.sh` はTERM=dumbでattachできず失敗し、TERM=xterm-256colorで再実行して成功した。`test_claude_links_complete.sh` はworktreeではskipされた。これらをno-build専用検証の成功と混同しない。

## 残機能の実装（2026-10-27）

- registryをdispatchの正本にし、capabilities/guideもそこを参照する。mainをimportする循環は作らず、GoのSymbols=falseからdef/refs/context非対応を求める。実flagは同じRunの-hだけを呼んだflags_helpとして取得する（flags_status=help_text）。型付きflag schemaではなく、別表の写経やAST/config/gitの実行はない。capabilitiesはtext/json・schema_version=1・65,536bytesまで。
- doctorはread-only git列挙と既存config/base解決を共用し、sourceを構文解析しない。gitのPATH解決もsubprocのcommand構築だけで行い、os/execの新しい起動口を作らない。エラーとwarning/not_checkedを分け、base解決だけでgate合格とはしない。reasonをUTF-8境界で4,096bytes、全体65,536bytesに制限する。
- guideはclaude/codex・all/swift/goの表示例を4,096bytes以内で返す。Goにはcontext/def/refsを案内しない。TYPE/TERM/SYMBOLはplaceholderで、実データに置換して使う。初期設計の自動書換えは追加せず、-printが必要。実装後のgateの例はbase=HEADを固定しない。
- Go通知だけを同梱wrapperの先頭-no-build能力問い合わせへ接続した。PATH上の旧wrapperへ渡すと古いwrapperがbuildしかねないため、同じcheckoutのcompanionを指定する。未準備/不正JSON/非対応なら具体例を出さず手動確認へ戻す。Swiftの通常メッセージ/stampは921の測定前に変更せず、全通知に1,024bytes上限を適用する。

### 検証と実測

- module lint/race全テストとshell構文/ShellCheck/テストlintを実行。capabilitiesは空PATH・壊れたconfig・parser trapでgit/ASTを起動しないことを確認。doctorは壊れたSwift/Go、config不在/不正、unborn/上流無し/shallow、別cwd/worktreeとread-only git許可リストでrepo/.gitの内容/metadata非変更を確認した。
- ガイドは2agent×3言語の6表示について、4つのargvを実際に既知helperのfixtureへ実行した（gateだけはfixtureのbaselineを明示する-base HEADを追加。表示するguide自体には固定しない）。hookは89 passed / 0 failed。strictなargc/各argv、no-build未準備・不正/非対応の能力情報、長いsymbolの予算、通知間隔と従来のSwift/Go振分けを検査する。
- 変異: 診断error→rc2の伝播無効化、能力表のサイズguard除去、実help取得の無効化、guideの言語条件逆転、hookのargvを1文字列へ潰す変更、hookの予算guard除去を、それぞれ想定assertで検出した。最初のguide変異では未対応contextがregistryで除外されて3例になることを観測し、検知したassertを正確に記録した。
- 既存出力比較は同じ924の固定snapshotで、def/refs/Swift callers/Go callers/indexのtext/json、report/gateのtext/json/xcodeを計16条件。rc/stdout/stderrが変更前と一致。共通helpには新しい3コマンドを追記した（正常な分析出力とは分ける）。

### 実CLIの導線・所要と残作業

以下はbuild準備済みのno-build起動を非git cwdから各5回実行した観測値。初回/cold buildを含む比較ではなく、速くなったという主張もしない。

| コマンド | stdout bytes | 中央値ms | 範囲ms |
|---|---:|---:|---:|
| capabilities | 10,372 | 12.36 | 11.83〜13.96 |
| doctor | 1,175 | 74.81 | 72.39〜77.05 |
| agent-guide / claude | 1,167 | 11.91 | 11.45〜13.06 |
| agent-guide / codex | 1,204 | 12.49 | 11.43〜12.79 |

- 実hookは追跡済みSwift+Go各1ファイルの同一fixture・各5回、900秒制限を無効にして比較した。準備済みGoは86.25→107.54ms（約21.29ms増）、Swiftは87.25→88.66ms。通常のメッセージは前後でbyte一致（Go268bytes / Swift417bytes）。追加コストと、古い/不正な能力を具体的なcommandとして広告しない利点とのtrade-off。大きなrepoやcold filesystemへの一般化はしない。
- Codex CLI 0.164.0のread-only/ephemeral環境で、capabilitiesとcodex/swiftのguideを実行し、両方rc=0・Swift context対応・index -returnsの導線を確認した。Claude Code 2.1.311のprint/dontAsk・Bash許可を指定したsmokeは、Bash権限拒否の応答となり成功を確認できなかった。制限を緩めて回避していない。この環境でのClaude実動は未確認として残す。CLI fixtureが通ることをClaude固有連携の成功とは扱わない。
- 通常のterminal共通CLI経路と、Codex固有hookを仮定しない表示は維持した。設定への自動追記や重複したhook登録は追加しない。

改善の再評価: 起動できるか/何が対応するかをソースのhelpや手書きの言語表から推測するしかなかった状態から、実dispatch/flag定義を読んだ能力表、ASTを起こさない診断、実行可能な短い例へ進めた。Go通知でbuildを避けつつ実能力を確かめられる。追加したregistry/診断/guide/境界テストと、通知1回あたりの約21msが複雑さ/費用。型解決・外部SDK/target解決を追加したものではなく、正しい再利用・総context/tokens・不要な重複追加の改善はまだ示していない。

残作業は(1)Claude側のBash許可を満たす環境での実動、(2)初回build/準備済み探索を分けた代表環境の検査、(3)917/921の条件を保った両agentの実課題A/B（再利用・誤誘導・重複追加・総context/時間/費用）、(4)typed flags/json-v2/共通serializerは具体的不足が出た場合の未採用候補（現行版の完了条件ではない）。実装が揃っただけで全受け入れ条件を満たしたことにはせず、928はopenで継続する。

- 2026-10-27後半: `feat(dupscan): diagnose local readiness without running analysis` / `feat(dupscan): derive discovery from executable command dispatch` / `feat(dupscan): expose actual command flags through discovery`を統合。`feat(dupscan): print bounded guides for Claude and Codex`と`feat(hooks): validate Go nudges against no-build capabilities`で残る導線を実装し、`docs(dupscan): record discovery readiness and onboarding evidence`で検証と残作業を記録する。

- 今後の評価ではcapabilities/doctor/guideや通知内部の自動問い合わせを、候補探索・再利用の実行回数と別集計する。今回の実装範囲を終えclaimを解除し、環境/実課題評価の未完了をopenで追跡する。

- 最終のstrict module gateはbase eb2d4bd7から変わった11単位で新しい重複なし、追加除外なし。全srcの通常gateは既存の意図的不正fixtureをbase/head各1件として解析不完全を通知する。issue整合テストも成功した。

- 2026-10-27: 925のtext searchをregistry/guideへ登録。guideは用途語と型で候補を探す例へ更新した。初期の「未実装searchを広告しない」は当時の状態で、現在は登録済みtext版を案内する。効果評価は未完了。共通v2統合も未実装だが、今回の再評価では条件付き候補とする。

- 2026-10-27: `docs(dupscan): reassess open epic 932 work against current behavior`で現実装と残設計を再照合し、上記の最小作業・着手条件へ更新。改善は誤った未実装扱いと一括実装の前提を減らしたこと。コード変更、開発効率/重複追加率の改善は今回の成果に含めない。

- 2026-10-27: `docs(dupscan): correct evidence terminology and conditional acceptance`で反証レビューP2を訂正。typed flags/json-v2を必須の残作業としていた記述を条件付き候補へ変更。環境確認と実課題評価は引き続き未完了。

## 現行版の効果評価と実編集pilot（2026-10-27）

対象09e983fc、toolは同commitの`go build`で固定し、cold buildを比較から除いた。旧doctorの2正例は使わない。CLI用の9課題は人がソースと責務を読んで正解を決め、UTC 12:35:50にquery/期待/Read範囲を凍結した（manifest SHA256 `393bff306da82aa763e19c4f993883ab01534cd18dd0abb5aa4fc7efce1cd82b`）。実装者が選んだ新規の探索的セットで、未使用の負例は含むが、第三者が保管したblind datasetではない。結果を見てqueryや正解を入れ替えていない。9課題はS1〜S4検索、E1〜E3補助証拠、C1〜C2影響調査で、各issueに全数と失敗を記録した。

CLIは同一scopeで5回ずつ、固定seed27で各方式の順を混ぜて実行。stdout/stderrのraw bytes、行数、rc、wall time中央値と範囲を収集した。Goは09e983fcのuserdirs/safefs、SwiftはObaket 386e287652125961d045fe565d0e18fe41495d89のCore source/testsをgit archiveした計330ファイル。元repoの書込みはない。比較は適切な語でのrgと必要範囲Read、同じ型filterのindex、search、context/個別query、evidence。異なる情報量を等価なcontext費用とはしない。rgの行順が変わるrunがあったため、その初期表示順位での勝敗は採点しない。

局所結果: searchは2正例で正解rank1だったがrgも成功し、aliasではfalse negative、source/doc後半では誤候補や取りこぼしが残った。evidenceはdoc/sourceの2正例へ到達したがrgより出力/時間が増えた。contextはC1で3問い合わせを1回へ集約した一方、C2は同名appendの461近似に目的テストが埋もれた。C2のqualified rg0件は不適切な対照と判断し除外、事後の型名rg→Read回復probeとして別集計した。この修正を事前固定の本番成功には数えない。

### Codex実編集の1対

UTC 12:37:46にtask/期待を別途凍結。09e983fcのuserdirs.go/userdirs_test.go/go.mod/go.sumだけを同一内容の別disposable git repoへ複製した。課題は「appごとのtheme.json pathを返し、absolute XDGとhome fallbackを守り、作成/移行をせず、AES-GCM helperがこのmodule内にあるかも答える」。既存関数名を課題文に与えず、正解は既存ConfigDirまたはConfigFileへの委譲と、暗号helperはこのmoduleに無いという限定回答。XDG/home選択規則の写しを新設した場合は不要な重複と採点する。

Codex CLI 0.164.0、各群fresh/ephemeral、workspace-write、project_doc_max_bytes=0。同じ基本promptで、基準群はrg/必要箇所Readのみ、併用群は最初にagent-guideを実行し任意でsearch/evidenceを使える条件。network/skills/別repo/subagentを禁止した。実行ログでは基準群のdupscan呼出しは0、併用群はguideを1回だけ呼び、search/evidenceを使わなかった。利用可能性を与えたことと、検索機能の効果を測れたことを区別する。

| 観測 | rg/Read群 | guide併用群 |
|---|---:|---:|
| 実diffの既存ルール再利用 | ConfigDirへ委譲1件 | ConfigDirへ委譲1件 |
| XDG/home規則の不要な写し | 0件 | 0件 |
| AES-GCMの回答 | module内に無い、と限定 | module内に無い、と限定 |
| 完了command events / raw tool出力bytes | 6 / 11582 | 7 / 16262 |
| CLI集計input tokens / cached input / output tokens | 147363 / 127488 / 1994 | 110442 / 98688 / 1820 |

両diffを人が確認し、新しいThemeConfigPathの実装はコメント以外同じで、ConfigDirから得たdirへtheme.jsonをjoinする9行。基準群は新規theme_test.go、併用群は既存testへの追記を作った。各agent内のoffline test実行後に、評価側でも両方の`go test ./...`を実行した。これは生成物の実行確認であり、テスト検知力の独立した証明ではない。製品の実装差分としてはcommitしない。

基準群は最初のGo build cache書込みで拒否され、writable cacheで再試行した。併用群は1回で通った。実行順は基準→併用の1対でcounterbalanceしていない。さらに両群とも小さなsource/testの全文を自発的にcatした。raw tool出力は併用群が4680bytes多いが、usageの集計input tokensは少ない。反復入力/cacheや再試行を含む別の観測量なので、この差からcontext window占有やtoken節約を断定しない。Codex実行の正確なwall timeは記録せず欠測（CLI9課題のwall timeとは別）。ツール転送後/モデル入力内source tokens/最大context window/課金額も取得していない。

結論: 独立したbefore/after diffで正しい再利用と不要な規則の写し0件を両群に観測できたが、基準群も解ける天井効果だった。search/context/evidenceを使っていないpilotなので、925/927/934の個別寄与や一般的な再利用率改善には使わない。本番12課題へ自動的に進めず、今回の結果を残した上で、別の実際の見落とし事例から必要最小の新pilotを事前固定する。

### Claudeと追加probeの権限制約

Claude Code 2.1.311で`-p --permission-mode dontAsk --tools Bash --allowedTools 'Bash(pwd)' --output-format json`を実行し、実cwdの一致を確認した。rc=0、2turn、duration_ms=10802、permission_denials=[]。これはsourceを読む・編集する実験ではない。以前のBash拒否という履歴は今回のpwd-only条件では再現せず、導線のread-only smokeに限って成功へ更新する。

続くuserdirs実編集pilotは実行前の自動承認レビューで拒否された。private source/task contextを外部Claude serviceへ送ることについてtrusted userの明示承認が無い、という理由である。private Swift sourceを読むCodex追加probeも同じ理由で実行前拒否された。危険bypass、設定変更、間接実行、拒否後の再試行はしない。必要な承認対象は、該当固定source/testsと課題文を各外部agentへ送って評価すること。制限をCLI不具合やagent失敗率には数えず、Claude編集効果とSwift課題のagent挙動を未検証とする。

親からの依頼後に追加したdefault引数probeは、凍結9課題と分ける。最初のMetadataDiff.initは全label付き実call1件とindex~1が一致し、省略引数の反例にはならなかった。次にRetryPolicy.swift:130のretryingはindex~0でも、裸名callersはproduction3/test12の15件を返す。署名の6引数からdefault引数が省略される実sourceを確認した。~0はunusedの証明にならない。この値をagentがどう解釈するかの追加実験は上記レビュー拒否で未実行。matcher拡張の効果が示されたとも扱わない。

### 決着と残作業

- 共通CLI/guideの導線とCodexの実編集1対を観測した。単なる起動回数より先へ進んだが、同等品質・天井効果・検索未利用という結果なので、効率改善の採用判定には不足する。
- JSON/typed flags/共通serializer/cache/cursorは、この評価から必要性を示せず現段階では不採用。parse error/近似の具体的な問題は既存926/930の担当へ渡し、包括設計を新たな前提にしない。
- 928はopenを維持する。次は実際に既存実装を見落とした履歴から新しい1課題を固定し、十分なrg対照と、検索を実利用したagentのdiffを比較する。外部へのsource開示が必要な実験は明示承認を得た範囲だけで行う。今回の小規模成功を消して課題を成功するまで選び直すことはしない。

生ログ/manifest/全コマンド/patchはこの実行の`dotfiles/tmp/dupscan-eval-20261027/`に保存。上記に結論・全数・却下理由を移し、再実行の前提は固定commit、scope、queryとここに記した。scratch script自体を恒久的な実行手順にはしない。

改善の再評価: 強いrg/Read対照、実編集の1対、成功だけでない誤誘導/取りこぼし/再試行費用を得た。導入案内があるだけで再利用やcontextを改善したという推測を退け、追加機能の一括実装を不採用にできた。コード・一般的なtoken削減/再利用率の改善を実証したという結論ではない。

- 2026-10-28: `docs(dupscan): evaluate current discovery with fixed tasks and edit pilot`で9課題とCodex実編集1対を評価。issue相対リンク/番号/旧path/next目印とbannerの検査は成功、dupscan gateは変更単位0・新重複/除外なし（既存不正fixtureを通知）。4issueの実証済み条件を分けて更新し、未実証の効果を残した。

## 今回追加したローカル導線検証（2026-10-28）

外部モデルは呼ばず、18673a39と925修正を含む専用worktreeで共通wrapper/CLIを直接実行した。GOPROXY/GOSUMDBはoff。新しい外部ソース送信への許可は未受領なので、ClaudeのGo実編集・CodexのSwift課題の再試行は行っていない。

- binary不在で `-no-build capabilities -format json`: rc2・stdout0bytes。通常起動ではローカルbuild込み1.192秒で準備できた。**compiler cacheはwarm**であり、Go cache全消去のcold buildや初回dependency取得の測定ではない。
- 準備後の `-no-build capabilities` は別cwdでもrc0/11849bytes、通常起動とbyte一致、約20ms（各単発）。出力を実JSONとしてparseした。
- claude/codex × go/swift/allのguide6条件はrc0、1221〜1357bytesで4096上限内、約12〜14ms（各単発）。これは表示用CLIの成功であり、各AIが内容を使ったという結果ではない。既存のTestAgentGuideOnlyAdvertisesExecutableRegisteredCommandsでも両templateの4argvをfixtureで実行する。
- config不在はrc0、壊れたconfigはrc2、stale markerでno-buildはrc2/stdout0、実際の非repo `/private/tmp` はrc2。生成済みmarkerは元へ戻し、検証用configは削除した。初回の非repo条件をdotfiles/tmp配下に置いた試行は親git repoに認識されrc0になったため棄却し、非repo成功例とは数えなかった。

rawはdotfiles/tmp/remaining-932-20261028/readiness.logとreadiness-followup.json。案内の手動実行経路を補強したが、CLI不存在/SDK検索/全混在matrixやcold-cacheを全達成とはしない。既存のdoctor/capabilities/guideテストと区別して残す。数値は非統制の単発観測で、latency改善を主張しない。

925の無益な再検索、927の型名回復、934の原文確認は個別の局所検証として記録する。これらをagentの正しい再利用・誤採用減少・不要な重複追加/実context削減の達成に置き換えない。本issueは実agentの比較が未実証のためopenを維持する。source開示を伴う追加評価は、対象を示した明示許可が得られた範囲だけ実行する。

- 2026-10-28: `docs(dupscan): record local onboarding checks and pending agent evaluation`で共通CLIの実起動6guide/準備状態/別cwd/設定/非repoを検証し、元の弱い非repo試行と未実施項目を区別した。コードの追加や新しい外部agent実験は行っていない。

- 2026-10-28: ローカル導線の追加記録をread-only subagentで反証し、追加指摘なし。AIによる実利用と効率の評価はopenのまま。

- 2026-10-28: 925/927/934統合後も既存C2で実argvを連鎖実行した。context 9998bytes→型名evidence 2263bytes→その先頭read_source 1571bytes/47行に5appendを含むことをローカルで確認。これは型解決や影響範囲網羅、agentの正しい選択を保証しない。rawはtmp/remaining-932-20261028/integrated-recovery.json。
