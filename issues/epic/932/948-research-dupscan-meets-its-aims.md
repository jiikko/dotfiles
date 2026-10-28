# 948 (research): dupscan は「重複を書かない・探しやすい・grep より context を食わない」という狙いどおりに動いているか

起票日: 2026-10-28

> 反証レビュー未実施 (起票時点。数字は 941〜947 と `scripts/dupscan_usage_report.py` の 2026-10-28 の出力から転記した。名前数個・repo 1 つの測定が多く、一般の効果ではない)。

## 概要

dupscan を Claude Code が使えば、①同じ役割のメソッドを重ねて定義しなくて済み、②必要なメソッドを探しやすくなり、③探索で消費する context が grep より減る、と見込んで作った。この 3 つの狙いが今どこまで成り立っているかを、手元の実測だけで判定し、狙いどおりにする案を並べる。

**結論: 局所的な発見・出力短縮の例はあるが、3つの一般効果は未実証。** 利用の少なさに加え、既定検出の取りこぼしと、検索を使っても説明誤り・全読が残る例がある。入口と精度のどちらが主因かは、このデータだけでは決められない。

| 狙い | 判定 | 一行の根拠 |
|---|---|---|
| ① 重複を書かない | **未達 (効果は未証明)** | 呼ばれていない (識別子検索 1,680 回に対し dupscan の探索 2 回 = 0.1%)。重複を見つける力も、既定では 21 件中 4 件 |
| ② 探しやすい | **部分達成** | 広い名前・大小違いには強いが、Go の `def` が無く、Go の `callers` / `index` は近似、0 件は不存在の証明にならない |
| ③ context が減る | **広い名前のCLI出力は短縮、agent全体は未実証** | `Join`: rg 約 43 万 B に対し 2.9 KB。定義 1 件: rg 340 B に対し 738 B |

## 実測と根拠

### ① 重複を書かない

- **呼ばれていない**。`scripts/dupscan_usage_report.py --days 30` (96 セッション、dupscan 自身を開発した 1 本を除く): 識別子らしい grep / rg **1,680 回**、dupscan の探索 **2 回** (`search` 1・`index` 1)。案内 hook の出た grep 1 回、その直後 3 手以内の dupscan 探索 0 回。
  - 案内 hook は 2026-10-28 に直したばかりで (941: 11:29、943: 12:28)、効果は**測れていない** (100 回以上の grep が溜まってから測る。再測定は 2026-11-04 と 2026-11-25)。
- **書いた後の防止 (`gate` を `make test` と CI で回す) は機能している**が、狙いの「書く前に探して再利用する」ではなく事後の検出である。
- **実在する重複を見つける力**: 統合前の 4 module (lockman / mac-lan-manager / ratelimit / restartable) に回して寄せた 21 件のうち、既定の `report` が出したのは **4 件** (946)。残りは `-min-nodes 12`・`near -similar`・名前の連番・人の目で見つけた。小さい単位 (20 節未満) を黙って比べていなかった。→ `report` は比べなかった数を出すようにし (946)、メンバー名 1 語だけが違う写しの段 `L1m` (opt-in) と、4 つの見方をまとめて回す `dupscan sweep` を足した。
- **書く前の `index` / `search` が重複を防いだ因果の証拠は無い**。928では固定Go sourceのClaude実編集2run・固定Swift sourceのCodex読取6runが明示承認され、実行済み。12課題の本番比較は未実施。Go両群とも既存helper再利用・規則の写し0件だったため、重複防止の因果効果は示せなかった。

### ② 探しやすい

- 強い所: 大小の違い (`ProcessRunning` は実体が `processRunning`。rg は 0 B、`index -name` は 1 件に到達)、広い名前 (`Join` は 14 件に絞れる)。
- 弱い所:
  - Go の `def` (定義を本体つきで読む) が無い (944、保留)。Swift だけ。
  - `callers` / `index` は型を解決しない近似。`callers or` は既定の 20 件で打ち切られ、先頭の案内を `grep '^src'` で削ったため呼び出しを取りこぼし、コンパイラで気づいた (947)。→ 打ち切ったら stderr にも 1 行出すようにした (946)。
  - 0 件は不存在の証明にならない (構文木だけを読み、型を解決しない)。agent が 0 件を「無い」と読む恐れは残る。
  - 読めないファイルがある (obaket: 57 ファイル)。通知は毎回出る。

### ③ context

| 検索 (dotfiles の `src` の Go) | grep 系 | dupscan |
|---|---|---|
| 広い名前 `Join` | 約 43〜45 万 B / 約 4,000 行 | `index` 2,900 B・`callers` 3,293 B (**約 150 倍小さい**) |
| 定義 1 件 `ReadCookies` | `rg 'func .*ReadCookies'` **340 B** | `index -name` **738 B** (**2.2 倍大きい**) |
| 呼び出し元 `ReadCookies` | 1,954 B | 1,811 B (ほぼ同じ) |

- 狭い検索で負ける原因は、結果に関係なく毎回出る注意書き (root 実行で `index` 471 B・`callers` 427 B) と、1 行あたり 173 B (rg は 111 B。doc の先頭行が 44〜48%) (942・943)。
- 行を縮めた (`ShortDoc`): 7,008 行で -11.6% (153 B 前後)。それでも rg の行より大きい。
- 探索の後の Read は、`index` の行に終端行 (`file:開始-終了`) を足したので範囲指定ができるようになったが、**agent が実際に offset / limit を使うかは未測定** (使えば `ReadCookies` で全体 14,882 B → 約 1,690 B)。
- 注意: 上は名前 3 つ・repo 1 つ。**928の承認後pilotでは探索+Readを含むtool出力量を測ったが、実context window占有は測れていない。** 固定3課題のSwift両群とも正解で、観測出力合計は対照229695 B / 併用315503 B。併用はguideを読んだだけで検索機能を使わなかった。Go編集1対では検索を実利用したが、tool-result出力は8686 B / 12764 Bで両群に同じ説明誤りが残った。agent/ログ形式の異なるbytesは直接合算しない。詳細・欠測・採点・固定commitは928を参照する。

## 改善案 (狙いどおりにするために)

優先順位は効きそうな順。**A が効かなければ B〜E は意味が薄い** (呼ばれなければ精度も context 削減も効果ゼロ)。

### A. 入口: agent が考えなくても候補が目に入るようにする (最優先)

- 現状は「grep の前に dupscan を使え」と案内して agent の判断に任せる pull 型で、30 日で 0.1%。
- **提案 A1 (push 型)**: 新しい関数を書く Edit / Write の直前 (PreToolUse) に hook が、`new_string` から追加される関数の名前と引数の型・戻り値を取り出して `dupscan index` を内部で引き、**似た名前・同じ署名の既存の定義 (上位 3 件まで、1 件 1 行) を注入する**。agent は呼ばなくても候補を見る。Go は既に「`src/` の .go を Write / Edit すると 1 回案内する」hook がある (suggest-dupscan-for-go-edit) ので、案内の代わりに結果を出す形に伸ばす。懸念: 遅延 (索引は 0.2〜0.3 秒)、雑音 (候補が無関係だと無視される)、`new_string` からの抽出の誤り。
- **提案 A2**: 案内を出す grep の形を増やす前に、941 の効果 (案内の後の dupscan 率) を 2026-11-04 に測る。上がらなければ pull 型の案内は打ち止めとし、A1 に進む。
- **提案 A3**: `CLAUDE.md` の「シンボル検索の使い分け」は毎セッション読まれているのに 0.1% なので、文面の追加は効かない前提で扱う。

### B. 狭い検索で負けない

- 942 の b' (解析不完全の通知を 1 行にする。926 の契約の範囲内) を先にやる。固定費の過半が消える。
- 定義 1 件を引く典型形 (`func .*Name`) を `index -name Name -limit 1` 相当に寄せる案内にする (あるいは案内に結果の大きさの見積もりを添える)。
- 「狭い名前は grep のまま」を公式にする案: 狭いと分かる名前 (結果の見込みが 3 件以下) では案内しない。dupscan を勧める場面を、広い名前・大小が不確かな名前・「似た関数があるか」に限る。

### C. 探索の後の Read を小さくする

- 範囲指定 (offset / limit) の使用率を `scripts/dupscan_usage_report.py` で測る (index の直後 3 手以内の Read のうち範囲指定ありの割合。今は 0 回のうち 0 回で母数が無い)。
- 使われないなら、`index` の行に `Read` の引数 (`offset=開始 limit=行数`) をそのまま貼る (`search` は `read_argv` を既に出す)。

### D. 重複を見つける力 (書いた後の検出)

- `sweep` を初めて入る module の入口として使う (実装済み)。残り: 連番の名前 (`contains` / `contains2`) を末尾の数字を除いた名前の一致として拾う、`testdata/` の fixture を印付きで出す (subproc の report の唯一の出力が fixture だった)、report の末尾に「小さい単位のうち近い完全一致が N 個ある」を添える (どれも f5 の所見。946 に記録済み)。
- `near -similar` は 517 単位で 0.5 以上が 1 件しか出ず、実用の発見手段は report だった。既定の閾値と、`sweep` で 0.6 にした根拠を、3 つ以上の module で測り直す。

### E. 効果の測り方を先に決める

- 通常利用ログは主に呼出し回数を測り、928では限定pilotの品質・出力量も観測した。一般効果の判定には狙い①②③それぞれに、**一度決めたら変えない指標**を置く:
  1. 入口: 識別子検索のうち dupscan の割合 (今 0.1%。A の後に 2 週間見る)。
  2. 重複: `gate` が止めた新規の重複の数と、CI で見つかった重複の数 (`dupscan gate` の出力を CI のログから数える。今は数えていない)。
  3. context: 1 タスクあたりの探索 + Read の byte。928の承認済み限定pilotを基準に、実際の見落とし履歴を使う未使用課題が必要かを先に決める。本番12課題×2群は未実施で、固定scope以外のsource送信まで今回の承認を広げない。
- 再測定の日: 2026-11-04 (941 の案内 / 943 の行の形の効果)、2026-11-25 (A の効果)。

## 具体化と対応状況 (2026-10-28)

| 案 | 具体的な内容 | 状態 |
|---|---|---|
| **A1** | PreToolUse(Write\|Edit) の hook `_claude/hooks/inject-dupscan-candidates-on-new-func.sh`。書く文字列 (Edit は `new_string`、Write は `content`) にある新しい `func name(` (Edit の `old_string` と書き換え前のファイルに無いもの) を最大 3 つ取り、名前ごとに `dupscan index -name <名前> -limit 20` を 1 回引いて、**名前が大小無視で等しい / 前方一致で共通部分 6 文字以上**の既存の定義を、名前ごとに最大 3 行、additionalContext として注入する。除く: 5 文字未満・Test/Benchmark/Fuzz/Example・init/main/String/Error/Close ほか・Go のメソッド。session × 名前で 1 回。候補が無ければ無音。全体で 6 秒の予算。失敗は無音 rc=0 | **実装済み**。テスト 18 件・変異 6 本で赤 (`tests/claude/test_inject_dupscan_candidates_on_new_func.sh`)。実機: 偽の `readCookies` を足す Edit で `Workspace.ReadCookies` を注入 (dotfiles の `src` で 1 名前あたり約 1 秒) |
| A1 の計測 | hook が注入のたびに `~/.cache/claude-dupscan-inject.log` へ `時刻 session 名前 候補数` を足す。`scripts/dupscan_usage_report.py` が「注入 N 回 / 候補あり M 回 / session K 本」を出す | **実装済み**。効果の判定は「候補ありの注入の後、同じ session で新しい関数が書かれなかった / 既存を呼んだか」を見る必要があり、**まだ測っていない** (転記元の transcript の読み方を決めてから) |
| A2 | 2026-11-04 に 941 の案内の効果を測る。上がらなければ pull 型の案内の追加は打ち止め | 日付待ち |
| A3 | `CLAUDE.md` に文面を足さない | 方針のみ |
| B (942 b') | 解析不完全の通知を 1 行にする | **採らない** (942 で決着。942 は c の縮小版を採って done) |
| C | index の行に `Read` の引数を貼る | 保留。範囲指定の使用率の母数が 0 (index の直後の Read が無い)。A1 の後に母数が増えてから測る |
| D | 連番の名前・testdata・近い完全一致の数 | 946 に記録済み。未実装 |
| E-1 | 入口の指標 | usage report で測定中 |
| E-2 | gate が止めた数 | 手段を決めた (2026-10-28): 手元の `make test-dupscan-gate` が `~/.cache/dupscan-gate.jsonl` に 1 回 1 行で追記する (`-record`。CI は記録しない)。`jq -s '[.[] \| select(.failed)] \| length' ~/.cache/dupscan-gate.jsonl` が「止まった実行の数」(同じ変更の再実行は別に数える)。CI の `gh run list` は止まりを数えられない (直近 30 回すべて success。手元の pre-push で先に止まるため)。集計は 2026-11-04 の再測定から |
| E-3 | 928 の実 agent 比較 | 928は診断/案内を限定採用して完了。本番比較とClaude/Swift未検証は本issueへ引継ぎ（末尾の計画・再開条件） |

A1 の既知の限界: 名前が違う同じ役割の関数は引けない (署名の近さは見ない)。名前の一致は `index` の語の一致で、型を解決しない。Swift は字下げのある `func` を新しい関数として扱うため、同じ型の中の同名メソッドも候補に出る。

### A1 の取りこぼしの修正 (f5、2026-10-28。40ea735e)

- **見つけ方**: 注入ログがまだ無かったので、使い捨ての repo に既存の `collectWidgets` を置き、新規 `collectWidgetsFast` の Write を hook に直接流した。候補は 0 件で何も注入されず、ログには「0」と残った。逆に短い `collectWidg` を書くと出る。
- **原因**: `dupscan index -name X` は「既存の名前が X を含む」ものを返すので、**新しい名前より短い既存の名前は、名前そのものでは引けない**。hook の判定は前方一致の両方向を持つが、そもそも候補に入ってこなかった。テストの偽物は引いた語に応じた行を返すだけで、この向きを持たなかったので緑のままだった。
- **修正**: 名前そのもので当たらないとき、名前の語の境目の前方 (6 文字以上・長い順に最大 2 つ) も index で引き、既存の名前が前方になっているものを拾う。テストを 2 本足した (短い既存を拾う / 前方で引いて出た無関係な名前は出さない)。本物の dupscan で `collectWidgetsFast` から `collectWidgets` が注入されることを確認。テストは 20 件 ok。
- **変異**: 前方で引く段を外すと赤。緑のまま残った 2 本は、当たったらやめる break を外す変異 (問い合わせ回数 = 遅延だけが変わる) と、前方の最小長を 6 から 1 に下げる変異 (commonprefix の 6 文字条件と重なり、振る舞いの差が出ない)。
- **未測定**: 遅延は名前 1 つにつき最大 +2 回の index (budget 6 秒は変えていない)。**実測していない**。
- **注入ログ**: c4 の確認 (2026-10-28 17:40) でも `~/.cache/claude-dupscan-inject.log` はまだ無い。hook を登録したセッションより前に始まったセッションでは hook が読み込まれていない可能性がある (未確認)。「A1 の雑音を測る」は、ログができてから。

### D の実測: `sweep` の節ごとの収穫 (c4・f5、2026-10-28)

`sweep` を実在の module に回して、**出た組を読んで実際に寄せたか**を節ごとに数えた。「出た数」ではなく「寄せられた数」。

| 節 | 回した対象 | 読んだ数 | 寄せた数 | 所見 |
|---|---|---|---|---|
| 1 同じ形の組 (既定の下限) | zundamon-kaisetsu | 1 | 1 (`lockHeldByOther`) | 既定の report でも出る |
| 2 下限 12 節 + `L1m` | tuikit・zundamon-kaisetsu・obaket | 3 + 3 + (obaket の上位) | 1 (obaket の `handleScreenMove*` ×3) | tuikit と zundamon の 6 組は全部「別物と書いてある・型が違う」雑音。obaket は決まりきった群 (`init` ×118 など) が上位を占めたので、20 個以上の群を畳んだ |
| 3 部分木の対 | dotfiles の `src` (上位 7 組) | 7 | **2** (`fakeRunner`・`realIssuesView`) | 残り 5 組は意図して分けている・見合いが無い (薄い委譲 / nil 守りの 3 行 / 自己完結を保つ 6 行 / `os.WriteFile` を包む 3 行)。tuikit・treefiler・zundamon では「なし」 |
| 3 部分木の対 | good-chrome-extensions の Go (上位 7 行。c6、2026-10-28) | 7 | **2 (寄せ先が既にある 1・同じ module の小さい 1)** | ①`updateToast` が 2 つの module (`pikpak-master` / `video-product-manager`) でほぼ字面まで同一 (Dice 1.000)。**どちらも `tuikit` に依存済み**なので、tuikit の toast 側に置ける (module をまたぐが寄せ先がある) ②`Brute.Compare` と `Mask.Compare` は Done と Indexes の比べ方が共通 (0.627。同じ module の小さい写し)。残り 5 行は見合いなし: `os.ReadFile`+`t.Fatal` を包む 3 つの test helper (`mustRead`・`readCacheFile`・`read`。パッケージ越えで、寄せ先は testutil の新設)・`os.Open` と `Open` を包む 2 つの helper (型が違う)・mutex で数を返す 3 行の `callCount` ×3 (型が違う)・4 行の accessor どうし (`isLive`~`Rendered` など。形だけの一致) |
| 3 部分木の対 | obaket の Swift (上位 7 行。`-similar-max-units 13000` が要る: 単位 12809 > 既定 8000 で既定では飛ばされる。15.5 秒。c6、2026-10-28) | 7 | **3 (明確) + 3 (部分的)** | 明確: `write` (test の中の写しと `Helpers/TransferActivityCenterTestDoubles.swift` の同名)・`waitUntilRequestedDurationCount` (同じ Helpers と別の test に同一)・`waitUntilInsertCount` (同じファイルの 2 つの fake に同一)。**3 つとも同じ module の test double で、字面まで同一 (Dice 1.000)**。部分的: `makeEncryptedBundle`~`makeEnvelope` (暗号の準備の 8 行が共通で、payload の作り方が違う)・`waitUntilCallFinished` ×2 (同じファイルだが先頭の guard が違う)・`makeRouteTestConnectionsViewModel`~`makeSidebarConnectionsViewModel` (組み立てが同じで引数が違う)。見合いなし: S3 の `list`~`listFlat` (クエリが仕様上違う) |
| 3 部分木の対 | pj_energy_matching の Go (backendV2 と tool の 3 module・2376 単位。上位 7 行 + module をまたぐ 1 行。2026-10-29) | 7 + 1 | **2 (明確) + 2 (部分的)** | 明確: `GetCSVLandCategory` ×2 (model の 2 つの型に 25 件の地目の対応表が丸ごと写されている。違いは関係の辿り方と「未登録」の定数だけ。Dice 0.653 と低いのに上位 1 位)・`ListBiddings`~`ListBiddingsAll` (違いは `qm.Load` の eager load の有無だけ)。**どちらも production のコードで、test double ではない** (obaket と違う)。部分的: `ValidateGeneratorPPAPrice`~`ValidateConsumerPPAPrice` (入札の取得と絞り込みの約 30 行が共通)・`GetMarketDataCSV`~`GetSupplyDemandResultCSV` (期間の引数を解く部分が共通)。見合いなし: consumer / generator の mapper ×2 (生成された型ごとの対応で型が違う)・echo の handler の定型 1。module をまたぐ `toPascalCase` (`tool/gen-go-datastore-sql` ~ `tool/gen-go-usecase`) は 2 つの module に共有の依存が無く寄せ先なし。ただし違いは片方だけの `Pf`→`PF` の置換 1 行で、**2 つの生成ツールの命名のずれの兆候**として読める (pj_energy_matching 側の判断なので報告だけ)。esa-cli (295 単位)・slack-cli (278 単位) は節 3 が「なし」 |
| 4 名前の束 | obaket (22 束) | 手がかり止まり | 0 | `showError` ×10 などは出たが、寄せた例は 0 件。treefiler の `take` ×5 も意味が同じなのは 2 つだけ |

- **節 3 の収穫は 2/7 → good-chrome-extensions の Go を足して 4/14 → obaket の Swift を足して 7/21 → pj_energy_matching の Go を足して 9/28 (明確なものだけ。約 3 割。部分的な候補を入れると 14/28)**。4 つの repo で 3 割前後のまま。分類は repo ごとに別の 1 人 (c4・c6・dotfiles-38) で、基準の揃い方は確かめていない。3 つの repo で 3 割前後に揃ったが、どれも上位を c4/c6 が読んだもので、n=21。**obaket の明確な 3 組はどれも字面まで同一の test double**で、一致度の高い順の上位に集まる (Dice 1.000 が 4 行)。なお、既定の上限 8000 では obaket の節 3 は丸ごと飛ばされる (『飛ばした』と出るだけで、収穫 0 に見える)。ただし 7 組は一致度の高い順 (並べ替えの前) に読んだ上位で、**1 つの repo (dotfiles の `src`)・1 人 (c4) の分類・n=7**。一般の割合とは言えない。
- **寄せた 2 組はどちらも同じ module の中の test helper**だった。module をまたぐ組 (`HandleKey`・`childExitCode`・`writeFile` 系) は、共有に新しい依存か module が要り、1 つも寄せられなかった。→ 節 3 の価値は「同じ module の中の、組にならなかった完全一致に近い写し」に偏る可能性がある (仮説。要検証)。
- 節 3 は 6128 単位の `src` で 5.0 秒かかる (既定の上限を 8000 に上げた)。節 1 と既定の report は 0.3 秒未満。
- 節 2 は、**docs / コメントに「別物」と書いてある組**が雑音の主因 (tuikit)。機械では読めないので、`dupscan:allow` の理由つきの印を使う（`sweep` が全員許可済みの組と対を除くよう直した）。

**判断に使う材料**: 節 3 を既定で出し続けるか、同じ module の中の対に絞るか、`-similar` を指定したときだけ出すかは、この表を増やしてから決める。**今の n では決めない**。

### A1 を本物の新規セッションで観測した (c4、2026-10-28 21:05)

使い捨ての git repo (`src/foo/a.go` に `collectWidgets` だけ)で、`claude -p --model haiku` の新規セッションに「`src/foo/b.go` を作って `collectWidgetsFast(xs []int) []int` を 1 つだけ書いて」と頼んだ (f5 の取りこぼしの修正後の hook。ユーザーの settings.json の配線のまま)。

- **届いた**: セッションの transcript に `PreToolUse:Write` の `hook_additional_context` が残っていた。内容は「`collectWidgetsFast` に近い既存の定義: `src/foo/a.go:3-11 collectWidgets(xs []int) []int [unexported]`」。hook の所要は **99 ms**。注入ログ (`~/.cache/claude-dupscan-inject.log`) にも `collectWidgetsFast 1` が残った。
- **行動は変わらなかった**: agent は `collectWidgets` と同じ本体の `collectWidgetsFast` を書いた (重複が増えた)。ただし**依頼文が「この名前の関数を書け」と明示していた**。n=1・小さいモデル (haiku)・既存の関数を読んだ後(a.go は Write の前に Read していた)の観測で、注入が行動を変えるかは**何も言えない**。
- **計測の注意**: `claude -p --output-format stream-json` の出力には PreToolUse の注入は載らない (`hook_response` は SessionStart のものだけ)。注入の有無は `~/.claude/projects/<project>/<session>.jsonl` の `hook_additional_context` を見る。
- **次に測るもの**: 「既存を使え」と言わない・名前を指定しない依頼で、注入があるときとないとき (hook を外した A-B) に、重複した関数を書く割合。依頼の形で結果が変わるので、928 の比較と同じく**事前に課題を決めてから**回す。

### A1 の A-B (pilot、c6、2026-10-28。保留)

codex に設計を反証させてから回した (指摘の採用: 問いを「最終成果物の不要な重複」に絞る・n=36 は検出力が低い pilot と書く・実行環境を `--restricted` + 腕ごとの `--settings` で隔離・`--tools` で道具を固定・固定ビルドの dupscan・隠しテストで機能の正しさを併記・割付を事前に乱数で固定)。

- **形**: Go の小さな fixture 6 課題 (各 10 ファイル。目的に近い既存関数が 1 つ、関係のない関数のファイルが 8 つ。依頼文は既存関数の名前も「再利用」も書かない)。`claude -p --model claude-sonnet-5-5 --restricted --tools Read,Edit,Write,Grep,Glob`。A = A1 の hook なし / B = あり。6 課題 × 10 反復 × 2 腕 = 120 run の予定で、35 run (A 17・B 18) で止めた (ユーザーの判断。調整のたびに回し直すと重い)。1 run 約 13 秒・約 $0.02。
- **結果**: 既存関数を呼ばずに書き直した run は **A 0/17・B 0/18** (各 95% 区間 0〜18%)。B では名前の近い 4 課題で注入が毎回出た (14/14) が、**注入が無くても毎回再利用する**ので差が出ない (天井効果)。fixture が小さく、agent が全部読めるのが原因と見ている。t4 の機能確認の失敗は、隠しテストが区切り行を 2 文字に決め打ちしていた fixture の誤り (agent は Markdown の 3 文字以上を使っていた)。
- **言えること**: この条件 (小さい repo・sonnet・Bash なし) では、A1 が無くても重複は起きない。A1 が効くかは何も言えない。
- **再開の条件と直す点**: ①目的の関数を見つけにくい課題にする (別パッケージに置く・数百ファイルの repo)。そうしないと天井のまま ②t4 の隠しテストを直す ③run を並列にして 1 周を数分にする。作り直したら codex に課題の作りを見せてから回す。
- 道具: 実験用に A1 の hook に `DUPSCAN_INJECT=off` の opt-out を足した (今回は腕を settings で分けたので使っていない)。作業ファイル (fixture・`run_one.sh`・`drive.sh`・`collect.py`) は本体の `tmp/a1-ab-20261028/` にある (追跡されない。消えたら上の形から作り直す)。

## 受け入れ条件

- [x] A1 を実装した (遅延は 1 名前あたり約 1 秒。雑音は未測定: 注入ログと、注入の後の挙動を 2026-11-04 に見る)。
- [x] A1 が本物のセッションで届くことを確かめた (上の節。99 ms)。
- [ ] A1 の雑音を測る (候補ありの注入のうち、無関係だった割合。transcript を読んで数える)。**2026-10-29 時点で標本 0**: 下の「2026-10-29 の refresh」節。
- [ ] A1 の有無で重複を書く割合が変わるかを、事前に決めた課題の A-B で測る (n=1 の観測では何も言えない)。**保留 (2026-10-28)**: 下の「A1 の A-B (pilot)」節。小さい fixture では天井効果で差が測れない。再開の条件と直す点はその節。
- [ ] 2026-11-04 の再測定の結果を、この issue の「実測と根拠」へ追記する (案内の後の dupscan 率・index 後の範囲指定の率)。
- [x] 942 b' の採否を決める → **採らない** (942 の「採らなかった方針」で決着。926 の hit 側の契約を弱める割に削れるのが 70 B 前後。942 は done)。
- [x] E-2 の指標 (gate が止めた数) を数える手段を決める (上の表。実装は Makefile の `DUPSCAN_GATE_RECORD`)。
- [ ] 928の承認後限定pilotを踏まえ、未使用課題での追加比較を実施するか、その採否とscopeを決める。
- [ ] 節 3 の収穫を、別の repo と module で数え増やす (n=28・Go の 3 repo と Swift の 1 repo。2026-10-29 に pj_energy_matching を足した)。**同じ module の中の対に絞る案は採らない方向**: good-chrome-extensions の `updateToast` は module をまたぐが、両方が既に依存する tuikit に寄せ先があり、絞ると最も確かな 1 組を落とす。「共有の依存がある module 間」は残す。Swift の obaket でも明確な 3 組は同じ module の中だった (module をまたぐ組は 0)。したがって『module をまたぐ組』は good-chrome-extensions の `updateToast` 1 組だけで、絞る案の可否は、module をまたぐ組をもう少し数えてから決める。2026-10-29 に pj_energy_matching で 2 つ目 (`toPascalCase`) が出た: 寄せ先なし (共有の依存が無い) だが、写しのずれ (片方だけの置換) を見せた。module をまたぐ組は 2 組 (寄せ先あり 1・なし 1) で、まだ決めない。
- [ ] この issue の判定表を、再測定の結果で書き換える。

## 未確認

- 「呼ばれていない」の 30 日は、hook を直す前の期間が大半。直した後の効果は未測定。
- ③には名前3つ/repo1つのCLI測定と928のSwift3課題/Go1課題のagent pilotがある。tool出力量・集計usageの観測であり、最大context占有や一般的なtoken削減は未測定。
- 重複の防止 (①) の因果効果は、限定pilotで対照も不要写し0件だったため示せていない。

## 関連

- [932](932-epic-dupscan-accuracy.md) (epic)、[928](done/928-feat-dupscan-agent-onboarding.md)、[941](941-ux-dupscan-nudge-definition-search-gaps.md)、[942](done/942-perf-dupscan-fixed-notice-overhead.md)、[943](943-perf-dupscan-result-row-shape.md)、[944](pending/944-feat-dupscan-go-def.md)、[946](done/946-feat-dupscan-recall-from-real-modules.md)、[947](../../done/947-retro-dupscan-multi-module-dedup-2026-10-28.md)

- 2026-10-28: `docs(dupscan): reconcile approved pilot with concurrent research`で928の承認・実行状態と限定観測を同期。「探索+Read未測定」と入口だけを主因とする断定を訂正した。A1等の案の採否・将来測定の条件を完了扱いにはしていない。

- 2026-10-28: `docs(dupscan): refresh onboarding status and make closure requirements explicit`でE-3の「許可待ち」を限定pilot実施済みへ訂正。928の残計画や将来課題の許可まで完了したとは扱わない。

## 928から引き継ぐ評価計画（2026-10-28）

928は共通診断/表示専用guideを限定採用して完了し、一般効果の研究は本issueで継続する。既存A1候補注入hookの実装・担当・今後の測定を928側で撤去/変更しない。

引き継ぐ未実施項目:

- 各agent4課題pilot→未使用12課題×2群×Claude/Codexの原計画。928のGo1課題とSwift3課題は既知課題の限定pilotであり、本番達成には数えない。対照も成功する天井効果、Swift併用がguideしか使わないこと、Go併用でも全読/説明誤りが残ったことを出発点にする。
- Claude/Swiftの実agent接続と同一課題比較は未検証。Codex/Swift guide成功や手動argv検査で代用しない。元のsource送信承認はGo→Claude/Swift→Codexという固定scopeなので、追加送信は必要な対象・送信先に対する承認の範囲を確認する。
- 対照群へのglobal指示の漏れを扱う隔離条件と、rc/wallをparse前に保存するcollectorの整備。欠測の旧runを都合よく再試行したり、別runの時間で埋めない。

再開の判断材料は、実際の見落とし履歴を固定commit/依頼から復元できた時、または既定の2026-11-04/11-25の通常利用測定で入口/読取量の不足を具体的に確認した時。直ちに48本の本番runを回す決定ではない。原計画を維持するか最小の未使用pilotへ改訂するかを理由付きで事前に確定し、改善が出るまで課題を交換しない。948 A1等はguideだけと別の介入として識別し、使わなかったrunも除外しない。適切なrg/Read対照、品質（再利用/不要写し/誤統合/誤回答）、全tool出力、利用率、usage/時間/失敗をagent別に保存する。

917の別マシン/1301導入後1か月、921の11/25再測定というtriggerは変更せず、結果の共有先とする。成功だけでなく効果なし・費用増も採否判断の結果として残す。928のdoneはこれらの研究完了や一般的なcontext削減の証明を意味しない。

- `docs(dupscan): close onboarding with bounded adoption and explicit research handoff`で上記の未実施計画・制限・再開条件を928から引き継いだ。

## 2026-10-29 の refresh (dotfiles-38)

- **A1 の雑音は測れない (標本 0)**: このマシンの transcript (直近 3 日・サブエージェント含む 94 本) で、A1 の hook が入った 2026-10-28 15:52 より後に `.go` / `.swift` へ新しい関数を足した Write / Edit は 0 回。それより前の 39 回 (dotfiles の Go 30・pj_energy_matching 6・esa-cli 2・Swift 1) は hook の前。dupscan 自身の開発は別のマシン (air5 ほか) で、注入ログ (`~/.cache/claude-dupscan-inject.log`) もマシンごとに書かれる。このマシンにはログのファイル自体が無かった (21:05 の観測で 1 行書いたはずのファイル。消した主体は未特定。A-B の pilot は隔離した環境で回したので、そちらのログは別)。hook は本番の配線のまま直接起動すると注入とログの両方を出す (確認済み)。**2026-11-04 の再測定で、各マシンのログと transcript を合わせて数える**。
- **A1 の hook の潜在的な無音の故障を直した**: 候補の判定で使っていた `os.path.commonprefix` が Python 3.15 で非推奨の警告を出していた。削除されると例外が hook 全体の `except` に吸われ、注入が黙って止まる。判定は `a.startswith(b) or b.startswith(a)` の後なので、共通部分の長さは短い方の長さと等しい → `min(len(a), len(b))` に置き換えた (同じ判定)。テスト 22 件 ok、閾値を 99 / 5 にする変異で赤。repo の中に他の `commonprefix` の使用は無い。
- 節 3 の収穫と 942 b' は上の表と受け入れ条件に書いた。
- 再測定 (2026-11-04)・A-B のやり直し・928 から引き継いだ比較の採否は変わらず未着手 (日付待ち・課題の作り直しが要る)。
