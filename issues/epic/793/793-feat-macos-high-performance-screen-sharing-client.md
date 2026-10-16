# 793 (feat): macOS 専用の画面共有クライアント (Apple の高性能モードのプロトコルを自前で話す)

起票日: 2026-10-16

> epic 793 の親 issue。本文は子 issue に書く。

## 目的

標準の画面共有 (Screen Sharing.app) の高性能モードと同じ性能で、複数の Mac に常時繋いでおき、一覧から選んで切り替える macOS のアプリを作る。
標準アプリでは修飾キーが手元と被ってデスクトップを切り替えられず、高性能で繋げるのも 1 台だけなので、自作を考えている (ユーザー依頼 2026-10-16)。

## 状態

調査中。まだ作るかどうかも決めていない。

## 子 issue

- [794](794-research-macos-screen-sharing-client-feasibility.md) — 要件・わかっていること・着手前に決めること
- [795](795-research-app-store-distribution-and-third-party-apps.md) — Swift で作って App Store で出せるか / 他社の画面共有アプリは何を話しているか

要望ごと (2026-10-16 に要望を調べて書き出した):

- [796](796-feat-screen-sharing-hp-protocol-client.md) — 高性能モードのプロトコルで繋ぐ (接続・認証・映像・入力の芯)
- [797](797-feat-screen-sharing-host-list.md) — 繋ぐ先の Mac を一覧から選ぶ
- [798](798-feat-screen-sharing-always-connected.md) — 常時接続にして、選んだらすぐ切り替わる
- [799](799-feat-screen-sharing-modifier-keys-and-spaces.md) — 修飾キーとデスクトップの切り替えを相手へ送る
- [800](800-research-screen-sharing-multiple-hp-sessions.md) — 複数の Mac に同時に高性能で繋げるか (自作の意味を決める)
