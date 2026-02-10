# Finder Quick Actions

macOS Finderの右クリックメニューから実行できるカスタムアクション集

## 📦 含まれるアクション

### Concat Videos (Terminal)

複数の動画ファイルを無劣化で結合する`concat`コマンドをFinderから実行します。

**特徴:**
- ターミナルが開いて進捗がリアルタイムで表示される
- zshrcの`concat`関数を使用
- 連番ファイルの自動検出
- コーデック不一致の検出

## 🚀 セットアップ

```bash
# Finder Quick Actionをインストール（サービスキャッシュも自動更新されます）
~/dotfiles/macos/finder-actions/setup-concat-finder-action.sh
```

**注意:** スクリプトが自動的に以下を実行します：
- ワークフローファイルの生成と配置
- サービスキャッシュの更新（`pbs` コマンド）

## 📖 使い方

### Concat Videos (Terminal)

1. Finderで動画ファイルを**2つ以上選択**
2. 右クリック → **クイックアクション** → **Concat Videos (Terminal)**
3. Terminalが開いて`concat`コマンドが実行される

**例:**
```
video_001.mp4
video_002.mp4
video_003.mp4
```
を選択して実行 → `video.mp4` が生成される

## 🔧 メンテナンス

### 再インストール

```bash
~/dotfiles/macos/finder-actions/setup-concat-finder-action.sh
```

### アンインストール

```bash
rm -rf ~/Library/Services/Concat\ Videos\ \(Terminal\).workflow
/System/Library/CoreServices/pbs  # キャッシュ更新（-flush は不要）
```

### メニューに表示されない場合

```bash
/System/Library/CoreServices/pbs  # 引数なしで軽量に更新
killall Finder
```

## 📁 インストール場所

- Quick Action: `~/Library/Services/Concat Videos (Terminal).workflow`
- セットアップスクリプト: `~/dotfiles/macos/finder-actions/setup-concat-finder-action.sh`

## 🛠 カスタマイズ

ワークフローファイルは以下の構造になっています：

```
~/Library/Services/Concat Videos (Terminal).workflow/
├── Contents/
│   ├── Info.plist          # サービスのメタデータ
│   └── document.wflow      # Automatorワークフロー定義
```

スクリプトを編集したい場合は、`setup-concat-finder-action.sh`の`COMMAND_STRING`部分を変更してください。

## 🐛 トラブルシューティング

### 「操作を完了できませんでした」エラー

- Terminal.app で `concat` 関数が使えるか確認:
  ```bash
  # 新しいTerminalウィンドウを開いて実行
  type concat
  ```
- `concat` 関数は `~/dotfiles/zshlib/_concat.zsh` に定義されています
- Terminal.app は起動時に自動で `.zshrc` をロードするため、`source` は不要です

### ターミナルが開かない

- システム環境設定 → プライバシーとセキュリティ → オートメーション
- Automatorに対してTerminalの制御を許可

### 初回実行時の権限確認

初回実行時、macOSが以下の確認ダイアログを表示します：

**「"Automator"がTerminal.appを制御しようとしています」**
- → **許可** をクリックしてください

後から許可する場合：
1. **システム設定** → **プライバシーとセキュリティ**
2. **オートメーション** → **Automator** → **Terminal.app** をオン

### セキュリティについて

このQuick Actionは以下のセキュリティ対策を実装しています：

- ファイルパスの適切なエスケープ処理
- AppleScript の `quoted form` を使用した安全なパス渡し
- 不要な環境変数ロードの削減

詳細は `setup-concat-finder-action.sh` のコード内コメントを参照してください。
