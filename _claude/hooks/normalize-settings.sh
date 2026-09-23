#!/bin/sh
# settings.json を「共有設定だけ・キー順は決定論」に正規化する。
#
# 2 つのことをする:
#   1. 揮発キー (model / effortLevel / modelSettings / advisorModel / voice / autoMode) を
#      非追跡の settings.local.json へ退避する
#   2. 残りを再帰的にキーソートする (jq -S)。配列は順序が意味を持つので触らない
#      (hooks の実行順・permissions の並び)
#
# 背景 1 (揮発キー): /model や /effort はマシン固有・頻繁に変わるのに、Claude Code は
# 追跡対象の settings.json へ直接書く。放置すると git pull のたびにコンフリクトする。
# settings.local.json の値は settings.json より優先されるため、退避後も選択は効き続ける。
#
# 背景 2 (キー順): Claude Code は設定を書き換えるとき、そのキーを削除して別の位置へ
# 再挿入する (実測 0f884dcb: preferredNotifChannel が 4 番目から末尾付近へ移動)。
# 🚨 ソートは churn を消さない。「次にこのスクリプトが走るまで順序が崩れたままになる」
# 窓は残る (設定を変えた直後に commit するとその崩れが履歴に入る)。ソートが保証するのは
# 「正規化後の形が入力の履歴に依らず一意」までで、diff を静かにするのはそこ。
#
# 実行タイミング: SessionStart hook (毎セッション冒頭 = その回の commit / pull より前) と
# `make pull` (セッションを起こさずに pull するとき)。書き込み直後を捕まえる Stop /
# SessionEnd は採らない — Stop は CLI の書き込みと競合し、SessionEnd は crash / kill で
# 発火しない。SessionStart は必ず来て冪等なので、前回の書き込みをここで畳む。
# 加えて ConfigChange hook (セッション中に設定ファイルが変わった直後) でも走らせ、
# /model 等の書き込みを次のセッションまで dirty のまま残さない。自分の書き戻しで
# ConfigChange が再発火しても、変化が無ければ書かないので 2 回目で止まる。
# 🚨 ConfigChange では exit 2 が「その変更を block する」意味になる (CLI の書き込みを
#    取り消しうる)。jq の usage error 等の rc=2 を漏らさないよう、EXIT trap で 1 に丸める。
#
# 🚨 SessionStart hook の stdout はセッションのコンテキストへ注入される。成功時は無言にし、
#    退避の通知も含めて出力は stderr へ出す (`make pull` では端末に出るので情報は失わない)。
# 🚨 settings.json は dotfiles への symlink。書き戻しは `cat >` で行う (mv は symlink を
#    実ファイルに置き換えてしまい、以後 dotfiles 側へ反映されなくなる)。
set -eu

CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-${HOME}/.claude}"
SETTINGS="${CLAUDE_DIR}/settings.json"
LOCAL="${CLAUDE_DIR}/settings.local.json"

# 退避対象の揮発キー。共有したくない・CLI が勝手に書き換えるものだけ。
# modelSettings は /effort の書き込み先。CLI が top-level の effortLevel から
# modelSettings.<model>.effortLevel のネスト形式へ移したため、effortLevel だけでは
# 空振りする (2026-09-02 実測: dc94919 が model と modelSettings を追跡ファイルへ持ち込み、
# /model・/effort のたびに settings.json が dirty になっていた = 共有 tree の ff pull を阻む)。
# top-level の effortLevel は旧形式の残骸を回収するために残す。
VOLATILE='["model","effortLevel","modelSettings","advisorModel","voice","autoMode"]'

[ -f "$SETTINGS" ] || exit 0
command -v jq >/dev/null 2>&1 || { echo "normalize-settings: jq not found; skip" >&2; exit 0; }
jq empty "$SETTINGS" 2>/dev/null || { echo "normalize-settings: settings.json is invalid JSON; skip" >&2; exit 0; }

tmp_dir=$(mktemp -d)
on_exit() {
  rc=$?
  rm -rf "$tmp_dir"
  [ "$rc" -eq 2 ] && rc=1
  exit "$rc"
}
trap on_exit EXIT

# 1. settings.json に含まれる揮発キーを settings.local.json へ退避する
extracted=$(jq -c --argjson keys "$VOLATILE" \
  'with_entries(select(.key as $k | $keys | index($k)))' "$SETTINGS")
if [ "$extracted" != "{}" ]; then
  [ -f "$LOCAL" ] || printf '{}\n' > "$LOCAL"
  if jq empty "$LOCAL" 2>/dev/null; then
    printf '%s' "$extracted" > "$tmp_dir/extracted.json"
    # settings.json 側が最新なので extracted を優先 (local * extracted)
    jq -S -s '.[0] * .[1]' "$LOCAL" "$tmp_dir/extracted.json" > "$tmp_dir/local.json"
    jq empty "$tmp_dir/local.json" 2>/dev/null \
      || { echo "normalize-settings: merge produced invalid JSON; abort" >&2; exit 1; }
    cat "$tmp_dir/local.json" > "$LOCAL"
    echo "normalize-settings: moved $(printf '%s' "$extracted" | jq -r 'keys | join(", ")') -> settings.local.json" >&2
  else
    # local が壊れているときは退避できない = settings.json から消すと値が失われるので、
    # 削除もしない (ソートだけ行う)。「判定不能」を成功にも失敗にも丸めない。
    echo "normalize-settings: settings.local.json is invalid JSON; keep volatile keys in settings.json" >&2
    VOLATILE='[]'
  fi
fi

# 2. 揮発キーの削除とキーソートを 1 パスで行い、1 回だけ書き戻す
#    (並行セッションの SessionStart が割り込みうるので、read-modify-write は最小回数にする)
jq -S --argjson keys "$VOLATILE" 'delpaths([$keys[] | [.]])' "$SETTINGS" > "$tmp_dir/settings.json"
jq empty "$tmp_dir/settings.json" 2>/dev/null \
  || { echo "normalize-settings: strip produced invalid JSON; abort" >&2; exit 1; }
cmp -s "$tmp_dir/settings.json" "$SETTINGS" && exit 0  # 変化なしなら書かない (mtime を動かさない)
cat "$tmp_dir/settings.json" > "$SETTINGS"  # symlink を壊さないため cat（mv 不可）
