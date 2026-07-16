#!/usr/bin/env zsh
# zsh の起動時間ベンチマーク。
#
# 計測するもの (いずれも 5 回計測の min を metric として出力):
#   startup       = `zsh -l -i -c exit` の wall time。zshenv → zshrc → zlogin の
#                   全ロードコスト (プロンプト描画・zle 初期化は含まない)
#   first_command = zpty 上に本物の対話 login シェルを spawn し、起動直後に投入した
#                   コマンドの出力が返るまで。= zshrc/zlogin ロード + zle/プロンプト初期化 +
#                   入力受け付け + 最初のコマンド実行完了 (zsh-bench の first-command-lag 相当)
#
# 使い方:
#   tests/zshrc/bench_zsh.sh
#
# 出力: "metric=<name> ms=<value>" 行の列挙。CI では tests/check_bench_budgets.sh が
# tests/zshrc/bench_budgets.ci の予算と突き合わせ、超過で fail する (デグレ検出ゲート)。
#
# ロード環境は tests/zshrc/test_zshrc.sh と同じ模擬 HOME 方式 (CI には dotfiles が
# インストールされていないため): ZDOTDIR の .zshrc/.zlogin から repo の _zshrc/_zlogin を
# source し、lazy loading が反応する dummy rbenv 等を置く。ローカル実行でも実 HOME を
# 汚さない・実環境の brew/anyenv 有無に左右されない、の両方をこの模擬 HOME が担保する。
# 注: 履歴ファイルは常に空 = 実運用で zle 起動時に乗る履歴ロードコスト (履歴量依存。
# 実測 300k 行で first_command +~77ms) は含まない。回帰ゲートとしては履歴はユーザーデータで
# コミット間不変のため意図的に除外している (ローカル体感値と直接比較しないこと)。

set -euo pipefail
unset CDPATH
zmodload zsh/datetime zsh/zpty

# checker の数値検証 (^[0-9]+(\.[0-9]+)?$) と min 抽出の sort -n は dot 小数前提。
# カンマ小数ロケールの影響を数値カテゴリだけ C 固定で封じる (bench_nvim.sh と同じ)
unset LC_ALL
export LC_NUMERIC=C

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)

TMP_ZDOTDIR=$(mktemp -d)
TMP_HOME=$(mktemp -d)
cleanup() { rm -rf "$TMP_ZDOTDIR" "$TMP_HOME"; }
trap cleanup EXIT

print -r -- "source \"$ROOT_DIR/_zshrc\"" > "$TMP_ZDOTDIR/.zshrc"
print -r -- "source \"$ROOT_DIR/_zlogin\"" > "$TMP_ZDOTDIR/.zlogin"

# test_zshrc.sh と同じ模擬 HOME レイアウト (lazy loading の分岐が実際に反応するように)
ln -s "$ROOT_DIR" "$TMP_HOME/dotfiles"
mkdir -p "$TMP_HOME/.rbenv/bin" "$TMP_HOME/.rbenv/shims" "$TMP_HOME/.nodebrew/current/bin"
cat > "$TMP_HOME/.rbenv/bin/rbenv" <<'RBENV'
#!/bin/sh
echo "dummy rbenv"
RBENV
chmod +x "$TMP_HOME/.rbenv/bin/rbenv"

# XDG_* / ZSH_COMPDUMP も模擬 HOME 側へ向ける: 呼び出し元シェルが XDG_CACHE_HOME 等を
# export していると _zshrc の `: "${XDG_CACHE_HOME:=$HOME/.cache}"` が素通りし、実ユーザーの
# zcompdump/.zwc を bench が上書きする (実測確認)。ZSH_COMPDUMP は空代入で十分
# (_zshrc の := は空値でもデフォルト側に落ちる)
typeset -a bench_env=(
  HOME="$TMP_HOME" ZDOTDIR="$TMP_ZDOTDIR"
  XDG_CACHE_HOME="$TMP_HOME/.cache" XDG_STATE_HOME="$TMP_HOME/.local/state" ZSH_COMPDUMP=
)

# --- startup: zsh -l -i -c exit の wall time (ms) ---
# rc ロード自体のハングで CI job の 10 分 timeout まで無言でぶら下がるのを防ぐため、
# timeout(1) があれば 60s で切る (Ubuntu は coreutils 同梱。素の macOS には無いので条件付き)
typeset -a with_timeout=()
command -v timeout >/dev/null 2>&1 && with_timeout=(timeout 60)

measure_startup() {
  local -F t0 t1
  t0=$EPOCHREALTIME
  # 終了ステータスを明示検査: 起動失敗を「高速な計測値」として false-pass させない
  "${with_timeout[@]}" env "${bench_env[@]}" zsh -l -i -c exit </dev/null >/dev/null 2>&1 || {
    print -u2 "startup 計測失敗: zsh -l -i -c exit が非 0 終了 (timeout 60s 超過を含む)"; return 1
  }
  t1=$EPOCHREALTIME
  printf '%.1f\n' $(( (t1 - t0) * 1000 ))
}

# --- first_command: zpty の対話 login シェルへ投入したコマンドが返るまで (ms) ---
# 起動直後 (プロンプト表示前) に書き込む入力は pty の入力バッファが保持するため失われない。
# zle が入力行を再描画するため、送信した文字列は出力側にもそのまま現れる (実測確認済み。
# tty の echo フラグと無関係に起きる)。送信文字列側の marker を '' で分断することで、
# 「再描画された入力」(引用符付き) と「コマンドの出力」(引用符なし) をパターン一致で区別する。
# この分断を外すと入力再描画に誤マッチし、コマンド実行完了前 (zle 初期化直後) の時刻を測ってしまう。
measure_first_command() {
  local marker='__ZSH_BENCH_READY__'
  local -F t0 t1 deadline
  local buf='' chunk
  t0=$EPOCHREALTIME
  zpty zbench env "${bench_env[@]}" zsh -l -i
  zpty -w zbench "print __ZSH_BENCH_''READY__; exit"
  deadline=$(( t0 + 30 ))
  # ⚠️ zpty の -t は引数を取らない (-rt = 非ブロッキング読み)。`zpty -r -t 1 name` と書くと
  # "1" が pty 名として解釈され、存在しない pty への read が黙って失敗し続ける (実測でハマった)
  while (( EPOCHREALTIME < deadline )); do
    if zpty -rt zbench chunk 2>/dev/null; then
      buf+="$chunk"
      [[ "$buf" == *${marker}* ]] && break
    fi
    sleep 0.005
  done
  t1=$EPOCHREALTIME
  # ⚠️ ここに 2>/dev/null を付けないこと: zsh 5.9 では zpty -d への一時リダイレクトが復元されず、
  # サブシェルの fd 2 が /dev/null を指したままになり直後の診断 print -u2 が握り潰される
  # (macOS/Ubuntu 両方で実測)。zpty -d は stderr ノイズを出さないので redirect 自体が不要
  zpty -d zbench || true
  if [[ "$buf" != *${marker}* ]]; then
    print -u2 "first_command 計測失敗: 30s 以内に marker が返らない (zle 初期化以降のハング?)"
    return 1
  fi
  printf '%.1f\n' $(( (t1 - t0) * 1000 ))
}

# --- min-of-5 で metric を 1 本出す ---
# shared runner のノイズは片側性 (遅くなる方にしか出ない) なので min が真の速度の最良推定
# (bench_nvim.sh startup と同じ流儀)。空サンプルは false-pass 防止のため即 fail。
emit_min_of_5() {
  local name="$1" fn="$2" i ms samples=""
  for i in 1 2 3 4 5; do
    ms=$("$fn") || return 1
    # 空・非数値・負値 (EPOCHREALTIME は wall clock なので時計補正で理論上負になり得る) を弾く。
    # 下流の checker も数値検証するが、bench 側で落とす方が原因 (どの計測か) が特定しやすい
    [[ "$ms" =~ '^[0-9]+(\.[0-9]+)?$' ]] || { print -u2 "$name 計測失敗: 不正なサンプル '$ms'"; return 1; }
    samples="$samples $ms"
  done
  print -r -- "$name samples (ms):$samples"
  print -r -- "metric=$name ms=$(print -r -- "$samples" | tr ' ' '\n' | sort -n | grep -m1 .)"
}

emit_min_of_5 startup measure_startup
emit_min_of_5 first_command measure_first_command
