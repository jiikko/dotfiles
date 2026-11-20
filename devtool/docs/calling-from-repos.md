# repo から devtool の道具を呼ぶ

道具の本体は dotfiles の `devtool/bin/` にあり、呼ぶ repo には小さな呼び口 (wrapper) だけを置く。判定の中身は本体が持ち、
呼び口はその repo に固有の値 (対象のディレクトリ・出力の名前・app を起こすコマンド) を渡すだけにする。

## 呼び口の形

呼び口は dotfiles の `bin/devtool-run` を **絶対パスで** 起こし、道具の名前と引数を渡す。

```bash
#!/usr/bin/env bash
# lint-issues の呼び口。本体は dotfiles の devtool/bin/lint-issues。ここではこの repo の issues/ を渡すだけ。
set -euo pipefail
unset CDPATH

root="$(cd "$(dirname "$0")/.." && pwd)"
run="${DEVTOOL_DOTFILES_ROOT:-$HOME/dotfiles}/bin/devtool-run"
if [ ! -x "$run" ]; then
  echo "✗ dotfiles の bin/devtool-run が無い: $run (dotfiles を pull するか DEVTOOL_DOTFILES_ROOT を直す)" >&2
  exit 2
fi
exec "$BASH" "$run" lint-issues "${@:-$root/issues}"
```

決めごと:

- **dotfiles の場所は `DEVTOOL_DOTFILES_ROOT` で受け、無ければ `~/dotfiles`**。CI や別の checkout の dotfiles を使うときはこの変数で渡す
- **PATH に頼らない**。dotfiles の `bin/` を PATH に足すのは対話のシェルの設定だけで、make・hook・CI の非対話のシェルは持たない
- **`"$BASH"` で起こす**。`devtool-run` は、shebang に bash と書いた道具を同じ `"$BASH"` で起こす。呼び口を起こした bash のバージョンがそのまま本体まで届く
  (shebang に任せると、macOS の `/bin/bash` 3.2 で走る)
- **dotfiles が無いときは、検査を通さずに落とす** (上の例は rc 2)。エラーを出さずに飛ばすと、dotfiles の無い環境で検査が緑に見える
- **`exec` で起こし、本体の終了コードをそのまま返す**。呼び口で rc を読み替えない
- 呼び口の名前は repo の慣習に合わせてよい (`bin/lint-issues`・`bin/<接頭辞>-lint-issues`・`libexec/check/check-<名前>`)

本体の `devtool/bin/<道具>` へ symlink を張る形は採らない。symlink だと `devtool-run` を通らない。道具の要る環境変数 (下) を渡せず、
dotfiles の場所も変えられない。

## Makefile から呼ぶ

```make
.PHONY: lint-issues
lint-issues:
	bin/lint-issues
```

lint の集約 (`make lint` など) に足すときは、dotfiles が無い環境で落ちることを前提にする。CI で走らせるなら次節の形で dotfiles を渡す。

## CI で決めた commit の dotfiles を取る

CI には dotfiles の checkout が無い。CI の前段で、決めた commit の dotfiles を浅く取り、`DEVTOOL_DOTFILES_ROOT` で渡す。
commit は repo の中のファイル (例: `ci/dotfiles-commit`) に 40 桁で書く。上げるときはそのファイルを書き換える
(dotfiles の master を追うと、dotfiles の変更が repo の CI を知らせずに変える)。

```bash
#!/bin/bash
# fetch-ci-dotfiles <取得先> — ci/dotfiles-commit の commit で dotfiles を浅く取る。取得先に同じ commit があれば取り直さない
set -euo pipefail
unset CDPATH

dest="$1"
sha="$(tr -d '[:space:]' < "$(dirname "$0")/dotfiles-commit")"
[[ "$sha" =~ ^[0123456789abcdef]{40}$ ]] || { echo "dotfiles-commit が 40 桁の commit でない: '$sha'" >&2; exit 1; }
if [ -d "$dest/.git" ] && [ "$(git -C "$dest" rev-parse HEAD 2>/dev/null)" = "$sha" ]; then exit 0; fi
rm -rf "$dest"; mkdir -p "$dest"; git init -q "$dest"
git -C "$dest" fetch -q --depth 1 https://github.com/jiikko/dotfiles.git "$sha"
git -C "$dest" checkout -q FETCH_HEAD
```

CI の前段:

```bash
# 取得の成否に関わらず取得先に固定する (CI の環境変数などで継承した別の dotfiles で検査を通さない)
export DEVTOOL_DOTFILES_ROOT="$PWD/tmp/ci-dotfiles"
ci/fetch-ci-dotfiles "$DEVTOOL_DOTFILES_ROOT" || echo "dotfiles を取得できない。dotfiles を要る検査は判定不能で落ちる" >&2
make lint
```

- 取得に失敗しても前段では止めず、dotfiles を要る検査が「dotfiles が無い」で落ちるのに任せる (どの検査が dotfiles を要るかを前段に写さない)
- `rm -rf` する取得先は、repo の中の決めた場所だけを受ける形にする (引数の誤りで別の場所を消さない)
- 本体の側で道具の引数や出力が変わったら、commit を上げる前に repo の CI と同じ検査を手元で回す

## テストから呼ぶ

呼び口の自己テストで本体を差し替えたいときは、偽の dotfiles のディレクトリを作り、`<偽>/bin/devtool-run` を置く。
呼び口は `DEVTOOL_DOTFILES_ROOT=<偽>` で起こす。テストで `HOME` を一時ディレクトリに差し替えるなら、その前に `DEVTOOL_DOTFILES_ROOT` を控えて export する
(後から `~/dotfiles` を解決すると、空の `HOME` の下を探して見つからない)。

```bash
export DEVTOOL_DOTFILES_ROOT="${DEVTOOL_DOTFILES_ROOT:-$HOME/dotfiles}"
export HOME="$work/home"
```

## 終了コード

- `devtool-run` は道具の rc をそのまま返す。`devtool-run` 自身の誤り (不正な道具の名前・無い道具・`devtool/bin` の外にある実体) は 2
- 道具ごとの rc は各文書と道具の `--help` (`"$run" <道具> --help`)

## `devtool-run` が道具へ渡す環境変数

呼び口が設定するものではない。`devtool-run` が dotfiles の道具を解決して渡す。

| 名前 | 中身 |
|---|---|
| `DEVTOOL_RUNTIMEOUT` | runtimeout のバイナリの絶対パス。起動の前に 1 回だけ解決する (自動で再ビルドするラッパーを run の途中で通さない)。解決できなければ空 |
| `DEVTOOL_SKILL_TMPDIR` | skill-tmpdir の絶対パス |
