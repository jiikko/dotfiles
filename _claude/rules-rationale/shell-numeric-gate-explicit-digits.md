# shell-numeric-gate-explicit-digits の根拠・起源・実例

## 起源: codex-fanout の timeout 検証 (2026-09-01, issue 150)

manifest に行単位 `timeout_s` 列を足したとき、既存の env 検証と同じイディオム
`case "$v" in *[!0-9]*) die ...` を複製した。敵対レビュー (read-only subagent 2 lens) が
再現手順つきで 2 経路の素通りを示した:

```sh
# 1) ロケール照合: ja_JP.UTF-8 では全角数字が [0-9] に収まる (実測)
LC_ALL= LANG=ja_JP.UTF-8 bash -c 'case "３" in *[!0-9]*) echo reject ;; *) echo PASS_THROUGH ;; esac'
# → PASS_THROUGH
LC_ALL= LANG=ja_JP.UTF-8 bash -c 'case "３" in *[!0123456789]*) echo reject ;; *) echo pass ;; esac'
# → reject (明示列挙はコードポイント一致なので照合順の影響を受けない)

# 2) 桁あふれ: bash の [ ] は intmax_t (19 桁 ≈ 9.2e18) を超えると評価エラー = 偽
bash -c '[ 0 -ge 9999999999999999999 ]'  # → integer expected, rc=2
```

どちらも通過後は watchdog のループ内 `[ "$slept" -ge "$run_timeout" ]` が毎回偽になり、
**die もせず kill もせず hang した codex を永久に待つ**。エラー行は per-run の `.log` にしか
出ないため、driver 側からは「まだ走っている」と区別できない (壊す lens が stub で実証:
global timeout 5 秒の環境で 8 秒後も driver・stub・孫プロセスが全部生存)。

## ゲートとフォールバックで扱いを分ける理由

このとき repo 内の `*[!0-9]*` は 25 箇所あったが、codex-fanout の 2 箇所以外はすべて
「非数値 → 安全な既定値へフォールバック」用途だった (tmux 出力・date・pid が入力源で、
悪い値が来ても既定値に落ちるだけ)。ゲート (die/reject) は「通った値は健全」という契約を
下流に配るので、素通りが機構の無音死に直結する。この非対称が、ルールの対象をゲートに
絞った理由 (一括書き換えは複雑性を動かすだけで、リスクの大きさに比例しない)。

## テストの最弱部

もともと書いていた非数値テストは `abc` だけで、ガードが主張する防御範囲の最弱部
(ロケール依存・桁あふれ) を踏んでいなかった。回帰テストは
`tests/codex_fanout.bats` の「全角数字 (ロケール照合) と 19 桁 (桁あふれ) の timeout も
起動前に弾く」が正本で、変異 2 本 (明示列挙→範囲式 / 桁上限除去) の red を確認済み。

## 関連の実測記録

- [`adversarial-review-own-safeguards.md`](adversarial-review-own-safeguards.md) の
  「実測 4 回目」節 — 変異 all-red の後に敵対レビューがこの 2 件を出した経緯
- issue: `issues/done/150-codex-fanout-timeout-for-adversarial-lenses.md` の「対応」節
