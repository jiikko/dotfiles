#!/usr/bin/env bash
# 本家 yomiyasu (Python) の lint・diff・check を、gen.py が作った入力に通して所要を測る。
#
#   bench/pybench.sh [-n <周の数>] <gen.py の出力の dir> <結果を書く dir>
#
# 周の数を指定しないときは 5 周。各周で全ケースを 1 回ずつ流し、周ごとにケースの順を (周の番号を種に) かき混ぜる。
# 結果は runs.tsv (1 run 1 行) と summary.tsv (ケースごとの中央値・最小・最大と、起動を引いた処理の時間)。
#
# 本家の動かし方は oracle の Runner (oracle/runner.go の env・pythonFlags・newSandbox) に合わせる:
#   - 環境変数は空から作り、PATH (python3 の dir と /usr/bin:/bin)・使い捨ての HOME・TZ=UTC・PYTHONHASHSEED=0 だけを渡す
#   - python3 -P -s -S -B -X utf8 で起動する (-I は PYTHONHASHSEED を無視するので使わない。理由は README の「環境の固定」)
#   - 本家のファイルは置き場 (oracle のキャッシュ) から upstream.sha256 と合うものだけを使い捨ての木に写して動かす
#   - 使い捨ての dir は OS の一時領域に作り、上に .git・.yomiyasu-words・.yomiyasu があれば止まる
#     (本家は cwd から上へ辿ってこれらを拾う。repo の tmp/ の下に置くと .git を拾うので、ここだけは repo の tmp/ を使わない)
# 測るのは env -i の起動から python3 の終了までの壁時計の時間。使い捨ての cwd・HOME の用意と入力の写しは測る区間の外に置く。
# Runner の RunCLI は run ごとに木を写し直すが、ここでは 1 回写した木を全 run で使う (写しは測りたい処理ではないため)。
set -euo pipefail
unset CDPATH

[ "${BASH_VERSINFO[0]}" -ge 5 ] || { echo "pybench: bash 5 以上が要る (EPOCHREALTIME を使う)" >&2; exit 2; }

rounds=5
if [ "${1:-}" = "-n" ]; then
  rounds="${2:?-n には周の数を渡す}"
  shift 2
fi
case "$rounds" in '' | *[!0-9]* | 0) echo "pybench: 周の数は 1 以上の整数 ($rounds)" >&2; exit 2 ;; esac
[ $# -eq 2 ] || { echo "使い方: $0 [-n <周の数>] <gen.py の出力の dir> <結果を書く dir>" >&2; exit 2; }
inputs="$(cd "$1" && pwd -P)"
results="$2"
mkdir -p "$results"
results="$(cd "$results" && pwd -P)"
[ -f "$inputs/manifest.tsv" ] || { echo "pybench: $inputs に manifest.tsv が無い (gen.py の出力を渡す)" >&2; exit 2; }

here="$(cd "$(dirname "$(readlink -f "$0")")" && pwd -P)"
sums="$here/../oracle/upstream.sha256"
# 本家の commit の正本は oracle/upstream.go の Commit。字面から読む (Go を起動しない)
commit="$(sed -n 's/^[[:space:]]*Commit = "\([0-9a-f]\{40\}\)"$/\1/p' "$here/../oracle/upstream.go")"
[ -n "$commit" ] || { echo "pybench: oracle/upstream.go から本家の commit を読めない" >&2; exit 2; }
cache="${XDG_CACHE_HOME:-$HOME/.cache}/yomiyasu-oracle/$commit"
[ -d "$cache" ] || { echo "pybench: 本家の置き場 $cache が無い (make -C src/ja-writing golden が取得する)" >&2; exit 2; }
py="$(command -v python3)" || { echo "pybench: python3 が見つからない" >&2; exit 2; }

work="$(mktemp -d)"
work="$(cd "$work" && pwd -P)"
trap 'rm -rf "$work"' EXIT
d="$work"
while :; do
  for m in .git .yomiyasu-words .yomiyasu; do
    [ ! -e "$d/$m" ] || { echo "pybench: 使い捨ての dir $work の上に $d/$m がある。TMPDIR を repo の外に向ける" >&2; exit 2; }
  done
  [ "$d" != / ] || break
  d="$(dirname "$d")"
done

# 本家の木を写す (SHA-256 が合うものだけ)
tree="$work/tree"
n=0
while read -r sum rel; do
  [ -n "$sum" ] || continue
  got="$(shasum -a 256 "$cache/$rel" | cut -d' ' -f1)"
  [ "$got" = "$sum" ] || { echo "pybench: $rel の SHA-256 が upstream.sha256 と合わない" >&2; exit 2; }
  mkdir -p "$(dirname "$tree/$rel")"
  cp "$cache/$rel" "$tree/$rel"
  n=$((n + 1))
done < "$sums"
[ "$n" -gt 0 ] || { echo "pybench: upstream.sha256 に本家のファイルが 1 つも無い" >&2; exit 2; }
scripts="$tree/skills/yomiyasu/scripts"

# ケース: 名前 \t 道具 (python -c のときは -) \t 引数 (入力は in/ の下)。起動だけの run を先に置く
cases=(
  $'startup-python\t-\t-c pass'
  $'startup-lint\tyomiyasu_lint.py\t--version'
  $'startup-diff\tyomiyasu_diff.py\t--version'
  $'startup-check\tyomiyasu_check.py\t--version'
)
for f in len-004k len-016k len-064k len-256k len-1024k rep-064k table-064k ai-064k line-032k; do
  cases+=("lint-$f"$'\t'"yomiyasu_lint.py"$'\t'"in/$f.md")
done
for p in pair-small pair-064k pair-256k; do
  cases+=("diff-$p"$'\t'"yomiyasu_diff.py"$'\t'"in/$p-before.md in/$p-after.md")
  cases+=("check-$p"$'\t'"yomiyasu_check.py"$'\t'"run --source in/$p-before.md --final in/$p-after.md --no-record --state-dir state")
done

# ケースが名指しする入力が manifest に在るかを確かめる (名前がずれた run を測らない)
for c in "${cases[@]}"; do
  for a in $c; do
    case "$a" in in/*) grep -qxF "${a#in/}" <<< "$(cut -f1 "$inputs/manifest.tsv")" || { echo "pybench: ケースの入力 ${a#in/} が manifest.tsv に無い" >&2; exit 2; } ;; esac
  done
done

# 入力が manifest どおりかを確かめる (別の commit の gen.py の出力を測らない)
(cd "$inputs" && while IFS=$'\t' read -r name _ sum; do
  [ "$(shasum -a 256 "$name" | cut -d' ' -f1)" = "$sum" ] || { echo "pybench: $name が manifest.tsv と合わない" >&2; exit 2; }
done < manifest.tsv)
python3 -I "$here/gen.py" "$work/expect" >/dev/null
cmp -s "$work/expect/manifest.tsv" "$inputs/manifest.tsv" || { echo "pybench: $inputs は今の gen.py の出力と違う (作り直す)" >&2; exit 2; }

pyver="$(env -i "$py" -P -s -S -B -X utf8 -c 'import sys; print(sys.version.split()[0])')"
{
  printf 'python\t%s\t%s\n' "$py" "$pyver"
  printf 'upstream\t%s\n' "$commit"
  printf 'host\t%s\t%s\n' "$(sysctl -n machdep.cpu.brand_string 2>/dev/null || uname -m)" "$(sw_vers -productVersion 2>/dev/null || uname -r)"
  printf 'rounds\t%s\n' "$rounds"
  printf 'started\t%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} > "$results/env.tsv"

runs="$results/runs.tsv"
printf 'round\tcase\trc\tseconds\tstdout_bytes\tstderr_bytes\tloadavg_1m\n' > "$runs"
total=${#cases[@]}
pypath="$(dirname "$py"):/usr/bin:/bin"
for ((r = 1; r <= rounds; r++)); do
  # 周ごとに、周の番号を種にしてケースの順をかき混ぜる (直前に走るケースを周ごとに変える。種が決まっているので run どうしで同じ順になる)
  read -ra order <<< "$(python3 -I -c 'import random, sys; o = list(range(int(sys.argv[1]))); random.Random(int(sys.argv[2])).shuffle(o); print(*o)' "$total" "$r")"
  for k in "${order[@]}"; do
    IFS=$'\t' read -r name tool args <<< "${cases[$k]}"
    run="$work/run.$r.$k"
    mkdir -p "$run/cwd" "$run/home"
    cp -R "$inputs" "$run/cwd/in"
    # shellcheck disable=SC2086 # args は空白で割る (ケースの表に空白を含む引数は無い)
    if [ "$tool" = - ]; then set -- $args; else set -- "$scripts/$tool" $args; fi
    load="$(sysctl -n vm.loadavg | awk '{ print $2 }')"
    cd "$run/cwd"
    # 測る区間は env の fork・exec と python3 の起動から終わりまで。サブシェル・コマンド置換は区間の外に置く
    # (env の分は startup-python に入り、どの道具のケースにも同じだけ入る)
    start=$EPOCHREALTIME
    set +e
    env -i PATH="$pypath" HOME="$run/home" TZ=UTC PYTHONHASHSEED=0 "$py" -P -s -S -B -X utf8 "$@" < /dev/null > "$run/out" 2> "$run/err"
    rc=$?
    set -e
    end=$EPOCHREALTIME
    cd "$work"
    secs="$(awk -v a="$start" -v b="$end" 'BEGIN { printf "%.4f", b - a }')"
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$r" "$name" "$rc" "$secs" "$(wc -c < "$run/out" | tr -d ' ')" "$(wc -c < "$run/err" | tr -d ' ')" "$load" >> "$runs"
    printf 'pybench: 周 %s/%s %-22s rc=%s %ss\n' "$r" "$rounds" "$name" "$rc" "$secs" >&2
    rm -rf "$run"
  done
done
printf 'finished\t%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$results/env.tsv"
# rc が 0 でない run と、周によって出力の大きさが違うケースがあれば、summarize.py が rc 1 で落ちる (失敗の混ざった中央値を出さない)
python3 -I "$here/summarize.py" "$runs" "$inputs/manifest.tsv" > "$results/summary.tsv"
cat "$results/summary.tsv"
