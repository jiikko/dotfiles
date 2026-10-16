# 796 (feat): 高性能モードのプロトコルで相手の Mac に繋ぐ (接続・認証・映像・入力の芯)

起票日: 2026-10-16

> epic 793 の子。要望の 1 つ目。全体の前提と決めることは 794、App Store の扱いは 795。

## 要望 (ユーザーの言)

「macOS ネイティブアプリの画面共有と同じなのだが、ハイパフォーマンスのプロトコルとリバースエンジニアリングみたいな感じで、同じ仕組みを考えている。
ググったら Python でそのような実装があるので、それの真似をしたい」

## 調べたこと (2026-10-16。手元では何も動かしていない)

- 高性能モードの通信は、制御が TCP 5900、映像が UDP 5901、音声と RTCP が UDP 5900。SRTP で暗号化する。映像は HEVC RExt 4:4:4、
  音声は AAC-ELD ([iShareScreen](https://github.com/renegadelink/iShareScreen) の README)。Apple は UDP 5900〜5902 が通ることを求める
  ([Apple](https://support.apple.com/guide/remote-desktop/apdf8e09f5a9/mac))。UDP 5902 の用途は分かっていない (iShareScreen は 5900〜5901 だけを使う)
- iShareScreen (AGPL) は `docs/apple_vnc_rfc.md` にプロトコルを書き起こしているらしい (ファイル名だけを見た。中身は読んでいない)
- **rootshell-vnc (MIT、Swift) は、この issue の部品をほぼ全部持っている** ([kitknox/rootshell-vnc](https://github.com/kitknox/rootshell-vnc) の README):
  - 部品の分け方: `RFBProtocol` (RFB の型・状態機械) / `RFBTransport` (TCP・UDP・認証・暗号・Apple の媒体の取り決め) /
    `RFBRendering` (画面の合成・カーソル・VideoToolbox の HEVC 復号・音声) / `rootshellVNC` (セッションの API・SwiftUI・入力・クリップボード)
  - 認証: 「Apple Remote Desktop authentication, including Apple DH and Mac/SRP flows」
  - 「High Performance `.adaptive` mode requires direct UDP reachability」
  - 🚨 パッケージは macOS 15 以降だが、**付属のビューア (`Examples/`) は macOS のアプリではない**:「The standalone viewer currently has an iOS and
    iPadOS 26.2 deployment target and also supports Mac Catalyst」。Mac では Catalyst で動かすことになり、要る macOS の版は Tahoe 相当の見込み
    (推定)。ビューアは App Sandbox を宣言していない (反証レビューが entitlements を読んだ)
- macOS 15 からの**ローカルネットワークのプライバシー**: アプリが手元の網の相手に繋ぐ・Bonjour で探すには許可が要り、Info.plist の
  `NSBonjourServices` に探すサービス型を並べる。許可が無いと `NWBrowser` は `-65570` (PolicyDenied) で待ちに入りうる。Terminal・ssh から
  走らせたコマンドの道具は対象外 ([TN3179](https://developer.apple.com/documentation/technotes/tn3179-understanding-local-network-privacy))
- RFB の Apple 独自の認証 (security type 30〜36) は IANA に Apple の割り当てとして載っていて、libvncclient に実装がある (795)

## 標準アプリの高性能モードと同じ技術か (ユーザーの問い 2026-10-16)

- **同じ**: 相手の Mac の側 (Apple の画面共有サーバ・画面の撮影・専用回路での HEVC の符号化) と、通信の中身 (プロトコル)。自作はそれと話すだけ
- **違う**: 手元のクライアントの実装。標準アプリの高性能の UDP は `avconferenced` が握っていた (2026-10-16 に `lsof` で観測。800 の実測)。
  これは Apple の非公開のフレームワークの一部と見られ、アプリからは使えない (審査ガイドライン 2.5.1「Apps may only use public APIs」)。
  自作は公開 API で組み直した実装 (rootshell-vnc の swift-nio・Network.framework、VideoToolbox) を使う
- 差が出うるのは手元の網の扱い (揺らぎを吸収するバッファ・落ちたパケットからの回復・帯域に合わせた画質の調整)。復号は同じハードウェアの
  復号器を通るはず。標準アプリと同じ条件で遅延と乱れを比べるまで、同等とは言わない (未実測)

## 作り方の案

- **rootshell-vnc を Swift のパッケージとして使い、macOS のアプリ (AppKit か SwiftUI) から呼ぶ** (MIT。794 の 4 の (c))。付属のビューアは iOS 向けなので、
  Mac のアプリの殻は自分で書く。この issue の大半は「組み込んで高性能モードで 1 台に繋ぐ」で済むかもしれない
- 使えない・足りない部分だけを自分で書く。AGPL の iShareScreen のコードと docs は写さない (795 の決定。App Store の道を閉じない)

## 確かめること

- [ ] 手元・相手の Mac の macOS の版 (パッケージは macOS 15 以降、付属のビューアを Catalyst で動かすなら Tahoe 相当)
- [ ] 付属のビューアを Catalyst で動かすか、パッケージを呼ぶ最小の macOS のアプリを書くかして、相手の Mac 1 台に高性能モードで繋がるか
- [ ] sandbox の中で動くか (Network.framework の UDP・VideoToolbox。795 の「中身は sandbox で動く形」)

## 受け入れ条件

- 自作のアプリから、相手の Mac 1 台に高性能モードで繋ぎ、画面が動き、キーとマウスが届く。「高性能モードで」の判定: UDP 5901 に映像が流れ
  (`nettop` 等で受信を見る)、VideoToolbox の HEVC の復号を通っている (アプリのログに出す)
