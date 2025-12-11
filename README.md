dotfiles
========

# Installing

```
cd ~
git clone git@github.com:jiikko/dotfiles.git || git clone https://github.com/jiikko/dotfiles.git
cd dotfiles
./setup.sh
```

[for Mac](./mac "for Mac")

## Testing

Run the regression test suite (Neovim, tmux, setup.sh, plus existing zsh tests) with:

```

make test
```

You can run individual checks as well:

```
make test-syntax # zsh/zlogin/setup.sh syntax checks + tmux/nvim smoke
make test-nvim   # verifies Neovim config loads and lazy.nvim is reachable
make test-tmux   # ensures _tmux.conf can boot a tmux server (skips if tmux sockets are disallowed)
make test-setup  # exercises setup.sh in a temporary HOME
make test-shellcheck # runs shellcheck on shell-compatible scripts
make test-yaml   # yamllint on workflow/pre-commit config
make test-json   # jq validation for JSON configs
make test-lint   # aggregate lint target (shellcheck + YAML + JSON)
make test-runtime # aggregate runtime target (syntax + zshrc + nvim + tmux + setup)
tests/zshrc/test_zshrc.sh  # existing zsh tests (also run via make test)
```

## Utility Functions

zshlib/に定義されている便利なシェル関数。

### repair

問題のある動画ファイルを修復します。

```bash
repair movie.mp4           # 単一ファイル
repair *.mp4 *.ts          # 複数ファイル
repair -h                  # ヘルプ
```

- mpegtsなど問題のあるコンテナをMP4に変換
- 異常なフレームレート（240fps超）を自動で30fpsに正規化
- 可能な限りストリームコピー（無劣化）
- 対応形式: `.mp4`, `.m4v`, `.mov`, `.ts`, `.mts`, `.m2ts`
- 出力: `*-repaired.mp4`
- 依存: `ffmpeg`, `ffprobe`

### av1ify

動画ファイルをAV1形式のMP4に変換します。

```bash
av1ify movie.avi           # 単一ファイル
av1ify /path/to/dir        # ディレクトリ内を再帰処理
av1ify -f list.txt         # ファイルリストから処理
av1ify /path/to/dir --dry-run  # 変更せず実行内容だけ確認
AV1_CRF=35 av1ify movie.mp4  # CRF指定
```

- SVT-AV1エンコーダを使用（なければAOM-AV1にフォールバック）
- 解像度に応じてCRFを自動調整
- 音声は可能な限りコピー、非対応形式はAACに再エンコード
- `--dry-run` で実行内容のみ表示（ファイルを変更しない）
- 対応形式: `.avi`, `.mkv`, `.rm`, `.wmv`, `.mpg`, `.mpeg`, `.mov`, `.mp4`, `.flv`, `.webm`, `.3gp`
- 出力: `*-enc.mp4`
- 依存: `ffmpeg`, `ffprobe` (SVT-AV1サポート付き)
