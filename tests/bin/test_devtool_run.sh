#!/bin/bash
# bin/devtool-run: devtool/bin/<道具> に、dotfiles の道具を解決した環境変数を渡して起こすこと。
# 偽の dotfiles の root (bin/devtool-run の写し・偽の runtimeout.sh・偽の skill-tmpdir・devtool/bin の偽の道具) で回す。
# devtool-run は自分の実体の置き場から root を決めるので、写しは偽の root を見る。
set -u
unset CDPATH

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
FAIL_COUNT=0
fail() { echo "✗ $1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
pass() { echo "✓ $1"; }

T="$(mktemp -d)" || { echo "✗ mktemp -d に失敗"; exit 1; }
trap 'rm -rf "$T"' EXIT
T="$(cd -P "$T" && pwd -P)" || exit 1
R="$T/root"
mkdir -p "$R/bin/lib" "$R/devtool/bin" || exit 1
cp "$ROOT_DIR/bin/devtool-run" "$R/bin/devtool-run" || exit 1
printf '#!/bin/bash\nexit 0\n' > "$R/bin/runtimeout-bin"
printf '#!/bin/bash\nexit 0\n' > "$R/bin/skill-tmpdir"
chmod +x "$R/bin/runtimeout-bin" "$R/bin/skill-tmpdir"
# 偽の runtimeout_resolve: FAKE_RESOLVE_FAIL=1 なら解決に失敗する
cat > "$R/bin/lib/runtimeout.sh" <<'EOF'
runtimeout_resolve() {
  [ "${FAKE_RESOLVE_FAIL:-0}" = 1 ] && { echo "✗ 偽: ビルドできない" >&2; return 1; }
  RUNTIMEOUT="$1/bin/runtimeout-bin"; export RUNTIMEOUT
}
EOF
cat > "$R/devtool/bin/show-env" <<'EOF'
#!/bin/bash
printf 'RT=%s\nST=%s\n' "${DEVTOOL_RUNTIMEOUT-<unset>}" "${DEVTOOL_SKILL_TMPDIR-<unset>}"
printf 'ARG=%s\n' "$@"
exit "${SHOW_ENV_RC:-0}"
EOF
chmod +x "$R/devtool/bin/show-env"
printf 'not executable\n' > "$R/devtool/bin/plain"

run() { env -u DEVTOOL_RUNTIMEOUT -u DEVTOOL_SKILL_TMPDIR "$@" > "$T/out" 2> "$T/err"; }

# 1. 解決した絶対パスを渡し、引数をそのまま渡す
run "$R/bin/devtool-run" show-env 'a b' --x; rc=$?
if [ "$rc" -eq 0 ] && grep -qxF "RT=$R/bin/runtimeout-bin" "$T/out" && grep -qxF "ST=$R/bin/skill-tmpdir" "$T/out" \
  && [ "$(grep -c '^ARG=' "$T/out")" -eq 2 ] && grep -qxF 'ARG=a b' "$T/out" && grep -qxF 'ARG=--x' "$T/out"; then
  pass "解決した runtimeout と skill-tmpdir の絶対パスを渡し、引数を分割せずに渡す"
else
  fail "環境変数か引数が違う (rc=$rc): $(cat "$T/out" "$T/err")"
fi

# 2. 道具の rc をそのまま返す
run env SHOW_ENV_RC=70 "$R/bin/devtool-run" show-env; rc=$?
[ "$rc" -eq 70 ] && pass "道具の rc (70) をそのまま返す" || fail "道具の rc が変わった: $rc"

# 3. runtimeout を解決できなくても道具は起こし、DEVTOOL_RUNTIMEOUT を空で渡して知らせる
run env FAKE_RESOLVE_FAIL=1 "$R/bin/devtool-run" show-env; rc=$?
if [ "$rc" -eq 0 ] && grep -qxF 'RT=' "$T/out" && grep -qF 'runtimeout を解決できなかった' "$T/err"; then
  pass "解決の失敗: 道具は起こし、DEVTOOL_RUNTIMEOUT を空で渡し、stderr で知らせる"
else
  fail "解決の失敗の扱いが違う (rc=$rc): $(cat "$T/out" "$T/err")"
fi

# 4. devtool/bin の外を指す名前・無い道具・実行できない道具は rc=2 で、道具を起こさない
# '../../bin/skill-tmpdir' は devtool/bin から dotfiles の bin/ へ実際に届く (名前の検査が無いと起こしてしまう形)
for bad in '../../bin/skill-tmpdir' '../bin/skill-tmpdir' '/bin/echo' '.hidden' '' 'absent' 'plain'; do
  run "$R/bin/devtool-run" "$bad"; rc=$?
  if [ "$rc" -eq 2 ] && [ ! -s "$T/out" ]; then
    pass "不正な道具 '$bad' は rc=2 で起こさない"
  else
    fail "不正な道具 '$bad' が rc=$rc: $(cat "$T/out" "$T/err")"
  fi
done

# 4b. 名前は正しくても、devtool/bin の外を指す symlink は起こさない。中を指す symlink は起こす
ln -s /bin/echo "$R/devtool/bin/escape" || exit 1
run "$R/bin/devtool-run" escape escaped; rc=$?
if [ "$rc" -eq 2 ] && ! grep -qF escaped "$T/out"; then pass "外を指す symlink の道具は rc=2 で起こさない"; else fail "外を指す symlink の道具が rc=$rc: $(cat "$T/out" "$T/err")"; fi
ln -s show-env "$R/devtool/bin/alias" || exit 1
run "$R/bin/devtool-run" alias; rc=$?
if [ "$rc" -eq 0 ] && grep -qxF "ST=$R/bin/skill-tmpdir" "$T/out"; then pass "中を指す symlink の道具は起こす"; else fail "中を指す symlink の道具が rc=$rc: $(cat "$T/out" "$T/err")"; fi

# 4c. runtimeout.sh が読めなくても道具は起こし、DEVTOOL_RUNTIMEOUT を空で渡す (rc=2 で止めない)
mv "$R/bin/lib/runtimeout.sh" "$R/bin/lib/runtimeout.sh.off" || exit 1
run "$R/bin/devtool-run" show-env; rc=$?
if [ "$rc" -eq 0 ] && grep -qxF 'RT=' "$T/out" && grep -qF 'runtimeout を解決できなかった' "$T/err"; then
  pass "runtimeout.sh が無い: 道具は起こし、DEVTOOL_RUNTIMEOUT を空で渡す"
else
  fail "runtimeout.sh が無いときの扱いが違う (rc=$rc): $(cat "$T/out" "$T/err")"
fi
mv "$R/bin/lib/runtimeout.sh.off" "$R/bin/lib/runtimeout.sh" || exit 1

# 5. 引数なしは rc=2
run "$R/bin/devtool-run"; rc=$?
[ "$rc" -eq 2 ] && pass "引数なしは rc=2" || fail "引数なしが rc=$rc"

# 6. symlink 越しに起こしても、実体の root の devtool を見る
ln -s "$R/bin/devtool-run" "$T/link-run" || exit 1
run "$T/link-run" show-env; rc=$?
if [ "$rc" -eq 0 ] && grep -qxF "ST=$R/bin/skill-tmpdir" "$T/out"; then
  pass "symlink 越しでも実体の root を使う"
else
  fail "symlink 越しの root が違う (rc=$rc): $(cat "$T/out" "$T/err")"
fi

if [ "$FAIL_COUNT" -gt 0 ]; then exit 1; fi
