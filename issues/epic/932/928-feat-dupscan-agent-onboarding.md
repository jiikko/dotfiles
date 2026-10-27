# 928 (feat): dupscan のClaude/Codex共通の導入案内・能力診断を用意する

> 🚨 **担当中: air5 (glogx)**（2026-10-27〜）

起票日: 2026-10-26

## 目的

CLIを使える環境でもagentが存在・適切なコマンド・対応言語を知らず、普段の探索へ戻る摩擦を減らす。Claude/Codexで共通のCLI契約と短い導入案内を持ち、環境依存の導線は別々に検証する。利用増加と開発効率の改善は未測定。今回は提案のみ。

## 現状の裏取り（5a9a6a98時点）

- `_claude/skills/dupscan/SKILL.md` と `_claude/CLAUDE.md` に使い方がある。
- `_claude/settings.json` は `suggest-dupscan-for-symbol-grep.sh` を呼ぶ。hookはClaude CodeのPreToolUseのBash/Grep入力を読む。grepらしい探索に案内を返し、拒否せず、既定900秒で通知を抑える。cwdのrepoでSwiftを追跡しているかを判定する。
- [921](../890/pending/921-feat-dupscan-explore-def-refs-and-grep-nudge.md) の案内hookは実装・push済みで、残りは1か月後の効果測定。新規のhookが必要だと一括して扱わない。
- `main.go` のusageは人向けで、対応コマンド・問い合わせ形式・言語別対応をmachine-readableに返す専用コマンドはない。Codexの各実行環境でこのClaude用hookが使えるかは未検証で、共通に動く前提にしない。

## コマンド・導線案（未実装）

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

- [ ] Claude/Codexの各利用環境について、発見経路・利用できたコマンド・未対応の機構を記録し、動作確認できた組だけ案内する。
- [ ] CLI不存在/未build/設定なし/壊れた設定/別cwd・worktree/対応言語が混在/非git repo/SDKのシンボルを含むケースを検査する。
- [ ] capabilities・guide・実際のCLIの対応表が一致し、初回buildと探索そのものの時間を分けて測る。導入の繰り返しで指示を重複させない。
- [ ] hookの通知は少数・短文で、追加の待ち時間を実測する。全repo解析やbuildが通知だけで起きないことを確認する。
- [ ] [917](../890/pending/917-test-dupscan-usefulness-verification.md) / 921の計測とつなぎ、両agentを別集計する。適切な呼び出し、実際の再利用、誤誘導、往復数・elapsed timeを比較し、利用回数の増加だけで採用しない。
- [ ] Codex/Claudeの固有連携が使えなくても、共通CLIを手動で呼べる導入経路が残る。

## 詳細設計案（2026-10-26、未実装）

### Command/adapter registryを情報源にする

`internal/commandmeta` にCommandSpec（名前、flag定義builder、Runの登録情報、対応出力、必要な解析feature）を置き、mainのdispatch・help・capabilities・guideが共用する。packageがmainのRun関数をimportする循環は作らず、main側が登録する。登録コマンドとhelp/capabilitiesの一致をテストする。

言語の能力は `internal/langs.All` / `lang.Adapter` から取得する。既存SymbolsをDefs/Refsの能力へ対応させ、Units/Calls等はadapterの実動作に合わせて登録する。手書きの別の言語表をguideへ埋め込まない。

9369b0be時点の初期表は、Swift=Units/Calls/Defs/Refsあり、Go=Units/Callsあり・Defs/Refsなし。従ってGoのindex/report/gate/near/explain/callersは案内し、Goのdef/refsと初期contextは案内しない。925/927等が未登録なら提案段階のcommandを広告しない。

### capabilitiesとdoctor

```sh
dupscan capabilities -format json-v2
dupscan doctor -format json-v2 -repo /path/to/repo
```

- capabilitiesはrepo無しでも実行でき、tool/build情報、schema、commands/flags/出力mode、言語ごとの対応、近似・外部SDK非対応等の制約を返す。検査対象sourceの走査やgit fetchは行わない。上限は既定65,536 bytesで、超過処理は926の契約を使う。
- doctorはcommand-vで起動した実体、git root/worktree、利用できるadapter、config読取、base候補の解決結果を返す。baseは現行resolveBaseの読取だけを使い、fetchや書込みはしない。HEADとbaseが同じなら注意情報にし、gate合格とは表示しない。
- doctorのsource調査は拡張子の列挙まで。AST解析・重複検出は起動しない。`.dupscan.json` 不在はoptional configとして正常、壊れた設定は明確な診断にする。
- 各checkは `ok / info / warning / error / not_checked` とreasonを持つ。診断が完了すればrc=0、実行・設定エラーなら2。対応sourceが無いこと等は用途別のreadinessに分け、個別のwarningを「ツール全体が使えない」と一括判定しない。

### buildを起こさない入口

現在のbin/dupscanはgo_autobuild経由で起動するため、binary内のdoctorだけでは未build時の自動buildを防げない。

初期案はwrapper専用の先頭option `-no-build` を追加し、既存go_autobuild helperにread-onlyのreadiness判定を用意する。入力fingerprintの規則を別実装に写さない。current binaryならoptionを取り除いてexecし、missing/stale/失敗状態ならstdoutを空にしてrc=2と短いstderrを返す。古いbinaryで新しいcapabilityを答えたことにしない。binary不存在時はv2 envelopeを返せないwrapperエラーとして契約化する。

```sh
dupscan -no-build capabilities -format json-v2
```

通知hookはこの経路か軽量な存在確認だけを使い、失敗時は1回の案内に留める。通常の手動導入ではno-buildを指定せず、既存のbuild経路を利用できる。変更したwrapperとhelperはshell fixtureで、buildコマンドが実行されないことを直接assertする。

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
