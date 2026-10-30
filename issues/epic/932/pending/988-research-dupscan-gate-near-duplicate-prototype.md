# 988 (research): gate に変わった単位だけの近似比較の段を、索引なしで試作して測る


起票日: 2026-10-30

> 保留 (2026-10-30): dotfiles では近似の段だけが見つける写しが 0 件で、効き目も誤検知の率も測れなかった。**再開の条件**: obaket の checkout があるマシンで、下の「再開の手順」の計測を回せるとき

> 2026-10-30: 902 と 988 を合流させた形 (変わった単位だけを既存の全単位と近似照合する) の検討と、それに欠けている前提を [1009](../1009-design-dupscan-post-write-body-similarity-candidates.md) に書いた。

## 概要

986 の改善 3。量産の主な形 (少し変えた写し・既存の小さい helper の本体の手書き) は gate に入っていない (946: 寄せた 21 件のうち既定の report が出したのは 4 件)。
sweep には近似の判定 (節 3 の部分木の Dice・節 5 の手書きの展開) が既にある。gate の差分で変わった単位だけに当てる経路を、索引なしで試作し、時間と誤検知の率を測ってから入れるかを決める。

## 測ること

- 時間: 変わった単位 N × 既存の単位 M。dotfiles の `src` (単位約 8,700) で、N が小さい普通の commit と、N が跳ねる commit (移動・生成・一括の rename) の両方。
  足してよい時間の上限を先に決める。参考: 977 の obaket の 69 push の実績で gate の中央値 44 秒 (pin 3 版・入力の変化が混ざり、同じ条件のベンチではない。今の dotfiles の CLI が常に 44 秒かかる証拠ではない、と 977 自身が書いている)
- 誤検知の率: 過去の commit を流して出た候補を 1 件ずつ「寄せるべき / 意図した差」に仕分ける。母集合 (どの repo の、どの期間の、何件) を測る前に決める
- 取りこぼし: 946 で寄せた 21 件のうち、この段が出せたものの数

## 入れ方の案 (測った後に決める)

- はじめは報告だけ。止める (rc=1) に上げる条件 (誤検知の率の閾値) と、報告だけの期間と見直しの日を決める (報告だけの段は読まれずに積み上がる)
- テスト関数の中は、はじめは見ない (fixture の似た形で誤検知が増える見込み。965 F1)
- 自分で作る検査なので、名指しする退行 (少し変えた写しを足す・helper の本体を手で展開する) を当てて赤を確認し (`adversarial-review-own-safeguards.md` §1.5)、敵対的レビューを最終ゲートにする

## 反証レビュー (2026-10-30、sonnet、読み取りのみ。987〜989 をまとめて 1 体)

P1 なし。
- P2 (採用): 「CI の gate は中央値 44 秒」は 977 の obaket の実績で、同じ条件のベンチではない → 出典の範囲を書いた
- 反証できなかった: hook の引用、`index -returns` / `-param-type` の実在、899 の事実 (x/tools の版・IllTyped・所要は未測定)、21 件中 4 件、単位約 8,700、ユーザー決定の書き方

## 結果 (2026-10-30、dotfiles)

- 対象: dotfiles の `origin/master` で `src/*.go` を触った直近 30 commit (merge を除く)。各 commit を使い捨ての worktree に出し、`git diff -U0 <c>^ <c>` の行と
  `dupscan index -file` の範囲が重なる関数を「変わった単位」とし、それぞれ `dupscan near -similar -similar-threshold 0.6 -limit 5 <file>:<name> src` を当てた
- 本番の関数: 変わった単位 327 (24 commit)。候補が出たのは 1 件 (`src/glogx/browse_anim.go:toastTimers` と `src/pro-con/ui/toast.go:toastTimers`、Dice 1.0)。
  これは glogx の分割の commit (「browseModel のメソッドを責務ごとの browse_*.go へ移す」) で移動しただけの**既存の写し**で、既定の report が既に L1 の組として出している。
  近似の段だけが新しく見つける写しは **0 件**
- テストのファイル: 変わった単位 31 (テスト関数そのものは dupscan の比較の対象外で、helper だけ)。候補 0 件
- 計測の経路の空振りでないことの確認: 既知の似た組 (`src/tuikit/anim/glide.go:Offset` と `Cursor`) は同じ経路で候補 1 件として出た
- 所要: 1 単位あたり中央値 0.30 秒 (最大 0.69 秒、比べた単位は約 8,975)。1 commit あたりの変わった単位は中央値 5・最大 167 (glogx の分割)。
  単位ごとに `near` を打つ形では最大の commit で 49 秒かかる。gate に入れるなら、変わった単位をまとめて 1 回の走査で比べる形が要る (費用の見積もりで、未実装)

### 結論

- **dotfiles では、この段の効き目も誤検知の率も測れない**: 写しがほとんど生まれていない (近似の段だけの候補が 0 件なので、寄せるべきもの・意図した差の仕分けも 0 件)
- gate にはまだ入れない。写しの実例がある obaket (946・954) で同じ計測をしてから決める。obaket の checkout はこのマシンに無い
- 測っていないもの: 946 で寄せた 21 件のうち、この段が出せた件数 (寄せる前の版が要る)

### 再開の手順 (obaket の checkout があるマシンで)

下の script を `tmp/<作業名>/probe.py` に置き、`python3 probe.py <repo> <使い捨ての worktree> <出力.json> 30` で回す (Go 用。Swift の repo では diff の `src/*.go`・`_test.go` と、`near` の起点の `src` を対象の言語と dir に替える)。
出力の候補を 1 件ずつ「寄せるべき / 意図した差 / 既存の写しの移動」に仕分け、誤検知の率と、近似の段だけが見つけた件数を本文に足す。

```python
#!/usr/bin/env python3
"""988 の試作の計測: commit ごとに変わった Go の関数を出し、それぞれ near -similar を当てて所要と候補を記録する (使い捨て)。"""
import json, re, subprocess, sys, time, os
repo, wt, out_path, n = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
def git(*a, cwd=repo):
    return subprocess.run(["git", "-C", cwd] + list(a), capture_output=True, text=True).stdout
commits = git("log", "--format=%H", "-n", str(n), "--no-merges", "origin/master", "--", "src/*.go").split()
RANGE = re.compile(r"^(\S+?):(\d+)-(\d+)\s+(?:[A-Za-z_][A-Za-z0-9_]*\.)?([A-Za-z_][A-Za-z0-9_]*)\(")
rows = []
for c in commits:
    git("checkout", "-q", "--detach", c, cwd=wt)
    diff = git("diff", "-U0", c + "^", c, "--", "src/*.go", cwd=wt)
    changed = {}  # file -> set(lines)
    cur = None
    for line in diff.splitlines():
        if line.startswith("+++ b/"):
            cur = line[6:]
            if cur.endswith("_test.go"):
                cur = None
        elif line.startswith("@@") and cur:
            m = re.search(r"\+(\d+)(?:,(\d+))?", line)
            s, k = int(m.group(1)), int(m.group(2) or 1)
            changed.setdefault(cur, set()).update(range(s, s + max(k, 1)))
    units = []
    for f, lines in changed.items():
        if not os.path.exists(os.path.join(wt, f)):
            continue
        idx = subprocess.run(["dupscan", "index", "-file", f, "-limit", "0", "-doc-bytes", "0", "src"], cwd=wt, capture_output=True, text=True).stdout
        for l in idx.splitlines():
            m = RANGE.match(l)
            if m and m.group(1) == f and lines & set(range(int(m.group(2)), int(m.group(3)) + 1)):
                units.append("%s:%s" % (f, m.group(4)))
    for u in sorted(set(units)):
        t0 = time.monotonic()
        r = subprocess.run(["dupscan", "near", "-similar", "-similar-threshold", "0.6", "-limit", "5", u, "src"], cwd=wt, capture_output=True, text=True)
        dt = time.monotonic() - t0
        head = r.stdout.splitlines()[0] if r.stdout else ""
        m = re.search(r"candidates=(\d+)", head)
        cands = [l for l in r.stdout.splitlines()[1:] if re.match(r"^\d\.\d{4} ", l)]
        rows.append({"commit": c[:9], "unit": u, "rc": r.returncode, "sec": round(dt, 2), "candidates": int(m.group(1)) if m else None, "top": cands[:5], "err": r.stderr[:200]})
    print(c[:9], len(set(units)), "units", flush=True)
json.dump(rows, open(out_path, "w"), ensure_ascii=False, indent=1)
print("done", len(rows))
```

## 関連

- 986 (設計の地図)・946 (取りこぼしの実測)・965 F1 (テスト関数の中)・977 (CI の gate の費用)・sweep の節 3 と節 5 (`src/dupscan/README.md`)
