# av1ify 分割計画

## 現状

| ファイル | 行数 | 内容 |
|---------|------|------|
| `zshlib/_av1ify.zsh` | 1001行 | 関数本体すべて |
| `tests/zshrc/av1ify/test_av1ify.sh` | 1011行 | テスト72件すべて |

## ソースの分割案

### 1. `zshlib/_av1ify.zsh` — エントリポイント + オーケストレーション (~345行)

- バージョン定数 (`__AV1IFY_VERSION` 等)
- グローバル状態変数 (`__AV1IFY_ABORT_REQUESTED` 等)
- `__av1ify_banner()`
- `__av1ify_on_interrupt()`
- `av1ify()` — オプション解析、-f処理、複数ファイル・ディレクトリの分配
- 他ファイルの `source` 読み込み

### 2. `zshlib/_av1ify_postcheck.zsh` — 変換後チェック (~165行)

- `__av1ify_mark_issue()` — 問題ファイルのリネーム
- `__av1ify_postcheck()` — 7つの検査項目
  - 音声ストリーム有無
  - 音ズレ (A/V sync)
  - 再生時間ズレ (ソース vs 出力)
  - フレーム数不一致
  - 出力解像度不一致
  - ファイルサイズ異常
  - 映像コーデック検証

### 3. `zshlib/_av1ify_encode.zsh` — エンコード処理 (~490行)

- `__av1ify_pre_repair()` — 事前リペア
- `__av1ify_one()` — 単一ファイルのエンコード
  - バリデーション (解像度/fps/denoise)
  - ドライラン
  - ソース解析 (解像度、fps、音声コーデック)
  - アップスケール防止・fpsキャップ
  - CRF自動調整
  - ffmpegフィルタ・引数構築
  - エンコード実行 + 音声copy失敗時のリトライ

## テストの分割案

### 1. `tests/zshrc/av1ify/test_av1ify_basic.sh` — 基本機能 (Tests 1-7)

- ヘルプ表示
- ドライラン
- 単一ファイル処理
- スキップ判定 (-enc, -encoded)
- 存在しないファイルのエラー

### 2. `tests/zshrc/av1ify/test_av1ify_batch.sh` — バッチ処理 (Tests 8-13)

- ディレクトリ再帰処理
- 複数ファイル指定
- -f ファイルリスト
- -f エラー (ファイルなし、引数なし)
- -f のヘルプメッセージ

### 3. `tests/zshrc/av1ify/test_av1ify_options.sh` — オプション (Tests 14-57)

- --resolution (バリデーション、ドライラン、実行、アップスケール防止、縦長)
- --fps (バリデーション、ドライラン、実行、fpsキャップ)
- --denoise (バリデーション、実行、組み合わせ)
- --compact (ドライラン、実行、上書き、音声判定)
- 環境変数 (AV1_RESOLUTION, AV1_FPS)
- CLIオプション優先順位
- 不明オプションのエラー

### 4. `tests/zshrc/av1ify/test_av1ify_postcheck.sh` — 変換後チェック (Tests 58-72)

- 再生時間ズレ検出 + 閾値カスタマイズ
- フレーム数不一致 + fps変更時のスキップ
- 出力解像度不一致 + 縦長
- ファイルサイズ異常 + 閾値カスタマイズ
- 映像コーデック検証

## ファイル構成 (分割後)

```
zshlib/
├── _av1ify.zsh              # エントリポイント (source で下2つを読む)
├── _av1ify_encode.zsh       # エンコード処理
└── _av1ify_postcheck.zsh    # 変換後チェック

tests/zshrc/av1ify/
├── test_helper.sh           # 共通セットアップ・モック・アサート関数
├── test_av1ify_basic.sh     # Tests 1-7
├── test_av1ify_batch.sh     # Tests 8-13
├── test_av1ify_options.sh   # Tests 14-57
└── test_av1ify_postcheck.sh # Tests 58-72
```

## 分割時の注意点

- テスト共通部分（モック ffmpeg/ffprobe、`assert_file_exists` 等）は `tests/zshrc/av1ify/test_helper.sh` に切り出し、各テストファイルから `source` する
- `_av1ify.zsh` 内の `source` で `_av1ify_postcheck.zsh` → `_av1ify_encode.zsh` の順に読み込む。`_zshrc` が `source $HOME/dotfiles/zshlib/_av1ify.zsh` とハードコードしているため、分割ファイルのパスも `$HOME/dotfiles/zshlib/` を基準にする（`${0:A:h}` は `_zshrc` 経由呼び出し時に不正確になるため避ける）
- Makefile の `test-zshrc` ターゲットを更新: 旧 `test_av1ify.sh` を削除し、新しい4つのテストファイルを追加する
- `__av1ify_one()` が `__av1ify_postcheck()` を呼ぶ依存関係があるので、`_av1ify_encode.zsh` は `_av1ify_postcheck.zsh` の後に source される必要がある
