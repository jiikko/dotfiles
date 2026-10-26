# 935 (test): doctorで重複候補の検出と探索量を比較する

起票日: 2026-10-26

## 結果

**doctorでは現行dupscanが名前のseedなしで有用な完全重複を見つけた。今回の探索出力は併用側で少なかったが、一般的な開発効率・context window占有・新規重複追加の削減は未実証。**

- dupscan既定report: 63 Go files / 359 units、parse error filesなし、L1の1組。displaycheckのsortedGateKeys/sortedKeysで、値型だけが違う同じソート処理を検出した。
- grep中心: diskテストのfakeRunner.run/deleteRunner.runにある最長prefix選択の部分重複を、2回とも発見した。現行reportはこの部分重複を既定/追加条件のどちらでも出さなかった。
- 各方式2回の有用な共通化候補Dは各run1組。互いに異なる候補を見つけ、どちらも網羅的ではない。独立判定した候補プールのDは2組であり、真の全重複を母集団としたrecallは計算していない。
- 探索のrecorder転送bytesの平均はgrep 54,783 → 併用28,116（48.7%少ない）。共通setupの23,886 bytesを各runへ足すと78,669 → 52,002（33.9%少ない）。**token量やwindow占有の削減率ではない。**
- doctorのソースは変更していない。共通化・新規実装の課題を行っておらず、不要なメソッド追加率/正しい再利用率が改善したとは言わない。

## 固定入力と実験条件

- source commit: `8887a53dbfec782c981d3bb275d61679ff8076a3`。対象src/doctorの63 filesはファイル名基準で非_test.go 40 / _test.go 23。非_test.goでも内部検査器/fixtureを含むため、この数を製品runtimeのproduction/test内訳とは扱わない。
- doctorのGoファイル集合/内容hash: `2fb525b8e881bd6175e4bf0da1fc3c8bd33e6a3de2209403f19a9a640fbc84d2`。計算法は相対pathの辞書順で、各pathのUTF-8 bytes、NUL、内容SHA-256の32 bytes digestを順に連結してSHA-256。全探索後も一致。issue935の予約commitのみを追加し、doctor/dupscanの実装は変更していない。
- binary: 929修正後のbuild済みdupscan。SHA-256 `99d3cafde719abac034ed4c57fe8288445248a70618d6d80bc6368b337a06087`。build時間は除外。
- 結果を見る前に条件を固定。各armにfresh agentを2回ずつ割り当て、他run/親の発見を渡さず、関数名のseedなしで「同一domainで共有すべき重複を最大5組探す」という同じ課題を渡した。親からmodel/effortを継承し、arm別のoverrideはしていない。samplingやhardware負荷は制御しておらず、小規模探索実験である。
- grep側はrg/grep/cat/sed/wc、併用側はそれにdupscanを追加。初期reportと、事前指定した追加条件min-nodes=8/L1〜L3を利用可とした。未実装のsearch/context/Type-3は使用していない。assist1の最初のsource取得前に、flag/valueを別argvにする表記を訂正した（検索名や候補は知らせていない）。
- 共通setupはCLAUDE.md/src/doctor/CLAUDE.md/README.mdを同じrecorderで別計測。全runとも23,886 bytes。以後はsearch 16回/総転送60,000 bytes/1回6,000 bytesをhard capにし、長い出力はUTF-8 prefixとmarkerへ制限。全runが上限内。
- raw bytesはstdout+stderr、転送bytesはrecorderのstdoutへ出した量。外側toolのserialization/制限後にmodelが実際に受け取った量・tokenは独立検証していない。全会話・最終回答・compaction・peak入力context・cacheを含む全context量ではない。
- setup以外の追加検証はソース読取だけ。doctor CLI、disk走査、削除・停止、テスト実行はしていない。検出した抽出案の挙動は未実装/未検証。

測定recorderはstdoutとstderrをこの順で連結し、上限を超えた場合は `\n[TRUNCATED BY RESEARCH BUDGET; use narrower query]\n` のbytesを予約して先頭を切り、UTF-8の不完全な末尾を除いてmarkerを足す。search回数と残り総転送量を実行前に検査し、上限後はrc=77で拒否する。subprocessの開始/終了UTC、perf_counter差分、argv/rc、元のstdout/stderr bytes、転送bytes/hashをJSONLへ記録した。

ローカル原本は `/private/tmp/doctor-discovery-protocol-20261026.json`、`/private/tmp/doctor-discovery-20261026.py`、`/private/tmp/doctor-discovery-{grep1,assist1,grep2,assist2}-20261026.jsonl`、`/private/tmp/doctor-command-benchmark-20261026.json`。一時ファイルの存続は保証しない。本issueには全search ledgerと仕様・集計を残すが、各agentへの指示文全文や推論は永続保存していないため、探索手順を完全再演できる記録ではない。

## 探索run全体の測定値

D=共有すべき同じ規則の重複、P=部分/意味上の重複だが抽出の純利益が未確定、I=構文や意味の類似はあるが意図的な差・境界・共有コストから今回の共通化対象に採らないもの。共通setup・最終回答と独立判定のコストは下表に含めない。command合計はsubprocess実行時間の和であり、agent全体の所要時間ではない。

| arm | search回数 | raw bytes | 転送bytes | command合計秒 | D | P | per-callで省略された回数 |
|---|---:|---:|---:|---:|---:|---:|---:|
| grep1 | 14 | 106605 | 57809 | 0.079728 | 1 | 2 | 4 |
| assist1 | 16 | 30242 | 30200 | 0.147385 | 1 | 0 | 1 |
| grep2 | 16 | 67232 | 51757 | 0.072016 | 1 | 0 | 3 |
| assist2 | 13 | 28844 | 26032 | 0.136334 | 1 | 0 | 1 |

探索の最初のcommand開始から最後のcommand終了までは、grep1 88.1秒 / assist1 86.0秒 / grep2 107.5秒 / assist2 66.3秒。model推論/待ちも混ざる単発spanで、setupや最終回答・判定は除く。並行実行・個体差・候補確認の順序があり、これを開発全体の速度差の因果推定には使わない。

### 最初のDを確認できた時点

候補名の発見だけでなく、本体と利用文脈の確認まで。担当の確認箇所とraw ledgerを照合した。assist2の本人集計はsetup込み14だったため、表はlogのsearch 13へ訂正している。

| arm | 確認search # | そこまでの転送bytes | command合計秒 | 最初のsearch開始から確認command終了まで秒 |
|---|---:|---:|---:|---:|
| grep1 | 11 | 48372 | 0.060208 | 67.179 |
| assist1 | 4 | 4513 | 0.081686 | 9.260 |
| grep2 | 10 | 43313 | 0.051465 | 76.917 |
| assist2 | 13 | 26032 | 0.136334 | 66.298 |

同じ個別の重複を見つける競争ではない。双方で確認したDの数は同じでも、候補の価値/網羅性が完全に同じだと証明したものではない。未知の候補や省略された末尾を検証済みと扱わない。

## ソースで確認した2組

### A. displaycheckのソート済みmapキー（D、併用が2/2で発見）

`internal/displaycheck/displaycheck.go:317 sortedGateKeys(map[string]Gate)` と `:326 sortedKeys(map[string]string)` は、容量len(m)のstring sliceを作り、mapキーをappendし、sort.Stringsして返す同じ本体。値は使わない。sanitizeGate(:223)/sanitizeExempt(:254)を決まった順に検査する同じcheckerの責務で、privateなgeneric sortedKeysへの共通化が候補になる。

このpackageは表示の無害化漏れを検査するテスト用ライブラリで、製品のdisk走査処理そのものではない。dupscanは非_test.goの関数として表示するが、runtime productionの改善と誤解しない。共通化は今回行っていない。

### B. diskテストの最長prefix応答選択（D、grepが2/2で発見）

`disk/scan_test.go:31 (*fakeRunner).run` と `disk/delete_test.go:32 (*deleteRunner).run` は、登録したcommand prefixのうちトークン境界まで一致する最長keyを選ぶloopを重ねて持つ。返すresponse型・stderr・onCall等は違うが、prefix選択の規則は同じdiskテスト基盤のもの。

全部のrunnerを統合せず、選択部分だけを共用する候補。追加処理とreturnの構造が違い、現在の関数全体hashは一致しない。[895](../895-feat-dupscan-type3-candidates.md)へ部分重複の具体例として戻す。既知例なのでheld-outの精度証明には使わない。

## 機械一致と共通化判断の違い

方式名を渡さない別のreadonly agentへ、両armと本体確認から得た12候補のsource対だけを渡して判定した。判定対象はこのpoolだけで、doctor全体の正解集合ではない。

| 候補 | 判定 | 同じ形でも残す理由/共有する規則 |
|---|---|---|
| displaycheck sortedGateKeys/sortedKeys | D | mapキーをソートする同じchecker規則。値型だけ違う |
| disk fakeRunner.run/deleteRunner.run | D | 最長prefixのトークン境界選択を共有。応答/副作用は分ける |
| Docker/SSDのstderr先頭行 | P | trimしてLF先頭を返す同じ意味。ドメインごとの表示文脈があり、共有owner/純利益は未確定 |
| diskdoctor/svcdoctorのemit部分 | P | JSON輸送は重なるがsanitizer/format/exit判定は別。callback等の複雑さとの比較が必要 |
| disk/DockerのHumanSize | I | 1024/KBと1000/kBで外部仕様が異なる |
| 二つのmain | I | 実行ファイルごとのos.Exit(run())。小さい配線 |
| disk/svcのbrewFormulae | I | 既にbrewledger.Installedへ委譲し、台帳の実装は共有済み |
| 二つのfailWriter.Write | I | 1行の失敗刺激のため共有test packageを足す利益が小さい |
| Docker/SSDのScan | I | package固有のsanitization境界。scan本体/Reportは別物 |
| disk/DockerのSize比較closure | I | 別型・別domainの並び順。比較式だけを共有する利益が小さい |
| ephEnv/keepEnv | I | 意図して反対のTMPDIR条件を表すtest fixture |
| 各packageのdisplay_coverageテスト入口 | I | 判定本体は既にdisplaycheck.Runへ共有。残りはpackage別設定 |

既定reportの1組はD。追加条件reportの7組はD=1/I=6で、Dの追加発見は無かった。この6組は構文一致の誤りという意味ではなく、共通化判断では採らない候補。無差別に閾値を下げて読む量を増やすと、節約の効果を相殺しうる。

## 補助測定: 単体commandの待ちと出力量

macOS/arm64、build済みbinary、warm-up1回を除いた5回。4queryをroundごとに逆順へ変えた。CPU/OS cacheは制御していない。rg inventoryは関数の一覧で、重複対を返すreportと同じ情報ではない。grep自体の速さと、候補へ到達する探索量を混同しない。

| query | 5回の秒数 | 中央値秒 | raw stdout bytes |
|---|---|---:|---:|
| `dupscan report -format json src/doctor` | 0.033305 / 0.034242 / 0.034130 / 0.034182 / 0.034428 | 0.034182 | 1727 |
| `dupscan report -limit 20 src/doctor` | 0.033929 / 0.034450 / 0.032619 / 0.034200 / 0.033370 | 0.033929 | 265 |
| `dupscan report -format json -min-nodes 8 -levels L1,L2,L3 src/doctor` | 0.033603 / 0.033284 / 0.033418 / 0.035460 / 0.033133 | 0.033418 | 9625 |
| `rg -n '^func ' src/doctor -g '*.go' -g '!**/*_test.go'` | 0.007159 / 0.006485 / 0.006592 / 0.006907 / 0.007425 | 0.006907 | 17753 |

## 評価と残る限界

- 具体的な完全重複を初期reportだけで小さく返した点は、doctorの探索で役立った。両armで有用な候補を得た4runでは、併用の転送出力が少ないという限定的な観測が得られた。
- grepは部分重複を拾い、dupscanはこれを逃している。候補の集合が異なるので、どちらかが網羅的に優れるとも、品質の非劣性が証明されたとも言わない。
- N=2/arm、同じmodule/親設定のagentのみ。推論の揺れ・作戦・候補価値の違いを統制できていない。全体のcontext token・window占有・課題成功・新規重複追加率の実証は[928](../928-feat-dupscan-agent-onboarding.md)の実装課題A/Bへ残す。
- 実装前のhelper探索と、実装後の既存重複探索は別の段階。今回の結果だけで未実装search/context/Type-3の効果を宣言しない。語や本体hintからの探索は925/933/934、部分重複は895、候補の段階取得とcontext量は927/928が継続対象。

<details>
<summary>全search command ledger（setupは各run共通23,886 bytesで別計測）</summary>

各commandのrc、実行秒、raw/転送bytesを残す。転送上限で切れた場合、未転送部分は読取済みに数えない。binaryは固定した上記SHAのもの。

| arm | # | argv | rc | 秒 | raw bytes | 転送bytes | 省略 |
|---|---:|---|---:|---:|---:|---:|---|
| grep1 | 1 | `rg --files src/doctor -g '*.go'` | 0 | 0.006406 | 2017 | 2017 | no |
| grep1 | 2 | `rg -n '^func ' src/doctor/disk src/doctor/svc src/doctor/docker src/doctor/ssd -g '*.go' -g '!**/*_test.go'` | 0 | 0.006828 | 14922 | 6000 | yes |
| grep1 | 3 | `rg -n '^func ' src/doctor -g '*.go' -g '!**/*_test.go' -g '!disk/guard.go' -g '!docker/scan.go' -g '!disk/catalog.go'` | 0 | 0.006942 | 17753 | 6000 | yes |
| grep1 | 4 | `rg -n '^func ' src/doctor/svc src/doctor/ssd src/doctor/runner src/doctor/cmd src/doctor/docker/size.go src/doctor/docker/display.go -g '*.go' -g '!**/*_test.go'` | 0 | 0.006511 | 4640 | 4640 | no |
| grep1 | 5 | `sed -n 1,100p src/doctor/docker/size.go src/doctor/disk/report.go src/doctor/svc/brew.go` | 0 | 0.002855 | 3224 | 3224 | no |
| grep1 | 6 | `rg -n -A 15 '^func (brewFormulae\|firstLine\|shellQuote\|ShellQuote\|dirExists)' src/doctor/disk src/doctor/svc src/doctor/ssd src/doctor/docker -g '*.go'` | 0 | 0.007837 | 3683 | 3683 | no |
| grep1 | 7 | `rg -n '^func ' src/doctor -g '*_test.go'` | 0 | 0.008360 | 32952 | 6000 | yes |
| grep1 | 8 | `rg -n '^func [a-z]\|^func \(' src/doctor/disk/scan_test.go src/doctor/disk/delete_test.go src/doctor/disk/catalog_reboot_test.go src/doctor/disk/chromium_cache_test.go src/doctor/svc/scan_test.go src/doctor/docker/scan_test.go src/doctor/ssd/scan_test.go src/doctor/cmd -g '*.go'` | 0 | 0.007210 | 5528 | 5528 | no |
| grep1 | 9 | `sed -n 350,485p src/doctor/docker/scan.go` | 0 | 0.002338 | 7167 | 5998 | yes |
| grep1 | 10 | `sed -n 1,100p src/doctor/disk/scan_test.go` | 0 | 0.002179 | 3444 | 3444 | no |
| grep1 | 11 | `sed -n 1,75p src/doctor/disk/delete_test.go` | 0 | 0.002742 | 1838 | 1838 | no |
| grep1 | 12 | `rg -n -A 35 '^func emit' src/doctor/cmd/diskdoctor/main.go src/doctor/cmd/svcdoctor/main.go` | 0 | 0.006083 | 3473 | 3473 | no |
| grep1 | 13 | `rg -n -A 18 '^func ' src/doctor/disk/display_coverage_test.go src/doctor/svc/display_coverage_test.go src/doctor/docker/display_coverage_test.go src/doctor/ssd/display_coverage_test.go` | 0 | 0.007128 | 2607 | 2607 | no |
| grep1 | 14 | `rg -n -B 3 -A 3 'firstLine\(' src/doctor/docker/scan.go src/doctor/ssd/diskutil.go src/doctor/ssd/smartctl.go` | 0 | 0.006309 | 3357 | 3357 | no |
| assist1 | 1 | `dupscan report -limit 20 src/doctor` | 0 | 0.036466 | 265 | 265 | no |
| assist1 | 2 | `dupscan report -min-nodes 8 -levels L1,L2,L3 -limit 20 src/doctor` | 0 | 0.035030 | 1211 | 1211 | no |
| assist1 | 3 | `sed -n 260,345p src/doctor/internal/displaycheck/displaycheck.go` | 0 | 0.003261 | 2595 | 2595 | no |
| assist1 | 4 | `rg -n 'sortedGateKeys\|sortedKeys\|^go ' src/doctor/internal/displaycheck src/doctor/go.mod` | 0 | 0.006928 | 442 | 442 | no |
| assist1 | 5 | `sed -n 1,125p src/doctor/cmd/diskdoctor/exit_test.go` | 0 | 0.002817 | 6040 | 5998 | yes |
| assist1 | 6 | `sed -n 40,80p src/doctor/cmd/svcdoctor/exit_test.go` | 0 | 0.002757 | 2002 | 2002 | no |
| assist1 | 7 | `cat src/doctor/svc/brew.go` | 0 | 0.002342 | 1042 | 1042 | no |
| assist1 | 8 | `sed -n 350,375p src/doctor/disk/guard.go` | 0 | 0.002127 | 1332 | 1332 | no |
| assist1 | 9 | `sed -n 1,65p src/doctor/disk/catalog_reboot_test.go` | 0 | 0.001990 | 3128 | 3128 | no |
| assist1 | 10 | `dupscan index -name sanitize src/doctor` | 0 | 0.040405 | 2412 | 2412 | no |
| assist1 | 11 | `sed -n 1,58p src/doctor/cmd/diskdoctor/main.go` | 0 | 0.001944 | 2953 | 2953 | no |
| assist1 | 12 | `sed -n 1,48p src/doctor/cmd/svcdoctor/main.go` | 0 | 0.002191 | 2633 | 2633 | no |
| assist1 | 13 | `sed -n 105,136p src/doctor/disk/scan.go` | 0 | 0.002338 | 819 | 819 | no |
| assist1 | 14 | `sed -n 625,670p src/doctor/docker/scan.go` | 0 | 0.002340 | 1094 | 1094 | no |
| assist1 | 15 | `sed -n 137,178p src/doctor/docker/scan.go` | 0 | 0.002311 | 1302 | 1302 | no |
| assist1 | 16 | `sed -n 66,103p src/doctor/ssd/scan.go` | 0 | 0.002138 | 972 | 972 | no |
| grep2 | 1 | `rg --files src/doctor -g '*.go'` | 0 | 0.006652 | 2017 | 2017 | no |
| grep2 | 2 | `rg -n '^func ' src/doctor/disk/scan_test.go src/doctor/disk/delete_test.go src/doctor/disk/report_test.go src/doctor/svc/scan_test.go src/doctor/docker/scan_test.go src/doctor/ssd/scan_test.go` | 0 | 0.007496 | 18366 | 6000 | yes |
| grep2 | 3 | `rg -n '^func ' src/doctor/disk/guard.go src/doctor/disk/scan.go src/doctor/disk/delete.go src/doctor/disk/size.go src/doctor/disk/paths.go` | 0 | 0.007393 | 7388 | 6000 | yes |
| grep2 | 4 | `rg -n '^func ' src/doctor/docker/scan.go src/doctor/ssd/smartctl.go src/doctor/svc/scan.go src/doctor/disk/display.go` | 0 | 0.005943 | 4159 | 4159 | no |
| grep2 | 5 | `sed -n 130,240p src/doctor/docker/scan_test.go` | 0 | 0.002440 | 5283 | 5283 | no |
| grep2 | 6 | `sed -n 350,505p src/doctor/docker/scan.go` | 0 | 0.002142 | 7719 | 5998 | yes |
| grep2 | 7 | `rg -n '^func ' src/doctor/disk/delete_test.go` | 0 | 0.003785 | 4645 | 4645 | no |
| grep2 | 8 | `sed -n 1,125p src/doctor/disk/delete_test.go` | 0 | 0.003362 | 3866 | 3866 | no |
| grep2 | 9 | `sed -n 1,145p src/doctor/disk/scan_test.go` | 0 | 0.003180 | 4901 | 4901 | no |
| grep2 | 10 | `rg -n 'strings.Join\(append\(\[\]string\{name\}\|func.*callLines\|best := ""\|func.*find\|func.*lookup' src/doctor -g '*.go'` | 0 | 0.009073 | 444 | 444 | no |
| grep2 | 11 | `sed -n 1390,1435p src/doctor/disk/delete_test.go` | 0 | 0.002698 | 2617 | 2617 | no |
| grep2 | 12 | `sed -n 240,260p src/doctor/docker/scan_test.go` | 0 | 0.002072 | 659 | 659 | no |
| grep2 | 13 | `sed -n 400,418p src/doctor/docker/scan_test.go` | 0 | 0.002594 | 892 | 892 | no |
| grep2 | 14 | `rg -n '^func ' src/doctor/disk/report.go src/doctor/docker/size.go src/doctor/svc/report.go src/doctor/ssd/scan.go src/doctor/disk/run.go` | 0 | 0.007342 | 1168 | 1168 | no |
| grep2 | 15 | `sed -n 1,75p src/doctor/docker/size.go` | 0 | 0.003148 | 2323 | 2323 | no |
| grep2 | 16 | `sed -n 1,28p src/doctor/disk/report.go` | 0 | 0.002696 | 785 | 785 | no |
| assist2 | 1 | `dupscan report -limit 20 src/doctor` | 0 | 0.036642 | 265 | 265 | no |
| assist2 | 2 | `dupscan report -min-nodes 8 -levels L1,L2,L3 -limit 20 src/doctor` | 0 | 0.038470 | 1211 | 1211 | no |
| assist2 | 3 | `sed -n 270,345p src/doctor/internal/displaycheck/displaycheck.go` | 0 | 0.002572 | 1923 | 1923 | no |
| assist2 | 4 | `dupscan index -name sanitize src/doctor` | 0 | 0.033829 | 2412 | 2412 | no |
| assist2 | 5 | `cat src/doctor/cmd/diskdoctor/main.go src/doctor/cmd/svcdoctor/main.go src/doctor/svc/brew.go` | 0 | 0.002180 | 8810 | 5998 | yes |
| assist2 | 6 | `sed -n 350,380p src/doctor/disk/guard.go` | 0 | 0.002283 | 1476 | 1476 | no |
| assist2 | 7 | `cat src/doctor/svc/brew.go` | 0 | 0.002585 | 1042 | 1042 | no |
| assist2 | 8 | `sed -n 100,145p src/doctor/disk/scan.go` | 0 | 0.002020 | 1104 | 1104 | no |
| assist2 | 9 | `sed -n 637,665p src/doctor/docker/scan.go` | 0 | 0.002212 | 776 | 776 | no |
| assist2 | 10 | `sed -n 140,180p src/doctor/docker/scan.go` | 0 | 0.002376 | 1381 | 1381 | no |
| assist2 | 11 | `sed -n 65,95p src/doctor/ssd/scan.go` | 0 | 0.002825 | 774 | 774 | no |
| assist2 | 12 | `sed -n 1,42p src/doctor/disk/catalog_reboot_test.go` | 0 | 0.002499 | 2191 | 2191 | no |
| assist2 | 13 | `rg -n -A 6 -B 3 'func \(failWriter\) Write\|sortedGateKeys\|sortedKeys' src/doctor/cmd/diskdoctor/exit_test.go src/doctor/cmd/svcdoctor/exit_test.go src/doctor/internal/displaycheck/displaycheck.go` | 0 | 0.005842 | 5479 | 5479 | no |

</details>

## 検証・進捗

- [x] 条件を結果取得前に固定し、各arm2回・出力上限を機械的に検査した。
- [x] default/追加条件の全機械候補と、独立したsource判定を照合した。
- [x] 対象Goソースのhashが終了後も同じことを確認した。doctorの実装は変更していない。
- [x] 数値・主張・リンクを読み取り専用Codex subagentで反証レビューし、Bash 5の適用issue検査・相対リンク・diff検査が成功。重大指摘なし、P3のI凡例とhash/recorder仕様・再現性限界を追記した。

- 2026-10-26: `issues: reserve doctor duplicate discovery comparison`で番号予約。grep/併用を各2回実施し、Dをそれぞれ別の1組として発見した。改善は検出の実例、転送量の観測、取りこぼし/意図的な一致を切り分けて残したこと。ソースの重複削減やmodel context削減を実装したものではない。

- 2026-10-26: `test(dupscan): record doctor discovery output comparison`で検出・限定測定を完了。935の調査範囲は完了、895/928の機能実装と実token・新規重複追加率の評価は継続。
