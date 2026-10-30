# 1010 (bug): claude-md-compact の fidelity.py が rename したファイルを照合できない

起票日: 2026-10-30

## 概要

`_claude/skills/claude-md-compact/fidelity.py` (SKILL.md の Step 4 の関門) は、前の版と今の版を**同じパス**で読む
(`base_text` は `git show <base>:./<path>`、`now_text` は同じ `<path>` のファイル)。縮めるついでにファイルを移した・改名した
(`git mv`) 場合、どちらのパスを渡しても照合できない。

## 再現 (2026-10-30)

`_claude/issue-rules.d/move-report-conclusions-to-issues.md` を `_claude/rules/` へ `git mv` し、本文の節を消した後に関門を回した。

```sh
python3 _claude/skills/claude-md-compact/fidelity.py --base HEAD \
  --dest _claude/rules-rationale/move-report-conclusions-to-issues.md -- \
  _claude/issue-rules.d/move-report-conclusions-to-issues.md _claude/rules/move-report-conclusions-to-issues.md
# → ✗ HEAD:_claude/rules/move-report-conclusions-to-issues.md を読めない   (rc=2)
```

- 新しいパスは base 側に無いので、`main` の「対象の base が読めない」で rc=2 になる
- 古いパスだけを渡すと、`now_text` が None を返したまま数え進み、**全部消えたと報告して rc=1** になる (反証レビューが一時 repo で再現: 消えた部品 2 件・読む候補 3 件、
  どれも「最も近い行: (無し)」)。黙っては通らないが、偽の赤で、移した先を読まない。両方のパスを渡すと、先に上の rc=2 で止まる
- 回避: 使い捨ての git repo に、前の版を**新しいパスに置いて** commit し、今の版で上書きしてから関門を回した。結果は消えた部品 9 件・読む候補 20 件で、
  ルール節に無かった 2 点を見つけた (この関門が要る場面だった)。毎回この手間がかかる

## 対応方針

1. **対象を `旧パス=新パス` で渡せるようにする**: 前の版は旧パスで、今の版は新パスで読む。`--dest` の側も同じ形を受ける (移し先を改名した場合)
2. **今の木に無い対象は数え進めない**: 古いパスだけを渡して今の木に無いときは、rc=2 と「改名したなら `旧=新` で渡す」を出す (今は全部消えたと読んで rc=1)。
   新しいパスだけを渡したとき (今も rc=2) にも同じ案内を足す

採らない: rename を `git diff -M` で自動で見つける案。pathspec を新パスだけにすると旧パスの削除が範囲の外になり `A` としか出ない。
両方のパスを渡しても、本文を縮めたファイルは類似度が閾値 (`-M30%` でも) に届かず `D` と `A` に分かれる (反証レビューの一時 repo で確認)。
縮めたファイルこそこの関門の対象なので、補助にもならない。再提起するなら、縮めたファイルで rename と判定される実測を添える

## 受け入れ条件

- [ ] `git mv` した fixture で、`旧=新` の形が前の版と今の版を照合し、消えた部品・読む候補が改名しない場合と同じ結果になる (`tests/claude/test_claude_md_compact_fidelity.sh` に足す)
- [ ] 古いパスだけ・新しいパスだけを渡したときに、rc=2 と `旧=新` の案内が出る (古いパスだけで「全部消えた」の rc=1 にならない)
- [ ] `旧=新` の範囲で、旧パスと新パスを 1 本の対象として数える (部品を 2 回数えない・照合先に旧パスの空の版を入れない)。`--dest` に `旧=新` を渡したとき、ディレクトリの列挙 (`expand` の `ls-tree`) とずれない
- [ ] 足したテストに、`旧=新` の解決を外す変異を当てて赤を確認する
- [ ] SKILL.md の Step 4 に、改名したファイルの渡し方を 1 行足す

## 関連ファイル

- `_claude/skills/claude-md-compact/fidelity.py` (`base_text` / `now_text` / `main`)
- `_claude/skills/claude-md-compact/SKILL.md` (Step 4)
- `tests/claude/test_claude_md_compact_fidelity.sh`

## 反証レビュー (2026-10-30、sonnet 1 体、読み取りのみ。一時 repo で git mv して実行)

- P1 (採用): 自動検出 (旧方針 2) は動かない → 「採らない」へ移した
- P2 (採用): 古いパスだけの挙動は未確認ではなく、全部消えたと読む rc=1 / 新しいパスだけの rc=2 は既にある (足すのは案内) / `旧=新` の範囲の数え方と `--dest` の期待値が無い → 再現と受け入れ条件に足した
- P3: 既存の rename の扱いは fidelity.py にもテストにも無い (grep で 0 件)
- 反証できなかった: 新しいパスだけで rc=2・両方で先に rc=2・`now_text` が旧パスで None
