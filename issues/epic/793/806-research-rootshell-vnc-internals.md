# 806 (research): rootshell-vnc の中身を調べる (このアプリの芯に使えるか)

起票日: 2026-10-16

> epic 793 の子。796 (1 台に繋ぐ芯) の前に、使うライブラリの中身を確かめる。目次は 802。

## 概要

ユーザー依頼 (2026-10-16):「rootshell-vnc の中身を調査する issue md を作って、./tmp に git clone をして調査するタスクを今先にやっていいよ」。

調べる元: [kitknox/rootshell-vnc](https://github.com/kitknox/rootshell-vnc) を `~/dotfiles/tmp/rootshell-vnc-2026-10-16/repo` に clone して読む (読むだけ。ビルドしない)。

## 問い

1. 公開 API の形 (繋ぐ・切る・ディスプレイを選ぶ・コマの統計・入力を送る)
2. Mac のアプリから使えるか (AppKit で使えるか、UIKit に縛られていないか)
3. 複数のセッションを同時に持てるか (グローバルな状態・UDP の受け口の番号の固定)。800 の B1 に効く
4. 負荷を下げる手段 (frame rate・帯域を相手に頼めるか・止められるか・IDR を頼み直せるか)。803 に効く
5. 画面を暗くする (curtain)・仮想ディスプレイの選び方。798 に効く
6. 認証と秘密の扱い
7. 版の付け方・依存・テスト・ライセンス
8. sandbox で問題になりそうな箇所 (外のプロセス・ファイル・待ち受け)

## 結果 (2026-10-16。commit `c56ae02` (2026-10-06)、タグ v0.1.6 を読んだ。ビルド・実行はしていない)

**結論: このアプリの芯に使える見込みが高い。** 通信・認証・暗号・高性能モードの映像の受信と復号はライブラリに任せ、Mac の入力と画面の殻を自分で書く。

1. **公開 API**: `rootshellVNC` の `VNCSession` (`Sources/rootshellVNC/API/VNCSession.swift`) に集まっている。`connect(credentials:)` / `disconnect()` /
   `reconnect(with:)` / `sendKeyEvent(downFlag:key:)` / `sendPointerEvent` / `sendScrollEvent` / `sendGestureEvent` / クリップボード / `setCurtainMode` /
   `updateRemoteDisplaySize` / `currentStatistics()` (受けたコマ・復号したコマ・欠け・通信の統計) / `isHighPerformanceMode` / `connectionState`。
   設定は `VNCConfiguration` (表示の決め方 `remoteDisplay` / `matchClient`、表示の組み方 `oneDisplay` / `allDisplaysCombined` / `twoVirtualDisplays`、
   セキュリティの方針、自前の通り道 `VNCTransportProvider` / `VNCDatagramProvider`)
2. **Mac で使えるか**: 下の層 (`RFBProtocol` / `RFBTransport` / `RFBRendering`) は Foundation・VideoToolbox・BSD ソケットだけで、OS に依らない。
   上の層は `#if canImport(UIKit)` / `#elseif canImport(AppKit)` で分けてあり、画像の表示には AppKit の経路 (`NSViewRepresentable`) がある。
   **ただしキーとポインタの受け取り (`VNCKeyboardCapture`・`KeyboardInputHandler`・`TouchInputHandler`) は UIKit だけ**で、`NSEvent` を扱うコードは無い。
   → Mac のアプリでは、入力 (NSEvent → `sendKeyEvent` / `sendPointerEvent`) を自分で書く。キーの振り分け (799) もここで書くので、むしろ都合がよい
   (UIKit 側には「アプリが持つショートカットは相手へ送らない」`VNCHostKeyboardShortcut` の考え方があり、同じ形を AppKit で作る)
3. **複数のセッション**: `VNCSession` はインスタンスごとに状態を持ち、セッションをまたぐ可変のグローバルな状態は見当たらない (定数とデバッグの統計だけ)。
   高性能の UDP は、相手が示した番号 (既定 5900、映像 1・音声・映像 2 の番号) と**同じ番号で手元でも bind して相手へ connect する** (symmetric RTP)。
   `SO_REUSEADDR` / `SO_REUSEPORT` を付けている (`Sources/RFBTransport/Connection/PosixUDPChannel.swift` の冒頭のコメント、
   `TransportSession.swift` の `configureAppleMediaUDP`)。connect 済みの UDP は相手ごとに受け分けられるので、**別々の Mac への 2 セッションが同じ番号で
   共存できる見込み** (未確認)。手元の番号は環境変数 `ROOTSHELL_VNC_MEDIA_LOCAL_UDP_PORT` で変えられる。
   参考: 2026-10-16 の観測で、Apple の `avconferenced` の 5901 は connect していない受け口 (`*:5901`) だった。これでは 2 台目の映像を受け分けられないので、
   Apple の「1 本まで」の理由として筋が通る (推測。800)
4. **負荷を下げる手段**: (a) 帯域: 手元から相手へ RCTL で希望の帯域を伝える作り (`AppleMediaRateController`。相手は助言として受ける。20 Mbps〜60 Mbps の段)。
   隠れた画面は低い帯域を伝えれば、網と相手の符号化が減る見込み。frame rate を直に頼む口は見つからなかった。
   (b) `suspendsDisplayPresentation`: 描画を止める。**復号は続ける** (次のコマにすぐ戻るため)。手元の CPU の節約は描画の分だけ。
   (c) `suspendsRemoteDisplaySizeUpdates`: 隠れている間の大きさの変更を相手へ送らない (コメントに「hosting view is occluded」の用途が書いてある)。
   (d) 落ちたコマからの回復の IDR は内部で扱っている (`VideoStreamManager`)。外から IDR を頼む公開の口は見つからなかった
5. **curtain・表示**: `setCurtainMode` がある (App Store の版の説明では、相手で Remote Management が有効なときだけ効く)。仮想ディスプレイは 1 枚・2 枚・物理の画面をまとめる、から選べる
6. **認証と秘密**: Apple の認証 (DH・SRP) を持つ。直前の認証情報は Keychain の汎用パスワードに置く (`LastConnectionCredentialStore`)
7. **版・依存・テスト・ライセンス**: タグ v0.1.2〜v0.1.6 (0.x なので API は変わりうる)。2026-07-09〜10-06 に 146 commit (活発)。テストのファイル 60 本。
   依存は swift-nio 2.95.0 (版を固定)・swift-nio-ssl・swift-nio-transport-services・BigInt。MIT (Copyright 2026 Rootshell LLC, Kit Knox)
8. **sandbox**: 外のプロセスを起こすコードは無い。BSD ソケットで UDP を bind・connect するので、外へ繋ぐ entitlement に加えて待ち受け (server) の entitlement が要る見込み。
   手元の Mac で画面共有のサーバ (screensharingd) が UDP 5900 を持っていても、`SO_REUSEPORT` で同じ番号に bind できる作り (コメントの「loopback」)

## 残った問い (ソースを読むだけでは分からない)

- [ ] 2 セッションを本当に同時に持てるか (800 の実測。このライブラリの最小の Mac のアプリで 2 台に繋ぐ)
- [ ] RCTL で低い帯域を伝えたとき、相手が本当に落とすか (803)
- [ ] macOS の sandbox の中で、同じ番号への bind (`SO_REUSEPORT`) が通るか

clone (`~/dotfiles/tmp/rootshell-vnc-2026-10-16/repo`) は結論をここに移した後に消した (2026-10-16、ユーザー指示)。読み直すときは commit `c56ae02` を clone し直す。
