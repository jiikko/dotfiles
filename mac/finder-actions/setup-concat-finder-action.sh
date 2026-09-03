#!/bin/bash
# Finder Quick Action: Concat Videos (Terminal版)
# ターミナルを開いてconcatコマンドを実行するFinderアクション

set -euo pipefail

# エラーハンドリング
trap 'echo "エラー: セットアップに失敗しました (line $LINENO)" >&2' ERR

WORKFLOW_NAME="Concat Videos (Terminal)"
SERVICES_DIR="$HOME/Library/Services"
WORKFLOW_DIR="$SERVICES_DIR/${WORKFLOW_NAME}.workflow"
CONTENTS_DIR="$WORKFLOW_DIR/Contents"

echo ">> Finderクイックアクションをセットアップ中..."

# ディレクトリ作成
mkdir -p "$CONTENTS_DIR"

# Info.plist作成
cat > "$CONTENTS_DIR/Info.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>NSServices</key>
	<array>
		<dict>
			<key>NSBackgroundColorName</key>
			<string>background</string>
			<key>NSIconName</key>
			<string>NSActionTemplate</string>
			<key>NSMenuItem</key>
			<dict>
				<key>default</key>
				<string>Concat Videos (Terminal)</string>
			</dict>
			<key>NSMessage</key>
			<string>runWorkflowAsService</string>
			<key>NSRequiredContext</key>
			<dict>
				<key>NSApplicationIdentifier</key>
				<string>com.apple.finder</string>
			</dict>
			<key>NSSendFileTypes</key>
			<array>
				<string>public.movie</string>
			</array>
		</dict>
	</array>
</dict>
</plist>
EOF

# document.wflow作成
cat > "$CONTENTS_DIR/document.wflow" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>AMApplicationBuild</key>
	<string>521.1</string>
	<key>AMApplicationVersion</key>
	<string>2.10</string>
	<key>AMDocumentVersion</key>
	<string>2</string>
	<key>actions</key>
	<array>
		<dict>
			<key>action</key>
			<dict>
				<key>AMAccepts</key>
				<dict>
					<key>Container</key>
					<string>List</string>
					<key>Optional</key>
					<true/>
					<key>Types</key>
					<array>
						<string>com.apple.cocoa.path</string>
					</array>
				</dict>
				<key>AMActionVersion</key>
				<string>2.0.3</string>
				<key>AMApplication</key>
				<array>
					<string>Automator</string>
				</array>
				<key>AMParameterProperties</key>
				<dict>
					<key>COMMAND_STRING</key>
					<dict/>
					<key>CheckedForUserDefaultShell</key>
					<dict/>
					<key>inputMethod</key>
					<dict/>
					<key>shell</key>
					<dict/>
					<key>source</key>
					<dict/>
				</dict>
				<key>AMProvides</key>
				<dict>
					<key>Container</key>
					<string>List</string>
					<key>Types</key>
					<array>
						<string>com.apple.cocoa.string</string>
					</array>
				</dict>
				<key>ActionBundlePath</key>
				<string>/System/Library/Automator/Run Shell Script.action</string>
				<key>ActionName</key>
				<string>Run Shell Script</string>
				<key>ActionParameters</key>
				<dict>
					<key>COMMAND_STRING</key>
					<string># Finderから渡されたファイルを配列に格納
files=()
while IFS= read -r line; do
    files+=("$line")
done

# ファイルが2つ未満の場合
if [[ ${#files[@]} -lt 2 ]]; then
    osascript -e 'display alert "エラー" message "最低2つのファイルを選択してください"'
    exit 1
fi

# ファイルパスをエスケープしてリスト化
file_args=""
for f in "${files[@]}"; do
    # シングルクォートをエスケープ
    escaped="${f//\'/\'\\\'\'}"
    file_args="${file_args} '${escaped}'"
done

# 最初のファイルのディレクトリを取得してエスケープ
first_dir="$(dirname "${files[0]}")"
# first_dirもシングルクォートエスケープを適用
first_dir_escaped="${first_dir//\'/\'\\\'\'}"

# Terminalを開いてconcatを実行
# quoted heredoc + osascript引数経由で安全に変数を渡す
osascript - "$first_dir_escaped" "$file_args" &lt;&lt;'APPLESCRIPT'
on run argv
    set firstDir to item 1 of argv
    set fileArgs to item 2 of argv
    tell application "Terminal"
        activate
        do script "cd " &amp; quoted form of firstDir &amp; " &amp;&amp; concat " &amp; fileArgs
    end tell
end run
APPLESCRIPT
</string>
					<key>CheckedForUserDefaultShell</key>
					<true/>
					<key>inputMethod</key>
					<integer>0</integer>
					<key>shell</key>
					<string>/bin/zsh</string>
					<key>source</key>
					<string></string>
				</dict>
				<key>BundleIdentifier</key>
				<string>com.apple.RunShellScript</string>
				<key>CFBundleVersion</key>
				<string>2.0.3</string>
				<key>CanShowSelectedItemsWhenRun</key>
				<false/>
				<key>CanShowWhenRun</key>
				<true/>
				<key>Category</key>
				<array>
					<string>AMCategoryUtilities</string>
				</array>
				<key>Class Name</key>
				<string>RunShellScriptAction</string>
				<key>InputUUID</key>
				<string>12345678-1234-1234-1234-123456789012</string>
				<key>Keywords</key>
				<array>
					<string>Shell</string>
					<string>Script</string>
					<string>Command</string>
					<string>Run</string>
					<string>Unix</string>
				</array>
				<key>OutputUUID</key>
				<string>87654321-4321-4321-4321-210987654321</string>
				<key>UUID</key>
				<string>ABCDEF01-2345-6789-ABCD-EF0123456789</string>
				<key>UnlocalizedApplications</key>
				<array>
					<string>Automator</string>
				</array>
				<key>arguments</key>
				<dict>
					<key>0</key>
					<dict>
						<key>default value</key>
						<integer>0</integer>
						<key>name</key>
						<string>inputMethod</string>
						<key>required</key>
						<string>0</string>
						<key>type</key>
						<string>0</string>
						<key>uuid</key>
						<string>0</string>
					</dict>
					<key>1</key>
					<dict>
						<key>default value</key>
						<false/>
						<key>name</key>
						<string>CheckedForUserDefaultShell</string>
						<key>required</key>
						<string>0</string>
						<key>type</key>
						<string>0</string>
						<key>uuid</key>
						<string>1</string>
					</dict>
					<key>2</key>
					<dict>
						<key>default value</key>
						<string></string>
						<key>name</key>
						<string>source</string>
						<key>required</key>
						<string>0</string>
						<key>type</key>
						<string>0</string>
						<key>uuid</key>
						<string>2</string>
					</dict>
					<key>3</key>
					<dict>
						<key>default value</key>
						<string></string>
						<key>name</key>
						<string>COMMAND_STRING</string>
						<key>required</key>
						<string>0</string>
						<key>type</key>
						<string>0</string>
						<key>uuid</key>
						<string>3</string>
					</dict>
					<key>4</key>
					<dict>
						<key>default value</key>
						<string>/bin/sh</string>
						<key>name</key>
						<string>shell</string>
						<key>required</key>
						<string>0</string>
						<key>type</key>
						<string>0</string>
						<key>uuid</key>
						<string>4</string>
					</dict>
				</dict>
				<key>isViewVisible</key>
				<integer>1</integer>
				<key>location</key>
				<string>449.500000:316.000000</string>
				<key>nibPath</key>
				<string>/System/Library/Automator/Run Shell Script.action/Contents/Resources/Base.lproj/main.nib</string>
			</dict>
			<key>isViewVisible</key>
			<integer>1</integer>
		</dict>
	</array>
	<key>connectors</key>
	<dict/>
	<key>workflowMetaData</key>
	<dict>
		<key>workflowTypeIdentifier</key>
		<string>com.apple.Automator.servicesMenu</string>
	</dict>
</dict>
</plist>
EOF

echo "✅ セットアップ完了！"
echo ""
echo "📍 インストール場所: $WORKFLOW_DIR"
echo ""

# サービスキャッシュを自動更新
echo ">> サービスキャッシュを更新中..."
/System/Library/CoreServices/pbs 2>/dev/null || true

echo ""
echo "使い方:"
echo "  1. Finderで動画ファイルを2つ以上選択"
echo "  2. 右クリック → クイックアクション → 'Concat Videos (Terminal)'"
echo "  3. Terminalが開いて、concatコマンドが実行されます"
echo ""
echo "🚨  注意: 初回実行時はmacOSが確認ダイアログを表示する場合があります"
