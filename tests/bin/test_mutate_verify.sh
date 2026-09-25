#!/bin/bash
#
# bin/mutate-verify (issue 408) の self-test。
#
# 🚨 **この道具は自作の安全機構**なので、正常系だけでは何も確かめたことにならない
# (`_claude/rules/adversarial-review-own-safeguards.md` 節 1)。異常系を実験で作り、
# **終了コードごとに** 1 ケース置く。
#
# fixture は隔離した git repo。本物の dotfiles を対象にすると遅いうえ、worktree を
# 作る対象が実 repo になる (テストが本番の worktree 一覧を汚す)。
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
MV="$ROOT_DIR/bin/mutate-verify"
[ -x "$MV" ] || { echo "NG: bin/mutate-verify が無い"; exit 1; }

work="$(mktemp -d)"
fails=0
# 🚨 EXIT trap は 1 本。後始末が増えてもここへ足す
cleanup() { rm -rf "$work"; }
trap cleanup EXIT

fail() { echo "NG: $*"; fails=$((fails + 1)); }

# 🚨 残骸の基準はテスト**開始時**に採り、判定は**全ケースの後**に置く。
# case 11 の中で採って case 11 で判定していたため、後続の新設ケース (12〜17) は
# 1 つも覆えていなかった (red team 2 周目 P2-4)。TMPDIR 全域を見ないのは、
# **並行して走っている他 run** を残骸と誤検出しないため (同 1 周目 P2-7)
leftovers_before="$(for x in "${TMPDIR:-/tmp}"/dotfiles-mutant.*; do [ -e "$x" ] && echo "$x"; done)"

# --- fixture: guard を持つスクリプトと、それを検証するテスト --------------------------------
make_repo() { # $1=repo dir
  local d="$1"
  mkdir -p "$d/scripts/lib"
  cp "$ROOT_DIR/scripts/lib/worktree_scratch.sh" "$d/scripts/lib/"
  cat > "$d/guard.sh" <<'G'
#!/bin/bash
check() {
  if [ "$1" = "bad" ]; then echo "rejected"; return 1; fi
  echo "accepted"
}
check "$@"
G
  cat > "$d/other.sh" <<'O'
#!/bin/bash
echo "other"
O
  # 検証コマンド。**サマリ行を出す** (zero execution と全 pass を区別するため)
  cat > "$d/verify.sh" <<'V'
#!/bin/bash
ng=0
out="$(bash guard.sh bad 2>&1)"
[ "$out" = "rejected" ] || { echo "FAIL: reject-bad (got=$out)"; ng=1; }
out="$(bash guard.sh ok 2>&1)"
[ "$out" = "accepted" ] || { echo "FAIL: accept-ok (got=$out)"; ng=1; }
echo "ran 2 checks"
exit "$ng"
V
  chmod +x "$d/guard.sh" "$d/other.sh" "$d/verify.sh"
  git -C "$d" init -q .
  git -C "$d" config user.email t@t; git -C "$d" config user.name t
  git -C "$d" add -A; git -C "$d" commit -qm init
}

# mv <repo> <追加引数...> : 既定の引数を埋めて mutate-verify を呼び、rc を返す
mv_run() {
  local d="$1"; shift
  ( cd "$d" && "$MV" --verify 'bash verify.sh' --baseline-expect '^ran 2 checks' "$@" ) \
    > "$work/out.log" 2>&1
}

# ---------------------------------------------------------------------------
# 1. 正常系: guard を外す変異で、狙ったケースが red になる → rc=0
# ---------------------------------------------------------------------------
d="$work/ok"; make_repo "$d"
mv_run "$d" --file guard.sh \
  --apply 'perl -0pi -e "s/if \[ \"\\\$1\" = \"bad\" \]/if false/" "$MUTATE_FILE"' \
  --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 0 ] || { fail "正常系が rc=$rc (期待 0)"; cat "$work/out.log"; }
grep -q '当てた変異' "$work/out.log" || fail "正常系で当てた diff を表示していない"

# ---------------------------------------------------------------------------
# 2. 変異が当たらない (--apply が何もしない) → rc=4
# ---------------------------------------------------------------------------
d="$work/noop"; make_repo "$d"
mv_run "$d" --file guard.sh --apply 'true' --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 4 ] || fail "当たらない変異が rc=$rc (期待 4)"
grep -q '変異が当たっていない' "$work/out.log" || fail "rc=4 の理由が出ていない"

# ---------------------------------------------------------------------------
# 3. 構文エラーの変異 → rc=5 (red でも green でもない第 3 の結果)
# ---------------------------------------------------------------------------
d="$work/syntax"; make_repo "$d"
mv_run "$d" --file guard.sh --apply 'printf "\nif [ \n" >> "$MUTATE_FILE"' --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 5 ] || fail "構文エラーの変異が rc=$rc (期待 5)"
grep -q '第 3 の結果' "$work/out.log" || fail "rc=5 が第 3 の結果だと説明していない"

# ---------------------------------------------------------------------------
# 4. 変異が緑のまま通る (テストが守っていない) → rc=6
#    guard.sh のコメント行を足すだけ = 挙動を変えない変異
# ---------------------------------------------------------------------------
d="$work/green"; make_repo "$d"
mv_run "$d" --file guard.sh --apply 'printf "\n# mutant\n" >> "$MUTATE_FILE"' --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 6 ] || fail "緑のまま通る変異が rc=$rc (期待 6)"
grep -q '何も守っていない' "$work/out.log" || fail "rc=6 の理由が出ていない"

# ---------------------------------------------------------------------------
# 5. red だが --expect に一致しない → rc=7 (別の検査が落ちている)
# ---------------------------------------------------------------------------
d="$work/wrongred"; make_repo "$d"
mv_run "$d" --file guard.sh \
  --apply 'perl -0pi -e "s/echo \"accepted\"/echo \"WRONG\"/" "$MUTATE_FILE"' \
  --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 7 ] || fail "別の検査が落ちた red が rc=$rc (期待 7)"
grep -q '別の検査が先に落ちている' "$work/out.log" || fail "rc=7 の理由が出ていない"
grep -q '固定文字列としては' "$work/out.log" && fail "本当に別の検査が落ちたときにエスケープ漏れの案内を出している"

# ---------------------------------------------------------------------------
# 5b. --expect のメタ文字をエスケープし忘れた → rc=7 のまま、固定文字列では一致すると案内する (issue 423)
#     `(got=accepted)` の括弧は ERE ではグループになり、出力の `(` `)` に一致しない
# ---------------------------------------------------------------------------
d="$work/unescaped"; make_repo "$d"
mv_run "$d" --file guard.sh \
  --apply 'perl -0pi -e "s/if \[ \"\\\$1\" = \"bad\" \]/if false/" "$MUTATE_FILE"' \
  --expect 'FAIL: reject-bad (got=accepted)'
rc=$?
[ "$rc" -eq 7 ] || { fail "エスケープ漏れの --expect が rc=$rc (期待 7)"; cat "$work/out.log"; }
grep -q '固定文字列としては出力に在る' "$work/out.log" || fail "エスケープ漏れを案内していない"

# ---------------------------------------------------------------------------
# 5c. `-` で始まるパターン (`--- FAIL:` など) を grep のオプションと読まない (retro 450 の 4)
#     `-?` は ERE では「- が 0 個か 1 個」なので、fixture の `FAIL: reject-bad` / `ran 2 checks` に一致する
# ---------------------------------------------------------------------------
d="$work/dashpat"; make_repo "$d"
( cd "$d" && "$MV" --verify 'bash verify.sh' --baseline-expect '-?ran 2 checks' --file guard.sh \
  --apply 'perl -0pi -e "s/if \[ \"\\\$1\" = \"bad\" \]/if false/" "$MUTATE_FILE"' \
  --expect '-?FAIL: reject-bad' ) > "$work/out.log" 2>&1
rc=$?
[ "$rc" -eq 0 ] || { fail "- で始まる --expect / --baseline-expect が rc=$rc (期待 0)"; cat "$work/out.log"; }

# ---------------------------------------------------------------------------
# 6. 誤ファイルへの変異 → rc=8
# ---------------------------------------------------------------------------
d="$work/wrongfile"; make_repo "$d"
mv_run "$d" --file guard.sh \
  --apply 'perl -0pi -e "s/if \[ \"\\\$1\" = \"bad\" \]/if false/" "$MUTATE_FILE"; printf "\n# stray\n" >> other.sh' \
  --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 8 ] || fail "誤ファイルへの変異が rc=$rc (期待 8)"
grep -q '誤ファイル' "$work/out.log" || fail "rc=8 の理由が出ていない"

# ---------------------------------------------------------------------------
# 7. baseline が red → rc=3 (判定不能)
# ---------------------------------------------------------------------------
d="$work/basered"; make_repo "$d"
perl -0pi -e 's/echo "accepted"/echo "broken"/' "$d/guard.sh"
git -C "$d" commit -qam "baseline を壊す"
mv_run "$d" --file guard.sh --apply 'printf "\n# m\n" >> "$MUTATE_FILE"' --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 3 ] || fail "baseline red が rc=$rc (期待 3)"
grep -q 'baseline が green でない' "$work/out.log" || fail "rc=3 (baseline) の理由が出ていない"

# ---------------------------------------------------------------------------
# 8. zero execution: rc=0 だが --baseline-expect が出ない → rc=3
#    🚨 これがこの道具の中心。rc だけ見る設計では「1 件も走っていない」を緑と読む
# ---------------------------------------------------------------------------
d="$work/zero"; make_repo "$d"
cat > "$d/verify.sh" <<'V'
#!/bin/bash
# 何も実行せず成功する (テストの絞り込みを間違えた状態)
exit 0
V
chmod +x "$d/verify.sh"; git -C "$d" commit -qam "何も実行しない verify"
mv_run "$d" --file guard.sh --apply 'printf "\n# m\n" >> "$MUTATE_FILE"' --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 3 ] || fail "zero execution が rc=$rc (期待 3)"
grep -q '1 件も実行していない' "$work/out.log" || fail "zero execution の理由が出ていない"

# ---------------------------------------------------------------------------
# 9. 引数の検証 → rc=2
# ---------------------------------------------------------------------------
d="$work/args"; make_repo "$d"
for miss in --expect --baseline-expect; do
  args=(--file guard.sh --apply true --verify 'bash verify.sh')
  [ "$miss" = --expect ] || args+=(--expect x)
  [ "$miss" = --baseline-expect ] || args+=(--baseline-expect x)
  ( cd "$d" && "$MV" "${args[@]}" ) > "$work/out.log" 2>&1
  rc=$?
  [ "$rc" -eq 2 ] || fail "$miss 無しが rc=$rc (期待 2)"
done
( cd "$d" && "$MV" --file guard.sh --apply true --verify v --expect x --baseline-expect y \
    --syntax '' ) > "$work/out.log" 2>&1
# 拡張子でも shebang でも決まらなければ明示必須 (黙って素通しすると構文検査 = rc=5 が消える)
printf 'echo hi\n' > "$d/noext"          # 拡張子なし・shebang なし
cp "$d/guard.sh" "$d/hasbang"             # 拡張子なし・shebang あり (bin/ のスクリプトの形)
git -C "$d" add -A; git -C "$d" commit -qm ext
( cd "$d" && "$MV" --file noext --apply true --verify 'bash verify.sh' \
    --expect x --baseline-expect '^ran' ) > "$work/out.log" 2>&1
rc=$?
[ "$rc" -eq 2 ] || fail "拡張子も shebang も無いファイルが rc=$rc (期待 2)"
grep -q '推定できない' "$work/out.log" || fail "推定できない理由が出ていない"
# 🚨 shebang からの推定が効くこと。`bin/` のスクリプトは拡張子を持たないので、これが無いと
# この道具は自分自身を変異検証できない (実装した日に踏んだ)
( cd "$d" && "$MV" --file hasbang --apply 'printf "\nif [ \n" >> "$MUTATE_FILE"' \
    --verify 'bash verify.sh' --expect x --baseline-expect '^ran 2 checks' ) > "$work/out.log" 2>&1
rc=$?
[ "$rc" -eq 5 ] || fail "shebang から推定した構文検査が効いていない (rc=$rc 期待 5)"
grep -q 'bash -n' "$work/out.log" || fail "shebang 推定の結果を表示していない"

# ---------------------------------------------------------------------------
# 10. 未コミットの変更が worktree へ持ち込まれる
#     🚨 これが無いと「今書いたテスト」を変異検証できない (in-place を避けた設計の要)
# ---------------------------------------------------------------------------
d="$work/dirty"; make_repo "$d"
# 未コミットで「新しい検査」を足す (commit しない)。変異がこの検査に当たることを見る。
# 🚨 perl の s/// で書くと置換文字列の $out が perl 変数として展開されて空になる
# (self-test を書いたその日に踏んだ)。fixture は素直に丸ごと書く
cat > "$d/verify.sh" <<'V'
#!/bin/bash
ng=0
out="$(bash guard.sh bad 2>&1)"
[ "$out" = "rejected" ] || { echo "FAIL: reject-bad (got=$out)"; ng=1; }
out="$(bash guard.sh edge 2>&1)"
[ "$out" = "accepted" ] || { echo "FAIL: uncommitted-check (got=$out)"; ng=1; }
echo "ran 3 checks"
exit "$ng"
V
chmod +x "$d/verify.sh"
# baseline のサマリ行も未コミット側の "ran 3 checks" になるので mv_run の既定は使えない
( cd "$d" && "$MV" --verify 'bash verify.sh' --baseline-expect '^ran 3 checks' --file guard.sh \
  --apply 'perl -0pi -e "s/echo \"accepted\"/echo \"NOPE\"/" "$MUTATE_FILE"' \
  --expect 'FAIL: uncommitted-check' ) > "$work/out.log" 2>&1
rc=$?
[ "$rc" -eq 0 ] || { fail "未コミットの検査を変異検証できない (rc=$rc)"; cat "$work/out.log"; }
# 🚨 未コミットの変更が **作業ツリーに残っている** こと (持ち込みは copy であって move ではない)
grep -q 'ran 3 checks' "$d/verify.sh" || fail "作業ツリーの未コミット変更が消えている"

# ---------------------------------------------------------------------------
# 11. 残骸ゼロ: 作業ツリーが 1 バイトも変わらず、worktree も残らない
#     🚨 この道具の存在理由 (復元事故) そのものなので、必ず見る
# ---------------------------------------------------------------------------
d="$work/clean"; make_repo "$d"
printf '\n# uncommitted\n' >> "$d/guard.sh"          # 未コミットの変更を置いておく
before_hash="$(shasum -a 256 < "$d/guard.sh" | awk '{print $1}')"
before_status="$(git -C "$d" status --porcelain --untracked-files=all)"
mv_run "$d" --file guard.sh \
  --apply 'perl -0pi -e "s/if \[ \"\\\$1\" = \"bad\" \]/if false/" "$MUTATE_FILE"' \
  --expect 'FAIL: reject-bad'
after_hash="$(shasum -a 256 < "$d/guard.sh" | awk '{print $1}')"
[ "$before_hash" = "$after_hash" ] || fail "🚨 作業ツリーのファイルが変異で書き換わった (復元事故)"
[ "$before_status" = "$(git -C "$d" status --porcelain --untracked-files=all)" ] ||
  fail "🚨 作業ツリーの git state が変わった"
# 🚨 パイプ越しの `grep -q` は pipefail 下で一致しても非 0 になる (issue 096)
grep -q 'dotfiles-mutant' <<<"$(git -C "$d" worktree list --porcelain)" &&
  fail "🚨 worktree が残っている"

# ---------------------------------------------------------------------------
# 12. 🚨 **すでに dirty / untracked なファイル**への誤変異も落とす → rc=8
#     red team P1-1: porcelain の「行」だけを比べると ` M x` → ` M x` で差分が出ず、
#     **この道具が自分で作る集合** (未コミット差分の持ち込み) がまるごと死角になっていた
# ---------------------------------------------------------------------------
mutate_guard='perl -0pi -e "s/if \[ \"\\\$1\" = \"bad\" \]/if false/" "$MUTATE_FILE"'
for kind in dirty-tracked untracked; do
  d="$work/others-$kind"; make_repo "$d"
  if [ "$kind" = dirty-tracked ]; then
    printf '\n# wip\n' >> "$d/other.sh"          # 未コミットの変更 (実運用で普通)
    victim=other.sh
  else
    printf 'echo new\n' > "$d/newfile.sh"        # untracked
    victim=newfile.sh
  fi
  mv_run "$d" --file guard.sh \
    --apply "$mutate_guard; echo STRAY >> $victim" --expect 'FAIL: reject-bad'
  rc=$?
  [ "$rc" -eq 8 ] || fail "$kind な別ファイルへの変異が rc=$rc (期待 8)"
done

# ---------------------------------------------------------------------------
# 13. 同名 basename / バックアップファイルを「--file 自身」と読まない → rc=8
#     red team P2-4: 除外が部分一致だと sub/guard.sh や guard.sh.bak を見逃す
# ---------------------------------------------------------------------------
d="$work/samename"; make_repo "$d"
mkdir -p "$d/sub"; cp "$d/other.sh" "$d/sub/guard.sh"
git -C "$d" add -A; git -C "$d" commit -qm sub
mv_run "$d" --file guard.sh --apply "$mutate_guard; echo STRAY >> sub/guard.sh" \
  --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 8 ] || fail "同名 basename の別ファイルへの変異が rc=$rc (期待 8)"

# ---------------------------------------------------------------------------
# 14. 🚨 --apply が **worktree の外 (元の作業ツリー)** を書いたら緑を返さない → rc=9
#     red team P1-2: 射程 1 (復元事故) の中心。警告 1 行で rc=0 を返していた
# ---------------------------------------------------------------------------
d="$work/outside"; make_repo "$d"
printf '\n# uncommitted impl\n' >> "$d/other.sh"   # 元 repo 側の未コミット実装
mv_run "$d" --file guard.sh \
  --apply "$mutate_guard; echo CLOBBER >> $d/other.sh" --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 9 ] || fail "worktree の外への書き込みが rc=$rc (期待 9)"
grep -q '元の作業ツリーが変わった' "$work/out.log" || fail "rc=9 の理由が出ていない"

# ---------------------------------------------------------------------------
# 15. --expect が baseline にも出る pattern なら使い方の誤りとして拒否 → rc=2
#     red team P1-3: verbose runner では常態で、別のテストが落ちた red を「想定どおり」と読む
# ---------------------------------------------------------------------------
d="$work/vague"; make_repo "$d"
mv_run "$d" --file guard.sh --apply "$mutate_guard" --expect 'ran 2 checks'
rc=$?
[ "$rc" -eq 2 ] || fail "baseline にも出る --expect が rc=$rc (期待 2)"
grep -q 'baseline の出力にも出ている' "$work/out.log" || fail "rc=2 (曖昧な expect) の理由が出ていない"

# ---------------------------------------------------------------------------
# 16. untracked な --file でも「当てた変異」の diff が出る (手順 1.6 の入力が消えない)
#     red team P2-5: 新規テストファイルを変異検証するのが主要ユースケースなのに無音だった
# ---------------------------------------------------------------------------
d="$work/untracked-file"; make_repo "$d"
cp "$d/guard.sh" "$d/newguard.sh"                    # untracked のまま
cat > "$d/verify.sh" <<'V'
#!/bin/bash
ng=0
out="$(bash newguard.sh bad 2>&1)"
[ "$out" = "rejected" ] || { echo "FAIL: reject-bad (got=$out)"; ng=1; }
echo "ran 1 check"
exit "$ng"
V
chmod +x "$d/verify.sh"
( cd "$d" && "$MV" --verify 'bash verify.sh' --baseline-expect '^ran 1 check' --file newguard.sh \
    --apply "$mutate_guard" --expect 'FAIL: reject-bad' ) > "$work/out.log" 2>&1
rc=$?
[ "$rc" -eq 0 ] || { fail "untracked な --file が rc=$rc (期待 0)"; cat "$work/out.log"; }
grep -q '^+.*if false' "$work/out.log" || fail "🚨 untracked な --file の diff が出ていない (手順 1.6 の入力が消える)"

# ---------------------------------------------------------------------------
# 17. untracked なファイルも worktree へ持ち込まれる (lib の主張のテスト)
#     red team P2-6: 「untracked も持ち込む」はコード上の主張なのにテストが無かった
# ---------------------------------------------------------------------------
d="$work/carry-untracked"; make_repo "$d"
cat > "$d/verify.sh" <<'V'
#!/bin/bash
ng=0
[ -f helper.sh ] || { echo "FAIL: untracked-not-carried"; exit 1; }
out="$(bash guard.sh bad 2>&1)"
[ "$out" = "rejected" ] || { echo "FAIL: reject-bad (got=$out)"; ng=1; }
echo "ran 2 checks"
exit "$ng"
V
chmod +x "$d/verify.sh"
printf 'echo helper\n' > "$d/helper.sh"             # untracked。持ち込まれないと baseline が red
mv_run "$d" --file guard.sh --apply "$mutate_guard" --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 0 ] || { fail "untracked が持ち込まれていない (rc=$rc)"; cat "$work/out.log"; }

# ---------------------------------------------------------------------------
# 19. index (staging) だけを動かす改変も検出する → rc=9
#     red team 2 周目 P1-2: 内容 hash だけを見ると `git add` / `git reset` が不可視になる
#     (旧実装の porcelain 行比較は見ていた能力なので、落とさないよう固定する)
# ---------------------------------------------------------------------------
d="$work/indexonly"; make_repo "$d"
printf '\n# wip\n' >> "$d/other.sh"                  # unstaged のまま置く
mv_run "$d" --file guard.sh \
  --apply "$mutate_guard; git -C $d add other.sh" --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 9 ] || fail "index だけを動かす改変が rc=$rc (期待 9)"

# ---------------------------------------------------------------------------
# 20. --file を `./guard.sh` と書いても誤検出しない (git の正規形へ揃える)
#     red team 2 周目 P2-1: 見逃しでなく**誤検出**で、rc=8 の信頼を壊す
# ---------------------------------------------------------------------------
d="$work/dotslash"; make_repo "$d"
mv_run "$d" --file ./guard.sh --apply "$mutate_guard" --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 0 ] || fail "--file ./guard.sh が rc=$rc (期待 0 — 正規形へ揃えていない)"

# ---------------------------------------------------------------------------
# 21. 異常終了 (rc=4〜7) でも元 repo が壊れていれば rc=9 を優先する
#     red team 2 周目 P2-2: rc=6 を受けた呼び出し側が「変異が弱い」と読んで
#     同じ --apply で再実行し、元 repo をもう一度壊す形が残っていた
# ---------------------------------------------------------------------------
d="$work/rc9-priority"; make_repo "$d"
printf '\n# uncommitted impl\n' >> "$d/other.sh"
mv_run "$d" --file guard.sh \
  --apply "printf '\n# harmless\n' >> \"\$MUTATE_FILE\"; echo CLOBBER >> $d/other.sh" \
  --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 9 ] || fail "緑のまま通る変異 + 元 repo 改変が rc=$rc (期待 9 — rc=6 を優先してはいけない)"

# ---------------------------------------------------------------------------
# 22. --verify が元 repo を壊したら、baseline 系の失敗でも rc=9 を優先する
#     red team 3 周目 P1-1: rc=3「baseline が green でない」は再実行を最も強く促す rc なので、
#     ここが素の exit だと「同じコマンドで再実行して元 repo をもう一度壊す」が残る
# ---------------------------------------------------------------------------
d="$work/verify-clobber"; make_repo "$d"
printf '\n# uncommitted impl\n' >> "$d/other.sh"
mv_run "$d" --file guard.sh --apply "$mutate_guard" --expect 'FAIL: reject-bad' \
  --verify "echo CLOBBER >> $d/other.sh; bash verify.sh; exit 1"
rc=$?
[ "$rc" -eq 9 ] || fail "--verify が元 repo を壊したのに rc=$rc (期待 9)"

# ---------------------------------------------------------------------------
# 23. index 上に rename がある状態でも snapshot のパースが崩れない
#     red team 3 周目 P2-1: `R  <new>\0<old>\0` の 2 レコードを 1 エントリとして読むと、
#     旧パス側が恒久的に <missing> になる (2 周目 P1-5 の「不可視」の別入口)
# ---------------------------------------------------------------------------
# 🚨 **「rename があっても誤検出しない」だけを見てはいけない**。誤パースは before/after の
#    両方に同じゴミ行を作るので判定は反転せず、**そのテストは何も守らない** (実測: 読み飛ばしを
#    外す変異が緑のまま通った)。壊れるのは**診断**なので、rc=9 の diff を観測点にする。
#    `other.sh` を誤パースすると `${entry:3}` = `er.sh` というゴミパスが snapshot に入る
d="$work/rename"; make_repo "$d"
mv_run "$d" --file guard.sh \
  --apply "$mutate_guard; git -C $d mv other.sh renamed.sh" --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 9 ] || fail "変異中の rename で rc=$rc (期待 9 — 元 repo を触っている)"
# 診断は `diff` の `< path<TAB>…` / `> path<TAB>…` 行。ゴミのパスはそこで行頭に来る
# (`--no-renames` なので other.sh は正当に「削除」として出る。部分文字列で探すとそれに当たる)
grep -qE '^[<>] er\.sh'$'\t' "$work/out.log" &&
  fail "🚨 rename の旧パスが誤パースされ、実在しないパスが診断に混ざっている"
grep -q 'renamed\.sh' "$work/out.log" ||
  fail "rename の新パスが診断に出ていない"

# ---------------------------------------------------------------------------
# 24. --file の表記ゆれ (大文字小文字 / symlink) で誤検出しない
#     red team 3 周目 P2-2: macOS は既定で case-insensitive なので `--file GUARD.SH` が通り、
#     除外だけ外れて **--file 自身が誤ファイル扱い**になる
# ---------------------------------------------------------------------------
d="$work/casefold"; make_repo "$d"
mv_run "$d" --file GUARD.SH --apply "$mutate_guard" --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 0 ] || fail "--file GUARD.SH が rc=$rc (期待 0 — git の表記へ揃えていない)"
d="$work/symlinkfile"; make_repo "$d"
ln -s guard.sh "$d/link.sh"; git -C "$d" add -A; git -C "$d" commit -qm link
mv_run "$d" --file link.sh --apply "$mutate_guard" --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 2 ] || fail "--file が symlink のとき rc=$rc (期待 2 — 実体を指定させる)"

# ---------------------------------------------------------------------------
# 25. baseline と変異の run で木の状態が同じ (道具の一時ファイル・intent-to-add で差を作らない)
#     red team 4 周目 P1-2: add -N を変異の run の前だけで当て、ログも worktree に置いていたので、
#     `git ls-files` / untracked を数える検証では**挙動を変えない変異**が red (rc=0) になった
# ---------------------------------------------------------------------------
d="$work/samestate"; make_repo "$d"
printf '#!/bin/bash\necho new\n' > "$d/newtest.sh"   # untracked の --file
cat > "$d/verify.sh" <<'V'
#!/bin/bash
n_tracked="$(git ls-files | wc -l | tr -d ' ')"
n_untracked="$(git ls-files --others --exclude-standard | wc -l | tr -d ' ')"
echo "state tracked=$n_tracked untracked=$n_untracked" > /dev/null
# 🚨 食い違ったら**非 0 で終わる**こと。FAIL を出すだけで exit 0 だと、退行 (baseline と変異の run で
# 木が違う) でも変異の run が緑になり、期待の rc=6 と同じ結果になって何も守らない (変異検証で実測)
ng=0
[ "$n_tracked" = "$(cat .expected-tracked 2>/dev/null || echo "$n_tracked")" ] || { echo "FAIL: tracked-count"; ng=1; }
echo "ran 2 checks"
[ -z "$(git status --porcelain -- .mv-baseline.log .mv-mutant.log mutate-verify.patch .mutate-verify.patch)" ] || { echo "FAIL: tool-files-visible"; exit 1; }
git ls-files | wc -l | tr -d ' ' > .expected-tracked
exit "$ng"
V
git -C "$d" add verify.sh; git -C "$d" commit -qm verify2
printf '.expected-tracked\n' > "$d/.gitignore"; git -C "$d" add .gitignore; git -C "$d" commit -qm ign
mv_run "$d" --file newtest.sh --apply 'printf "# mutant\n" >> "$MUTATE_FILE"' --expect 'FAIL: tracked-count'
rc=$?
[ "$rc" -eq 6 ] || { fail "🚨 挙動を変えない変異が rc=$rc (期待 6 — baseline と変異の run で木が違う)"; tail -20 "$work/out.log"; }
grep -q 'FAIL: tool-files-visible' "$work/out.log" && fail "🚨 道具の一時ファイルが検証コマンドから見えている"

# ---------------------------------------------------------------------------
# 26. rc=0 でも、巻き添えで落ちた検査を必ず見せる (機械では判定しない。人が見る材料)
#     red team 4 周目 P1-1: スクリプトを丸ごと殺す変異でも --expect が 1 行出れば rc=0 だった
# ---------------------------------------------------------------------------
d="$work/collateral"; make_repo "$d"
mv_run "$d" --file guard.sh --apply 'perl -pi -e "s/^check \"\\\$\@\"/exit 3/" "$MUTATE_FILE"' \
  --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 0 ] || { fail "全部を壊す変異が rc=$rc (期待 0 — 想定の検査は落ちている)"; tail -20 "$work/out.log"; }
grep -q '^> FAIL: accept-ok' <<<"$(sed -n '/出力の差/,$p' "$work/out.log")" ||
  fail "🚨 巻き添えで落ちた検査 (accept-ok) を表示していない"

# ---------------------------------------------------------------------------
# 27. clean な元 repo で HEAD を動かされても rc=9 (status のエントリだけでは見えない)
#     red team 4 周目 P2-1
# ---------------------------------------------------------------------------
d="$work/headmove"; make_repo "$d"
printf '#\n' >> "$d/other.sh"; git -C "$d" commit -qam second
mv_run "$d" --file guard.sh --apply "$mutate_guard" \
  --verify "bash verify.sh; git -C $d checkout -q HEAD~1 2>/dev/null; true" --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 9 ] || fail "🚨 元 repo の HEAD が動いたのに rc=$rc (期待 9)"
# HEAD だけが動き、index も作業ツリーも変わらない形 (空 commit)。index の hash では捕まらないので、
# snapshot の HEAD の行を外す退行はこちらでしか見えない (変異検証で実測)
d="$work/headonly"; make_repo "$d"
mv_run "$d" --file guard.sh --apply "$mutate_guard" \
  --verify "bash verify.sh; git -C $d commit -q --allow-empty -m moved; true" --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 9 ] || fail "🚨 元 repo の HEAD だけが動いた (空 commit) のに rc=$rc (期待 9)"

# ---------------------------------------------------------------------------
# 28. worktree 側の rename (`mv` で名前を変えた untracked + 削除) で rc=8 を誤検出しない
#     red team 4 周目 P2-3: Y 列の ` R` を 2 レコードとして読まず、旧パスを誤読した
# ---------------------------------------------------------------------------
d="$work/wtrename"; make_repo "$d"
mv "$d/guard.sh" "$d/guard2.sh"
sed -i '' 's/guard\.sh/guard2.sh/g' "$d/verify.sh"
mv_run "$d" --file guard2.sh \
  --apply 'perl -0pi -e "s/if \[ \"\\\$1\" = \"bad\" \]/if false/" "$MUTATE_FILE"' --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 0 ] || { fail "🚨 worktree 側の rename で rc=$rc (期待 0)"; tail -10 "$work/out.log"; }

# ---------------------------------------------------------------------------
# 29. --file の glob 文字で別ファイルを選ばない (`:(icase)` が glob として効いた)
#     red team 4 周目 P2-4
# ---------------------------------------------------------------------------
d="$work/globname"; make_repo "$d"
cp "$d/guard.sh" "$d/g[1].sh"; cp "$d/guard.sh" "$d/g1.sh"
git -C "$d" add -A; git -C "$d" commit -qm glob
mv_run "$d" --file 'g[1].sh' --apply 'printf "# x\n" >> "$MUTATE_FILE"' --expect 'FAIL: reject-bad'
grep -q 'a/g1\.sh' "$work/out.log" && fail "🚨 --file 'g[1].sh' で g1.sh を変異させた"
grep -q 'a/g\[1\]\.sh' "$work/out.log" || fail "--file 'g[1].sh' の diff が出ていない"

# ---------------------------------------------------------------------------
# 30. 構文検査が変異前の本物でも落ちるなら判定不能 (rc=3)。zsh の .sh に bash -n が当たった
#     red team 4 周目 P2-5
# ---------------------------------------------------------------------------
d="$work/zshsh"; make_repo "$d"
printf '#!/usr/bin/env zsh\nfor a (x) { echo $a }\n' > "$d/z.sh"
git -C "$d" add z.sh; git -C "$d" commit -qm zsh
mv_run "$d" --file z.sh --apply 'printf "# x\n" >> "$MUTATE_FILE"' --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 3 ] || fail "🚨 本物でも落ちる構文検査で rc=$rc (期待 3)"
grep -q '変異前の本物' "$work/out.log" || fail "rc=3 の理由 (構文検査の選び方) が出ていない"

# ---------------------------------------------------------------------------
# 31. untracked の持ち込みに失敗したら判定不能 (rc=3)。元と違う木の上で測らない
#     red team 4 周目 P3
# ---------------------------------------------------------------------------
d="$work/cpfail"; make_repo "$d"
printf 'x\n' > "$d/unreadable.txt"; chmod 000 "$d/unreadable.txt"
mv_run "$d" --file guard.sh --apply "$mutate_guard" --expect 'FAIL: reject-bad'
rc=$?
chmod 644 "$d/unreadable.txt"
[ "$rc" -eq 3 ] || fail "🚨 untracked を持ち込めないのに rc=$rc (期待 3)"

# ---------------------------------------------------------------------------
# 32. lib の後始末が、並行して走る別 run (pid が接頭辞関係) のプロセスを殺さない
#     red team 4 周目 P2-2: `pkill -f <path>` は正規表現の部分一致なので .452 が .4521 に当たった
# ---------------------------------------------------------------------------
kt="$work/killtest"; mkdir -p "$kt/dotfiles-mutant.4521"
perl -e 'sleep 60' "$kt/dotfiles-mutant.4521/guard.sh" &
victim=$!
perl -e 'sleep 60' "$kt/dotfiles-mutant.452/guard.sh" &
target=$!
# パスの直後に引用符や `;` が続く argv (`sh -c 'cd <path>; …'` の形) も当の worktree のもの (5 周目 P3-6)
perl -e 'sleep 60' "$kt/dotfiles-mutant.452\";x" &
target2=$!
( . "$ROOT_DIR/scripts/lib/worktree_scratch.sh"; wts_kill_holders "$kt/dotfiles-mutant.452" )
sleep 0.2  # kill の配送を待つ (成立条件のポーリングにできない否定の assert: 生き残ることを見る)
kill -0 "$victim" 2>/dev/null || fail "🚨 wts_kill_holders が別 run (.4521) のプロセスを殺した"
kill -0 "$target" 2>/dev/null && fail "wts_kill_holders が当の worktree (.452) のプロセスを止めない"
kill -0 "$target2" 2>/dev/null && fail "wts_kill_holders がパスの直後に引用符が続く argv を止めない"
kill "$victim" "$target" "$target2" 2>/dev/null; wait "$victim" "$target" "$target2" 2>/dev/null

# ---------------------------------------------------------------------------
# 33. 最初の失敗で止まる runner でも、巻き添えで**走らなくなった**検査が見える (消えた行も出す)
#     red team 5 周目 P1-1: 増えた行だけを出していたので、`set -e` の検証では後続が消えるだけで見えなかった
# ---------------------------------------------------------------------------
d="$work/failfast"; make_repo "$d"
cat > "$d/verify.sh" <<'V'
#!/bin/bash
set -e
out="$(bash guard.sh bad 2>&1 || true)"
[ "$out" = "rejected" ] || { echo "FAIL: reject-bad (got=$out)"; exit 1; }
echo "ok: reject-bad"
out="$(bash guard.sh ok 2>&1)"
[ "$out" = "accepted" ] || { echo "FAIL: accept-ok"; exit 1; }
echo "ok: accept-ok"
echo "ran 2 checks"
V
git -C "$d" commit -qam failfast
mv_run "$d" --file guard.sh --apply 'perl -pi -e "s/^check \"\\\$\@\"/exit 3/" "$MUTATE_FILE"' --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 0 ] || fail "fail-fast の検証で rc=$rc (期待 0)"
grep -q '^< ok: accept-ok' <<<"$(sed -n '/出力の差/,$p' "$work/out.log")" ||
  fail "🚨 巻き添えで走らなくなった検査 (accept-ok) の消えた行を表示していない"

# ---------------------------------------------------------------------------
# 34. core.quotePath=true (git の既定) でも非 ASCII 名の --file を扱える
#     red team 5 周目 P2-2: 正規化の ls-files が -z なしで、引用された名前がそのまま --file になった
# ---------------------------------------------------------------------------
d="$work/quotepath"; make_repo "$d"
git -C "$d" mv guard.sh 設定.sh; sed -i '' 's/guard\.sh/設定.sh/g' "$d/verify.sh"
git -C "$d" commit -qam rename; git -C "$d" config core.quotePath true
mv_run "$d" --file 設定.sh --apply "$mutate_guard" --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 0 ] || { fail "🚨 core.quotePath=true で非 ASCII 名の --file が rc=$rc (期待 0)"; tail -5 "$work/out.log"; }

# ---------------------------------------------------------------------------
# 35. 構文検査の副作用 (py_compile の __pycache__) で、baseline と変異の run の木が変わらない
#     red team 5 周目 P2-3: 構文検査を baseline の後に当てていたので、変異の run だけに生成物が見えた
# ---------------------------------------------------------------------------
if command -v python3 >/dev/null; then
  d="$work/pycache"; make_repo "$d"
  printf 'def f():\n    return 1\n' > "$d/g.py"
  cat > "$d/verify.sh" <<'V'
#!/bin/bash
n="$(git ls-files --others --exclude-standard | wc -l | tr -d ' ')"
echo "ran 2 checks"
[ "$n" = "$(cat .n 2>/dev/null || echo "$n")" ] || { echo "FAIL: tree-changed (untracked=$n)"; exit 1; }
echo "$n" > .n
V
  printf '.n\n' > "$d/.gitignore"
  git -C "$d" add -A; git -C "$d" commit -qm py
  mv_run "$d" --file g.py --apply 'printf "# x\n" >> "$MUTATE_FILE"' --expect 'FAIL: tree-changed'
  rc=$?
  [ "$rc" -eq 6 ] || { fail "🚨 構文検査の副作用で 2 回の run の木が変わった (rc=$rc, 期待 6)"; tail -5 "$work/out.log"; }
fi

# ---------------------------------------------------------------------------
# 36. 元 repo で同じ commit の別ブランチへ切り替えられても rc=9 (commit hash だけでは見えない)
#     red team 5 周目 P3-3
# ---------------------------------------------------------------------------
d="$work/branchswitch"; make_repo "$d"
git -C "$d" branch other
mv_run "$d" --file guard.sh --apply "$mutate_guard" \
  --verify "bash verify.sh; git -C $d switch -q other; true" --expect 'FAIL: reject-bad'
rc=$?
[ "$rc" -eq 9 ] || fail "🚨 元 repo のブランチが切り替わったのに rc=$rc (期待 9)"

# ---------------------------------------------------------------------------
# 末尾. 🚨 全ケースを通した**後**の残骸ゼロ。
#     この判定は必ずファイル末尾に置く — 2 周目に「末尾に置く」と書きながら、その後で
#     新ケースを 3 つ判定より前に足して同じ穴を再生産した (red team 3 周目 P1-2)。
#     **ケースを足すときは、このブロックより上に足すこと**
# ---------------------------------------------------------------------------
for leftover in "${TMPDIR:-/tmp}"/dotfiles-mutant.*; do
  [ -e "$leftover" ] || continue
  grep -qxF "$leftover" <<<"$leftovers_before" || fail "🚨 この run が残した worktree: $leftover"
done

# ケース数は見出しから数える (文字列で固定すると、足した・欠番にしたケースと食い違う。red team 4 周目 P3)
# (番号の付いた見出しだけ。末尾の残骸判定は番号を付けていないので数えない)
ncases="$(grep -cE '^# [0-9]+\. ' "$0")"
if [ "$fails" -eq 0 ]; then echo "OK: mutate-verify ($ncases ケース)"; else echo "FAILED: $fails"; exit 1; fi
