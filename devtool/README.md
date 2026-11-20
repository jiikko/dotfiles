# devtool/

repo や CI から呼ばれる開発の道具の置き場。いずれ別の repo (仮称 toolbox) へ切り出す前提で、dotfiles の設定と分けて置く (epic issue 1091)。
ここには概要と参照だけを置き、道具ごとの使い方と呼び出し例は [`docs/`](docs/) に置く。

## 約束

- **devtool/ の中から devtool/ の外を参照しない**。依存の向きは dotfiles → devtool だけ。
  dotfiles の `bin/`・`src/`・`scripts/`・`_claude/` をパスで指さない (`make test-devtool-boundary` が止める。止める形と検出しない形は `scripts/check_devtool_boundary.sh` の冒頭)
- dotfiles の道具が要るときは、dotfiles の `bin/devtool-run` が解決して環境変数で渡す。devtool の道具は PATH を探さない
- テストは dotfiles の `tests/devtool/` に置く (`tests/` だけを走査する lint と runner の対象にそのまま入れるため)。切り出すときに一緒に移す
- 文書 (この README と `docs/`) には、道具を呼ぶ個別の repo の名前を書かない。呼ぶ側は `<repo>` のような置き換えの名前で書く

## 道具

| 道具 | 概要 | 文書 |
|---|---|---|
| `mutate` | 今の作業ツリーで実装をわざと壊し (変異)、テストが落ちるかを RED / GREEN / 判定不能で出す | [docs/mutation.md](docs/mutation.md) |
| `mutate-verify` | 使い捨ての worktree で変異を当て、前提の検査 (baseline が緑・当たったか・帰属など) を機械で強制する | [docs/mutation.md](docs/mutation.md) |
| `mutate-verify-list` | 一覧に書いた変異を `mutate-verify` で 1 本ずつ当てて表にする | [docs/mutation.md](docs/mutation.md) |
| `lint-issues` | `issues/` の直下の issue のファイル名と type の語を確かめる | [docs/lint.md](docs/lint.md) |
| `comment-provenance` | Swift のコメントに変更の経緯・レビューの出所を書いていないかを見る | [docs/lint.md](docs/lint.md) |
| `restartable-run` | restartable を決めたバージョンで入れて起こす (開発中のアプリの build と再起動のループ) | [docs/dev-loop.md](docs/dev-loop.md) |

## 文書

| 文書 | 中身 |
|---|---|
| [docs/calling-from-repos.md](docs/calling-from-repos.md) | repo から呼ぶ形 (呼び口の書き方・dotfiles の見つけ方・Makefile・CI で決めた commit の dotfiles を取る・終了コード・`devtool-run` が渡す環境変数) |
| [docs/mutation.md](docs/mutation.md) | 変異検証の 3 つの道具の使い分けと呼び出し例 |
| [docs/lint.md](docs/lint.md) | issue のファイル名とコメントの検査の呼び出し例 |
| [docs/dev-loop.md](docs/dev-loop.md) | restartable で build と再起動を回す呼び出し例 |

## lib

`lib/uint.sh`・`lib/git_location_env.sh`・`lib/worktree_scratch.sh` は devtool の道具が使う。dotfiles の他の道具も使うので、元の場所 (`bin/lib/`・`scripts/lib/`) に symlink を置いている (dotfiles → devtool の向き)。
