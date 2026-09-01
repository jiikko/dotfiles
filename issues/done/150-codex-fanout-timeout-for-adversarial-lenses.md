# 150 codex-fanout: 敵対レビュー lens が既定 timeout (rc=143) で落ちやすい

- 起票: 2026-09-01 (旧番号 148。glogx doctor の 148 と重複していたため、pushed commit からの
  参照が少ない本 issue 側を 150 へ改番 — 2026-09-01)
- 起源: obaket issue 651/635 の codex-drive セッション。敵対レビュー系の lens (luna-max・
  「反証を構築せよ」型) が **3〜4 回 rc=143 (SIGTERM = CODEX_FANOUT_TIMEOUT 超過)** で落ちた。
  発見型レビューや実装 run はほぼ落ちない — 反証構築は思考時間が長い

## 症状

- fanout の exit 2 (一部失敗)。runs.tsv で当該 lens だけ rc=143
- 単発再実行 (timeout 無し / 長め) では毎回完走し、内容も有用だった (= モデルでなく時間の問題)

## 対応候補 (どれか)

1. `CODEX_FANOUT_TIMEOUT` の既定を引き上げる (現行値を確認して 1.5〜2 倍)
2. manifest 行ごとに timeout を指定できるようにする (敵対 lens だけ長く)
3. rc=143 の行を driver が自動で 1 回だけ再実行する (retry は 143 限定)

## 受け入れ条件

- 敵対 lens 3 本の fanout が通常の 651 級タスクで rc=143 を出さない (または自動回収される)
- 変更後の既定値/挙動を `bin/codex-fanout` の usage と codex-drive SKILL.md の該当箇所に反映

## 対応 (2026-09-01)

**候補 2 を採用**: manifest に任意の第 6 列 `timeout_s` を追加し、その行だけ timeout を上書き
(省略行は `CODEX_FANOUT_TIMEOUT`、既定 1200 のまま)。watchdog を per-run timeout 化。

- 採用理由: 反証構築の遅さは lens の性質で決定論的 (単発再実行では毎回完走 = flaky ではない)
  なので、行の属性として manifest に置くのが構造的
- 候補 1 (既定引き上げ) 却下: 全 run の hang 検出まで一律に遅くなり、敵対 lens が上げ幅を
  超えれば再発する
- 候補 3 (rc=143 自動再実行) 却下: kill 済み run の API 消費が捨てになり壁時計も倍。同じ
  timeout での再実行は決定論的に再び 143 になる公算が高い
- ドキュメント反映: `bin/codex-fanout` usage / codex-drive SKILL.md「並列起動の作法」手順 2 と
  `[3.6]` (敵対 lens 行に 2400 を付け、900 秒超なので detach 起動)

敵対レビュー (read-only subagent 2 lens。codex 不使用方針のため代替) の指摘と採否:

- **採用 P1 — ロケール照合の素通り**: `*[!0-9]*` の範囲式は `ja_JP.UTF-8` (この開発機の既定)
  の照合順で全角数字を通し、通過後は `[ -ge ]` が integer expected で常に偽 = **watchdog が
  無音で無効化** (die せず hang を永久に待つ)。明示列挙 `*[!0123456789]*` に変更
  (env 側 `CODEX_FANOUT_TIMEOUT` の既存同型も同時修正、`validate_timeout` へ共通化)
- **採用 P1 — 桁あふれ**: 19 桁以上の純数字も `[ -ge ]` が integer expected で同じ無音無効化。
  桁数上限 9 (≈31 年) で起動前に拒否
- **却下 P3 — timeout_s=0 の即 kill レース** (0 指定の run がまれに完走しうる): 既存挙動
  (`CODEX_FANOUT_TIMEOUT=0` で従来から到達可能) かつ 0 秒指定に実用途がなく、実害方向も
  「殺し損ね」でなく「速すぎて生き残る」の低 severity。推測防御を足さない原則に従い未対応
- **却下 — `*[!0-9]*` イディオムの横展開** (repo 内 25 箇所): 他は全て「非数値 → 安全な既定値へ
  フォールバック」用途で、die ゲートとして watchdog を守るのは codex-fanout のみ。入力面も
  tmux 出力・date・pid で全角数字は来ない。修正しない

受け入れ条件の照合:

- usage / SKILL.md 反映 → 済
- 「敵対 lens 3 本が rc=143 を出さない」→ **機構は実装済みだが実タスクでの実測は未実施**
  (実 codex を回す 651 級タスクが要る。codex は明示指示なしに起動しない方針)。
  trigger: 次回 codex-drive の `[3.6]` で timeout 列 2400 を使い、runs.tsv に 143 が
  無いことを確認する。SKILL.md に 2400 の指示を入れたので次回 run が自然に検証になる

テスト: tests/codex_fanout.bats に 4 主張を追加 (行上書き / 非数値拒否 / 全角・桁あふれ拒否 /
CRLF 6 列)。変異 5 本 (行 timeout 無視・\r 剥がし除去・検証除去・範囲式へ戻す・桁上限除去)
すべて red を確認。
