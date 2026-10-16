# 797 (feat): 繋ぐ先の Mac を一覧から選ぶ

起票日: 2026-10-16

> epic 793 の子。要望の 2 つ目。

## 要望 (ユーザーの言)

「接続したホストを一覧で選択して」

## 調べたこと (2026-10-16)

- 画面共有を有効にした Mac は Bonjour で `_rfb._tcp` を出す。issue 747 の mac-lan-manager は、この一覧と台帳
  (`~/.config/mac-lan-manager/hosts.toml`) を合わせて出し、画面共有は `open vnc://<有線の IP>` で標準アプリを開いている (747 の本文。
  反証レビューで照合済み)
- 一度繋いだ Mac を覚えておく形は、標準アプリにもある (Window > Connections > All Connections。[Apple](https://support.apple.com/guide/remote-desktop/apdf8e09f5a9/mac))
- Bonjour の広告に、高性能モードに対応しているか (Apple silicon・Sonoma 以降か) が載るかは未確認

## 作り方の案

- 一覧の出どころは 2 つ: Bonjour (`NWBrowser` で `_rfb._tcp` を探す) と、一度繋いだ Mac の記録
- 747 の台帳を読むか: 747 は Go の TUI で、台帳は TOML。Swift のアプリが同じファイルを読めば、別名 (host に付けた名前) と内部の網の IP を
  共有できる。ただし App Store 版は sandbox で `~/.config` を読めない (ユーザーにファイルを選ばせるか、手元でビルドする版だけの機能にする)
- 747 の一覧から「このアプリで繋ぐ」を起動できるようにする手もある (URL scheme を受ける)

## 確かめること

- [ ] Bonjour の `_rfb._tcp` の TXT レコードに、版や高性能モードの対応が載るか (相手の Mac で `dns-sd -L` を見る)
- [ ] 747 の台帳を共有するか、このアプリが自分の記録を持つか (ユーザーに聞く)

## 受け入れ条件

- 一覧に、内部の網で見えている Mac と一度繋いだ Mac が出て、選ぶと繋がる (常時接続のときは、繋いだままの画面へ切り替わる。798)
