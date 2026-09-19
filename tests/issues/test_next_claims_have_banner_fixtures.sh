#!/usr/bin/env bash
# test_next_claims_have_banner.sh の回帰テスト。repo の issues/ は claim 0 件のことが多く、
# 本体を repo に当てるだけでは「何も検査していない緑」と区別できないので、fixture で両側を固定する。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECK="$ROOT_DIR/tests/issues/test_next_claims_have_banner.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
fails=0
ng() { echo "NG: $1"; fails=$((fails + 1)); }

mk() { # $1=dir $2=issue 相対パス $3=本文 → issue を作り、同じ階層の next/ に目印を張る
  local dir="$1" rel="$2" body="$3" base parent
  base=$(basename "$rel"); parent="$dir/$(dirname "$rel")"
  mkdir -p "$parent/next"
  printf '%b' "$body" > "$parent/$base"
  ln -s "../$base" "$parent/next/$base"
}

# 1) バナーあり (2 書式・global と epic) → 通る
ok="$WORK/ok"
mk "$ok" 010-bug-a.md '# a\n\n> 🚨 **担当中: s1**（2026-09-19〜）\n\n## 概要\n'
mk "$ok" epic/g/020-bug-b.md '# b\n\n**着手中 (2026-09-19 / s2)**\n\n## 概要\n'
out=$(bash "$CHECK" "$ok" 2>&1) || ng "バナーありの claim で落ちた: $out"
case "$out" in *"2 件を検査"*) ;; *) ng "claim 2 件を数えていない: $out" ;; esac

# 2) 欠落 (global / epic / 見出しより後にだけある / 旧運用の実ファイル) → 4 件とも名指しで落ちる
bad="$WORK/bad"
mk "$bad" 030-bug-c.md '# c\n\n## 概要\n'
mk "$bad" epic/g/040-bug-e.md '# e\n\n本文\n'
mk "$bad" 050-bug-f.md '# f\n\n## 概要\n\n> 🚨 **担当中: s3**\n'
mkdir -p "$bad/next"; printf '# g\n\n本文\n' > "$bad/next/060-bug-g.md"
if out=$(bash "$CHECK" "$bad" 2>&1); then ng "バナー欠落で通った"; fi
for n in 030 040 050 060; do
  case "$out" in *"next/$n-"*) ;; *) ng "$n の欠落を名指ししていない" ;; esac
done

if [ "$fails" -eq 0 ]; then echo "OK: next claim バナー検査 (fixture 2 系統 / 欠落 4 形)"; else exit 1; fi
