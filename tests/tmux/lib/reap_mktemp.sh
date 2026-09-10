# shellcheck shell=bash
# 一時 dir を「作成と登録を 1 つの関数で」行う (issue 305)。zsh 用。
#
# なぜ: 「作った dir を cleanup の rm -rf にも書く」を人の記憶に預けていたため、
# test_reap_orphan_servers.sh のケース E の PROT_DIR が抜けて**正常終了のたびに 1 個ずつ**
# /tmp/reapp.* を残していた (実測 39 個)。作成と登録を 1 つにすれば書き忘れが構造的に起きない
# (掃除機構を足すのではなく発生源を断つ側。adversarial-review-own-safeguards.md §0-A)。
#
# 🚨 **なぜ lib へ出したか** (敵対レビュー 3 周目 P1-2)。ヘルパーがテスト本体に在ると、検査側は
# 「ヘルパーの行範囲」を awk で切り出す必要があり、その終端アンカー `/^\}$/` が**上限のない探索**
# だった。`}` に行末コメントを 1 つ足すだけで信頼窓が 6 行 → 51 行へ**無警告で**広がり、
# その中に旧イディオムで dir を足すと検出されない。しかも開き側は既に行末コメント付きで、
# 「`}` にコメントを足す」はこの repo の家風。**アンカーを直すのではなく、アンカーを無くした** —
# ヘルパーがここに在れば、検査は「テスト本体に素の mktemp が 1 つも無いか」で済む。
#
# 🚨 **パスを stdout で返さない**。`X=$(reap_mktemp_d …)` はコマンド置換 = サブシェルなので、
# 配列への登録が親シェルに届かず**cleanup が 1 件も消さないまま緑**になる (最初そう書いた)。
# 呼び出し元の変数へ typeset -g で直接入れる形にして、登録と代入を同じシェルで行う。

typeset -ga REAP_TMPDIRS=()

reap_mktemp_d() {  # reap_mktemp_d <変数名> <テンプレート>
  local d
  d=$(mktemp -d "$2") || return 1
  REAP_TMPDIRS+=("$d")
  typeset -g "$1"="$d"
}

# reap_cleanup_tmpdirs は登録された dir を全部消す。呼ぶのは呼び出し側の cleanup。
reap_cleanup_tmpdirs() {
  if (( ${#REAP_TMPDIRS} )); then rm -rf "${REAP_TMPDIRS[@]}"; fi
}
