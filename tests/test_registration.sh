#!/bin/sh
# test_registration.sh — tests/ 配下のテストが Makefile に登録されているか検証する meta テスト
#
# 目的: テストを書いたのに Makefile (test-zshrc / test-nvim 等) への登録を忘れ、CI で
#   永久に実行されない「死蔵テスト」を防ぐ。実際に concat / repair_mp4 / video_health /
#   nvim ftplugin の 8 件が登録漏れのまま CI 未実行で放置されていた (2026-06-28 検出)。
#
# 判定: tests/**/test_*.sh と tests/**/*.bats のうち、ヘルパー (*helper*) を除いた全ファイル
#   について、その相対パス文字列が Makefile 内に出現するかを grep で確認する。出現しなければ
#   未登録 = fail。意図的に CI 対象外にしたいファイルは下の ALLOWLIST に相対パスを追加する。
set -eu
unset CDPATH

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT_DIR"

# 登録不要なファイル (意図的に CI 対象外にしたい場合のみ、相対パスをスペース区切りで追加)
ALLOWLIST=""

find_tests() {
  find tests -type f \( -name 'test_*.sh' -o -name '*.bats' \) ! -name '*helper*' | sort
}

# パターンの先頭 ( は、case を $(...) 内で使う際に閉じ ) をコマンド置換の
# 終端と誤認させないための平衡用 (POSIX 準拠)。外すと sh が構文エラーになる。
missing=$(find_tests | while IFS= read -r f; do
  case " $ALLOWLIST " in
    (*" $f "*) ;;                              # ALLOWLIST 済みは検査しない
    (*) grep -qF "$f" Makefile || echo "$f" ;; # 未登録なら出力
  esac
done)

if [ -n "$missing" ]; then
  echo "✗ Makefile に未登録のテストがあります (CI で実行されません):" >&2
  echo "$missing" | sed 's/^/  - /' >&2
  echo "→ Makefile の test-* ターゲットに追加するか、test_registration.sh の ALLOWLIST へ" >&2
  exit 1
fi

echo "[registration] 全テスト ($(find_tests | wc -l | tr -d ' ') 件) が Makefile に登録済み"
