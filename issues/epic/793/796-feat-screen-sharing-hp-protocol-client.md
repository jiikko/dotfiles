# 796 (feat): 高性能モードのプロトコルで相手の Mac に繋ぐ (接続・認証・映像・入力の芯)

起票日: 2026-10-16

> epic 793 の子。要望の 1 つ目。全体の前提と決めることは 794、App Store の扱いは 795。

## 要望 (ユーザーの言)

「macOS ネイティブアプリの画面共有と同じなのだが、ハイパフォーマンスのプロトコルとリバースエンジニアリングみたいな感じで、同じ仕組みを考えている。
ググったら Python でそのような実装があるので、それの真似をしたい」

## 調べたこと (2026-10-16。手元では何も動かしていない)

- 高性能モードの通信は、制御が TCP 5900、映像が UDP 5901、音声と RTCP が UDP 5900。SRTP で暗号化する。映像は HEVC RExt 4:4:4、
  音声は AAC-ELD ([iShareScreen](https://github.com/renegadelink/iShareScreen) の README)。Apple は UDP 5900〜5902 が通ることを求める
  ([Apple](https://support.apple.com/guide/remote-desktop/apdf8e09f5a9/mac))
- iShareScreen (AGPL) は `docs/apple_vnc_rfc.md` にプロトコルを書き起こしているらしい (ファイル名だけを見た。中身は読んでいない)
- **rootshell-vnc (MIT、Swift) は、この issue の部品をほぼ全部持っている** ([kitknox/rootshell-vnc](https://github.com/kitknox/rootshell-vnc) の README):
  - 部品の分け方: `RFBProtocol` (RFB の型・状態機械) / `RFBTransport` (TCP・UDP・認証・暗号・Apple の媒体の取り決め) /
    `RFBRendering` (画面の合成・カーソル・VideoToolbox の HEVC 復号・音声) / `rootshellVNC` (セッションの API・SwiftUI・入力・クリップボード)
  - 認証: 「Apple Remote Desktop authentication, including Apple DH and Mac/SRP flows」
  - 「High Performance `.adaptive` mode requires direct UDP reachability」
- RFB の Apple 独自の認証 (security type 30〜36) は IANA に Apple の割り当てとして載っていて、libvncclient に実装がある (795)

## 作り方の案

- **rootshell-vnc を Swift のパッケージとして使う** (MIT。794 の 4 の (c))。この issue の大半は「組み込んで高性能モードで 1 台に繋ぐ」で済むかもしれない
- 使えない・足りない部分だけを自分で書く。AGPL の iShareScreen のコードと docs は写さない (795 の決定。App Store の道を閉じない)

## 確かめること

- [ ] rootshell-vnc の `Examples/` のビューアを手元でビルドし、手元の Mac から相手の Mac 1 台に高性能モードで繋がるか
- [ ] rootshell-vnc の macOS の最低版 (README は macOS 15 以降) と、手元・相手の Mac の版
- [ ] sandbox の中で動くか (Network.framework の UDP・VideoToolbox。795 の「中身は sandbox で動く形」)

## 受け入れ条件

- 自作のアプリから、相手の Mac 1 台に高性能モードで繋ぎ、画面が動き、キーとマウスが届く
