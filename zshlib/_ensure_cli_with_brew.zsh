# Homebrew で CLI を保証するユーティリティ
function _ensure_cli_with_brew() {
  local cmd="$1"
  local formula="${2:-$1}"

  # 実行ファイルが存在するか確認（functionやaliasではなく、実行可能なバイナリのみ）
  if type -p "$cmd" > /dev/null 2>&1; then
    return 0
  fi

  # Homebrewが利用可能か確認
  if ! command -v brew > /dev/null 2>&1; then
    echo "Error: '$cmd' is not installed and Homebrew is unavailable." >&2
    return 1
  fi

  # すでにインストール済みか確認
  if brew list "$formula" > /dev/null 2>&1; then
    # インストール済みだがPATHに入っていない可能性がある
    echo "Warning: '$formula' is installed via Homebrew but '$cmd' command is not available." >&2
    echo "Try running: brew link $formula" >&2
    return 1
  fi

  echo "Installing $formula via Homebrew..." >&2
  if ! brew install "$formula"; then
    echo "Error: Failed to install $formula via Homebrew." >&2
    return 1
  fi

  # コマンドハッシュをリフレッシュして新しくインストールされたコマンドを認識
  hash -r

  # インストール後に再度コマンドが利用可能か確認
  if ! type -p "$cmd" > /dev/null 2>&1; then
    echo "Error: '$cmd' was installed but is not available in PATH." >&2
    echo "Try running: brew link $formula" >&2
    return 1
  fi

  return 0
}
