# 開発中のアプリの build と再起動 (restartable-run)

`restartable-run` は restartable (アプリを build して起動し、端末の R で止めて build し直して起動し直すループ) を、決めたバージョンで入れてから起こす。
restartable のバージョンを持つのはこの道具だけで、各 repo はアプリに固有の引数だけを渡す。

```
restartable-run --bin-dir <dir> [--] [restartable の引数…]
```

- `<dir>/restartable.version` の印が無い・違うバージョンなら `go install` し直す (印は書き終わってから置き換える)
- go が無いとき・`go install` が落ちたときは、restartable を起こさずに rc 1
- `<dir>` は呼び口が決める (普段は repo の `tmp/bin`)。外から再起動する道具 (`bin/dev-restart` など) も同じ `<dir>/restartable` を `status` / `restart` で叩く
- バージョンを上げるときは、道具の中の pseudo-version を dotfiles に push した commit のものに書き換える (道具の `--help`)

## 呼び出し例

呼び口 (`bin/dev-fg-loop`) は、build・停止・起動のコマンドを restartable の引数として渡す。

```bash
#!/usr/bin/env bash
set -uo pipefail
unset CDPATH

root_dir="$(cd "$(dirname "$0")/.." && pwd)"
devtool_run="${DEVTOOL_DOTFILES_ROOT:-$HOME/dotfiles}/bin/devtool-run"
[[ -x "$devtool_run" ]] || { echo "dev-fg-loop: dotfiles の bin/devtool-run が無い: $devtool_run" >&2; exit 1; }
printf -v root_q '%q' "$root_dir"

cd "$root_dir"
# restartable は build / stop-cmd / 起動するコマンドに環境を引き継ぐ。親の make の再帰フラグだけを外す
unset MAKEFLAGS MFLAGS MAKEOVERRIDES MAKELEVEL
exec "$BASH" "$devtool_run" restartable-run --bin-dir "$root_dir/tmp/bin" -- \
  --build "make -C $root_q --no-print-directory build" \
  --stop-cmd "exec $root_q/bin/launch-app --quit" \
  -- "$root_dir/bin/launch-app"
```

```make
.PHONY: dev-fg-loop
dev-fg-loop:
	bin/dev-fg-loop
```

- build が失敗したら restartable は古いアプリを起動せずに待つ。直してから R で起動し直す
- 停止のコマンド (`--stop-cmd`) は、アプリの通常の終了 (保存などの後処理を通る) を求める形にする。強制終了だけにすると後処理を飛ばす
- `--build`・`--stop-cmd` はシェルの文字列として実行されるので、パスは `%q` で引用する。最後の `--` の後ろ (起動するコマンド) は引数の並びのまま渡る
- 渡せる引数 (`--ready-cmd`・`--pull-cmd`・`--id-env` など) は restartable の `--help`
