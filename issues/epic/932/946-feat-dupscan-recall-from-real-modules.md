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

1. **「1 語だけ違う写し」を既定の探索の段に入れる**: L1 の形のうち、メンバー名・外部 package の識別子 (`os.Stdout` と `os.Stderr`) だけが違う組を、別の段 (例 `L1m`) として report に出す。評価は、4 module の `captureOutput` のような既知の組を拾い、`Kind.String` / `Key` のような別の規則を何件誤って出すかを数える。895 の「全体 group 化は不採用」とは別で、`near -similar` の全単位走査ではなく、メンバー名を畳んだ hash の一致だけを見る (費用は L2 と同程度)。
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
- [ ] 提案 1 の評価: 4 module で `L1m` 相当を試し、拾える既知の組と誤検知の数を表にする。
- [ ] 提案 2・3 は、1 の結果と、使われ方 (941 / 943 の測定) を見てから採否を決める。
