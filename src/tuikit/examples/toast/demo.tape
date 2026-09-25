# tuikit の toast (右下の通知スタック) を gif に撮る。src/tuikit で `make demo` (= vhs このファイル)。
Output examples/toast/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1000
Set Height 480
Set Framerate 50
Set Padding 16
Set TypingSpeed 60ms

# ビルドは画面に出さない (コンパイル待ちを gif に入れない)
Hide
Type `bin="$(mktemp -d)/toast" && go build -o "$bin" ./examples/toast && clear`
Enter
Sleep 8s
Show

Type `"$bin"`
Enter
Sleep 1s

# 成功の通知: 右の画面外から滑り込み、3 秒止まって、右へ滑り出る
Type "s"
Sleep 4.5s

# 進行中 → 結果: 進行中の通知は次の通知が来たら退く
Type "i"
Sleep 1.5s
Type "s"
Sleep 1.2s

# 積む: 新しい通知は上に積まれ、古い通知は下から抜けていく
Type "f"
Sleep 700ms
Type "s"
Sleep 700ms
Type "f"
Sleep 5s

Type "q"
Sleep 500ms
Hide
Type `rm -rf "$(dirname "$bin")"`
Enter
