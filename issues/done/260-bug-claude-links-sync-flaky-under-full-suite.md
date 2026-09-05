# test_claude_links_sync.sh が `make test` の通しで 1 度だけ落ちた (原因確定・修正済み)

起票日: 2026-09-05
カテゴリ: bug
優先度: 低 → **決着 2026-09-05** (下の「原因と修正」節)

## 観測 (1 回だけ)

2026-09-05 10:13、master `27df01a0` で `make test` を通したとき、並列腕で落ちた。

```
[FAIL] tests/claude/test_claude_links_sync.sh
NG: 並行 apply #1.1 が非 0:
linked: .../parallel/.claude/agents/a1.md -> .../parallel/dotfiles/_claude/agents/a1.md
linked: .../parallel/.claude/commands/c1.md -> ...
linked: .../parallel/.claude/rules/r1.md -> ...
refused: .../parallel/.claude/hooks/h1.sh は symlink でない実ファイル。上書きしない (手で退避してから ./setup.sh)
linked 3
FAIL: 1 件
```

ケース 15 (並行 apply 3 本同時 × 5 回) の 1 回目。`scripts/claude_links.sh:109` の
`[ -e "$link" ] && [ ! -L "$link" ]` が真になった = **h1.sh が「存在するが symlink でない」と
観測された**。

🚨 **辻褄が合わない**。テストは直前に `ln -sfn <存在しない gone.md> h1.sh` で **壊れた symlink**
にしており、壊れた symlink なら `-e` は偽になる。3 本の apply が張り直す先
(`$DOT/_claude/hooks/h1.sh`) は実在するので、張り終えていれば `-e` 真 / `-L` **真**で refused には
ならない。BSD `ln -sfn` の unlink → symlink の窓に入っても、見えるのは「不在」であって
「実ファイル」ではない。**どの経路で実ファイルに見えたのかが未解明**。

## 再現の試み: 合計 55 回、すべて緑

| 方法 | 試行 | 結果 |
|---|---|---|
| CPU 負荷 (論理コア数 × 2 のビジーループ) 下で単独実行 | 10 回 | 0 件 |
| テストを 14 本同時に実行 | 3 ラウンド × 14 = 42 回 | 0 件 |
| `make test-discovered` (実際に落ちた経路。並列腕 + 直列腕) | 3 回 | 0 件 |

**同じ手を繰り返さないこと** (このリストが全数勘定)。

## 原因について言えること / 言えないこと

- ❌ **「`59f9e48c` の並列化が原因」とは言えない**。起票者は当初そう報告したが、上表のとおり
  並列腕を 3 回通しても再現せず、**根拠が無い**。並列化以前にこのテストが落ちた記録も無いが、
  「1 回だけ観測された事象」に対して 3 回の緑は反証として弱い
- ⚠️ このテストは**元から負荷に敏感**ではある: `e7678f37 fix(claude): 並行 apply の状態確認を
  最大 5 回やり直す (CI で残っていたレース)` / `b2f8ede1 ... 並行 apply の偽 failed を状態判定で
  消す (issue 160 の 2 周目)` と、同じ場所が 2 度直されている
- ⚠️ 観測時、**同一マシンで別セッションが `make test` を並行実行していた** (dotfiles-c6)。
  ただし各テストは `mktemp -d` の独立した WORK を使うため、共有経路は見つかっていない。
  14 本同時の再現試行はこの条件を模したもので、再現しなかった

## 対応 (この issue で入れたもの)

**推測に基づく防御コードは足さない** (`_claude/rules/instrument-before-second-fix.md`)。
代わりに、次に出たときに切り分けられるよう**観測だけ**足した:

- `tests/claude/test_claude_links_sync.sh` のケース 15 の失敗時に `par_state()` で
  `ls -l` の実状態 (h1.sh / r1.md / a1.md / c1.md) を出す。symlink / 実ファイル / 不在が 1 行で
  区別でき、`-e` / `-L` の判定と突き合わせられる
- 変異検証済み: 判定を必ず失敗させる変異 (`rc=0` → `rc=999`) を当て、`--- 実状態` の行と
  `ls -l` の出力が出ることを確認 (出なければこの issue は次も何も分からないまま閉じる)

## 次に出たときにやること

1. `--- 実状態` の行を読む。`h1.sh` が本当に実ファイルなら **誰が作ったか**を追う
   (apply は symlink しか作らない = 第三者が書いている疑い)
2. symlink だったなら、`-e` / `-L` の観測と `ls -l` の観測のあいだに状態が動いている
   = 判定を 1 回の `stat` に寄せる修正 (`[ -e ] && [ ! -L ]` の 2 回 stat をやめる) を検討する
3. 再現条件が固まるまで `scripts/claude_links.sh` を触らない

## 関連

- `59f9e48c` — test-discovered の並列化 (観測はこの後に出た。因果は未確認)
- issue 259 — この件を「並列化が負荷経由で既存の競合テストを壊す筋」として参照している
- issue 160 / `e7678f37` / `b2f8ede1` — 同じケースの過去 2 回の修正

## 原因と修正 (2026-09-05)

**「辻褄が合わない」は前提の読み違いだった。** `[ ! -L "$link" ]` は **不在に対しても真**になる。
経路はこう:

1. apply A が `h1.sh` (壊れた symlink) を `ln -sfn` で有効な symlink に張り直す
2. apply B が `[ -e "$link" ]` を評価 → 有効な symlink なので**真**
3. apply C の `ln -sfn` が unlink → symlink の **unlink 側**を実行 (BSD ln -f は非アトミック)
4. apply B が `[ ! -L "$link" ]` を評価 → 不在なので**真** → 「symlink でない実ファイル」で refused

つまり `-e` と `-L` の **2 回の lstat のあいだ**に相手の unlink が挟まる TOCTOU。「壊れた symlink なら
-e は偽」は正しいが、B が見た時点では A が既に張り直していた。

**再現**: プリミティブだけを取り出して 8 秒の競合を作ると、`[ -e ] && [ ! -L ]` は
**861,981 回中 1,718 回 (0.2%)** 「実ファイル」を返した。1 回の lstat (`stat -f %HT`) に替えた版は
同条件で 0 回 (5,132 回。サブシェル fork ぶん試行数は少ない)。単一 syscall は中間状態を原理的に
見ないので、試行数の差は結論を動かさない。

**修正** (`scripts/claude_links.sh`): 種別判定を `stat -f '%HT'` の 1 回に寄せた
(`'' | 'Symbolic Link'` 以外を実ファイルとして refused)。macOS 専用 repo なので BSD stat で書いている。
**回帰ガード** (`tests/claude/test_claude_links_sync.sh` ケース 15b): 挙動では pin できない
(窓がマイクロ秒で決定的に作れない) ので、2 回 stat の形が戻っていないことを静的に pin する。
変異 (2 回 stat に戻す) で red を確認。実ファイルの refused 自体はケース 6 が守っている。

「次に出たときにやること」の 2 (1 回の stat に寄せる) を先に実行した形。1 (第三者が書いた疑い) は
不要になった (apply 自身の判定の穴だった)。
