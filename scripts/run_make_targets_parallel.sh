#!/bin/sh
# run_make_targets_parallel.sh — 引数の make ターゲットを並列に走らせ、出力を
# ターゲット単位にバッファして**引数順**に出し、失敗を集約して返す。
#
# Makefile の `run_all_targets` (逐次) の並列版。**呼び出し側で opt-in する**もので、
# run_all_targets を置き換えるものではない。
#
# 🚨 全部の呼び出しを並列化してはいけない。`test-discovered` / `test-discovered-rest` は
# run_all_targets で「並列腕 + 直列腕」を束ねており (Makefile の該当箇所)、直列腕は
# tmux サーバに触るため**並列腕と同時に走らせて安全かは未検証**。安全性が確かめられて
# いるのは、互いに独立した静的検査だけを並べる test-lint の呼び出し。
#
# 契約 (逐次版と同じもの。issue 109 / 130):
# - 途中で失敗しても**全ターゲットを最後まで走らせる** (最初の失敗で残りを隠さない)
# - 失敗したターゲット名をまとめて出し、非 0 で返す
# - 対象 0 件は失敗 (呼び出しが壊れている = 検査ゼロを緑にしない)
#
# 🚨 失敗したターゲットの stdout と stderr を**両方**出す。指摘が stdout に、集約行が
# stderr に出るターゲットがあり、片方だけ読むと「通った」と誤読する
# (_claude/rules/verify-execution-not-just-exit-code.md)。
#
# 🚨 rc ファイルが無いターゲットは失敗として扱う (kill された / シェルが死んだ)。
# 「判定できなかった」を成功へ丸めない。
set -u
unset CDPATH

[ "$#" -gt 0 ] || { echo "✗ run_make_targets_parallel に対象が 0 件 (呼び出しが壊れている)" >&2; exit 1; }

outdir=$(mktemp -d) || { echo "✗ 一時ディレクトリを作れなかった" >&2; exit 1; }

# 🚨 中断時は**子孫を看取ってから** outdir を消す (issue 301)。
#
# trap が rm -rf だけをして抜けると、make とその子 (shellcheck / actionlint / go toolchain) は
# 生き残り、既に消えたパスへ書き続ける。実測 2026-09-06: 偽 make 2 本にランナーへ TERM を
# 送ると、fakemake 2 + 孫 4 = 6 プロセスが孤児として残った。
#
# 直接の子 (サブシェル) だけを kill しても孫は残るので、**pgid 単位**で殺す。
# そのために set -m で各バックグラウンドジョブを独立したプロセスグループに置く。
# 🚨 `kill -- -$$` は使わない: このスクリプトは #!/bin/sh で make から呼ばれるため $$ は
#    pgid ではなく (グループリーダーは make 側)、make ごと、あるいは無関係なグループへ TERM が飛ぶ。
set -m

PGIDS=""
reap() {   # 子孫を pgid 単位で落とす。順序が重要 (outdir の削除より先)
  for pg in $PGIDS; do
    kill -TERM "-$pg" 2>/dev/null
  done
  wait 2>/dev/null
}
cleanup() { reap; rm -rf "$outdir"; }
trap 'cleanup' EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

: "${MAKE:=make}"

for t in "$@"; do
  # ターゲット名はファイル名にそのまま使う (make のターゲット名に / は無い前提。
  # 万一入ったら mkdir されていないパスへの書き込みで失敗し、rc 欠落 = 失敗になる)
  ( $MAKE "$t" > "$outdir/$t.out" 2> "$outdir/$t.err"; echo $? > "$outdir/$t.rc" ) &
  PGIDS="$PGIDS $!"   # set -m の下では、バックグラウンドジョブの pid = その pgid
done
wait
PGIDS=""   # 全部看取った。正常終了時に生存プロセスへ TERM を送らない

failed=""
for t in "$@"; do
  [ -s "$outdir/$t.out" ] && cat "$outdir/$t.out"
  [ -s "$outdir/$t.err" ] && cat "$outdir/$t.err" >&2
  rc=$(cat "$outdir/$t.rc" 2>/dev/null) || rc=""
  case "$rc" in
    0) ;;
    '') echo "✗ $t: 終了コードを回収できなかった (中断 or 起動失敗)" >&2; failed="$failed $t" ;;
    *) failed="$failed $t" ;;
  esac
done

if [ -n "$failed" ]; then
  echo "" >&2
  echo "✗ 失敗したターゲット:$failed" >&2
  exit 1
fi
