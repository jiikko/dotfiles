# 848 (feat): lan-manager の o で、画面共有を SyakeScreen の URL で開く

> 🚨 **担当中: MacBookPro (glogx)**（2026-10-20〜）

起票日: 2026-10-20

## 概要

my-products の screen-share-app (SyakeScreen) の issue 812 で、URL scheme `syakescreen://connect?host=<宛先>&user=<ユーザー名>[&port=<番号>]` を入れた (2026-10-20)。
lan-manager の `o` (外のアプリで開く) は今 `open vnc://<宛先>` で Apple の画面共有を開く (`src/mac-lan-manager/lanmgr/model.go` の `case "o"`)。
これを SyakeScreen の URL にすると、有線の宛先で高性能の接続を 1 手で開ける。

## アプリ側で決まっていること (812 の実装。2026-10-20)

- `user` は必須 (アプリの一覧の鍵が `user@host[:port]` なので、省くと同じ Mac の 2 本目になる)。無い・不正なら開かずに理由を出す
- パスワードは渡さない (URL に載せても捨てる)
- 一覧に同じ相手が居れば選ぶ。宛先の書き方が違っても、ユーザー名とポートが同じで名前を引いたアドレスが重なれば同じ相手 (ユーザー決定 2026-10-20)。
  一覧に居なければ宛先を埋めたフォームを開くだけで、人が押すまで繋がない
- 宛先は ASCII の名前か 4 つの 10 進の組の IPv4 (数字だけ・0x の組・空の組は断る)。IPv6 は渡せない

## やること

- `o` の宛先を、SyakeScreen が入っていれば `syakescreen://connect?host=<m.target(r)>&user=<userFor(r)>`、無ければ従来の `vnc://` にする (または別のキーにする。どちらにするかは着手のときにユーザーに聞く)
  - ユーザー名は `userFor(r)` (台帳に無ければ推測の名前)。推測の名前で開いたときは、アプリ側でフォームか「パスワードが未登録」になる
  - 入っているかの判定: `open -Ra SyakeScreen` か LaunchServices に scheme を聞く (`/usr/bin/open` の終了コードだけで判定しない。未登録の scheme でも開くダイアログが出る)
- 確かめる: 有線の名前 (`<ホスト名>.wired.local`) と IP の両方で、SyakeScreen の一覧の同じ Mac が選ばれる (812 の受け入れ条件の lan-manager 側)。
  一覧の相手が Wi-Fi の IP で覚えてあり、URL が有線のアドレスだと、アドレスが重ならずフォームに回る (812 の照合の限界)

## 関連

- my-products: apps/screen-share-app の issue 812・docs/manual.md の「URL で開く」
- issue 747 (lan-manager の要件)
