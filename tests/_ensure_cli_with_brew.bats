#!/usr/bin/env bats

# テスト用のセットアップ
setup() {
  # _ensure_cli_with_brew関数を読み込む
  source "$BATS_TEST_DIRNAME/../zshlib/_ensure_cli_with_brew.zsh"

  # テスト用の一時ディレクトリを作成
  export TEST_BREW_INSTALL_LOG="$BATS_TEST_TMPDIR/brew_install.log"
  rm -f "$TEST_BREW_INSTALL_LOG"
}

# brewコマンドのモック
brew() {
  case "$1" in
    list)
      # 未インストールとして扱う
      return 1
      ;;
    install)
      # brew installが呼ばれたことを記録
      echo "brew install $2" >> "$TEST_BREW_INSTALL_LOG"
      return 0
      ;;
    --prefix)
      echo "/opt/homebrew"
      return 0
      ;;
  esac
}

# type -pコマンドのモック（存在しないコマンド用）
type() {
  if [[ "$1" == "-p" ]]; then
    # 実行ファイルが存在しないことをシミュレート
    return 1
  fi
  command type "$@"
}

@test "PATHに存在しないコマンドはbrew installを呼び出す" {
  # brewとtypeをモック関数としてエクスポート
  export -f brew
  export -f type

  # 存在しないコマンドで_ensure_cli_with_brewを実行
  run zsh -c "
    source $BATS_TEST_DIRNAME/../zshlib/_ensure_cli_with_brew.zsh
    $(declare -f brew)
    $(declare -f type)
    TEST_BREW_INSTALL_LOG='$TEST_BREW_INSTALL_LOG'
    _ensure_cli_with_brew nonexistent_command nonexistent_formula
  "

  # brew installが呼ばれたことを確認
  [ -f "$TEST_BREW_INSTALL_LOG" ]
  grep -q "brew install nonexistent_formula" "$TEST_BREW_INSTALL_LOG"
}

@test "PATHに存在するコマンドはbrew installを呼び出さない" {
  run zsh -c "
    source $BATS_TEST_DIRNAME/../zshlib/_ensure_cli_with_brew.zsh
    $(declare -f brew)
    TEST_BREW_INSTALL_LOG='$TEST_BREW_INSTALL_LOG'
    _ensure_cli_with_brew ls
  "

  # brew installが呼ばれていないことを確認
  [ ! -f "$TEST_BREW_INSTALL_LOG" ] || ! grep -q "brew install" "$TEST_BREW_INSTALL_LOG"
}
