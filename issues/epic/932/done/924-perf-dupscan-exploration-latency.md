# 924 (perf): dupscan の探索コマンドの待ち時間を減らす

起票日: 2026-10-26

## 概要

Swiftの実装中に繰り返し使う `index` / `def` / `refs` の待ち時間を減らす。obaketでの反復計測では、1問い合わせの中央値は1.56〜2.48秒だった。現行は探索でも全ファイルの解析・全単位の正規化を通るため、処理を省ける余地がある。

**2026-10-27に実装・計測済み**。探索時の比較用本体変換と正規化を省き、固定した大規模Swift入力の代表4 queryで中央値が約27〜46%短縮した。小規模の速度改善は未確定。旧baseline/案は下に履歴として保持し、最新の検証結果は末尾に記録する。

## 現状の裏取り

dotfiles `e4489100` 時点のSwift実装を確認した。

- `src/dupscan/main.go` の `scanSourceOptions` はファイル一覧・全内容を読み、`parseAll` を呼ぶ。`parseAll` は各単位に `norm.Normalize` を行い、L1〜L3の正規化結果を作る。
- `index_cmd.go` は `-file` をqueryに設定した後も、元のscope全体を `scanSource` へ渡す。ファイルでの絞り込みは `report.BuildIndex` で行う。
- `def_cmd.go` / `refs_cmd.go` も `scanSourceOptions` を経由する。探索結果は主に単位のmetadataを使い、重複用のハッシュを出力しない。
- 解析結果のキャッシュは持たず、問い合わせのたびに読み直す。
- `index -file` にもrepo内の呼び出しを数えた `calls_approx` がある。対象ファイルだけを読む変更ではこの値が変わるため、単純に走査範囲を狭めればよいとは言えない。

関連: [900](../../890/900-research-dupscan-next-languages.md) ではGo追加に向けてadapter選択・コマンド層を変更中。着手時に取り込み、共通の走査経路を確認し直す。900の言語追加・Go較正と、本issueのSwift探索latencyを混同しない。

## 現状の計測（2026-10-26）

- macOS / arm64、ビルド済みCLIを固定して使用。コンパイル時間は含めない。
- CLIのSHA-256: `b106423de3aa7dc9713195050b5f0628abc4596e0dbbbf055daeca6eb67f5c20`。
- 入力repo: obaket `7e140e9abf261b7c6c89b0f25388b0f1073e9b92`。計測前後とも `git status --porcelain` は空。
- 各queryで最初の1回をウォームアップとして除き、続く5回のwall timeを測った。query間の順序は表の順に固定。CPU負荷・OSのファイルキャッシュは制御していないため、この数字だけで他の版との性能差は判断しない。

| query（共通して `-format json`） | 5回の秒数 | 中央値 | min〜max | matches（全回同じ） |
|---|---|---|---|---|
| `def awaitTaskValue` | 2.2389 / 2.2457 / 2.2293 / 2.2629 / 2.2203 | 2.2389 | 2.2203〜2.2629 | 2 |
| `index -file shared/Sources/ObaketPresentation/Transfer/TransferRetryCoordinator.swift` | 1.4576 / 1.5578 / 1.5878 / 1.4837 / 1.5779 | 1.5578 | 1.4576〜1.5878 | 40 |
| `refs TransferRetryCoordinator` | 2.2804 / 2.3381 / 2.2940 / 2.3846 / 2.4985 | 2.3381 | 2.2804〜2.4985 | 28 |
| `def TransferRetryCoordinator.canRetry` | 2.4709 / 2.4831 / 2.9536 / 2.5221 / 2.3918 | 2.4831 | 2.3918〜2.9536 | 1 |

前回の各1回の観測（1.4〜2.2秒）とは条件を揃えたA/Bではなく、退行を示す比較には使わない。

## 改善候補と進める順

1. **内訳を測る**。列挙・読取・parse・正規化・呼び出し集計・整形について、wall timeとCPU profile等で時間を使う箇所を特定する。並列workerのCPU時間や段階の合計を、コマンド全体のwall timeと混同しない。
2. **探索で不要な重複用正規化を省く**。metadataの抽出と重複比較用ハッシュ生成を分ける案を、現在の結果と同じ出力で比較する。型のoutlineや呼び出しの集計に必要な単位・metadataは残す。
3. **既知ファイルのoutlineを軽くする**。まず既存の呼び出し数を保てる範囲で処理を省く。全repoの呼び出し数を省略する高速モードを作るなら明示的な選択にし、既定の `calls_approx` の意味を変えない。追加モードの必要性は測定後に判断する。
4. **同一repoの連続問い合わせをまとめる案**。上の単純化だけでは待ちが残る場合、1走査で複数queryを処理するbatchや、内容hashに基づく解析結果の再利用を比較する。永続cacheや常駐プロセスを最初から前提にしない。

cacheを採る場合は、未コミット編集・untracked追加・削除・改名・worktree分離・解析器の版・設定変更で古い結果を返さない条件と検査を設ける。キャッシュしない版の出力との一致、保持上限、並列実行、破損時の再解析、調査対象repoへ書き込まない契約も確認する。

## 再計測の入口

dotfilesの比較したい版でCLIを別々にbuildし、同じobaketの固定worktreeを入力にする。下の例では固定済みの入力repoと実行ファイルを引数で渡す。既存の一時スクリプトには依存しない。

```sh
go -C src/dupscan build -o /private/tmp/dupscan-perf-before .
python3 - /path/to/fixed-obaket /private/tmp/dupscan-perf-before <<'PY'
import json, statistics, subprocess, sys, time
repo, exe = sys.argv[1:]
queries = [
    ['def', '-format', 'json', 'awaitTaskValue'],
    ['index', '-format', 'json', '-file',
     'shared/Sources/ObaketPresentation/Transfer/TransferRetryCoordinator.swift'],
    ['refs', '-format', 'json', 'TransferRetryCoordinator'],
    ['def', '-format', 'json', 'TransferRetryCoordinator.canRetry'],
]
for query in queries:
    times, expected = [], None
    for run in range(6):
        start = time.perf_counter()
        result = subprocess.run([exe, *query], cwd=repo, capture_output=True,
                                text=True, check=True)
        elapsed = time.perf_counter() - start
        value = json.loads(result.stdout)
        if expected is None:
            expected = value
        assert value == expected, query
        if run:
            times.append(elapsed)
    print(json.dumps({'query': query, 'seconds': times,
                      'median': statistics.median(times),
                      'matches': expected['matches']}, ensure_ascii=False))
PY
```

before / afterの採否を決める際は、入力・設定・ビルド条件を固定し、複数roundで実行順を交互にして同じqueryごとの中央値・ばらつきを比較する。stdoutのJSON全体・text・終了コードも比較する。batch / cache案なら、同じ連続問い合わせを両方式で通して全体のwall timeも別に測る。

## 受け入れ条件

- [x] 900取り込み後の現状を再確認し、代表queryと小規模repoでもbaselineを取る。
- [x] プロファイルを根拠に候補を選び、同じworkloadのbefore / afterを記録する。
- [x] 観測したばらつきを超える待ち時間の短縮を確認する。差が判定できなければ回数・roundを増やし、改善済みとは扱わない。他の代表queryの退行も評価する。
- [x] 既存モードの結果順・metadata・近似呼び出し数・終了コード・探索範囲を維持する。変更・追加・削除・改名・別worktree・読めない構文の扱いを回帰で確認する。
- [x] cache等を採る場合は保持・無効化・破損・並列実行を検査し、単純化だけの案より採る利益を示す。
- [x] `make -C src/dupscan lint test` が成功し、追加した複雑さと実測した改善を最終diffで再評価する。

## 起票時の詳細設計案（2026-10-26）

### 共通の解析経路

9369b0beでGo adapterと `internal/lang` / `internal/langs` が追加された。以下はその構造を前提にし、先のSwift計測値は旧版baselineとして保持する。Go版や最新版が同じ秒数だとは扱わない。

`repo.Source → ReadSet → AnalysisPlan → AnalysisView → 各report builder` と分ける。最初はprocess内の1回の実行で共有し、永続cache・常駐化は入れない。

- `ReadSet`: canonical repo root、scope、列挙したファイル、読み込んだbytes、設定bytes、内容hash。読取失敗は結果を黙って欠落させずエラーとして返す。
- `AnalysisPlan`: `Calls / Defs / Refs / Normalize` を指定。`Units` は現行の囲む単位・outlineのため常に抽出する。言語ごとの対応判定は928のregistryを使う。
- `AnalysisView`: Unitsと任意のCalls/Definitions/Types/References、file単位のparse error/skip理由を持つ。正規化結果は別のoptionalなfieldに持つ。
- gate/report/near/explainは `Normalize=true`。index/def/refs/callersと新しいsearch/contextはfalse。正規化を要求するconsumerが結果なしで呼ばれたら内部エラーにし、ゼロhashで比較しない。
- gateの出どころ推定はL3も使うため、既定の表示段がL1/L2でもL3計算を削らない。本変更は探索の不要な正規化の分離に限定する。
- adapterのparserはworkerごとに作り、言語ごとに再利用して終了時にCloseする現行の所有関係を維持する。ReadSetとViewはコマンド終了で解放する。

| consumer | Calls | Defs | Refs | Normalize |
|---|---|---|---|---|
| index | yes | no | no | no |
| def / 型outline | yes | yes | no | no |
| refs | no | no | yes | no |
| callers | yes | no | no | no |
| report / gate / near / explain | no | no | no | yes |
| search初期版 | no | no | no | no |
| context初期版 | yes | yes | yes | no |

### ファイル指定と互換性

indexの `-file` は結果の絞り込みであり、既存 `calls_approx` は走査scope内の呼び出しを数える。この意味を保つ間は、他ファイルのCallsも収集する。まず正規化の分離を測り、単一ファイルだけのscanに置き換えない。呼び出し数を省くoutline modeは、必要性を測ってから別の明示modeとして設計する。

### 計測・採用手順

1. 最新Go対応版を基点に、Swift/Go/混在、小規模/obaket規模の固定入力を用意する。CLI buildと入力commit/dirty状態、設定、OS/CPU、実行環境を記録する。
2. 現行と候補を同じbuild条件で別binaryにし、queryごとにwarm-up後の反復を交互の順序で測る。中央値・min/max・全生データを残し、CPU profileとpeak RSSも別に測る。
3. legacy text/JSON、終了コード、件数、順序、近似数を同一入力で比較する。parse error、untracked、削除、改名、symlink、別worktreeも含める。
4. 正規化の分離だけで差が出るかを先に判断する。差が測定のばらつき内なら高速化済みとしない。次のcache/batchはその結果を見て提案を更新する。

927のcontextはこのReadSet/Viewを1回構築して利用する。cacheを追加する場合も独立の二重cacheを作らず、本issueで無効化条件・上限・回復を評価する。

## 今回の3回のセルフレビュー（2026-10-26）

各回で読み取り専用Codex subagentの反証レビューと本体の自己点検を行い、指摘を次の回の前に反映した。

1. **CLI仕様・互換性**: 共通契約との整合を確認し、Go追加後のbaseline取り直し・L3維持・calls_approx維持を詳細化した。
2. **agentの使いやすさ・効果測定**: contextとの共有ReadSetを確認し、出力上限と入力/ASTメモリ量を分けて評価する方針を保持した。
3. **実装構造・運用コスト**: parserの所有関係・正規化の任意化・永続cacheを先送りする境界を静的に確認した。

改善の評価: 起票時の機能案から、引数・状態・依存関係・異常系・検査手順を実装/レビュー可能な契約へ具体化した。共通仕様の正本は926、registry/評価手順は928、解析経路は924とし、重複した実装を避ける。これは設計文書の改善であり、実装・性能・開発効率の向上は未検証。受け入れ条件は未完了のままopenに残す。

## 追加観測と探索の目的（2026-10-26）

目的はgrepで見落としていた既存実装へ到達し、似たメソッドの新設を減らすこと。速度は繰り返し使うための条件だが、名前を既に知っている検索の速度だけで探索の有効性を決めない。検索から実装/レビューまでの比較は[928](../928-feat-dupscan-agent-onboarding.md)に集約する。

9d2e801fのbuild済みbinaryとripgrep 15.2.0で追加観測した。入力obaketは `4eefca2724484a57d7f8df2551b2fb8a0e90985f`、計測前後のSwiftファイル集合/内容のSHA-256は `1143d229d1d08a58837363ee4c0e1d1676cf939fcb870ec167f7307bc10ca0b6` で同じ。binaryのSHA-256は `87cec7d3d2bc0e2b0ccb645fe46de5b93a11dbed99eaaf85971f92973f2aee73`。初回1回を除いた5回、4queryの順序をroundごとに反転、compile時間は含めない。CPU負荷/OS cacheは制御していない。rgは並列出力の順序が変わりうるので行のmultiset、dupscanはstdout全体が反復中に同じことを確認した。

| query | 5回のwall time（秒） | 中央値（秒） | stdout bytes |
|---|---|---|---|
| `dupscan def -format json awaitTaskValue` | 2.1526 / 2.2055 / 2.1140 / 2.1917 / 2.1970 | 2.1917 | 1772 |
| `rg -n -F -g '*.swift' awaitTaskValue .` | 0.0234 / 0.0238 / 0.0490 / 0.0249 / 0.0241 | 0.0241 | 75818 |
| `dupscan refs -format json TransferRetryCoordinator` | 2.0644 / 2.1013 / 2.1278 / 2.1749 / 2.1813 | 2.1278 | 16378 |
| `rg -n -F -g '*.swift' TransferRetryCoordinator .` | 0.0211 / 0.0231 / 0.0232 / 0.0229 / 0.0222 | 0.0229 | 5450 |

補足: `rg -n -g '*.swift' 'func\s+awaitTaskValue\b' .` は別のwarm-up後5回で0.021052 / 0.020782 / 0.022418 / 0.021445 / 0.021081秒、中央値0.021081秒、206 bytesだった。定義行だけを返すのでdupscan defの本体付き出力とは同じ情報ではない。

この比較は既知の名前について構文検索と文字列検索を比べたもので、未知の名前を発見する課題でも、同じ情報を得るまでのend-to-end比較でもない。約2秒対約0.02秒という待ち時間の差は観測できる一方、grepによる既存実装の見落としを否定する結果ではない。defは少ないbytesに本体をまとめるが、refsのJSONはrgより大きく、常に出力量を減らすとも断定できない。

実装後は、正規化の分離が同一出力で待ちを減らすかに加え、候補確認の総tool往復/bytes/時間がどう変わったかを測る。単純grep相当の速度への到達を既存実装発見の成功条件に置き換えない。現在は高速化・見落とし削減とも未実装/未検証。

## 進捗

- 2026-10-26: 起票。実装は未着手。5回ずつのbaselineと、ソース上の候補を記録した。現状の改善は「次の実装時に再現・比較できる条件を残したこと」であり、高速化の効果は未実測。
- 2026-10-26: Codexの読み取り専用subagentで反証レビューを実施。探索の正規化経路、`calls_approx`、計測と仮説の区別、900との境界、再計測スクリプトについて静的な指摘なし。レビュアーによるbuild・benchmark再実行は行っていない。
- 2026-10-26: 本文内Pythonの構文・相対リンク・diffを検査。Bash 5環境の `make test-changed PATHS="issues/924-perf-dupscan-exploration-latency.md"` でissue関連テストが成功（標準Bash 3.2では既存issue_doneスクリプトの構文で停止するため、repo方針のBash 5で再実行）。

- 2026-10-26: 既知名のrg比較を追加記録し、単純な検索速度と既存実装の発見・重複削減の評価を区別した。性能改善は未実装。

- 2026-10-26: 既存実装発見・比較評価について読み取り専用Codex subagentで反証レビュー。925の「候補の適切性をCLIが判断するように読める」P3を訂正し、候補抽出とagentによる採用判断を分けた。 issue内の相対リンク・JSON/Python構文・diffとBash 5のissue関連テストが成功。929の本文内手順と930の3問い合わせをCLIで再現した。文書の改善は、出力誤認の再現・責務と、既存実装発見/不要な重複追加を測る条件を具体化したこと。機能改善の効果は未検証。

## 実装と採否の根拠（2026-10-27）

`perf(dupscan): skip clone body conversion in discovery scans`で実装。起点は4fe84707（claimのみで、実装baselineはe297872a）。正規化だけを省いた予備版ではprofile benchmarkが2.0786→2.0745秒/回で、改善を判断できなかった。CPU profileでNormalizeの累積は1.66秒/42.31秒=3.92%、Swift本体convは10.94秒/42.31秒=25.86%だったため、不要な比較用本体への変換も省く方針を選んだ。これらは並列workerを含むCPU sampleの累積で、wall timeの内訳や足し算で推計した速度差ではない。

- `analysisPlan`でCalls/Defs/RefsとNormalizeを明示。探索のindex/def/refs/callers/context/evidenceはUnitのmetadataを保ち、clone用Body/DelegationとL1〜L3正規化を作らない。
- Go/Swift adapterに内部のMetadataOnlyを追加。言語の構文木自体は全体をparseし、closure/owner/位置/doc/型/署名/解析エラーを従来どおり抽出する。機能を切り替える新CLI flagは追加しない。
- report/gate/near/explainは完全な本体と全L1〜L3を引き続き作る。cloneEntries guardが、metadata-only結果を誤って「重複なし」に使う経路を拒否する。
- 読取内容は既存Source/contents、解析結果はscannedへ保持し、ReadSet/Viewのためだけの別層は追加しなかった。ファイル列挙・scope・config・workerごとのparser所有とCloseは維持する。
- index -fileもscope全体のCallsを数える。対象fileだけへの走査縮小、近似数の省略、cache、batch、常駐化は採らなかった。呼び出し数やindexの順位の意味は変えない。cacheの受け入れ条件は採用しないため対象外。

## 同条件A/Bの入力・手順

- macOS / arm64 / Apple M5、Go1.26.0。build済みbinaryを使いcompile時間を除外。CPU負荷・OS cacheは制御していない。最初の測定は他のテスト/profile処理との重なりもあるため、ばらつきが大きかったqueryは後から追加計測した。
- Swift入力: obaket 4eefca2724484a57d7f8df2551b2fb8a0e90985fのtracked Swiftを一度だけcopyした固定repo。1669 files、相対path順でpath UTF-8+NUL+各内容SHA256の32-byte digestを連結してhashし、`1143d229d1d08a58837363ee4c0e1d1676cf939fcb870ec167f7307bc10ca0b6`。元repoは変更していない。設定.dupscan.jsonもcopy。snapshot commit `596eccdf9151bffcdc2bfa4b6473d59b6cd7bb2b`。
- Go入力: doctorの64 Go filesを固定copy。path/content hashは同手順で`a26af4e66ad0bbe4e34440dcbdbc2f2bfc1552b7a64be4993c734d61ff1258d7`（copy先からの相対path）。小規模混在入力はSwift/Go各1fileで、スクリプトに全文を持つ。
- before binary SHA256: `65aa504552c8bb9775f3d01724471105817a5893b2eacfbaf2764626faf70827`。
- after binary SHA256: `bce01754c2a4b799cc4dec676f67a44266bf42f918f1a384e352695549a35448`。
- 各query/variantのwarm-up1回を除き、2round×3回。round/反復ごとにquery順とbefore/after順を反転。全runでrc/stdout/stderrのbyte一致を検査した。
- 最初の反復で範囲が重なったoutlineと小規模混在2 queryは、warm-up後10回を交互順で追加。Go単独queryもwarm-up後10回。測定回数を増やす際も、小規模に改善があると仮定しなかった。

### 最初の2round（中央値秒、括弧内min〜max）

| query | before | after | 中央値の短縮率 |
|---|---:|---:|---:|
| swift-def | 2.390704 (2.270136〜2.707225) | 1.289073 (1.210850〜1.581668) | 46.1% |
| swift-outline | 1.477559 (1.424849〜1.878650) | 1.083661 (0.987843〜1.676553) | 26.7% |
| swift-refs | 2.499106 (2.258511〜2.801805) | 1.556939 (1.418490〜1.693187) | 37.7% |
| swift-member | 2.601285 (2.217429〜3.063561) | 1.431660 (1.163115〜1.542043) | 45.0% |
| small-go-index | 0.020591 (0.016635〜0.027256) | 0.018707 (0.015775〜0.021676) | 9.1% |
| small-swift-def | 0.023822 (0.020236〜0.027306) | 0.025144 (0.020141〜0.027557) | -5.5% |

queryは順に、`def -format json awaitTaskValue`、`index -format json -file shared/Sources/ObaketPresentation/Transfer/TransferRetryCoordinator.swift`、`refs -format json TransferRetryCoordinator`、`def -format json TransferRetryCoordinator.canRetry`、小規模混在の`index -format json -name total`、同`def -format json Worker.run`。大規模3 queryでは全測定範囲が分離。outlineはこの時点では範囲が重なり、追加測定で判断した。

### 追加測定（各10回、中央値秒）

| 入力/query | before | after | 評価 |
|---|---:|---:|---|
| swift-outline | 1.396515 | 0.968007 | 範囲が分離し短縮を再確認 |
| small-go-index | 0.015632 | 0.015674 | 範囲が重なり改善/退行とも未確定 |
| small-swift-def | 0.019227 | 0.019553 | 範囲が重なり改善/退行とも未確定 |
| Go単独doctor index -name run | 0.026299 | 0.023500 | 中央値は約11%短いが範囲が重なり、確定的な改善とはしない |

小規模Swift queryの中央値は初回23.82→25.14 ms、追加19.23→19.55 msでわずかに増えたが、前後の分布範囲は重なる。Go混在queryも追加では15.63→15.67 ms。小規模での高速化や、微小な退行が無いことを証明したとは扱わない。大規模Swiftについては各代表queryの追加/初回の範囲が分離し、待ち時間の短縮を確認できた。

### CPU・割当・peak RSS（latency反復とは別の観測）

- 同じdef queryをprofile用benchmarkで3回通し、CPU sample累積42.31→23.05秒、profile wall 6.37→3.45秒。alloc_space sample累積6173.71→2288.57 MB。alloc_spaceはテストprocess全体のsampling累計で、1queryの保持量やpeak RSSではない。
- 同じdefのCLIを `/usr/bin/time -l` で交互順に各3回実行。peak RSS(bytes)はbefore [867401728, 874856448, 860356608]、after [346226688, 345145344, 362643456]。中央値867,401,728→346,226,688 bytes（約60%少ない）。同機・同入力の観測であり一般化しない。

## 互換性・回帰・改善の再評価

- 混在fixtureの初期/編集/追加/改名/削除/symlink/構文エラー/設定変更/別rootで、index/outline/def/refs/callers/context/evidenceとreport/gate/near/explainの178条件を比較し、rc/stdout/stderrがbyte一致した。Goのみ・Swift大規模の全反復でも一致。
- 別担当がstatic・入れ子関数・computed property・accessor・observer・init・extensionを含む15条件を追加で比較し、一致した。ソースの読み取り専用反証レビューに修正必須の指摘なし。
- 混在parserで通常/metadataのUnit metadata・Calls/Definitions/Types/References・ファイルエラーが一致し、metadata Body/clone entriesは無いことをテスト。clone側はL1〜L3 hashを持つ。
- outlineが別fileの呼び出し数を維持し、未追跡fileの追加・編集・改名・削除・別rootの値を反映する回帰を追加。永続状態を追加せず、毎回の最新ソースを読む。
- `make -C src/dupscan lint test`が成功（lint 0 issues、race全package）。dupscan対象のstrict gateは新規違反/解析エラーなし。除外追加なし。
- 増えた複雑さは内部plan/adapter modeとclone consumer guard。キャッシュ等を増やさず、出力を保ったまま大規模Swiftの待ち・memoryを減らせたため採用した。grepでの既存実装発見率、不要な重複追加率、実context token削減の評価は928で継続する。

<details>
<summary>反復の全値（秒）</summary>

| 測定 | before | after |
|---|---|---|
| 初回 swift-def | 2.270136 / 2.275581 / 2.339935 / 2.676764 / 2.707225 / 2.441474 | 1.311171 / 1.210850 / 1.266975 / 1.559436 / 1.581668 / 1.244348 |
| 初回 swift-outline | 1.424849 / 1.443277 / 1.438380 / 1.878650 / 1.851179 / 1.511841 | 1.006277 / 0.987843 / 1.030227 / 1.676553 / 1.191010 / 1.137096 |
| 初回 swift-refs | 2.258511 / 2.291784 / 2.500680 / 2.801805 / 2.615528 / 2.497532 | 1.418490 / 1.449979 / 1.521536 / 1.592342 / 1.693187 / 1.677614 |
| 初回 swift-member | 2.229354 / 2.217429 / 3.063561 / 2.577593 / 2.738167 / 2.624977 | 1.189575 / 1.163115 / 1.542043 / 1.466913 / 1.435952 / 1.427369 |
| 初回 small-go-index | 0.020812 / 0.016635 / 0.027256 / 0.020453 / 0.020728 / 0.018346 | 0.018253 / 0.015775 / 0.021676 / 0.021577 / 0.019161 / 0.018054 |
| 初回 small-swift-def | 0.021852 / 0.020236 / 0.027306 / 0.025324 / 0.022572 / 0.025072 | 0.020141 / 0.020480 / 0.025534 / 0.027557 / 0.024754 / 0.025841 |
| 追加 swift-outline | 1.356233 / 1.360390 / 1.356707 / 1.392763 / 1.437676 / 1.435220 / 1.400266 / 1.388665 / 1.449824 / 1.434608 | 0.893623 / 0.931026 / 0.959331 / 0.982838 / 0.950995 / 1.007537 / 0.946351 / 0.993014 / 0.976683 / 1.000097 |
| 追加 small-go-index | 0.017170 / 0.016053 / 0.015852 / 0.015414 / 0.015068 / 0.015808 / 0.015456 / 0.015179 / 0.015884 / 0.015325 | 0.017655 / 0.015475 / 0.016066 / 0.015874 / 0.015694 / 0.015655 / 0.015281 / 0.014702 / 0.015172 / 0.016056 |
| 追加 small-swift-def | 0.019098 / 0.019600 / 0.018444 / 0.019890 / 0.019355 / 0.017925 / 0.018946 / 0.018559 / 0.021166 / 0.021777 | 0.019550 / 0.018096 / 0.019556 / 0.018719 / 0.019531 / 0.019726 / 0.017950 / 0.022540 / 0.022085 / 0.020261 |
| 追加 Go doctor | 0.028463 / 0.027011 / 0.025822 / 0.025868 / 0.027437 / 0.025362 / 0.026172 / 0.027315 / 0.025982 / 0.026425 | 0.024455 / 0.023744 / 0.023341 / 0.025948 / 0.023272 / 0.024571 / 0.022728 / 0.023398 / 0.023601 / 0.023203 |

</details>

再実行のローカル原本は `/private/tmp/924-measure.py` / `924-followup-measure.py` / `924-compatibility.py`、結果JSONは `924-measure-results.json` / `924-measure-ledger.json` / `924-followup-results.json` / `924-go-measure.json` / `924-compatibility-results.json`。CPU/mem profileとtop、必須検証logも同prefixに保存。一時ファイルの存続を保証しないため、条件・全latency値・互換性の全数勘定は本issueへ転記した。

- 2026-10-27: `perf(dupscan): skip clone body conversion in discovery scans`で実装と検証を記録。cache/batchを導入する前に、探索だけで不要だった処理を省く案を採用。既存のCLI契約を保ち、実測上の改善と小規模の未確定範囲を分けて評価した。

- 2026-10-27: 一時コピーで本体変換復活・metadataの正規化復活・全scope呼出し数の欠落・metadata結果のclone consumerへの誤投入・clone正規化の欠落、の5変異を入れ、回帰テストがすべて期待値違反として検出した。計測担当も72件のledger、入力hash、profile/RSSを反証レビューし修正必須の指摘なし。

- 2026-10-27: 最終反証レビューのP3で、別git rootの確認をlinked worktreeの確認と混同しないよう指摘を受けた。実際に `git worktree add --detach` したfixtureで初期/未追跡追加後の22条件を追加比較し、rc/stdout/stderrの一致を確認した（原本 `/private/tmp/924-linked-worktree-check.py` / `924-linked-worktree-results.json`）。通常fixture178条件と合わせ200条件。src全体の通常gateは既知の意図的なbroken.go fixtureにより解析不完全で、新規違反なし。dupscan scopeのstrict gate成功と区別する。

- 2026-10-27: `docs(dupscan): record discovery performance results and complete 924`で測定と完了を記録。Markdownの適用issue整合検査、dupscanの適用lint/race、移動後の相対リンク検査、diff検査が成功した。

- 2026-10-27: `docs(dupscan): record additional self-review of discovery optimization`で追加セルフレビューを記録。全consumerのNormalize選択、Body/Delegation参照、parserのClose・例外経路を再読し、修正必須の不具合なし。空scope/完全な構文エラー/生成Goコード/line directive・generic呼出しとSwift accessorを含む28条件でbefore/afterのrc/stdout/stderrが一致した（`/private/tmp/review924-edge-results.json`）。コード追加なし。小規模latencyの未確定性とmetadata-only分岐の保守コストは引き続き残る。
