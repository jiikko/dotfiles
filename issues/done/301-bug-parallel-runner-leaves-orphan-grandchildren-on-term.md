# bug: 並列ランナーが中断時に子孫を看取らず、孤児プロセスと「消えたパスへの書き込み」が残る

起票日: 2026-09-06
カテゴリ: bug
優先度: 中（発火条件の一部が未実測。下記）
出典: /audit resource-leaks 2026-09-06（forge Minimum+）。私が独立に再現

## 何が起きているか

`scripts/run_make_targets_parallel.sh` は中断時に**一時ディレクトリだけ**を消し、
起こした子プロセスには何もしない。

```sh
outdir=$(mktemp -d) || ...
trap 'rm -rf "$outdir"' EXIT
trap 'rm -rf "$outdir"; exit 130' INT
trap 'rm -rf "$outdir"; exit 143' TERM     # ← kill が 1 つも無い

for t in "$@"; do
  ( $MAKE "$t" > "$outdir/$t.out" 2> "$outdir/$t.err"; echo $? > "$outdir/$t.rc" ) &
done
wait
```

TERM を受けるとランナーは `outdir` を消して抜けるが、`make` とその子（shellcheck / actionlint /
go toolchain）は**生き残り、既に消えたパスへ書き続ける**。

## 実測（2026-09-06、私が再現）

孫を起こす偽 make を 2 ターゲット分走らせ、ランナーへ `kill -TERM` を送った:

```
before: fakemake=2  grandchild=0
after TERM: fakemake=2  grandchild=4     ← 孤児 6 本が生存
```

`outdir` は trap で消えているので、生き残った子の `> "$outdir/$t.out"` は消えたパスへの書き込みになる。

## 推奨対応

- 冒頭に **`set -m`** を足して各バックグラウンドジョブを独立 pgid にし、trap で
  **`kill -TERM -"$pgid"`** を回す（実測で孤児 0 にできたのはこの形）
- **`outdir` の削除は子を看取ってから**（順序が逆だと「消えたパスへの書き込み」が残る）

## 🚨 採ってはいけない修正案（却下理由）

- **`kill -- -$$`**: このスクリプトは `#!/bin/sh` で `make` から呼ばれるため **`$$` は pgid ではない**
  （プロセスグループのリーダーは make 側）。運が悪ければ **make ごと、あるいは無関係なグループへ
  TERM が飛ぶ**
- **`kill $pids`（直接の子だけ）**: サブシェルしか殺せず、孫（実測 4 本）が残る

## 🚨 未実測（優先度をこれ以上上げる前に測ること）

監査の一次報告は「CI で確実に踏む」と断定していたが、**測ったのは bare TERM の挙動だけ**。
GitHub Actions の cancel / `timeout-minutes` が**プロセスツリーごと殺すのか単一 pid へ TERM なのか**は
未測定で、前者ならこの経路は CI で踏まない。手元での Ctrl-C / `kill` は確実に踏む。

## 検証

上の再現手順（偽 make が孫を起こす → ランナーへ TERM → `pgrep` で残存を数える）をテストにする。
**判定は rc ではなく残存プロセス数**で、上限つきポーリングで待つ
（[`avoid-wall-clock-assertions.md`](../../_claude/rules/avoid-wall-clock-assertions.md)）。
変異検証は `kill -TERM -"$pgid"` を消すと残存が 0 でなくなって red。

## 対応済み（2026-09-07 / commit ffc3af01）

`scripts/run_make_targets_parallel.sh` に `set -m` を入れて各ターゲットを独立した
プロセスグループで起こし、`kill -TERM -"$pgid"` で子孫ごと回収するようにした。

- `PGIDS` に `$!`（= `set -m` 下では pgid）を積み、`reap` が group 単位で TERM を撃つ
- `trap` を EXIT / INT / TERM の 3 本にし、INT は 130、TERM は 143 で返す
- `wait` 完走後は `PGIDS=""` にして、正常終了時に自分の子を撃たない

### 変異検証

- `reap` の `kill -TERM "-$pg"` を削除 → **red**（Test 7 の孫プロセス生存チェックが落ちる）
- Test 7 は `fake-make-tree`（孫を産む make の代役）を使い、`exec` でシグナルを実体へ届かせている
