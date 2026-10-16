# 799 (feat): 修飾キーとデスクトップ (操作スペース) の切り替えを相手の Mac へ送る

起票日: 2026-10-16

> epic 793 の子。要望の 4 つ目。App Store との関係は 795。

## 要望 (ユーザーの言)

「apple 公式アプリだと modifier キーが被っていてマルチデスクトップ操作ができない」

## 調べたこと (2026-10-16。手元では何も試していない)

- 標準アプリには、システムのキーを相手へ送らず手元に残す隠し設定 `DoNotSendSystemKeys` がある (`defaults write com.apple.ScreenSharing
  DoNotSendSystemKeys -bool YES`。10.6 のころの記事 [computerwoche](https://www.computerwoche.de/a/don-t-send-special-keys-in-10-6-s-screen-sharing,1926509))。
  逆向き (全部を相手へ送る) の設定があるか、今の版と高性能モードで効くかは未確認
- 🚨 **受ける側 (相手の Mac) で効かない形もある**: macOS Sierra のころ、修飾キー + 矢印 (ctrl+→ など) が**相手の画面共有のサービスを通らない**
  不具合があり、10.12.2 で直った (Edovia の記事「Updated: macOS Sierra and broken keyboard shortcuts」。元の URL `blog.edovia.com/?p=307` は今
  別の記事に飛ぶ)。Mojave でも、TeamViewer で ctrl+↑ が相手に素の矢印として届いた報告がある
  ([TeamViewer community](https://community.teamviewer.com/English/discussion/47331/mac-10-14-mojave-control-arrow-combination-not-registered-correctly))。
  手元で止めて送れても、相手の側でスペースが切り替わるとは限らない
- 仮想マシンの例 ([Parallels forum](https://forum.parallels.com/threads/keyboard-in-vm-not-redirecting-shortcuts-on-focus.361803/)) は、Apple の
  仮想化の枠組みの制約 (「we use the Apple framework and are quite limited」) で、手元の macOS 一般の話ではない
- rootshell-vnc は「Software and hardware keyboard capture with host-reserved shortcut routing」を持つ (どのキーをどう振り分けるかは README に無い)
- 手元のキーを止めるには、止められるイベントタップ (`defaultTap`) が要る。sandbox では作れない見込み (795)。読むだけのタップでは止められない

## ユーザー決定 (2026-10-16)

「接続先の Mac を単独で使うこともあるので操作感を変えたくない」

- 相手の Mac の設定はどの段階でも触らない (相手を単独で使うときの操作感は変わらない)
- **段階 1 (手元の Mission Control のショートカットを別のキーへ振り替える) は採らない**: 手元の Mac の操作スペースの切り替えキーが、画面共有を
  使っていない間も変わる。切り替えのたびにシステムの設定を書き換える形も、前面の判定とずれる・書き戻し損ねるので採らない

## 作り方の案

- **本命: アプリが前面の間だけ**、止められるタップで ctrl+←→ などを手元で効かせず、相手へ送る。アプリが後ろへ回れば手元は元どおり。
  手元でビルドする版だけの機能 (795 の決定。App Store 版は止めずに送るだけ)
- 次: 送るか手元に残すかを、キーごとに切り替えられる設定にする

- rootshell-vnc のキーの受け取りは UIKit だけで、Mac の入力 (NSEvent → `sendKeyEvent`) は自分で書く (806 の 2)。キーの振り分けはその中に作る

## 確かめること

- [ ] 標準アプリの今の挙動: どのキーが相手へ届き、どのキーが手元で効くか (ctrl+←→ / ctrl+↑ / cmd+tab / cmd+space)。`defaults read com.apple.ScreenSharing`
- [ ] 相手の Mac の側で ctrl+←→ が効くか: 標準アプリで相手へ届いているキーについて、相手の操作スペースが実際に切り替わるか
- [ ] 手元でビルドした版で、`defaultTap` で ctrl+←→ を止めて相手へ送れるか (アクセシビリティの許可)

## 受け入れ条件

- 相手の画面を前面にしている間、ctrl+←→ で相手の Mac の操作スペースが切り替わり、手元の Mac の操作スペースは動かない
