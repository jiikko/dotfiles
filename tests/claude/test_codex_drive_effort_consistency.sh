#!/usr/bin/env bash
#
# codex-drive の既定モデル / effort が、写しの間で食い違っていないことを検査する。
#
# なぜ: この値は「1 箇所」に寄せられない。SKILL.md の codex 起動例は driver
# (bin/codex-fanout / bin/codex-run) を経由しない直の `codex exec` (fallback 経路) で、実行時に
# driver の既定を読む術がない。env 参照に書き換えると、変数が未設定のとき ~/.codex/config.toml の
# 既定を拾う (SKILL.md「モデルはスキル側で明示する」が警告している事故) ため、リテラルの
# まま持つのが正しい。したがって「単一の出典」ではなく「写し同士の一致」を検査で強制する。
#
# 実例 (2026-08-25): commit 8736d37 が effort を low から max へ変えた際、SKILL.md と
# bin/codex-fanout は更新されたが tests/codex_fanout.bats が取り残されて赤のままだった。
#
# 既定は run の種類で 2 組 (SKILL.md のモデル表。ユーザー決定 2026-10-02):
#   書く run (danger-full-access) と集約 = bin/codex-fanout の merger 既定 ← 基準
#   読む run (read-only / review)         = bin/codex-run の既定 ← 基準 (codex-run は ro / review しか起動しない)
# 検査する写し:
#   1. 各 driver の実既定と、同じファイルの usage コメントが述べる既定値
#   2. SKILL.md の全起動例 (```bash ブロック内の codex exec。ヒアドキュメントの本文は除く。`\` の継続行は 1 本に繋ぐ):
#      sandbox の種類ごとに (モデル, effort) の組が上の基準と一致すること
#   3. SKILL.md に現れる全ての `-m gpt-*` / effort が 2 組のどちらかであること (散文の上振れ先の記述を含む)
#
# 値そのものの pin は tests/codex_fanout.bats (merger) と tests/codex_run.bats (読む run) が持つ。
# こちらは相対的な一致だけを見るので、両方が揃って初めて「どこか 1 箇所を変えたら赤」になる。
#
# codex-drive 以外の skill (codex-lead / codex-review) は対象外。
#
# 止めるのは「既定を変えたときに写しの一部が追随しない」こと。検出しないと決めた形:
#   - コードブロックの外の散文で、`-c ...=xhigh` (引用符なし) / `--model` など起動例と違う書き方をしたもの
#   - 起動例そのものを消すこと (本数の減少は追随漏れではない。0 本になったときだけ落とす)
#   - ```bash / sh / zsh 以外のブロック (言語の書き忘れ・~~~・4 字下げ) と散文のインラインに書いた起動例。
#     言語なしのブロックを対象にしないのは、[2] の工程図のように散文で codex exec を書くブロックがあるため
#     SKILL.md の起動例は ```bash に書く (散文のモデル / effort は写し 3 が値だけ見る。組の取り違えは見ない)
#   - `-c model="..."` のように config 経由でモデルを指定する形 (-m だけを数える)
#   - 継続行の \ の付け忘れ・付け過ぎで、次のブロックの 1 行目を飲み込む形 (typo)
#   - 集約役 (merger / 変更マップ) は read-only だが luna-max を使う。起動例を SKILL.md に足すなら、
#     この検査の分類に集約役を足すこと (今は散文にしか無い)

set -euo pipefail
unset CDPATH  # CDPATH が export されていると `cd foo` が解決先を stdout に出し、
              # SCRIPT_DIR=$(cd ... && pwd) が 2 行に化ける (tests/CLAUDE.md の CDPATH の項)

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
DRIVER="$ROOT_DIR/bin/codex-fanout"
RUNNER="$ROOT_DIR/bin/codex-run"
SKILL_MD="$ROOT_DIR/_claude/skills/codex-drive/SKILL.md"

fail=0

for f in "$DRIVER" "$RUNNER" "$SKILL_MD"; do
  [ -f "$f" ] || { echo "FAIL: 検査対象が存在しない: $f" >&2; exit 1; }
done

# 抽出はすべて `|| true` を付ける。`set -euo pipefail` の下では、抽出コマンドが無マッチで
# 非 0 を返した時点で**代入ごとスクリプトが死ぬ**。そうなると下の「抽出できたか」の検査に
# 到達できず、**出力ゼロで exit 1** という原因の分からない失敗になる (2026-08-26 に実際に
# CI をこの形で赤にした)。空を受け取って明示的に FAIL を出す方に倒す。
need() {  # $1=値 $2=説明。抽出できないまま緑を返さない (書式が変わったら検査自体が無効になるため)
  [ -n "$1" ] || { echo "FAIL: $2 を抽出できない (書式が変わった?)" >&2; exit 1; }
}
same() {  # $1=写し $2=基準 $3=説明
  if [ "$1" != "$2" ]; then
    echo "FAIL: $3 ($1) が実既定 ($2) と食い違う" >&2
    fail=1
  fi
}

# 書く run と集約: driver (codex-fanout) の merger 既定
w_model=$(sed -n 's/.*CODEX_FANOUT_MERGER_MODEL:-\([^}]*\)}.*/\1/p' "$DRIVER" | head -1 || true)
w_effort=$(sed -n 's/.*CODEX_FANOUT_MERGER_EFFORT:-\([^}]*\)}.*/\1/p' "$DRIVER" | head -1 || true)
need "$w_model" "$DRIVER の merger の既定モデル (:- 既定)"
need "$w_effort" "$DRIVER の merger の既定 effort (:- 既定)"
doc=$(sed -n 's/.*CODEX_FANOUT_MERGER_MODEL .*既定 \([A-Za-z0-9.-]*\).*/\1/p' "$DRIVER" | head -1 || true)
need "$doc" "$DRIVER の usage コメントの既定モデル"
same "$doc" "$w_model" "bin/codex-fanout の usage コメントの既定モデル"
doc=$(sed -n 's/.*CODEX_FANOUT_MERGER_EFFORT .*既定 \([a-z]*\).*/\1/p' "$DRIVER" | head -1 || true)
need "$doc" "$DRIVER の usage コメントの既定 effort"
same "$doc" "$w_effort" "bin/codex-fanout の usage コメントの既定 effort"

# 読む run: codex-run の既定
r_model=$(sed -n 's/^work_dir=.* model="\([^"]*\)".*/\1/p' "$RUNNER" | head -1 || true)
r_effort=$(sed -n 's/^work_dir=.* effort="\([^"]*\)".*/\1/p' "$RUNNER" | head -1 || true)
need "$r_model" "$RUNNER の既定モデル (model=\"...\")"
need "$r_effort" "$RUNNER の既定 effort (effort=\"...\")"
doc=$(sed -n 's/^#  *-m model  *既定 \([A-Za-z0-9.-]*\).*/\1/p' "$RUNNER" | head -1 || true)
need "$doc" "$RUNNER の usage コメントの既定モデル"
same "$doc" "$r_model" "bin/codex-run の usage コメントの既定モデル"
doc=$(sed -n 's/^#  *-e effort  *既定 \([a-z]*\).*/\1/p' "$RUNNER" | head -1 || true)
need "$doc" "$RUNNER の usage コメントの既定 effort"
same "$doc" "$r_effort" "bin/codex-run の usage コメントの既定 effort"

if [ "$w_model $w_effort" = "$r_model $r_effort" ]; then
  echo "FAIL: 書く run と読む run の組が同じ ($w_model / $w_effort)。2 組を 1 組に戻したなら、この検査の分類も畳むこと" >&2
  fail=1
fi

# 写し 2: SKILL.md の起動例。言語が bash / sh / zsh のコードブロック (```bash) の中で、ヒアドキュメントの
# 本文とコメント行を除き、`codex exec` を含む行を全部起動例とみなす (前に timeout / env / VAR=x が付いても拾う)。
# 継続行 (末尾 \) は 1 本に繋ぐ。判定はプロンプト本文を除いた「オプション部分」(</dev/null より前) だけで行う
# (本文に -s danger-full-access と書かれていても種類を取り違えない)。
# 1 本に「種類 モデル effort -m の個数 effort の個数 </dev/null の有無」を出す
launches=$(awk '
  /^[[:space:]]*```/ {
    if (!fence) { fence = 1; shell = ($0 ~ /^[[:space:]]*```(bash|sh|zsh)[[:space:]]*$/) } else { fence = 0; shell = 0 }
    heredoc = ""; next
  }
  !shell { next }
  heredoc != "" { t = $0; gsub(/^[[:space:]]+|[[:space:]]+$/, "", t); if (t == heredoc) heredoc = ""; next }
  pending != "" { pending = pending " " $0 }
  pending == "" && $0 !~ /^[[:space:]]*#/ && /codex exec/ { pending = $0 }
  pending != "" && !/\\[[:space:]]*$/ { print pending; pending = "" }
  # 行内でヒアドキュメントが始まったら、次の行から終端タグまでを読み飛ばす (プロンプト本文の codex exec を拾わない)
  # コメント行の <<'EOF' (注記) と here-string の <<<WORD では始めない
  $0 !~ /^[[:space:]]*#/ && match($0, /(^|[^<])<<-?[[:space:]]*["\047]?[A-Za-z_][A-Za-z0-9_]*/) {
    heredoc = substr($0, RSTART, RLENGTH); sub(/^[^<]?<<-?[[:space:]]*["\047]?/, "", heredoc)
  }
' "$SKILL_MD" | awk '{
  nul = match($0, /<[[:space:]]*\/dev\/null/)
  opts = nul ? substr($0, 1, nul - 1) : $0
  kind = "unknown"
  if (opts ~ /(-s|--sandbox)[[:space:]=]+(danger-full-access|workspace-write)/) kind = "write"
  else if (opts ~ /(-s|--sandbox)[[:space:]=]+read-only/ || opts ~ /codex exec review/) kind = "read"
  model = "-"; effort = "-"
  if (match(opts, /-m gpt-[A-Za-z0-9.-]+/)) model = substr(opts, RSTART + 3, RLENGTH - 3)
  if (match(opts, /model_reasoning_effort="[a-z]+"/)) effort = substr(opts, RSTART + 24, RLENGTH - 25)
  t = opts; nm = gsub(/(^|[[:space:]])(-m|--model)[[:space:]=]/, "", t)
  t = opts; ne = gsub(/model_reasoning_effort=/, "", t)
  print kind, model, effort, nm, ne, (nul ? "y" : "n")
}' || true)
need "$launches" "$SKILL_MD の起動例 (bash のコードブロック内の codex exec)"

n_write=0; n_read=0
while read -r kind model effort nm ne nul; do
  if [ "$nul" != y ]; then
    echo "FAIL: SKILL.md に </dev/null の無い codex 起動例がある (codex-review スキルの必須規約。オプション部分の境界も取れない)" >&2
    fail=1; continue
  fi
  if [ "$nm" -gt 1 ] || [ "$ne" -gt 1 ]; then
    echo "FAIL: SKILL.md に -m または effort を 2 回指定した起動例がある (codex は後勝ちで、検査は先頭を読むので食い違う)" >&2
    fail=1; continue
  fi
  case "$kind" in
    write) want="$w_model $w_effort"; n_write=$((n_write + 1)) ;;
    read)  want="$r_model $r_effort"; n_read=$((n_read + 1)) ;;
    *) echo "FAIL: SKILL.md に sandbox の種類が分からない起動例がある (-s danger-full-access / workspace-write / read-only / exec review のどれでもない)" >&2
       fail=1; continue ;;
  esac
  if [ "$model $effort" != "$want" ]; then
    echo "FAIL: SKILL.md の $kind run の起動例が $model / $effort だが、既定は $want (どちらかが追随漏れ。'-' は指定が無い)" >&2
    fail=1
  fi
done <<< "$launches"
# 片方の種類が 0 本なら、その組の一致は何も検査していない
[ "$n_write" -gt 0 ] || { echo "FAIL: SKILL.md に書く run の起動例が 1 本も無い (抽出の書式が変わった?)" >&2; fail=1; }
[ "$n_read" -gt 0 ] || { echo "FAIL: SKILL.md に読む run の起動例が 1 本も無い (抽出の書式が変わった?)" >&2; fail=1; }

# 写し 3: 散文も含め、SKILL.md に現れるモデル / effort は 2 組のどちらか
while IFS= read -r m; do
  [ -z "$m" ] || [ "$m" = "$w_model" ] || [ "$m" = "$r_model" ] || {
    echo "FAIL: SKILL.md にモデル $m があるが、許可は $w_model (書く run) と $r_model (読む run) だけ" >&2
    fail=1
  }
done < <(grep -o -- '-m gpt-[A-Za-z0-9.-]*' "$SKILL_MD" | sed 's/^-m //' | sort -u || true)
while IFS= read -r e; do
  [ -z "$e" ] || [ "$e" = "$w_effort" ] || [ "$e" = "$r_effort" ] || {
    echo "FAIL: SKILL.md に effort=$e があるが、許可は $w_effort (書く run) と $r_effort (読む run) だけ" >&2
    fail=1
  }
done < <(grep -o 'model_reasoning_effort="[a-z]*"' "$SKILL_MD" | sed 's/.*="\(.*\)"/\1/' | sort -u || true)

if [ "$fail" -ne 0 ]; then
  echo "" >&2
  echo "codex-drive の既定を変えるときは次をすべて同時に更新すること:" >&2
  echo "  1. bin/codex-fanout (書く run と集約) / bin/codex-run (読む run) の既定と usage コメント" >&2
  echo "  2. _claude/skills/codex-drive/SKILL.md の起動例とモデル表" >&2
  echo "  3. tests/codex_fanout.bats / tests/codex_run.bats の絶対 pin" >&2
  exit 1
fi

echo "ok: codex-drive の既定が driver・usage・SKILL.md で一致 (書く run $w_model/$w_effort ${n_write} 本・読む run $r_model/$r_effort ${n_read} 本)"
