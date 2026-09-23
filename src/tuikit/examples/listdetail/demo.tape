# tuikit の「一覧 → 詳細」の遷移を gif に撮る。src/tuikit で `make demo` (= vhs このファイル)。
# 演出は -slow 3 (3 倍に伸ばしている)。実速度の体感は `go run ./examples/listdetail` で見る。
Output examples/listdetail/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1100
Set Height 560
Set Framerate 50
Set Padding 16
Set TypingSpeed 60ms

# ビルドは画面に出さない (コンパイル待ちを gif に入れない)
Hide
Type `bin="$(mktemp -d)/listdetail" && go build -o "$bin" ./examples/listdetail && clear`
Enter
Sleep 8s
Show

# 起動: 一覧が上から順に右から流れ込む (layout.SlideIn)
Type `"$bin" -slow 3`
Enter
Sleep 1.5s

# カーソル移動と半ページ移動 (描画カーソルだけが滑る: anim.CursorGlide)
Type "jj"
Sleep 600ms
Space
Sleep 1.8s
Type "b"
Sleep 1.8s
Type "jjj"
Sleep 600ms

# 詳細を開く: 右外から滑り込み、左に一覧の端が残る (anim.Transition + layout.ComposeDrawer)
Enter
Sleep 1.5s

# 詳細の半ページスクロール (anim.ScrollGlide)
Space
Sleep 1.2s

# 開いたまま隣の項目へ (板は動かず中身だけ替わる。左の一覧のカーソルが追従する)
Type "J"
Sleep 900ms
Type "J"
Sleep 900ms

# 閉じる: 開く動きの逆再生
Escape
Sleep 1.5s

Type "q"
Sleep 500ms
Hide
Type `rm -rf "$(dirname "$bin")"`
Enter
