# shellcheck shell=bash
# tests/tmux/lib/stub_env.sh — PATH スタブ方式の tmux テストが共通で必要とする 2 つ:
#   ① 既定 socket パス (TT_DEFAULT_SOCK)
#   ② 補助プロセス (偽サーバ・長寿命プロセス) の**安全な**起こし方と、存在しない pid の得方
# source する前に FAKE_PIDS 配列と TMP_DIR を用意すること (cleanup は呼び出し側の trap が持つ)。
#
# ⚠️ ② を各テストへコピペしないこと。下の trap 注記は load-bearing で、これが 3 本へ
#   コピーされた結果、名前 (spawn_helper / spawn_fake_server) とコメントの精度が既に
#   3 者で乖離していた (issue 081)。

# tmux が既定で使う socket のパス。テストは実サーバに触らないため「この形の socket は
# 本番」と判定する側の分岐を踏ませるのに使う (5 ファイルで逐語同一だったので一本化)。
# shellcheck disable=SC2034 # source した側のテストが参照する (lib 内では未使用に見える)
TT_DEFAULT_SOCK="$(realpath /tmp 2>/dev/null || echo /tmp)/tmux-$(id -u)/default"

# 使い捨ての長寿命プロセスを起こし、pid を REPLY_PID に返す (FAKE_PIDS にも積む)。
#
# ⚠️ 補助プロセスは必ず `( trap - EXIT; exec ... ) &` で起こすこと。素の `cmd &` は fork 直後
#    (exec 前) に kill されると子が EXIT trap を継承したまま走り、cleanup の `rm -rf` が
#    **テスト途中で TMP_DIR を消す** → 以降 stub が消えて実コマンドに落ちてハングする。
#    2026-07-30 に tests/tmux/test_periodic_save.sh で実際に踏み、bash -x で観測して特定した。
#    「サーバ役を kill して死亡検知させる」テスト (server_watchdog) はこのレースを最も踏みやすい。
tt_spawn_fake_proc() {
  ( trap - EXIT; exec sleep 300 ) &
  REPLY_PID=$!
  FAKE_PIDS+=("$REPLY_PID")
}

# 存在しない pid を「作らずに」得る。プロセスを起こして kill する方式は上記のレースを踏むうえ、
# kill 直後の pid が再利用される可能性もある。
#
# ⚠️ 引数なしの呼び出しは**決定的**で、同じ環境では毎回同じ pid を返す。「死んだサーバ」と
#    「死んだ owner」のように **別々の死 pid が要る**ときは、2 つ目以降に開始値を渡すこと
#    (`tt_free_pid $((first - 1))`)。同じ値のまま使うと、掃除で消えたのか lock の通常解放で
#    消えたのか区別できない fixture になる (2026-08-25 に issue 078 の掃除テストで実際に踏み、
#    掃除を no-op にする変異が green のまま通った)。
tt_free_pid() {
  local p="${1:-999999}"
  while kill -0 "$p" 2>/dev/null; do p=$((p - 1)); done
  REPLY_PID="$p"
}
