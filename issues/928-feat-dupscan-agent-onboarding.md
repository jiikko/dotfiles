# 928 (feat): dupscan のClaude/Codex共通の導入案内・能力診断を用意する

起票日: 2026-10-26

## 目的

CLIを使える環境でもagentが存在・適切なコマンド・対応言語を知らず、普段の探索へ戻る摩擦を減らす。Claude/Codexで共通のCLI契約と短い導入案内を持ち、環境依存の導線は別々に検証する。利用増加と開発効率の改善は未測定。今回は提案のみ。

## 現状の裏取り（5a9a6a98時点）

- `_claude/skills/dupscan/SKILL.md` と `_claude/CLAUDE.md` に使い方がある。
- `_claude/settings.json` は `suggest-dupscan-for-symbol-grep.sh` を呼ぶ。hookはClaude CodeのPreToolUseのBash/Grep入力を読む。grepらしい探索に案内を返し、拒否せず、既定900秒で通知を抑える。cwdのrepoでSwiftを追跡しているかを判定する。
- [921](epic/890/pending/921-feat-dupscan-explore-def-refs-and-grep-nudge.md) の案内hookは実装・push済みで、残りは1か月後の効果測定。新規のhookが必要だと一括して扱わない。
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
- [ ] [917](epic/890/pending/917-test-dupscan-usefulness-verification.md) / 921の計測とつなぎ、両agentを別集計する。適切な呼び出し、実際の再利用、誤誘導、往復数・elapsed timeを比較し、利用回数の増加だけで採用しない。
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
- 対照群は現行のindex/def/refs/callersと現行の案内、介入群は新CLIと新guideを使う。925/927/928個別の寄与はこの一括比較から断定せず、必要なら同じ条件で1機能ずつ追加する追試を設計する。
- 評価者は群名を伏せた最終diff/回答と事前の正解を照合し、課題成功、正しい再利用、誤った再利用/不存在断定、影響箇所の見落としを判定する。最初の適切な候補までのtool往復、全tool出力bytes、wall time、取得可能なusage/費用も記録する。取得不能な値は欠測とし推定で埋めない。
- 生データ・失敗例・中央値/ばらつき・成功件数を群別/agent別に残す。正しさの悪化や重大な誤誘導があれば採用を保留する。正しさを保ち、往復/時間の差がばらつきを超えた範囲だけを改善と呼ぶ。12課題は一般化の証明ではなく、差が不明なら追加検証する。実装未完・未計測の現在は効率改善済みとしない。

## 今回の3回のセルフレビュー（2026-10-26）

各回で読み取り専用Codex subagentの反証レビューと本体の自己点検を行い、指摘を次の回の前に反映した。

1. **CLI仕様・互換性**: Go callersは対応済み、Go def/refsは非対応として能力表と案内を修正した。
2. **agentの使いやすさ・効果測定**: pilotで設定漏れ/基準率を確認し、固定12課題・fresh session・agent別集計・独立採点の評価手順を追加した。
3. **実装構造・運用コスト**: registryの依存方向とread-only doctor/no-buildを確認し、共通契約を926へ集約する境界を明確にした。

改善の評価: 起票時の機能案から、引数・状態・依存関係・異常系・検査手順を実装/レビュー可能な契約へ具体化した。共通仕様の正本は926、registry/評価手順は928、解析経路は924とし、重複した実装を避ける。これは設計文書の改善であり、実装・性能・開発効率の向上は未検証。受け入れ条件は未完了のままopenに残す。

## 進捗

- 2026-10-26: 起票。既存Claude hookと新たな共通入口を分けた。CLI・設定・agent連携の実装は未着手。
- 2026-10-26: Codexの読み取り専用subagentで、CLIの事実・互換性とagent導線・効果測定の2観点から反証レビュー。P1/P2/P3の指摘なし。静的レビューであり、新機能の実装・性能・agentによる利用効果を検証したものではない。
- 2026-10-26: 相対リンク・diffと、Bash 5でのissue関連テストを検査して成功。提案の本文を記録し、機能の実装・効果測定はopenの作業として残す。
