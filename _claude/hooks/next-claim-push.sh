#!/usr/bin/env bash
#
# PostToolUse(Bash) フック: issue を `issues/next/` へ移した (= 着手を claim した) 直後に、
# 「その claim を単独で commit して push したか」をモデルのコンテキストへ注入する。
#
# なぜ: この repo は複数マシンのセッションが同じ issue 列を触る。claim がローカルに
# 留まっている間、他マシンからは「誰も着手していない issue」に見えるため、同じ issue を
# 2 人が同時に片付ける二重作業が起きる (実例 2026-09-02: retro 164 の切り出しを別マシンと
# 同時にやり、ルール追記が衝突した)。claim は **push されて初めて claim になる**。
# 規範: _claude/rules/claim-issue-in-next-and-push.md
#
# 入力: PostToolUse の hook JSON を stdin (.tool_input.command を見る)
# 出力: issues/next/ への移動を含むコマンドのときだけ additionalContext を emit。
#       それ以外では無出力で exit 0 (全 Bash 呼び出しで安全に no-op)。
#
# 🚨 このフックが見えるのは **Claude が Bash で動かした移動だけ**。glogx の issues viewer の
#    `n` キー (Go 側で移動する) は Bash を通らないので発火しない。人が viewer で付けた目印を
#    push するのは人の側の運用で、ここでは扱わない。
# 🚨 判定は「コマンド文字列に issues/next/ への移動が現れたか」の静的検査。実際に移動が
#    成功したかは見ない (成功していなければ次の commit で気づく)。誤発火の害は注意書きが
#    1 回出るだけなので、取りこぼすより出す側へ倒す。
# 🚨 本文は必ず jq --arg で組む。heredoc に状態文字列を直接埋めると、その実改行が JSON の
#    文字列に入り「control characters must be escaped」で壊れる (実測 2026-09-02: 最初の
#    実装がこれで、出力を jq に食わせて初めて分かった)。

input=$(cat)

command -v jq >/dev/null 2>&1 || exit 0

# 適用範囲は「作業中の repo に issues/next/ が実在するとき」だけ。仕事の repo のように
# issues/ を持たない repo では、この規律ごと無効にする (ユーザー要求 2026-09-02)。
# PostToolUse は移動の**後**に走るので、`mkdir -p issues/next && git mv ...` で運用を
# 始めた回もこの時点では存在する = 正しく発火する。
repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0
[ -n "$repo_root" ] && [ -d "$repo_root/issues/next" ] || exit 0

cmd=$(printf '%s' "$input" | jq -r '.tool_input.command // ""')

# 判定: 「mv 系コマンドの**移動先**が issues/next か」を見る。
#
# 🚨 素朴な行 grep では駄目だった (敵対的レビュー 2026-09-02 で P1 が 4 件)。実測した抜け:
#   - `grep` は行単位なので、行継続 (`\` 改行) や複数行スクリプトだと mv と宛先が別行になり外れる
#   - `... issues/next/; git add ...` のように `;` が空白なしで隣接すると外れる
#   - `git mv a issues/next/186-x.md` (宛先にファイル名まで書く形) が外れる
#   - `for f in ...; do git mv "$f" issues/next/; done` が外れる
# そこで **改行を空白に潰し、コマンド区切り (; && || |) でセグメントへ割り、mv を含む
# セグメントの最後のトークンが issues/next を指すか**で判定する。最後のトークン = 移動先、
# という近似なので `git mv issues/next/x.md issues/` (claim の解除) は発火しない。
#
# 残る既知の穴 (静的検査の限界。取りこぼすより出す側へ倒す方針は変えない):
#   - 宛先が変数・相対パス (`D=issues/next; git mv x "$D/"` / `cd issues && git mv x next/`)
#   - `mv -t issues/next/ a.md` のような宛先が末尾に来ない形 (macOS の mv / git mv には -t が無い)
#   - issues/next 内でのリネームは発火する (claim 済みへの再通知。害は注意書き 1 回)
fires=$(printf '%s' "$cmd" | tr '\n' ' ' | awk '
  {
    n = split($0, seg, /;|&&|\|\||\|/)
    for (i = 1; i <= n; i++) {
      if (seg[i] !~ /(^|[[:space:]])(git[[:space:]]+)?mv([[:space:]]|$)/) continue
      # セグメント末尾の空白を落として最後のトークンを取る (= 移動先の近似)
      sub(/[[:space:]]+$/, "", seg[i])
      m = split(seg[i], tok, /[[:space:]]+/)
      if (m < 2) continue
      last = tok[m]
      gsub(/^["'"'"']|["'"'"']$/, "", last)   # 前後のクォートを外す
      if (last ~ /(^|\/)issues\/next(\/|$)/) { print "fire"; exit }
    }
  }')
[ "$fires" = fire ] || exit 0

state=$(
  {
    branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)
    [ -n "$branch" ] && printf 'branch: %s\n' "$branch"
    printf -- '--- git status -sb (自分の他の変更が混ざっていないか) ---\n'
    git status -sb 2>/dev/null | head -20 || true
    printf -- '--- 未 push の commit ---\n'
    unpushed=$(git log --branches --not --remotes --format='%h %s' 2>/dev/null | head -10)
    if [ -n "$unpushed" ]; then printf '%s\n' "$unpushed"; else printf '(なし)\n'; fi
  } 2>/dev/null
)

# 何も取れなければ (git リポジトリ外など) 注入しない
[ -n "$state" ] || exit 0

jq -n --arg ctx "$state" '{
  hookSpecificOutput: {
    hookEventName: "PostToolUse",
    additionalContext: (
      "issues/next/ への移動 (= 着手の claim) を検出した。claim は push されて初めて他マシンから見える。\n" +
      "次の順で閉じること (規範: _claude/rules/claim-issue-in-next-and-push.md):\n" +
      "  1. この移動**だけ**を pathspec で commit する (git mv は旧パスと新パスの両方を書く)\n" +
      "  2. 他に未 push の commit や無関係な変更が無ければ、そのまま push する\n" +
      "  3. push できない状況 (remote が進んでいる / 他の作業が混ざっている) なら、その理由を\n" +
      "     ユーザーに一言伝える。黙ってローカルに claim を寝かせない\n\n" + $ctx
    )
  },
  suppressOutput: true
}'
