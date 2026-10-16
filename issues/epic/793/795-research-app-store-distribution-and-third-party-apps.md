# 795 (research): Swift で作って App Store で出せるか / 他社の画面共有アプリは何を話しているか

起票日: 2026-10-16

> epic 793 の子。要件と、着手前に決めることの正本は 794。

## 概要

ユーザーの問い (2026-10-16):
- Swift で作ったら App Store で公開できるのか
- 他社の画面共有アプリはいくつもあるが、リバースエンジニアリングしているのか。全部 VNC なのか

下は 2026-10-16 の Web 調査の初期の答え。手元では何も試していない。

## App Store で出せるか (初期の答え: 原理的には出せそうだが、794 の要件のうち 2 つが審査・sandbox とぶつかる)

- **画面共有のクライアント自体は Mac App Store に並んでいる**: Edovia の [Screens 5](https://apps.apple.com/us/app/screens-5-vnc-remote-desktop/id1663047912)、
  [Jump Desktop](https://apps.apple.com/py/app/jump-desktop-rdp-vnc-fluid/id524141863?mt=12)。この種類のアプリが出せないわけではない
- **ネットワークのプロトコルを自前で話すことは、非公開 API の利用には当たらないはず**: 審査ガイドライン
  [2.5.1](https://developer.apple.com/app-store/review/guidelines/) は「Apps may only use public APIs」。クライアントが使うのは
  Network.framework・VideoToolbox (HEVC の復号) などの公開 API で、プロトコルは通信の中身。ただしこれは解釈で、審査の判断ではない
- **リモートデスクトップの条項 4.2.7** は「特定のソフト・サービスを映すアプリ」(ゲームのストリーミングなど) が対象で、端末の画面全体を映す
  一般のクライアントには当たらない、と文言から読める: 「If your remote desktop app acts as a mirror of specific software or services rather
  than a generic mirror of the host device」
- 🚨 **修飾キーの横取りは sandbox とぶつかる** (794 の 2):
  - Mac App Store のアプリは sandbox が必須 (ガイドライン 2.4.5 (i))
  - Apple の DTS の回答 ([forums 707680](https://developer.apple.com/forums/thread/707680)・[789896](https://developer.apple.com/forums/thread/789896)):
    sandbox のアプリはアクセシビリティの権限を使えない。CGEventTap でキーを**読む**のは入力監視の権限で、sandbox でもできる
  - キーを**送る**側 (CGEvent.post) はアクセシビリティの権限 (kTCCServicePostEvent) が要る。それを使ったクリップボードのアプリが
    2.4.5 で却下された例がある ([forums 820594](https://developer.apple.com/forums/thread/820594))。リモートのクライアントがキーを相手へ送るのは
    ネットワーク越しなので CGEvent.post は要らないはず。問題は、手元の macOS が先に食う ctrl+←→ などを奪えるかで、入力監視のタップで
    (読むだけでなく) 止められるかは未確認
- 🚨 **AGPL の移植は App Store と相性が悪い** (794 の 4 の (a)): App Store の利用規約と GPL 系のライセンスがぶつかる、という見方が広く
  知られている (FSF の立場。この issue ではまだ一次資料を確かめていない)。App Store で出すなら、(b) 書き直すか (c) MIT の rootshell-vnc の道になる
- Apple はプロトコルを公開していないので、macOS の更新で予告なく変わりうる。審査を通っても、後から壊れる
- **自分の Mac だけで使うなら App Store は要らない**: 自分でビルドして署名すれば、sandbox も審査も外せる (アクセシビリティの権限も使える)。
  App Store で出すかどうかで、取れる設計が変わる

## 他社のアプリは何を話しているか (初期の答え: 大きく 2 つの系統。全部が VNC ではない)

1. **Mac に最初から入っている画面共有のサーバ (screensharingd) と話す系統** — こちらは VNC (RFB) 
   - Screens 5 (名前も「VNC Remote Desktop」)、Jump Desktop の VNC の接続など。Edovia の案内は、相手の Mac で標準の画面共有を有効にする手順
     ([Edovia](https://help.edovia.com/en/screens-5/getting-started/prepare-mac-for-screen-sharing))
   - RFB 自体は公開された仕様。Apple 独自の部分 (Apple Remote Desktop の認証方式など) は仕様が公開されておらず、各社や libvncserver などが
     解析して実装している、と言われる (未確認。この issue ではまだ一次資料を確かめていない)
   - **高性能モード (HEVC・UDP) を話すものはまだ少ない**: rootshell-vnc / iShareScreen / remotex など、どれも解析による実装 (794)。
     Screens と Jump Desktop が高性能モードを話すという記述は見つからなかった
2. **相手の Mac にも自社のアプリ (host agent) を入れてもらい、自社のプロトコルで話す系統** — Apple の仕組みを解析する必要が無い
   - Jump Desktop の Fluid: 「Screen Sharing features require the free Jump Desktop Connect app」(App Store の説明)
   - Parsec / AnyDesk / TeamViewer / RustDesk も同じ系統と思われる (この issue では一次資料を確かめていない)。
     画面は ScreenCaptureKit で撮り、入力は相手の Mac の上で自分で起こす
   - 794 の要件に当てると、この系統なら「1 本まで」も「画面の暗転」も Apple の制約ではなくなる。代わりに相手の Mac に自作の
     サーバを常駐させることになり、画面収録とアクセシビリティの権限が要る

## 残っている問い

- [ ] App Store で出したいのか、自分の Mac で使えればよいのか (ユーザーに聞く。794 の設計の分かれ目)
- [ ] 794 の方針として、系統 1 (Apple のサーバと話す) と系統 2 (自作のサーバを相手に置く) のどちらを取るか。
  系統 2 は 794 の 1 と 3 の制約を外せるが、作る量が倍になる
- [ ] 入力監視のタップで ctrl+←→ を止められるかを、sandbox の中で試す (App Store で出すときだけ要る)
