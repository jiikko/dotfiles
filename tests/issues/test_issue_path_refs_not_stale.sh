#!/usr/bin/env bash
# issues/ の **外** (コード・docs・_claude/rules*) から issue を「裸のパス」で参照している箇所が、
# issue の移動で腐っていないかを検査する (issue 350)。
#
# なぜ既存の検査で足りないか:
#   - tests/issues/test_issue_links_valid.sh は **issues/ 配下しか歩かない**
#   - scripts/issue_done.sh の張り直しと stale_ref_report は **`](…)` の形しか見ない**
#     (AWK_REBASE が markdown リンクだけを対象にしている)
#   → 散文・インラインコードの裸パスは、done へ移すたびに誰にも気づかれず 1 本ずつ切れる
#
# ## 判定軸: 「番号 + スラッグの一致」であって「番号の一致」ではない
#
#   書かれたパスが解決せず、**同じ番号かつ同じスラッグ**の issue が issues/ 配下に実在する
#   → その issue が書かれた場所から動いた = 腐り。
#
#   番号だけで判定すると、テスト fixture (`issues/186-x.md`) と他 repo の issue
#   (obaket `issues/724-….md`) が実在の dotfiles issue と番号衝突して偽陽性になる。
#   スラッグまで見ると除外リストが 1 件も要らない (2026-09-11 の全数照合で確認)。
#
# ## 検出しない形 (射程の外。issue 350 に記録)
#
#   - **glob / 省略を含む参照** (`issues/done/189-*.md` / `issues/done/010-...`)。
#     リテラルとして解決せず、スラッグも `*` なので原理的に見えない
#   - **他 repo の issue パス** (obaket / ThumbnailThumb 等)。この repo からは正しさを判定できない
#   - **`](…)` の markdown リンク**。issue_done.sh の手順 4 と stale_ref_report が持ち主
#
# ## tests/ を走査から外さないこと
#
#   母集合 (候補件数) の大半はテスト fixture で、外すと候補が 1 桁まで落ちて
#   「抽出が壊れても違反 0 件 = 緑」に戻る。スラッグ一致の判定は fixture で誤爆しないので、
#   fixture は**母集合を維持するための錘**として意図的に走査対象に残している。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"

# issues/ 配下の全 md を "ベース名<TAB>repo root からの相対パス" で索引する。
build_index() {
  local root="$1"
  find "$root/issues" -type f -name '*.md' -print 2>/dev/null |
    while IFS= read -r p; do printf '%s\t%s\n' "${p##*/}" "${p#"$root"/}"; done
}

# 走査本体。$1 = repo root, $2 = 索引ファイル。
#   候補を "CAND<TAB>file:line<TAB>書かれたパス"、腐りを "VIOL<TAB>file:line<TAB>書かれたパス<TAB>実パス" で出す。
scan_tree() {
  local root="$1" idx="$2" loc written base actual
  while IFS=$'\t' read -r loc written; do
    [ -n "$written" ] || continue
    printf 'CAND\t%s\t%s\n' "$loc" "$written"
    [ ! -e "$root/$written" ] || continue          # 解決するなら腐っていない
    base="${written##*/}"
    actual="$(awk -F'\t' -v b="$base" '$1 == b { print $2; exit }' "$idx")"
    [ -n "$actual" ] || continue                   # 同名の issue が無い = fixture / 他 repo
    printf 'VIOL\t%s\t%s\t%s\n' "$loc" "$written" "$actual"
  done < <(
    cd "$root" &&
    grep -rnI 'issues/' . \
      --exclude-dir=issues --exclude-dir=.git --exclude-dir=tmp --exclude-dir=node_modules \
      2>/dev/null |
    perl -ne '
      next unless s/^\.\///;
      next unless /^([^:]+):(\d+):(.*)$/s;
      my ($f, $l, $t) = ($1, $2, $3);
      $t =~ s/\]\([^)]*\)/](LINK)/g;               # markdown リンク先は持ち主が別なので外す
      while ($t =~ m{((?:[A-Za-z0-9_.-]+/)*issues/(?:[A-Za-z0-9_.-]+/)*[0-9]{3}-[A-Za-z0-9._-]+\.md)}g) {
        print "$f:$l\t$1\n";
      }'
  )
}

# $1 = repo root, $2 = 見出し, $3 = 出力先の prefix ($3.sum に "候補<TAB>腐り" を書く),
# $4 = 1 なら内訳を出さない (canary は腐り 1 件が正常なので、失敗に見える行を出さない)
report() {
  local root="$1" label="$2" pre="$3" quiet="${4:-0}" idx cand viol
  idx="$pre.idx"
  build_index "$root" > "$idx"
  scan_tree "$root" "$idx" > "$pre.out"
  cand="$(grep -c '^CAND' "$pre.out" || true)"
  viol="$(grep -c '^VIOL' "$pre.out" || true)"
  printf '%s: 候補 %s 件 / 腐り %s 件\n' "$label" "$cand" "$viol"
  # 🚨 `grep | while read` は無マッチだと status 1 を返し、set -e がここでスクリプトを殺す
  # (腐り 0 件 = 正常系で落ちる)。`|| true` で受けてから件数で判定する。
  while IFS=$'\t' read -r _ loc written actual; do
    printf '  %s: `%s` は解決しない (実体は %s)\n' "$loc" "$written" "$actual"
  done < <([ "$quiet" = 1 ] || grep '^VIOL' "$pre.out" || true)
  printf '%s\t%s\n' "$cand" "$viol" > "$pre.sum"
}

fail=0

# --- canary: 本走査と同じ関数を、既知の答えを持つ fixture へ通す -------------------------------
# 🚨 式をコピーして別に書かない。ここが緑なら「抽出と判定が生きている」ことの証拠になる。
canary="$(mktemp -d)"
trap 'rm -rf "$canary"' EXIT
mkdir -p "$canary/issues/done" "$canary/docs"
: > "$canary/issues/done/091-feat-lockman-directory-lease-lock.md"
: > "$canary/issues/done/186-human-verify-schedkeys-pane-indicator.md"
: > "$canary/issues/done/333-retro-ruby-lsp-selection-2026-09-08.md"
# 腐りは **先頭以外**に置く (「最初に見つけたものだけ見る」変異を素通しさせない)
cat > "$canary/docs/a.md" <<'CEOF'
fixture: issues/186-x.md は番号だけ一致する別物
他 repo: obaket `issues/724-test-transfer-activity-center-tests-two-second-poll-timeouts.md`
他 repo: VLCMultiVideoPlayer `issues/done/333-bug-click-seek-perceived-no-op.md`
腐り: `issues/091-feat-lockman-directory-lease-lock.md`
リンクは対象外: [x](../issues/091-feat-lockman-directory-lease-lock.md)
CEOF
work="$(mktemp -d)"
trap 'rm -rf "$canary" "$work"' EXIT
report "$canary" '  canary (腐り 1 件が正常)' "$work/c" 1
read -r ccand cviol < "$work/c.sum"
if [ "$ccand" -ne 4 ]; then
  printf '✗ canary の候補件数が想定と違う (want 4, got %s) — 抽出が壊れている\n' "$ccand" >&2; fail=1
fi
if [ "$cviol" -ne 1 ]; then
  printf '✗ canary の腐り件数が想定と違う (want 1, got %s) — 判定が壊れている\n' "$cviol" >&2; fail=1
fi

# --- 本走査 -----------------------------------------------------------------------------------
report "$ROOT_DIR" 'issues/ の外からの裸パス参照' "$work/m"
read -r mcand mviol < "$work/m.sum"
if [ "$mcand" -lt 50 ]; then                        # 収集 0 件 / 極端な減少は失敗扱い
  printf '✗ 候補が %s 件しかない — 走査が空振りしている (2026-09-11 の実測は 114 件)\n' "$mcand" >&2; fail=1
fi
if [ "$mviol" -ne 0 ]; then
  printf '✗ 移動で切れた裸パス参照がある。issues/README.md のとおり「issue NNN」の番号参照へ直す\n' >&2; fail=1
fi

[ "$fail" -eq 0 ] || exit 1
printf '✓ issues/ の外からの裸パス参照に腐りは無い\n'
