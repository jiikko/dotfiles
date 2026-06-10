# dotfiles

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
make test-bats   # bats tests (skips if bats is not installed)
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

### concat

複数の動画ファイルを無劣化で結合します。

```bash
concat video_001.mp4 video_002.mp4 video_003.mp4  # 連番ファイルを結合
concat --force video1.mp4 video2.mp4               # コーデック不一致でも強制実行
concat -h                                          # ヘルプ
```

- FFmpegのconcat demuxerを使用して無劣化結合
- 同一コーデック・フォーマットの動画を高速に連結
- ファイル名の連続性（連番パターン）を自動チェック
- コーデック・解像度の不一致を検出
- クラウドストレージ（Dropbox等）の自動プリフェッチに対応
- 対応形式: `.mp4`, `.avi`, `.mov`, `.mkv`, `.webm`, `.flv`, `.wmv`, `.m4v`, `.mpg`, `.mpeg`, `.3gp`, `.ts`, `.m2ts`
- 出力: 共通プレフィックス + `.mp4` (例: `video_001.mp4, video_002.mp4` → `video.mp4`)
- 依存: `ffmpeg`, `ffprobe`

## tmux

### ウィンドウ名の自動反映

ウィンドウ名は「**アクティブペインのタイトル (pane_title) に自動追従**」する構成。

```
各ペイン内のプログラムが OSC 2 で pane_title をセット
  ├─ zsh: preexec/precmd が実行コマンドの表示名をセット (zshlib/_tmux_window_name.zsh)
  │       コマンド名 → アイコン付き表示名のマッピングは zshlib/tmux-window-name.yaml
  ├─ nvim: 編集中のファイル名を自身でセット
  └─ Claude Code: セッションの topic を自身でセット
        ↓
tmux の automatic-rename-format '#{pane_title}' が
アクティブペインのタイトルをウィンドウ名に反映 (_tmux.conf)
        ↓
ステータスバーでは #{=15:window_name} で 15 文字に切り詰めて表示
(ウィンドウ名自体・ペイン境界の表示はフルのまま)
```

- ウィンドウ名を直接リネームする `\033k` エスケープは `allow-rename off` で遮断している。
  旧構成 (zsh が `\033k` でウィンドウ名を直接書き換え) では「最後にプロンプトを出した
  ペイン」が非アクティブでもウィンドウ名を奪う事故があった (split 直後に zsh へリセット等)。
  タイトルはペイン単位の OSC 2 に一本化し、ウィンドウ名への昇格は tmux 側に任せる
- 既に起動中の zsh は古い zshlib を読んだままなので、挙動を反映するには `exec zsh` が必要

### Claude Code の作業状態表示

Claude Code を動かしているペインの境界に作業状態 (`⚙ working` / `✓ idle`) が出る。

- Claude Code の hooks (`_claude/settings.json`) が `_claude/hooks/tmux-pane-state.sh` を呼び、
  ペイン単位オプション `@claude_state` を出し入れする
- `_tmux.conf` の `pane-border-format` が `#{?@claude_state,...,}` で表示。未設定ペイン
  (通常シェル) には何も出ない。セッション終了 (SessionEnd) で自動クリア

## macOS Integration

### Finder Quick Actions

Finderの右クリックメニューから動画処理コマンドを実行できます。

```bash
# セットアップ
~/dotfiles/mac/finder-actions/setup-concat-finder-action.sh
```

詳細は [mac/finder-actions/README.md](./mac/finder-actions/README.md) を参照。
