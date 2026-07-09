# tmux セッション永続化（Resurrect + Continuum）

このリポジトリは `tmux-resurrect` と `tmux-continuum` を使って、tmux のセッション保存・復元を自動化する。さらに、周期保存だけでは取りこぼす「最後の保存以降の構成変化」を **イベント駆動の debounce 保存** で秒オーダーに縮め、全保存経路を **単一 lock の wrapper** で直列化している（後述「仕組み」）。

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

このリポジトリでは、必要なプラグインを `vendor/tmux-plugins/` にベンダーし、`_tmux.conf` から直接 `run-shell` で読み込む。

- 同梱しているもの: `tmux-resurrect` / `tmux-continuum` / `tpm`
  - `tpm`（Tmux Plugin Manager）も同梱しているが、**標準構成ではプラグインマネージャとしては使わない**（プラグインは run-shell で直接ロードする）
- 追加の `git clone` や `prefix + I` は不要
- 更新は `git pull` だけ（反映は tmux 再起動 or リロード）
- オフライン環境でも動作する（初回取得時は別途）
- バージョン管理: `vendor/tmux-plugins/VERSIONS.txt` に取得元 URL と **コミット SHA を pin** してある。加えて continuum には **ローカルパッチ**（upstream の `set -x` を無効化してノイズ出力を抑止）を当てている。アップデート時は VERSIONS.txt を更新し、パッチの再適用要否を確認すること

## 前提条件

- tmux 3.6 以降を想定（現行は 3.7b。`pane-scrollbars` など 3.6+ の機能はバージョンガード付きで使う。なお scrollbar は幅を縮めるため off 運用）
- 最低ラインは tmux 3.4 以上（`pane-border-indicators` など 3.x の新しめの機能を前提）
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
| `C-t Ctrl-s` | セッションを保存（このリポジトリでは保存 wrapper 経由） |
| `C-t Ctrl-r` | セッションを復元 |

保存先は `~/.tmux/resurrect/`。

`C-t Ctrl-s` は、resurrect 標準の「vendored save.sh を直接叩く」bind をロード後に **保存 wrapper（`scripts/tmux_resurrect_save.sh`）へ貼り替え**ている。これにより手動保存も自動保存（continuum 周期 / debounce）と同じ単一 lock を通り、保存経路の競合を防ぐ（後述「仕組み」）。

### 自動保存 / 自動復元（tmux-continuum）

- **周期保存**: 15分ごと（continuum デフォルト。このリポジトリでは `@continuum-save-interval` を明示設定していないためデフォルトのまま）にバックグラウンドで保存
- **イベント駆動 debounce 保存**: window / pane の構成変化を契機に追加で保存（このリポジトリ独自。下記「仕組み」参照）
- **自動復元**: tmux **サーバー起動時**に最後の状態を復元（`@continuum-restore` が `on`）

注意: 自動復元は「サーバー起動時のみ」なので、すでに tmux サーバーが動いている状態で `source-file` しても自動復元は走らない。

## 保存される内容

Resurrect が保存・復元できるもの（代表例）：

- セッション / ウィンドウ / ペイン構成と順序
- レイアウト（ズーム状態含む）
- 各ペインのカレントディレクトリ
- ペイン内で実行中の一部プログラム（vim, less, man など）
- ペイン内容（スクロールバック）：このリポジトリでは `@resurrect-capture-pane-contents 'on'` で有効

「すべてのプロセスが完全に復元される」わけではない点は注意。

## 設定

### このリポジトリで実際に設定している主な値（`_tmux.conf`）

```tmux
# 自動復元（サーバー起動時）
set -g @continuum-restore 'on'

# ペイン内容（スクロールバック）も保存・復元
set -g @resurrect-capture-pane-contents 'on'

# continuum の autosave は status 更新を使うため status はオンに保つ
set -g status on
set -g status-interval 1   # status-left の毎秒点滅の駆動（continuum 用には >0 であればよい）
# 右側は非表示だが、continuum の hook 用に最小限の長さ(1)は確保する
set -g status-right-length 1
set -g status-right ""
```

`@continuum-save-interval` は明示設定していない（= デフォルト 15 分）。`@resurrect-strategy-vim/nvim` も標準構成では設定していない（vim/nvim のセッション復元は別途 vim-obsession 等が必要）。

### その他のよく使うノブ（参考）

必要になったら以下を追加する：

```tmux
# 自動保存の間隔（分）。0 で無効化
set -g @continuum-save-interval '30'

# vim/neovim をセッションとして復元（別途 vim-obsession 等が必要）
set -g @resurrect-strategy-vim  'session'
set -g @resurrect-strategy-nvim 'session'
```

## 仕組み（このリポジトリ固有）

`_tmux.conf` 末尾で、ベンダー済みプラグインを `run-shell` で読み込み、その後に保存経路を直列化する設定を上書きする。`DOTFILES_DIR` を設定すれば `~/dotfiles` 以外の配置にも対応できる（`${DOTFILES_DIR:-$HOME/dotfiles}` は tmux では展開できないため、`run-shell` の単一引用符内で `/bin/sh` に解決させている）。

### 1. プラグインのロード（依存順を厳守）

```tmux
# tmux-continuum は tmux-resurrect に依存するため順序を守る
run-shell 'f="${DOTFILES_DIR:-$HOME/dotfiles}/vendor/tmux-plugins/tmux-resurrect/resurrect.tmux"; [ -f "$f" ] && bash "$f"'
run-shell 'f="${DOTFILES_DIR:-$HOME/dotfiles}/vendor/tmux-plugins/tmux-continuum/continuum.tmux"; [ -f "$f" ] && bash "$f"'
```

### 2. イベント駆動 debounce 保存

continuum の周期保存（15分）だけだと、最後の保存以降に増やした window / pane が再起動で失われる（snapshot staleness）。window / pane の構成変化フックから debounce 保存を呼び、損失窓を秒オーダーに縮める。

```tmux
set-hook -g window-linked      "run-shell -b '...scripts/tmux_resurrect_debounced_save.sh'"
set-hook -g window-unlinked    "run-shell -b '...scripts/tmux_resurrect_debounced_save.sh'"
set-hook -g after-split-window "run-shell -b '...scripts/tmux_resurrect_debounced_save.sh'"
set-hook -g after-kill-pane    "run-shell -b '...scripts/tmux_resurrect_debounced_save.sh'"
set-hook -g pane-exited        "run-shell -b '...scripts/tmux_resurrect_debounced_save.sh'"
```

debounce token・多重起動 lock・「復元中は保存しない」ガードは `scripts/tmux_resurrect_debounced_save.sh` 側に集約している。

### 3. 全保存経路の直列化（単一 lock wrapper）

continuum 周期保存 / debounce 保存 / 手動 `C-s` が同時に upstream の `save.sh` を起動すると、共有の `save/` ディレクトリ・`pane_contents.tar.gz`・同一秒の layout ファイルを壊し合い、空／部分アーカイブが `last` になりうる。これを防ぐため、全経路を単一 lock の wrapper に集約する。

```tmux
# ロード後に保存スクリプトのパスを wrapper へ上書き（resurrect.tmux が load 時に
# vendored save.sh を設定するので、必ず load 後に上書きする）
run-shell 'tmux set-option -g @resurrect-save-script-path "...scripts/tmux_resurrect_save.sh"'

# 手動保存 C-s も wrapper 経由に貼り替え（resurrect は C-s に vendored save.sh を
# 直接パスで bind し @resurrect-save-script-path を経由しないため）
bind-key C-s run-shell '...scripts/tmux_resurrect_save.sh'
```

### 4. 復元進行フラグ（部分復元 attach の防止）

復元の進行状態を示すグローバルオプションを resurrect のフックで管理する。

- `@tt-restore-in-progress` … 復元中フラグ（開始 epoch を格納）。debounce 保存はこの間ガードされ、復元途中の部分状態を `last` に焼き付けない
- `@tt-restore-complete` … 復元完了フラグ。シェル関数 `tt()` はこのフラグが立つまで待ってから attach するため、復元途中での attach（部分復元）やサーバー巻き込み kill を避けられる
- `@tt-restore-duration` … 復元所要秒。`~/.cache/tt-restore-duration.log` に追記され、attach 時にステータスラインへ「復元: Ns」と flash 表示する

```tmux
set -g @resurrect-hook-pre-restore-all  '... @tt-restore-in-progress=<開始epoch> を立てる ...'
set -g @resurrect-hook-post-restore-all '... 所要秒を記録し @tt-restore-complete=1 を最後に立てる ...'
```

## ポータビリティ / 注意点

- **bash 必須**: `bash` がない環境では動かない
- **status line 必須（autosave）**: continuum は status line の更新を使う。`status off` や `status-interval 0` だと continuum 由来の自動保存が止まる（ただしこのリポジトリでは window/pane フックの debounce 保存があるため、構成変化時の保存はそちらでも走る）
- **テーマや別設定で status-right を上書き**すると continuum の autosave が止まることがある（continuum を最後に読み込むのが基本。このリポジトリでは status-right を空にしつつ length 1 を確保している）
- **`Ctrl-s` が効かない**場合: 端末のフロー制御に奪われていることがある（必要なら `stty -ixon`）

## トラブルシューティング

### ベンダー済みプラグインが見つからない

```bash
ls "${DOTFILES_DIR:-$HOME/dotfiles}/vendor/tmux-plugins/"
cat "${DOTFILES_DIR:-$HOME/dotfiles}/vendor/tmux-plugins/VERSIONS.txt"
```

### 自動保存されない

- `status` が `on` になっているか
- `status-interval` が 0 になっていないか
- `status-right` を別設定が上書きしていないか（テーマ導入時によくある）
- 構成変化時の debounce 保存が走るかは `scripts/tmux_resurrect_debounced_save.sh` のガード（復元中 / lock）を確認

### 自動復元されない

- 自動復元は「tmux サーバー起動時のみ」なので、いったんサーバーを落として起動し直す必要があるケースがある
- 手動復元（`C-t Ctrl-r`）で戻るかを確認する

### 保存ファイルが空 / 壊れている

- 複数の保存経路が直列化されているか（`@resurrect-save-script-path` が `scripts/tmux_resurrect_save.sh` を指しているか）を確認
- `tmux show -gv @resurrect-save-script-path`

## 参考リンク

- [tmux-resurrect](https://github.com/tmux-plugins/tmux-resurrect)
- [tmux-continuum](https://github.com/tmux-plugins/tmux-continuum)
- [TPM - Tmux Plugin Manager](https://github.com/tmux-plugins/tpm)（vendor には同梱しているが、標準構成ではプラグインマネージャとしては未使用）
