# swift-off-actor-callback-outside-actor の根拠

## 起源 (screen-share-app, 2026-10-17)

macOS アプリで ⌘Tab を相手へ送るための event tap (`CGEvent.tapCreate`) を、main thread から専用の thread へ移した。tap の C の callback を
`@MainActor` の型のメソッドの中の closure で書いていた。closure は main actor に属するので、別の thread から呼ばれた瞬間に Swift の
実行時の検査で落ち、起動直後に SIGTRAP になった。単体テストは tap を張らないので緑のままだった。callback をファイルの外側の関数
(actor に属さない場所) に移して直した (screen-share-app の `Sources/SSAKit/SystemShortcutCapture.swift` の `tapCallback`)。

## なぜ新しいルールか

dotfiles の issue 818 で検討した。Swift の並行性のルールは他に無く、発動点 (actor に属する型の中で別 thread から呼ばれる callback を書く瞬間) は
既存のどのルールとも違う。Swift のファイルを読んだときだけ読み込む (`paths:`) ので、常時読み込みの字数には足さない。
起票時点で事例は 1 回。もう 1 回踏むまで待つ案もあったが、落ち方 (起動直後のクラッシュ・テストは緑) が重いので先に置いた。
