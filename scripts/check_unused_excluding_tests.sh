#!/usr/bin/env bash
# production から到達できないシンボルを検出する (issue 315)。
#
# なぜ golangci-lint の `unused` では足りないか: `unused` は**テストファイルも解析対象に含める**
# のが既定なので、「production の最後の呼び出し元が消えて、テストだけが呼んでいる関数」は
# 「使われている」ことになり、**CI は永久に緑**のままになる。issue 316 で消した到達不能シンボルは
# 全部このクラスで、個別に消しても次に呼び出しを差し替えた人が同じ状態を作る。
# `staticcheck -checks=U1000 -tests=false` はテストを母集合から外すのでこのクラスが出る。
#
# 🚨 **脅威モデル** (`_claude/rules/adversarial-review-own-safeguards.md` §8):
#   止めたいのは「**production の呼び出しを消した / テストへ差し替えたのに、誰も気づかない**」こと。
#   止めないのは「意図的に残した test seam」で、それは allowlist に理由つきで登録する。
#
# 🚨 **検出しないと決めた形**:
#   - **exported なシンボル** (staticcheck の U1000 は他 package から使われうる exported を
#     原則報告しない)。main package 以外の公開 API の死蔵はこのゲートの射程外
#   - **テスト専用ファイルの中の未使用** (`-tests=false` なので解析対象から外れている)
#   - **反射・生成コード経由の参照**。staticcheck が追えないものは誤検出として allowlist へ回す
#
# 🚨 **allowlist は「消せ」の圧力から test seam を守るためにある**
#   (`list-masked-failure-modes-before-removing-guard.md`)。登録を外す前に、その seam が
#   マスクしていたもの (テストの可読性 / 依存しているテストの本数) を数えること。
set -uo pipefail
unset CDPATH

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT_DIR" || exit 1

# allowlist: "<モジュール相対パス>:<シンボル>|<理由>"
# 🚨 理由を書けない登録はしない。stale (= もう報告されない) 登録はこのスクリプトが検出する。
ALLOW=$(cat <<'ALLOWEOF'
src/glogx/zoom.go:(*appZoom).start|開く演出は tui.go の Init でコメントアウトされている (起動が待たされる方が体感を損ねるという判断)。対の startClose は生きているので、消すと片側だけの実装になる。理由は zoom.go の関数直上に書いてある
src/schedkeys/editor.go:(*editor).setValue|テストが依存する test seam。editor_test / regression_test / render_test の 14 箇所が使う。消すと大量に壊れる
ALLOWEOF
)

if ! command -v staticcheck >/dev/null 2>&1; then
  echo "✗ staticcheck が無い (go install honnef.co/go/tools/cmd/staticcheck@latest)" >&2
  exit 1
fi

mods=$(find src -maxdepth 2 -name go.mod | sed 's|/go.mod||' | sort)
# 🚨 対象 0 件を成功にしない (verify-execution-not-just-exit-code.md)。モジュールの探し方が
# 壊れると「違反 0 件」で緑になるのが、この手のゲートで最も危ない壊れ方。
if [ -z "$mods" ]; then
  echo "✗ src 配下に go.mod が 1 件も見つからない (抽出が壊れている)" >&2
  exit 1
fi
nmods=$(printf '%s\n' "$mods" | wc -l | tr -d ' ')

findings=""
for m in $mods; do
  out=$( cd "$m" && staticcheck -checks=U1000 -tests=false ./... 2>&1 ) || true
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    # `zoom.go:66:19: func (*appZoom).start is unused (U1000)` → `src/glogx/zoom.go:(*appZoom).start`
    file=${line%%:*}
    sym=$(sed -E 's/.*: (func|var|const|type|field) ([^ ]+) is unused.*/\2/' <<< "$line")
    if [ "$sym" = "$line" ]; then
      # 想定外の出力形。**判定不能を合格に畳まない**
      findings+="?? $m: 解釈できない staticcheck の出力: $line"$'\n'
      continue
    fi
    findings+="$m/$file:$sym"$'\n'
  done <<< "$out"
done

fails=0
reported=""
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in '?? '*) printf '✗ %s\n' "$f" >&2; fails=$((fails + 1)); continue ;; esac
  if grep -qF "$f|" <<< "$ALLOW"; then
    reported+="$f"$'\n'
    continue
  fi
  printf '✗ production から到達できない: %s\n' "$f" >&2
  printf '   テストだけが呼んでいる可能性がある。消すか、test seam なら理由つきで allowlist へ。\n' >&2
  fails=$((fails + 1))
done <<< "$findings"

# stale な allowlist (もう報告されない = production から使われるようになった / 消された)
while IFS= read -r entry; do
  [ -n "$entry" ] || continue
  key=${entry%%|*}
  if ! grep -qxF "$key" <<< "$reported"; then
    printf '✗ allowlist が stale: %s は staticcheck から報告されていない (production で使われるようになった? 消された?)\n' "$key" >&2
    fails=$((fails + 1))
  fi
done <<< "$ALLOW"

if [ "$fails" -gt 0 ]; then
  printf '\n✗ production 到達不能の検査: %d 件\n' "$fails" >&2
  exit 1
fi
printf '✓ production 到達不能なシンボルなし (Go モジュール %d 件を staticcheck -tests=false で走査。allowlist %d 件)\n' \
  "$nmods" "$(grep -c '|' <<< "$ALLOW")"
