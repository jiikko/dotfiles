# Mac

新しい Mac を用意する手順と、macOS 用の設定ファイル。いま使っているのは macOS 27.0。

| ファイル | 中身 |
|---|---|
| `setup_system.sh` | `defaults write` で入れるシステム設定 (キーリピート・トラックパッド・Finder・時計の表示) |
| `ClaudeWarm.terminal` | Terminal.app のプロファイル。`setup.sh` が `scripts/terminal_profile_restore.sh` で既定にする |
| `karabiner.json` | Karabiner-Elements の設定の正本。扱いは [`docs/tools/macos.md`](../docs/tools/macos.md) |
| [`finder-actions/`](finder-actions/README.md) | Finder の右クリックから動画を結合するクイックアクション |

## 手順

### 1. Command Line Tools

```shell
xcode-select --install
```

### 2. Homebrew

```shell
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

### 3. dotfiles を取って張る

```shell
cd ~
git clone git@github.com:jiikko/dotfiles.git || git clone https://github.com/jiikko/dotfiles.git
cd dotfiles
./setup.sh
```

`setup.sh` は設定ファイルの link のほか、Terminal.app のプロファイル (`mac/ClaudeWarm.terminal`) も既定にする。
nvim は `_nviminit.lua` が `~/.config/nvim/init.lua` に張られ、初回の起動で lazy.nvim が `_lazy-lock.json` の版でプラグインを入れる。

### 4. パッケージ

`Brewfile` は repo の root にあるので、dotfiles の中で打つ:

```shell
cd ~/dotfiles && brew bundle
```

### 5. システム設定

```shell
sh ~/dotfiles/mac/setup_system.sh
```

### 6. ログインシェルを Homebrew の zsh にする

zsh は `Brewfile` で入る。

```shell
echo /opt/homebrew/bin/zsh | sudo tee -a /etc/shells
chsh -s /opt/homebrew/bin/zsh
```

### 7. Karabiner-Elements

`mac/karabiner.json` は symlink ではなくコピーで置く (キーボードの種類に合わせて書き換えるため)。入れるのも、編集した後も:

```shell
bin/restore_karabiner_config.sh
```

### 8. ssh の鍵

```shell
ssh-keygen -t rsa -b 4096 -C "jiikko"
```

### textlint (任意)

```shell
npm i -g textlint textlint-rule-preset-ja-technical-writing
textlint --preset ja-technical-writing <校正対象ファイル>
```

## 手で設定するもの

- 壁紙変更
- 音量変更音を有効
- 設定 -> アクセシビリティ -> マウス/トラックパッド -> トラックパッドオプション -> ドラッグを有効にする「3 本指のドラッグ」
