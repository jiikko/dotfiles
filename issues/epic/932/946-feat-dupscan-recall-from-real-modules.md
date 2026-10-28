# 946 (feat): 実際の module に回して分かった、重複の見落としと対策 (lockman / mac-lan-manager / ratelimit / restartable)

起票日: 2026-10-28

## 概要

dotfiles の Go の 4 module (lockman・mac-lan-manager・ratelimit・restartable) に dupscan を回して、重複した helper を実際に寄せた (`refactor(lockman)` ほか 4 commit)。寄せた **21 件**のうち、**既定の `report` が出していたのは 4 件 (19%)** だった。どの段階の手段で見つかったか、何が見つからなかったかを残し、dupscan に取り込む。

## 実測 (寄せた 21 件が、どの手段で見つかったか)

| 手段 | その手段で新しく見つかった件数 | 累計 |
|---|---|---|
| 既定の `report` (節の下限 20) | 4 (lockman の後始末のクロージャ・mac-lan-manager の `DefaultPath`・restartable の PTY runner と `envDuration`) | 4 (19%) |
| `report -min-nodes 12 -min-closure-nodes 12` | 9 (`fmtSize` / `fmtAge`・`or`・「Device が同じ」「Host が同じ」「経路の IP が同じ」のクロージャ・テストの `IfName` の偽物・`paceUnit`・`sourceFile`・`shellQuote`) | 13 (62%) |
| `near -similar` を全単位に回す (類似度 0.4 以上) | 5 (`captureStderr` / `captureStdout`・テストの testdata を読む helper・`padLeft` / `padRight`・`readStatuslineRaw` / `readWriters`・`waitPTYOutput` / `waitPTYCondition`) | 18 (86%) |
| どの手段も出さなかった (名前と本文を読んで見つけた) | 3 (lockman の待ち合わせの helper 4 つ・`takeoverMarks` と `tmpEntriesWithSuffix`・restartable の短い socket 用 dir を作る 4 つの定義) | 21 |

- `-min-nodes 12` が出した 17 組のうち、寄せたのは 14 組・一部だけ寄せたのが 1 組・別の規則として残したのが 2 組 (`Kind.String` と `Key`: 画面の名前と保存する鍵。`StaticIP` の印のパス)。誤検知に近いのは 2/17 (約 12%)。
- **`captureStderr` / `captureStdout` は `os.Stderr` / `os.Stdout` の 1 語しか違わない 20 行の写し**だが、`report` / `gate` / `near` は `-min-nodes 8` や L3 でも出さなかった。`explain -compare` は違いを「メンバー名 `Stdout` / `Stderr` の 3 か所」と正しく示した。つまり dupscan は「1 語だけ違う写し」の形を持っているのに、既定の探索の段に入っていない。
- 全単位への `near -similar` は、mac-lan-manager (695 単位) でも数分で終わった (8 並列)。895 が「全体 group 化 / LSH は不採用」とした費用の面は、module 単位の規模では問題にならない。ただし出力は候補で、同じ規則かの判断は 1 組ずつ要る。

## 取り込み済み

- **`report` の 1 行目に、節が下限に満たず比べなかった単位の数を出す** (`・小さい単位 81 個は比べていない (節が 20 未満。-min-nodes で下げる)`。JSON は `below_min_nodes` / `min_nodes`)。lockman の「重複なし」は 181 単位のうち **81 単位 (45%) を比べていなかった**。0 件の「重複なし」が、比べていない部分を黙らせないため。比べなかった単位が 0 のときは出さない (1 行を短く保つ)。変異 6 本で赤を確認 (`internal/group` の `CountBelowMin` と `report` / JSON のテスト)。
- README に、小さい単位が比較の外であることと、`-min-nodes` を下げたときの実測 (17 組中 14 組が実際の重複) を足した。
- 意図して分けた組は、理由つきの `// dupscan:allow intentional …` を単位の直前のコメントに書く (`netinfo.Kind.String` / `Key`)。組は「(許可)」の印つきで残り、gate は止めない。

## 提案 (未実装。測ってから採る)

1. ~~**「1 語だけ違う写し」を既定の探索の段に入れる**~~ → **opt-in の段 `L1m` として実装した** (下の「提案 1 の評価と実装」。既定には入れない): L1 の形のうち、メンバー名・外部 package の識別子 (`os.Stdout` と `os.Stderr`) だけが違う組を、別の段 (例 `L1m`) として report に出す。評価は、4 module の `captureOutput` のような既知の組を拾い、`Kind.String` / `Key` のような別の規則を何件誤って出すかを数える。895 の「全体 group 化は不採用」とは別で、`near -similar` の全単位走査ではなく、メンバー名を畳んだ hash の一致だけを見る (費用は L2 と同程度)。
2. **module を一括で掘る入口**: `report` (既定) → `-min-nodes 12` → `near -similar` の全単位、を 1 コマンドにして、類似度順に候補を出す (今回は下の使い捨ての script で手作業していた)。21 件のうち 18 件はこの 3 段で出た。
3. **名前の語の束で、形が違う同じ目的の helper を出す**: `wait*` / `await*` / `capture*` / `fmt*` / `count*` のように、動詞の語頭が同じ小さい helper の束を `index` で 1 行ずつ。形が違う 3 件のうち待ち合わせの helper 4 つは、名前の束で見えた。誤検知 (`Test*`・`New*`) の除外が要る。
4. **`callers` の既定の `-limit 20` で打ち切られた事実を見落とさない**: 今回、`callers or` が 20 件で打ち切られ、呼び出し元の一部 (`lanmgr` の 5 ファイル) を見落としたまま置き換えかけた。943 で足した件数ヘッダ (`呼び出し N 件 (20 件表示 / …)`) は出ていたが、結果を `grep '^src'` で絞って自分で捨てた。リファクタ前に「全件」を取る用途のために、`callers -limit 0` を案内に添えるか、打ち切ったときの終了コードを区別するかを検討する。

## 使い捨ての探索 script (module の全単位に near -similar)

```python
import re, subprocess, sys
from concurrent.futures import ThreadPoolExecutor
module, units_file = sys.argv[1], sys.argv[2]   # units_file は `dupscan index -limit 0 <module>` の各行を `file:name` にしたもの
LINE = re.compile(r'^([0-9.]+) shared_weight=\d+ "([^"]+)":(\d+):\d+')
def run(u):
    f, n = u.split(':', 1)
    p = subprocess.run(['dupscan', 'near', '-similar', f'{f}:{n}', module], capture_output=True, text=True)
    m = [x for x in (LINE.match(l) for l in p.stdout.splitlines()) if x]
    return u, ((m[0].group(1), m[0].group(2), m[0].group(3)) if m else None)
with ThreadPoolExecutor(8) as ex:
    rows = [r for r in ex.map(run, [l.strip() for l in open(units_file) if l.strip()]) if r[1]]
for u, (score, f, line) in sorted(rows, key=lambda r: -float(r[1][0])):
    print(f'{score} {u}  ~  {f}:{line}')
```

## 受け入れ条件

- [x] `report` が比べなかった小さい単位の数を出す (text・JSON)。
- [x] 提案 1 の評価: 4 module と obaket で `L1m` を試し、表にした (opt-in の段として実装)。
- [x] 提案 2 (一括の入口): `dupscan sweep` を実装 (report / 下限 12 + L1m / 部分木の総当たり / 名前の束の 4 節)。
- [x] 提案 3 (名前の束): `sweep` の 4 節として実装。
- [x] 提案 4 (打ち切りの見逃し): callers / index が打ち切ったら stderr にも 1 行出す (`| grep` で先頭の案内が削れても残る)。
- [ ] 実運用での採否: f5 の所見 (連番の名前・testdata の fixture・満点の組の数) を反映するか決める。

## 提案 1 の評価と実装 (2026-10-28)

L1 に加えて**メンバー名** (`a.b` の `b`、`RoleMember`) を記号にする段 `L1m` を作り (リテラル・局所の名前の番号・構造は L1 のまま)、統合前の木 (`0973b244^`) の Go 4 module と、Swift の obaket に流した。

| 対象 | 既定の下限 (20) で `L1m` が新しく出す組 | 下限 12 |
|---|---|---|
| lockman | 2 (`captureStderr` / `captureStdout`・`RenewTimed` / `ReleaseTimed`) | 2 |
| mac-lan-manager | 1 (`lanmgrtest.Scan` の setter 3 つ) | 2 |
| ratelimit | 0 | 0 |
| restartable | 2 (`outputText` / `text` / `String` ほか) | 3 |
| **obaket (Swift)** | **105 (本番 59・テスト 29)** | 214 (本番 117) |

- **Go**: 既存の見落としだった `captureStderr` / `captureStdout` を**既定の下限で拾った**。ほかは意図して残したタイムアウト包み (`RenewTimed` / `ReleaseTimed`)・setter 3 つ・既に L1 で出ていた組で、新しい真の写しは 1 件だった。再現率への寄与は大きくない (寄せた 21 件のうち 1 件)。
- **Swift**: 既定の下限でも本番の組が **3 → 62** に増える。本番 59 組から 16 組を抜き取ると、実際の写しは 9〜10 組 (`executeResolvedCopy` と `executeResolvedMove`・2 つの型の `removeEmptyState`・待ち合わせの closure 群・`write(_ bytes:)`)、誤検知は 4〜5 組 (SwiftUI の `body` の closure・`ObaketDebugAPIServer.routes` のハンドラの表 74 件)、判断が分かれるものが 2 組。**精度は約 6 割で、組が倍増する**。
- 費用: obaket (26,149 単位) の `report` が 1.06 秒 → 1.14 秒 (約 +8%)。
- 判断: **既定には入れない** (`-levels L1,L2,L1m` で明示)。理由は Swift の本番の組が倍増して半分近くがノイズになること。Go だけの repo なら既定に入れる余地はあるが、言語で既定を変えるほどの根拠 (新しい真の写し 1 件) はまだ無い。
- 実装: `norm.LM` (表示は `L1m`)。`Levels` には入れず、`ParseLevel` と hash の計算にだけ足した。`exclude` / `near` / `explain` は既定の段のまま。段の大小で判定していた箇所 (`>= L3` / `>= L2`) を、段を明示した判定に直した。
- 検証: `norm` のテスト (メンバー名だけが L1m で同じ・構造 / リテラル / 局所の名前の番号は畳まない・形に畳んだ印が出る・既定の段に入らない)、Go と Swift の端から端のテスト (既定では組にならず `-levels L1,L2,L1m` で組になる)。変異 6 本 (畳まない・L1 でも畳む・リテラルも畳む・式を畳む・`ParseLevel` が受けない・既定の段に入れる) で赤を確認。

### Swift について分かったこと (Go の 4 module では見えなかったもの)

- obaket は既定でも **213 組** (うち 98% はテストコード)、本番は 3 組。Go の module (0〜3 組) とは領域が違い、ここで効くのは**見落としを減らすことより、本番の組を優先して絞り込むこと**。
- 読めない構文のファイルが **57 件 (3.4%)** ある (tree-sitter の Swift 文法の限界)。これらは比較の外で、Go では起きていない Swift 固有の取りこぼし。
- 小さい単位を比べていない割合は 45% で lockman と同じ。下限を 12 にすると本番の組が 3 → 141 に増える。
- Go で測った再現率 (既定 19% → 調整して 86%) は Swift には転用できない。**Swift で同じ手順 (21 件の統合に相当する実測) を回すのは未実施**。


## 6 module の追加の実測 (2026-10-28、c4 と f5)

統合前の 4 module の次に、`schedkeys` / `serialrun` / `subproc` / `treefiler` / `tuikit` に回した (`sweep` は tuikit だけ。他は既定の report・`-min-nodes 12`・`near -similar` を別々に回した)。

| module | 統合 | 寄せなかった組と理由 |
|---|---|---|
| treefiler | 3 件 (a9f51e46): `historyPath`/`placesPath`/`configPath` → `configFile`、`contains`/`contains2` → `slices.Contains`、`newVirtPair` のクロージャ → `restampTree` | `near -similar` 0.556 の組はテストが production の計算を独立に再計算する形 |
| subproc | 0 | 6 組すべて `boundarycheck/testdata` の fixture (clean と waitdelay を別パッケージに並べる意図的な写し) |
| schedkeys | 0 | `keyMenu`/`keyPick` は循環と端で止まる違いが意図 (コメントが `listnav.MotionOf` との統合を禁じる)。`viewMenu`/`viewPick` は描く中身が違う |
| serialrun | 0 | 候補はどれも意図的、または既に共有済み |
| tuikit | 0 | `ScrollGlide.Offset`/`CursorGlide.Cursor` (doc が「別物」と明記)、`Exec.SetStdin/Stdout/Stderr` (型が違い helper が薄い wrapper を残すだけ)、`List.Advance`/`Pager.Advance` (相手の型が別で interface が要る)、`fastDispWidth`/`archKernel` (build tag が違う差分テスト用スタブ) |

見つけた方法の内訳 (treefiler の 3 件): 既定の report の L2 が 1 件、`-min-nodes 12` の L1 (と `near -similar` 1.0) が 1 件 (`contains`)、既定の report の L1 test が 1 件。**既定の report は 3 組中 2 組しか出さなかった**。

### 実測から出たフィードバック (未実装。採否は後で決める)

1. 連番の名前 (`contains` / `contains2`) を、末尾の数字を除いた名前の一致として拾う。
2. `testdata/` の fixture が、subproc の report の唯一の出力になる。既定で外すか「testdata」と印を付ける。
3. report 末尾の「小さい単位が N 個ある」に、**近い完全一致 (near 1.0) の数**を添える (`contains` はそこに隠れていた)。
4. 完全一致の組に **build tag / `GOOS` の違い**を出す (`fastDispWidth` / `archKernel` は tag が違うスタブで、別プラットフォームの写しと一目で分かる)。
5. `dupscan:allow` の理由つきの一覧を出す口 (次の人が組を読み直さずに済む)。tuikit の `sweep` 節 2 は 3 組中 3 組が「別物と書いてある」雑音だった。
6. `sweep` 節 4 (名前の束) は `is*` のような述語の束が雑音 (`markdown/ is* ×4` は入力の型が全部違う)。述語系の動詞 (`is`/`has`/`should`/`can`) を束から除くか、束のメンバーの引数の型が同じものだけに絞る。
7. `near -similar` は 517 単位 (treefiler) と 320 単位 (tuikit) で、満点か 0.4 台しか出ず、0.5 以上は 1 件ずつだった。実用の発見手段は report の方で、`sweep` 節 3 は 2 module とも「なし」。節 3 の効用は未確定 (Swift で回して判断する)。
