# serial-run を他のプロジェクトに組み込む

`serial-run` は重いコマンドを 1 本ずつ流す待ち列 (本体と設計は [src/serialrun/README.md](../../src/serialrun/README.md))。

- **既定は git の repo ごとの列**。同じ repo のコマンドは worktree が違っても 1 本ずつ、別の repo とは同時に走る
  (置き場は `~/.local/state/serial-run/repo-<dir 名>-<hash>/`)
- **マシン全体で 1 本にしたいとき**は `SERIAL_RUN_SCOPE=machine` (置き場は `~/.local/state/serial-run/default/`)。
  Claude Code の hook も含めて全部をそうするなら、Claude Code の設定の env とシェルの環境の両方に置く
- 1 回だけ別の列にするなら `serial-run -l <名前> --`
このページは、dotfiles 以外の repo で「どこまで自動で並ぶか」と「足りない経路をどう並べるか」を書く。

## 1. 何もしなくても並ぶもの (Claude Code の hook)

dotfiles の `_claude/settings.json` の PreToolUse hook が、**どの repo でも** Claude Code の Bash ツールが打つ次のコマンドの前に
`serial-run -- ` を差し込む (前提: `~/dotfiles/bin` が PATH にある)。

| コマンド | 並ぶ形 | 並ばない形 |
|---|---|---|
| `make` / `gmake` | target が `test` / `lint` / `test-*` / `lint-*` | `-n` / `-q` / `-p` / `-t` / `--help` などの何もしない呼び方、それ以外の target (`make build` など) |
| `go test` | `go test …` / `go -C dir test …` | `go test -h`、`go build` など test 以外 |
| `swift` | `swift build` / `swift test` | help |
| `xcodebuild` | build / test など | `-version` / `-list` などの情報表示 |
| `mutate-verify` / `mutate-verify-list` | 普通の呼び出し | help |

`runtimeout 600 make test` のような前置きの後ろも見る。判定の正本は `src/serialrun/hook.go` の `isHeavy`。

並ばないのは次の経路:

- 人が端末で打つコマンド・git hook・CI 以外の自動化・スクリプトの中で呼ばれる重い処理 (字面に出ないもの)
- codex・pro-con の PG など、ユーザーの hook を持ち込まない実行者
- 上の表に無いコマンド (`npm test` / `pnpm test` / `cargo test` / `bundle exec rspec` / `pytest` など)

これらを並べたいときに、下の 2・3 を使う。

## 2. Makefile がある repo: 入口の target を包む

共有の断片 `~/dotfiles/make/serial-run.mk` を include し、重い入口の recipe の先頭に `$(serial_run)` を置く。本体は別の target に分ける。

```make
include $(HOME)/dotfiles/make/serial-run.mk

test:
	@+$(serial_run) $(MAKE) --no-print-directory test-all

test-all:
	# 今の test の中身をここへ移す
```

- `$(serial_run)` は `~/dotfiles/bin/serial-run --` に展開される。次のときは空になり、包まずに走る:
  `CI` がある / `SERIAL_RUN_MAKE_DISABLE=1` / `make -n`・`-q`・`-t` / `~/dotfiles/bin/serial-run` が無い (dotfiles を入れていない機械・CI)
- 🚨 recipe の行には `+` を付ける。`$(MAKE)` を呼ぶ行で並列の jobserver を引き継ぐため
- 🚨 本体を別の target に分ける (`test:` の recipe の中で `$(MAKE) test` を呼ばない)。`$(serial_run)` が空のときに自分自身を呼んで無限に回る
- 入れ子は気にしなくてよい。hook が包んだ `make test` の中・`make test` の中の `make` は、serial-run が札を照合して素通しにする (二重に並ばない)
- 包むのは時間のかかる入口 (`test` / `lint` / `build` の重いもの) だけにする。秒で終わる target まで包むと、他の重いコマンドの後ろで待たされる
- その repo に Makefile の lint や「集約は `test` にある」ことを見るテストがあれば、`test-all` に向け直す (dotfiles では `tests/scripts/test_no_failfast_entrypoints.sh` をそうした)

## 3. Makefile が無い repo: コマンドの前に `serial-run --` を書く

```sh
serial-run -- npm test
serial-run -- bundle exec rspec
serial-run -- xcodebuild -scheme App test
```

- package.json なら `"test": "serial-run -- vitest run"` のように scripts に書けるが、dotfiles を入れていない機械 (CI・他の人) では
  `serial-run` が無くて落ちる。共有する repo では scripts を書き換えず、手元の alias や Makefile で包む
- 呼ぶ道具の中で別の重いコマンドを起こす場合も、外側の 1 回だけ包めばよい (中の serial-run は素通し)

## 4. 動作の確かめ方

```sh
serial-run status                                   # cwd の列 (lock:)・先頭・待ち・hook の最近の判断 (置き場を作らない)
tail ~/.local/state/serial-run/repo-*/history.log   # 1 本 1 行: 番号・時刻・待ち・所要・rc・cwd・コマンド・結末 (列ごと)
```

- 組み込んだ入口を同じ repo の 2 つの端末 (か 2 つの session・worktree) で同時に打ち、2 本目に `serial-run: 待ち 1 番目` が出て、
  history.log の 2 本目の `wait=` が 1 本目の所要と同じくらいになれば並んでいる
- Makefile に入れたなら、`make -n test` が待たずに返ることも見る

## 5. 気をつけること

- **rc=125 はテストの失敗ではない**。待ちの上限 (既定 1 時間)・置き場の失敗・serial-run の入口のビルドの失敗。stderr の `serial-run:` の行に理由が出る
- **待ちの上限**: `--max-wait <秒>` で変える (`0` は上限なし)。Makefile の入口なら包まない条件を保ったまま
  `$(if $(serial_run),$(SERIAL_RUN_BIN) --max-wait 7200 --)` と書く
- **Claude Code の Bash の timeout** (foreground は最大 10 分) は待ちも数える。長い入口は run_in_background で起こす
- **許可ルール**: Claude Code は hook が書き換えた後のコマンドで許可を判定する。`Bash(make test:*)` のような deny / ask を書いている repo は、
  `Bash(serial-run -- make test:*)` の形も並べて書く
- **列の範囲**: 既定は repo ごと (別の repo とは同時に走る)。マシンが重くなるのを repo をまたいで避けたいなら `SERIAL_RUN_SCOPE=machine`。
  同じ repo の中で別に流したいもの (たとえば GPU だけを使うもの) は `serial-run -l <名前> --` で別の列にする
- **止める**: その repo の Makefile の包みだけ外すなら `SERIAL_RUN_MAKE_DISABLE=1`、hook を止めるなら Claude Code の環境に `SERIAL_RUN_HOOK_DISABLE=1`。
  `~/dotfiles` の serial-run がビルドできない状態のとき、Makefile の入口はテストを走らせずに 125 で落ちる。その日は `SERIAL_RUN_MAKE_DISABLE=1` で回す
- **置き場を差し替えた環境** (`HOME` / `XDG_STATE_HOME` / `SERIAL_RUN_STATE_DIR` を変えたテストなど) は別の置き場の列に並ぶ
- 子が残した背景の孫は止めない (lock の外で走り続ける)。テストの後に常駐プロセスを残す入口は、その入口の側で後始末する
