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
| B (942 b') | 解析不完全の通知を 1 行にする | **942 は air5 が claim 中のため手を付けない** (claim は next/ にある)。air5 に確認してから |
| C | index の行に `Read` の引数を貼る | 保留。範囲指定の使用率の母数が 0 (index の直後の Read が無い)。A1 の後に母数が増えてから測る |
| D | 連番の名前・testdata・近い完全一致の数 | 946 に記録済み。未実装 |
| E-1 | 入口の指標 | usage report で測定中 |
| E-2 | gate が止めた数 | 手段未決 (CI ログから数えるか、gate が実行ログを残すか) |
| E-3 | 928 の実 agent 比較 | 固定scopeの限定pilotは承認・実施済み。本番比較は未実施で、928の現受入条件に沿い実施か移管を決める |

A1 の既知の限界: 名前が違う同じ役割の関数は引けない (署名の近さは見ない)。名前の一致は `index` の語の一致で、型を解決しない。Swift は字下げのある `func` を新しい関数として扱うため、同じ型の中の同名メソッドも候補に出る。

## 受け入れ条件

- [x] A1 を実装した (遅延は 1 名前あたり約 1 秒。雑音は未測定: 注入ログと、注入の後の挙動を 2026-11-04 に見る)。
- [ ] A1 の雑音を測る (候補ありの注入のうち、無関係だった割合。transcript を読んで数える)。
- [ ] 2026-11-04 の再測定の結果を、この issue の「実測と根拠」へ追記する (案内の後の dupscan 率・index 後の範囲指定の率)。
- [ ] 942 b' の採否を決める。
- [ ] E-2 の指標 (gate が止めた数) を数える手段を決める。
- [ ] 928の承認後限定pilotを踏まえ、未使用課題での追加比較を実施するか、その採否とscopeを決める。
- [ ] この issue の判定表を、再測定の結果で書き換える。

## 未確認

- 「呼ばれていない」の 30 日は、hook を直す前の期間が大半。直した後の効果は未測定。
- ③には名前3つ/repo1つのCLI測定と928のSwift3課題/Go1課題のagent pilotがある。tool出力量・集計usageの観測であり、最大context占有や一般的なtoken削減は未測定。
- 重複の防止 (①) の因果効果は、限定pilotで対照も不要写し0件だったため示せていない。

## 関連

- [932](932-epic-dupscan-accuracy.md) (epic)、[928](928-feat-dupscan-agent-onboarding.md)、[941](941-ux-dupscan-nudge-definition-search-gaps.md)、[942](942-perf-dupscan-fixed-notice-overhead.md)、[943](943-perf-dupscan-result-row-shape.md)、[944](pending/944-feat-dupscan-go-def.md)、[946](946-feat-dupscan-recall-from-real-modules.md)、[947](../../done/947-retro-dupscan-multi-module-dedup-2026-10-28.md)

- 2026-10-28: `docs(dupscan): reconcile approved pilot with concurrent research`で928の承認・実行状態と限定観測を同期。「探索+Read未測定」と入口だけを主因とする断定を訂正した。A1等の案の採否・将来測定の条件を完了扱いにはしていない。

- 2026-10-28: `docs(dupscan): refresh onboarding status and make closure requirements explicit`でE-3の「許可待ち」を限定pilot実施済みへ訂正。928の残計画や将来課題の許可まで完了したとは扱わない。
