# 799 (feat): 修飾キーとデスクトップ (操作スペース) の切り替えを相手の Mac へ送る

起票日: 2026-10-16

> epic 793 の子。要望の 4 つ目。App Store との関係は 795。

## 要望 (ユーザーの言)

「apple 公式アプリだと modifier キーが被っていてマルチデスクトップ操作ができない」

## 調べたこと (2026-10-16。手元では何も試していない)

- 標準アプリには、システムのキーを相手へ送らず手元に残す隠し設定 `DoNotSendSystemKeys` がある (`defaults write com.apple.ScreenSharing
  DoNotSendSystemKeys -bool YES`。10.6 のころの記事 [computerwoche](https://www.computerwoche.de/a/don-t-send-special-keys-in-10-6-s-screen-sharing,1926509))。
  逆向き (全部を相手へ送る) の設定があるか、今の版と高性能モードで効くかは未確認
- macOS Sierra のころ、修飾キー + 矢印 (ctrl+→ のスペースの切り替えなど) が画面共有のサービスを通らない不具合があった
  ([Edovia の blog](https://blog.edovia.com/?p=307))。今の版で直っているかは未確認
- 仮想マシンでも、cmd+tab や ctrl+↑ が手元の macOS に取られる症状が報告されている ([Parallels forum](https://forum.parallels.com/threads/keyboard-in-vm-not-redirecting-shortcuts-on-focus.361803/))。
  手元の macOS が、アプリより先に処理するキーがある
- rootshell-vnc は「Software and hardware keyboard capture with host-reserved shortcut routing」を持つ (どのキーをどう振り分けるかは README に無い)
- 手元のキーを止めるには、止められるイベントタップ (`defaultTap`) が要る。sandbox では作れない見込み (795)。読むだけのタップでは止められない

## 作り方の案

- 段階 1 (安い): 手元の Mac の Mission Control のショートカットを、相手と被らないキーへ振り替える (システム設定 > キーボード > ショートカット)。
  自作せずに済むかもしれない
- 段階 2: アプリが前面の間だけ、止められるタップで ctrl+←→ などを手元で効かせず、相手へ送る。手元でビルドする版だけの機能 (795 の決定)
- 段階 3: 送るか手元に残すかを、キーごとに切り替えられる設定にする

## 確かめること

- [ ] 標準アプリの今の挙動: どのキーが相手へ届き、どのキーが手元で効くか (ctrl+←→ / ctrl+↑ / cmd+tab / cmd+space)。`defaults read com.apple.ScreenSharing`
- [ ] 段階 1 だけでユーザーの不満が消えるか (ユーザーに聞く)
- [ ] 手元でビルドした版で、`defaultTap` で ctrl+←→ を止めて相手へ送れるか (アクセシビリティの許可)

## 受け入れ条件

- 相手の画面を前面にしている間、ctrl+←→ で相手の Mac の操作スペースが切り替わり、手元の Mac の操作スペースは動かない
