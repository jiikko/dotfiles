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
make test-lint   # aggregate lint target (shellcheck + zsh syntax + YAML + JSON + karabiner + actionlint + gitconfig + ruby syntax)
make test-src    # lint + test for all Go projects under src/ (same coverage as CI's src_*.yml)
make test-runtime # aggregate runtime target (syntax + auto-discovered tests/**/test_*.sh + bats)
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

prefix は `C-t`。設定の正本は [_tmux.conf](./_tmux.conf)（コメントに各設定の意図と経緯を記載）。

### ペイン操作

| キー | 動作 |
|---|---|
| `C-t v` / `C-t \|` | 左右に分割（カレントパス引き継ぎ） |
| `C-t s` / `C-t -` | 上下に分割（同上） |
| `C-t h/j/k/l` | ペイン移動（repeat 対応で連打可。`C-h/j/k/l` でも可） |
| `M-h/j/k/l` | prefix なしでペイン移動（端でループしない） |
| `C-t H/J/K/L` | リサイズ（連打可） |
| `C-t z` | ズーム（ズーム中はペイン境界に 🔍 ZOOM と解除ヒントが出る） |
| `C-t x` | ペインを kill（**画面中央に gum の確認ダイアログ**。要 `brew install gum`） |
| `C-t q` | 自分以外の全ペインを kill（同上、誤爆防止でデフォルトは「やめる」） |

### ペインの入れ替え・移動

| キー | 範囲 | 動作 |
|---|---|---|
| `C-t S` → `h/j/k/l` | 同一 window | 入れ替えモード。連打で押し込める。他のキーで終了 |
| `C-t e` → 数字 | 同一 window | ペイン番号オーバーレイから選んで現在ペインと交換 |
| `C-t m` → 相手ペインで `C-t >` → `s` | **window 跨ぎ可** | マークしたペインと Swap Marked で交換 |
| `C-t G` | window 跨ぎ | 現在のペインを fzf popup で選んだ window へ送る (give)。get 側の旧 `C-t g` は git popup に転用済み |
| `C-t !` | — | ペインを独立した window に切り出す (break-pane) |

### ウィンドウ操作・ジャンプ

| キー | 動作 |
|---|---|
| `C-t c` / `C-t C-c` | 新規 window（カレントパス引き継ぎ） |
| `C-t Space` / `C-t BSpace` | 次 / 前の window（`M-n` / `M-p` なら prefix なし） |
| `C-t Tab` | 直前にいた window とトグル（claude 窓 ⇄ 作業窓の往復用） |
| `C-t f` | **fzf popup** で全セッションの window を曖昧検索してジャンプ（プレビュー付き） |
| `C-t w` | choose-tree（標準のツリー画面） |
| `C-t <` / `C-t >` | window メニュー / pane メニュー（Swap・Kill・Rename 等） |

### popup・その他

| キー | 動作 |
|---|---|
| `C-t g` / `C-g` | **git 操作 popup**（`C-g` = Ctrl+g なら prefix 不要）。いま見ているペインの cwd の repo に対して fzf のインクリメンタル操作で status/diff/add/commit（nvim や Claude のペインからでも効く。`Tab`/`Enter` = stage⇄unstage、`C-a` = 全 add、`C-o` = commit、`C-d` = diff 全画面、`Esc` = 閉じる） |
| `C-t t` | **スクラッチターミナルのトグル**。専用セッション scratch をフローティング表示し、popup 内でもう一度押すと閉じる（セッションは生きるので作業状態は保持） |
| `M-[` | prefix なしでコピーモードへ。vi キーバインド、`v`/`Space` で選択開始、`y`/`Enter` で pbcopy にコピーして抜ける |
| `C-t R` | 設定リロード |
| `C-t C-s` | レイアウトの手動保存（tmux-resurrect。自動保存・復元は continuum + 独自 hook で常時動作） |

### 視認性まわり

- アクティブペインの境界は **cyan の発光帯**（fg=bg 塗りつぶし + heavy 線 + 矢印インジケータ）
- カーソルは **cyan の明滅ブロック**（カーソルはアクティブペインにしか無いので現在地の点光源になる）
- 各ペイン上端にタイトルバー（ペイン番号 + パス + 実行コマンド。アクティブは cyan 帯）
- copy-mode 中だけ右端にスクロール位置バー（tmux 3.6+）

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

Claude Code を動かしているペインの境界に作業状態が出る。

| 表示 | 色 | 意味 |
|---|---|---|
| `⚙ working` | 黄 | 応答処理中 |
| `🔔 input` | 赤 | permission 承認待ち・質問への回答待ち (承認すると working に自動復帰) |
| `✓ idle` | 緑 | 完了・次の指示待ち |

さらに `🔔 input` / `✓ idle` への遷移時、**そのペインがどのクライアントでも前面に
見えていない**なら macOS 通知センターに通知が飛ぶ (input は音あり、idle は音なし)。
複数 claude 並走時の手待ち検知が画面監視なしでできる。
同じアイコンはステータスバーのウィンドウリストにも出るため、別ウィンドウの claude の
状態も一覧できる。

- Claude Code の hooks (`_claude/settings.json`) が `_claude/hooks/tmux-pane-state.sh` を呼び、
  ペイン単位オプション `@claude_state` を出し入れする
- `_tmux.conf` の `pane-border-format` が `#{?@claude_state,...,}` で表示。未設定ペイン
  (通常シェル) には何も出ない。セッション終了 (SessionEnd) で自動クリア

## macOS Integration

### Karabiner-Elements

設定の正本は [mac/karabiner.json](./mac/karabiner.json)（ANSI 基準。`bin/restore_karabiner_config.sh`
が適用時にマシンの実キーボード (JIS/ISO) を判定してパッチする。`bin/backup_karabiner_config.sh` は
live 設定の丸ごとコピーなので、JIS マシンで実行すると ANSI 基準が壊れる点に注意）。

- complex modifications は `make test-karabiner` (karabiner_cli) で意味レベル lint される
- simple_modifications の **`japanese_eisuu` → `a` は意図的なマッピング**（愛用中。削除しないこと。
  英数切り替えはコマンドキー単押し・Ctrl+T 等の complex rule 側が担っている）

### Finder Quick Actions

Finderの右クリックメニューから動画処理コマンドを実行できます。

```bash
# セットアップ
~/dotfiles/mac/finder-actions/setup-concat-finder-action.sh
```

詳細は [mac/finder-actions/README.md](./mac/finder-actions/README.md) を参照。
