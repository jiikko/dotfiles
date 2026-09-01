# 148 codex-fanout: 敵対レビュー lens が既定 timeout (rc=143) で落ちやすい

- 起票: 2026-09-01
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
