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
- [x] 実運用での採否: f5 の所見のうち、連番の名前・testdata の印 (sweep)・小さい単位の組の数 (report)・同名メソッドの束・述語の除外を実装。build tag の表示・allow の理由一覧・`near -similar` の順位は見合いが低く見送り (理由は下の各項)。

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

1. (実装済み) 連番の名前 (`contains` / `contains2`) を、末尾の数字を除いた名前の一致として `sweep` 節 4 に出す。
2. (`sweep` で実装済み。`report` は未変更) `testdata/` の fixture が、subproc の report の唯一の出力になる。`sweep` はメンバーが全部 testdata の下の組を `testdata` と印を付けて出す。`report` の既定の対象から外す案は、report の意味が変わるので採らない。
3. (実装済み) report 末尾の「小さい単位が N 個ある」に、**同じ形の組が下限 12 節で N 組見える**ことを添える (`contains` はそこに隠れていた。near 1.0 のうち本体が同じ形のものだけで、1 文違う近い写しは数えない)。
4. (未実装: 単位が build tag を持たず、ファイルの先頭行を読む実装が要る。見合いが低い) 完全一致の組に **build tag / `GOOS` の違い**を出す (`fastDispWidth` / `archKernel` は tag が違うスタブで、別プラットフォームの写しと一目で分かる)。
5. (未実装: 必要が出たら) `dupscan:allow` の理由つきの一覧を出す口 (次の人が組を読み直さずに済む)。tuikit の `sweep` 節 2 は 3 組中 3 組が「別物と書いてある」雑音だった。
6. (実装済み) `sweep` 節 4 (名前の束) は `is*` のような述語の束が雑音 (`markdown/ is* ×4` は入力の型が全部違う)。述語系の動詞 (`is`/`has`/`should`/`can`) を束から除くか、束のメンバーの引数の型が同じものだけに絞る。
7. `near -similar` は 517 単位 (treefiler) と 320 単位 (tuikit) で、満点か 0.4 台しか出ず、0.5 以上は 1 件ずつだった。実用の発見手段は report の方で、`sweep` 節 3 は 2 module とも「なし」。節 3 の効用は未確定 (Swift で回して判断する)。

### f5 の grep 突き合わせ (treefiler / subproc / tuikit。2026-10-28)

dupscan が出さなかったが grep なら見つかった写しは 2 つだけで、どちらも寄せる価値が低かった (取りこぼしというより順位と束の定義の話)。
1. `gitWatch.take` と `walker.take` (版つき手渡し: lock → 比較 → 代入 → return)。間に 2 文が挟まるので完全一致の段に入らず、`near -similar` は 0.444 で他の 0.4 台に埋もれた。**未対応**: Lock/defer Unlock の枠を除いて比べる近似。実例が 2 つで共有できるのは 3 行・並行処理の状態を触るため、寄せる価値が低く、実装の見合いが無い。
2. `take` ×5 / `busy` ×3 のような同名のメソッドは、先頭の語で束ねる節 4 に出なかった。**対応済み**: 同じ dir に同名で受け口の型が 2 種類以上のメソッドが 3 個以上 (節の数が 2 倍以内) あれば、節 4 に `take (同名のメソッド) 受け口 ×N` として出す。標準の interface の名前 (`String` `Error` `Close` ほか)・4 文字未満の名前は除く。述語 (`is*` `has*`) も束から除いた。
3. 標準ライブラリの再実装 (`tfprobe` の `writeFile` は `os.WriteFile` の手書き) は、dupscan が repo 内の写しだけを比べるため出ない。さらに `os.WriteFile` は forbidigo が禁止していて手書きは意図的 (置き換えると lint が落ちる)。dupscan で扱わない。

### 実装後の再実測 (f5、2026-10-28。treefiler / tuikit / zundamon-kaisetsu / subproc)

- **同名メソッドの束**: treefiler の `take` ×5 だけが出た (他 3 module は 0)。意味が同じなのは 2 つ (gitWatch / walker)、残りは別物。雑音は少ないが、**実際に寄せられた例は 0 件で、役に立つと言える根拠はまだ無い**。`busy` ×3・`start` ×3 は出ない (節の比が 3 倍を超える等。理由は未確認)。
- **連番の名前**: zundamon の `Round` / `Round3` が出たが、整数への丸めと小数 3 桁への丸めで別の仕事 (偽陽性)。→ 節の数が 1.5 倍以内の連番だけに絞った (数字が桁数・版を表す名前は大きさが違う)。`contains` / `contains2` は先に寄せたので実例の確認はできていない。
- **testdata の印**: subproc の 2 組に `[L1] testdata` が付き、意図的な写しと一目で分かった。有用。
- **report の添え書き**: 「うち同じ形の組が N 組」は treefiler 2・tuikit 1・zundamon 3・subproc 1 で実数と合い、`contains` を隠していた問題は解消した。

### obaket (Swift・13122 単位) への `sweep` (f5、2026-10-28)

- **問題**: 節 2 の `L1m` が `init` ×118 / routes の closure ×74 / `init` ×77・×49 のような決まりきった群で埋まり、重み順の先頭 4 つを占めた。実在した `handleScreenMove*` (`L1m` ×3・節 141) は 427 行目に沈んだ。
- **修正** (af8b9787): 20 個以上の群を後ろへ回し、先頭 5 件だけ出して「… 他 N 件」に畳む。境界 (19 と 20) のテストと変異 4 本 (閾値 21 / 19・畳み外し・並べ替え外し) で赤を確認。obaket の出力は 555 行 → 220 行、`handleScreenMove*` は 427 行目 → 48 行目に上がった。
- **統合 1 件** (obaket 8f0674bc5): `handleScreenMoveResume` / `ResumeBanner` / `Discard` (`L1m` ×3・節 141。既定の report と節 1 には出ず、節 2 で見つかった) を `runScreenMoveClaimed` に寄せた。結果型 3 つは `toJSON()` 以外の共通契約が無いのでファイル内 protocol で束ねた。`discard/request` は claim を取らない別形なので寄せず。`make lint` / `make test` は rc=0。**訂正 (f5、2026-10-28)**: 上の「HTTP 経由のテストは無い・変異は未実施」は誤りだった。`DebugAPIRouteContractTests` に `guardedRoutesRejectOutsidePrefixOverHTTP` (3 route を含む 4 route を 403 で検査) と `screenMoveResumeHandlerBehavior` (resume の handler) がある。`bin/mutate` で helper の guard を `{ _ in true }` にする変異を当てると RED (落ちたのはその 2 本、実行 62 件は baseline と一致。復元済み)。**未確認**: discard / banner の handler の成功時の応答を見るテストは、resume の分しか見ていない。
- **寄せなかった組**: `ProviderForm.hasStoredSecret` / `needsSecretToUpdateCredential` (`L1m` ×2・節 134) は obaket の issue 1226 に「寄せると嘘の表示が出る」とある別の話。`DropboxClient` の closure 組と adapter の `capabilities` ×6 / `transferWireCapabilities` ×5 は、過去の epic で「寄せる / 寄せない」を決め済みの残りで、節 1 に出続ける (`dupscan:allow` の印が付いていない)。
- **節 3 は取りこぼし**: 「単位 6845 が上限 3000 を超えたので飛ばした」で何も出ない。総当たりの所要は未測定。
- **所見**:
  - 判断済みの組が節 1 に毎回出る。理由つきの許可 (`dupscan:allow`) を repo 側に持つ手段が obaket では使われていないので、sweep の出力に「許可印の付け方」を 1 行添える、または report が許可済みの数を出すと、残りが見える (**未実装**)。
  - 節 4 は 22 束で、`showError` ×10 / `setBrowser` ×6 / `dismiss` ×5 (presentation の同名メソッド) が出た。手がかりとしては良いが、**実際に寄せた例は 0 件**。
  - 巨大 module 1 回は 0.67 秒 (13122 単位)。速度は問題ない。

### 節 3 の所要の実測と上限の見直し (c4、2026-10-28)

- **実測**: dotfiles の `src` (単位 8705・節 3 の対象 6128) で、`sweep` は上限 3000 のままだと 0.56 秒で節 3 を飛ばし、`-similar-max-units 9000` だと **5.0 秒** (総当たりは約 4.5 秒)。出力は 59 対で、glogx のテストの `fakeRunner` ×2 (Dice 1.000)・`realIssuesView` ×2 (1.000) のように、**組にならなかった完全一致に近い写し**が先頭に並んだ (どれも寄せるかは未確認)。
- **変更**: 既定の上限を 3000 から **8000** に上げた (obaket の 6845 単位は入る見込みだが、obaket では未測定)。計算量は単位数の 2 乗なので、上限を上げるほど遅くなる。
- **許可印の案内**: f5 の提案どおり、`sweep` の先頭に「`// dupscan:allow <false-positive|intentional> <理由>` で静かになる」の 1 行を添えた。
- **見送りで閉じる項目** (理由つき): 完全一致の組への build tag の表示 (単位が build tag を持たず、ファイル先頭を読む実装が要る。見合いが低い)。`allow` の理由つき一覧 (必要が出たら)。`near -similar` の順位 (満点か 0.4 台に偏り、`sweep` 節 3 の方が実用的)。版つき手渡しの Lock 枠を除く近似 (実例が 2 つ)。
