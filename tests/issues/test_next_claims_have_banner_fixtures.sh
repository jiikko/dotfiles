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
# glogx の n が書く書式そのもの (src/glogx/issues/banner.go の ClaimBanner)。Go 側のテストは go test の
# キャッシュでこのスクリプトの変更を見ないことがあるので、書式の受け入れはこちらでも固定する
mk "$ok" 025-bug-c.md '# c\n\n> 🚨 **担当中: koji-mbp (glogx)**（2026-09-19〜）\n\n## 概要\n'
out=$(bash "$CHECK" "$ok" 2>&1) || ng "バナーありの claim で落ちた: $out"
case "$out" in *"3 件を検査"*) ;; *) ng "claim 3 件を数えていない: $out" ;; esac

# 2) 欠落 (global / epic / 見出しより後にだけある / 旧運用の実ファイル) → 4 件とも名指しで落ちる
bad="$WORK/bad"
mk "$bad" 030-bug-c.md '# c\n\n## 概要\n'
mk "$bad" epic/g/040-bug-e.md '# e\n\n本文\n'
mk "$bad" 050-bug-f.md '# f\n\n## 概要\n\n> 🚨 **担当中: s3**\n'
mkdir -p "$bad/next"; printf '# g\n\n本文\n' > "$bad/next/060-bug-g.md"
# 🚨 バナーに見えるが冒頭の担当者表示ではない形 (敵対レビュー 2026-09-19 で実測した偽の緑)
# shellcheck disable=SC2016 # バッククォートは markdown のフェンス
mk "$bad" 070-bug-h.md '# h\n\n```\n**担当中: x**\n```\n'
mk "$bad" 080-bug-i.md '# i\n\n<!-- **担当中: x** -->\n'
mk "$bad" 090-bug-j.md '# j **担当中じゃない**\n'
mk "$bad" 101-bug-l.md '# l\n\n~~~\n**担当中: x**\n~~~\n'
# shellcheck disable=SC2016 # バッククォートは markdown のフェンス
mk "$bad" 102-bug-m.md '# m\n\n  ```\n**担当中: x**\n  ```\n'
mk "$bad" 100-bug-k.md '# k\n\n過去は**担当中**だったが解除済み\n'
if out=$(bash "$CHECK" "$bad" 2>&1); then ng "バナー欠落で通った"; fi
for n in 030 040 050 060 070 080 090 100 101 102; do
  case "$out" in *"next/$n-"*) ;; *) ng "$n の欠落を名指ししていない" ;; esac
done

# 末尾の / が複数あっても対象を見失わない (0 件の緑にならない)
if bash "$CHECK" "$bad//" >/dev/null 2>&1; then ng "末尾 // で検査対象を見失った"; fi

# 3) 大文字小文字違いの置き場・拡張子 (links_valid と glogx は目印と読む) も対象にする
for d in NEXT Epic MD; do
  w="$WORK/case-$d"
  case "$d" in
    NEXT) mkdir -p "$w/NEXT"; printf '# a\n本文\n' > "$w/110-bug-a.md"; ln -s ../110-bug-a.md "$w/NEXT/110-bug-a.md" ;;
    Epic) mkdir -p "$w/Epic/g/next"; printf '# a\n本文\n' > "$w/Epic/g/110-bug-a.md"; ln -s ../110-bug-a.md "$w/Epic/g/next/110-bug-a.md" ;;
    MD) mkdir -p "$w/next"; printf '# a\n本文\n' > "$w/110-bug-a.MD"; ln -s ../110-bug-a.MD "$w/next/110-bug-a.MD" ;;
  esac
  if bash "$CHECK" "$w" >/dev/null 2>&1; then ng "$d の置き場の claim を見逃した"; fi
done

# 4) 目印でないもの (meta ファイル / dangling) はこの検査では落とさず、数にも入れない (links_valid の担当)
skip="$WORK/skip"
mk "$skip" 120-bug-a.md '# a\n\n> 🚨 **担当中: s**\n'
printf '# readme\n' > "$skip/README.md"; ln -s ../README.md "$skip/next/README.md"
ln -s ../999-gone.md "$skip/next/999-gone.md"
out=$(bash "$CHECK" "$skip" 2>&1) || ng "meta / dangling を落とした: $out"
case "$out" in *"1 件を検査"*) ;; *) ng "meta / dangling を数えた: $out" ;; esac

if [ "$fails" -eq 0 ]; then echo "OK: next claim バナー検査 (欠落 10 形 / 末尾 // / 大文字小文字 3 形 / 対象外 2 形)"; else exit 1; fi
