# tmux セッション永続化（Resurrect + Continuum）

このリポジトリは `tmux-resurrect` と `tmux-continuum` を使って、tmux のセッション保存・復元を自動化する。

## なぜ必要か

tmux はデフォルトではサーバーが終了するとセッション情報が消える。これにより以下の問題が起きる：

- **マシン再起動** → ウィンドウ構成がすべて消える
- **tmux kill-server** → 作業環境がリセットされる
- **SSH切断 + サーバー停止** → 復帰できない

## ユースケース

### 1. マシン再起動後の作業復帰

```
[再起動前]
Session: dev
├── Window 0: frontend (~/project/frontend) - npm run dev 実行中
├── Window 1: backend  (~/project/backend)  - rails server 実行中
├── Window 2: editor   (~/project)          - nvim 編集中
└── Window 3: logs     (~/project)          - tail -f 実行中

[再起動後に tmux 起動]
→ 上記のウィンドウ構成・ディレクトリが自動復元
→ プロセスは再起動が必要だが、構成を手で作り直す必要なし
```

### 2. 複数プロジェクトの切り替え

プロジェクトごとにセッションを分けている場合、すべてのセッションが復元される：

```
Session: projectA  ← 復元される
Session: projectB  ← 復元される
Session: dotfiles  ← 復元される
```

### 3. 「昨日どこまでやったか」の復帰

- 開いていたディレクトリ、ペイン配置がそのまま戻る
- vim/nvim は Session.vim があればセッションごと復元可能（別途設定）

### 4. 誤って tmux を落とした場合のリカバリ

```bash
# うっかり
$ tmux kill-server

# 復旧
$ tmux
# → 自動復元、または C-t Ctrl-r で手動復元
```

## 方針（ベンダー運用）

このリポジトリでは、必要なプラグインを `vendor/tmux-plugins/` にベンダーし、`_tmux.conf` から直接読み込む。

- 追加の `git clone` や `prefix + I` は不要
- 更新は `git pull` だけ（反映は tmux 再起動 or リロード）
- オフライン環境でも動作する（初回取得時は別途）

## 前提条件

- tmux 1.9 以上
- bash（プラグインが bash 前提）
- git（`git pull` 用）

```bash
tmux -V
```

## セットアップ

### 初回（新規端末）

`setup.sh` が `~/.tmux.conf` → `~/dotfiles/_tmux.conf` を張るので、基本は以下だけ：

```bash
cd ~/dotfiles
./setup.sh
tmux
```

### 更新（すでに導入済み）

```bash
cd ~/dotfiles
git pull
```

反映は tmux 側で行う：

- 設定再読み込み: `prefix + R`（このリポジトリでは `C-t R`）
- もしくは tmux 再起動

## 使い方

### 手動保存 / 復元（tmux-resurrect）

| キーバインド | 動作 |
| ------------ | ---- |
| `C-t Ctrl-s` | セッションを保存 |
| `C-t Ctrl-r` | セッションを復元 |

保存先は `~/.tmux/resurrect/`。

### 自動保存 / 自動復元（tmux-continuum）

- **自動保存**: 15分ごと（デフォルト）にバックグラウンドで保存
- **自動復元**: tmux **サーバー起動時**に最後の状態を復元（`@continuum-restore` が `on` の場合）

注意: 自動復元は「サーバー起動時のみ」なので、すでに tmux サーバーが動いている状態で `source-file` しても自動復元は走らない。

## 保存される内容

Resurrect が保存・復元できるもの（代表例）：

- セッション / ウィンドウ / ペイン構成と順序
- レイアウト（ズーム状態含む）
- 各ペインのカレントディレクトリ
- ペイン内で実行中の一部プログラム（vim, less, man など）

「すべてのプロセスが完全に復元される」わけではない点は注意。

## 設定（よく触るもの）

`~/.tmux.conf`（このリポジトリでは `_tmux.conf`）に書く。

```tmux
# 自動保存の間隔（分）。0 で無効化
set -g @continuum-save-interval '30'

# 自動復元
set -g @continuum-restore 'on'   # 有効
set -g @continuum-restore 'off'  # 無効

# pane の内容も保存（サイズ次第で重くなる）
set -g @resurrect-capture-pane-contents 'on'

# vim/neovim をセッションとして復元（別途 vim-obsession 等が必要）
set -g @resurrect-strategy-vim  'session'
set -g @resurrect-strategy-nvim 'session'
```

## 仕組み（このリポジトリ固有）

`_tmux.conf` 末尾で、ベンダー済みプラグインを `run-shell` で読み込む。

```tmux
set -g @continuum-restore 'on'
if-shell 'test -f "${DOTFILES_DIR:-$HOME/dotfiles}/vendor/tmux-plugins/tmux-resurrect/resurrect.tmux"' "run-shell 'bash ${DOTFILES_DIR:-$HOME/dotfiles}/vendor/tmux-plugins/tmux-resurrect/resurrect.tmux'"
if-shell 'test -f "${DOTFILES_DIR:-$HOME/dotfiles}/vendor/tmux-plugins/tmux-continuum/continuum.tmux"' "run-shell 'bash ${DOTFILES_DIR:-$HOME/dotfiles}/vendor/tmux-plugins/tmux-continuum/continuum.tmux'"
```

`DOTFILES_DIR` を設定すると `~/dotfiles` 以外の配置にも対応できる（ただし `setup.sh` は `~/dotfiles` 前提）。

## ポータビリティ / 注意点

- **bash 必須**: `bash` がない環境では動かない
- **status line 必須（autosave）**: continuum は status line の更新を使う。`status off` や `status-interval 0` だと自動保存が止まる
- **テーマや別設定で status-right を上書き**すると autosave が止まることがある（continuum を最後に読み込むのが基本）
- **`Ctrl-s` が効かない**場合: 端末のフロー制御に奪われていることがある（必要なら `stty -ixon`）

## トラブルシューティング

### ベンダー済みプラグインが見つからない

```bash
ls "${DOTFILES_DIR:-$HOME/dotfiles}/vendor/tmux-plugins/"
```

### 自動保存されない

- `status` が `on` になっているか
- `status-interval` が 0 になっていないか
- `status-right` を別設定が上書きしていないか（テーマ導入時によくある）

### 自動復元されない

- 自動復元は「tmux サーバー起動時のみ」なので、いったんサーバーを落として起動し直す必要があるケースがある
- 手動復元（`C-t Ctrl-r`）で戻るかを確認する

## 参考リンク

- [tmux-resurrect](https://github.com/tmux-plugins/tmux-resurrect)
- [tmux-continuum](https://github.com/tmux-plugins/tmux-continuum)
- [TPM - Tmux Plugin Manager](https://github.com/tmux-plugins/tpm)（このリポジトリの標準構成では未使用）
