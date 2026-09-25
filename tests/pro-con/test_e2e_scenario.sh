#!/usr/bin/env bash
# pro-con の e2e モードの通し (pro-con e2e scenario): 依頼 → 偽の PM が分解 → 偽の PG が質問 → 画面で回答 → テストの係 → レビュー → Q → quit。
# PG は台本どおりの偽物 (src/pro-con/daemon/e2e.go) なので claude は起動しない。画面は置き場ごとの隔離した tmux サーバで動く。
set -u
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
command -v tmux >/dev/null 2>&1 || { echo "[skip] tmux が無い"; exit 77; }
work="$(mktemp -d)" || exit 1
trap 'rm -rf "$work"' EXIT
root="$work/e2e"
"$ROOT_DIR/bin/pro-con" e2e scenario "$root" > "$work/out" 2> "$work/err"
rc=$?
if [ $rc -ne 0 ] || ! grep -q "通った" "$work/out"; then
  echo "✗ e2e の通しが通らない (rc=$rc)"; cat "$work/out"; sed -n '1,40p' "$work/err"
  exit 1
fi
echo "✓ e2e の通し: $(grep -c . "$work/out") 段"
# 閉じた後に画面・隔離サーバ・daemon が残っていない
if "$ROOT_DIR/bin/pro-con" e2e screen "$root" > /dev/null 2>&1; then
  echo "✗ 閉じた後も隔離サーバが残っている"; "$ROOT_DIR/bin/pro-con" e2e stop "$root"; exit 1
fi
if ps -A -o command= | grep -F -- "--e2e $root" | grep -v grep > /dev/null; then
  echo "✗ 閉じた後も e2e の daemon / 画面のプロセスが残っている"; exit 1
fi
if ! jq -e '.cards[0].State == 4' "$root/state/cards.json" > /dev/null; then
  echo "✗ カードがレビューの列に居ない"; jq -c '.cards[0]' "$root/state/cards.json"; exit 1
fi
echo "✓ 閉じた後に残っているものは無い / カードはレビューの列"
