# 検査の道具 (lint-issues・comment-provenance)

どちらも repo の lint の集約 (`make lint` など) から呼ぶ想定の道具。呼び口の書き方は [calling-from-repos.md](calling-from-repos.md)。

## lint-issues

`issues/` の直下の issue のファイル名が `NNN-<type>-<slug>.md` の形かを確かめる。type は決めた 16 語 (dotfiles の issue 規約の「type 語彙」の表と同じ) のどれかでなければならない。

- 見るのは直下の `*.md` だけ (`done/`・`pending/` などの下は見ない)。番号で始まらない `.md` (README・メモ) は飛ばす
- 引数を省くと cwd の git の root の `issues/`
- 終了コード: 0 すべて合う / 1 合わないファイルがある / 2 引数の誤り・ディレクトリが無い

呼び口 (`bin/lint-issues`):

```bash
#!/usr/bin/env bash
set -euo pipefail
unset CDPATH
root="$(cd "$(dirname "$0")/.." && pwd)"
run="${DEVTOOL_DOTFILES_ROOT:-$HOME/dotfiles}/bin/devtool-run"
[ -x "$run" ] || { echo "✗ dotfiles の bin/devtool-run が無い: $run" >&2; exit 2; }
exec "$BASH" "$run" lint-issues "${@:-$root/issues}"
```

```make
lint: lint-issues
lint-issues:
	bin/lint-issues
```

## comment-provenance

Swift のコメント (文字列リテラルの外の `//`) に、変更の経緯やレビューの出所 (「codex review P2」「敵対レビュー指摘」「P1-C」など) を書いていないかを見る。
コメントに書くのは「なぜ素直な別の書き方をしないか」だけにし、経緯と出所はコミットログと issue に書く、という約束を機械で止める。

```
comment-provenance --root <repo root> [--label <名前>] [path ...]
```

- path を省くと `--root` の repo で git が追跡している `*.swift` 全部。path を渡すと、その file / dir の下の `*.swift`
- `--label` は出力の先頭の名前 (呼び口の名前を渡す)
- 終了コード: 0 違反なし / 1 違反あり / 2 判定不能 (対象 0 件・読めない file・git の失敗)
- 検出しない形 (ブロックコメント・語彙に無い言い回し・複数行文字列の中など) は道具の冒頭

呼び口 (`bin/check-comment-provenance`):

```bash
#!/usr/bin/env bash
set -euo pipefail
unset CDPATH
root="$(cd "$(dirname "$0")/.." && pwd)"
run="${DEVTOOL_DOTFILES_ROOT:-$HOME/dotfiles}/bin/devtool-run"
[ -x "$run" ] || { echo "check-comment-provenance: 判定不能: dotfiles の bin/devtool-run が無い: $run" >&2; exit 2; }
exec "$BASH" "$run" comment-provenance --root "$root" --label check-comment-provenance "$@"
```

CI で走らせるなら、[calling-from-repos.md](calling-from-repos.md) の「CI で決めた commit の dotfiles を取る」で dotfiles を渡す。
