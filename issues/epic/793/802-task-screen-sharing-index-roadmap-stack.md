# 802 (task): epic 793 の index — 子 issue の一覧・作業の順番・ブロッカー・技術スタック

起票日: 2026-10-16

> epic 793 の子。子 issue の地図と段取りの正本。各 issue の中身はそれぞれの本文が正本で、ここには写さない。
> 子 issue を足したら、下の表と段取りに 1 行足す。

## 子 issue の一覧

| issue | 何か | 状態 | 依存 |
|---|---|---|---|
| [794](794-research-macos-screen-sharing-client-feasibility.md) | 要件・なぜ自作するのか・わかっていること・着手前に決めること | 決めることが残る | — |
| [795](795-research-app-store-distribution-and-third-party-apps.md) | App Store で出せるか / 他社アプリの仕組み | 決定済み: 自分で使うのが第一、出せるなら出す | — |
| [796](796-feat-screen-sharing-hp-protocol-client.md) | 高性能モードで 1 台に繋ぐ芯 (接続・認証・映像・入力) | 未着手 | 794 の 4・6 |
| [797](797-feat-screen-sharing-host-list.md) | 繋ぐ先の一覧 (747 から `open` で受け渡す) | 未着手 | 796・747 側の改修 |
| [798](798-feat-screen-sharing-always-connected.md) | 常時接続・ショートカットで切り替え・ウインドウの大きさを覚える・相手のディスプレイの切り替え | 未着手 | 796・800 |
| [799](799-feat-screen-sharing-modifier-keys-and-spaces.md) | 修飾キーと操作スペースの切り替えを相手へ送る | 未着手 | 796・下の B2 |
| [800](800-research-screen-sharing-multiple-hp-sessions.md) | 複数の Mac に同時に高性能で繋げるか | 一部実測 (標準アプリでは 1 本が高性能、2 本目は標準) | — |
| [801](801-feat-screen-sharing-ssh-side-channel-reactive.md) | 画面共有と並べて ssh を張り、相手の出来事を反映する | 案 | 798・747 の鍵 |

## 作業の順番

段ごとに、前の段の結果で次の段の形が変わる。段 0 は作るより安い実測で、自作の意味を決める。

1. **段 0 — 実測 (ユーザーの手で数分〜。コードは書かない)**
   - 799: 標準アプリで MacBook Pro に繋いだまま ctrl+→ を押し、手元・相手・どちらも動かない、のどれかを見る (全画面でも) → B2
   - 800: 体感の遅延を標準と高性能で撮り比べる (スマホのスローモーション)。標準で足りるなら B1 が軽くなる
   - 800: 2 台に同時に高性能で繋げるか。一番安いのは iShareScreen を Terminal から 2 本 (動かすだけで、コードは写さない) → B1
2. **段 1 — 決める (ユーザー)**: ライセンスの道 (794 の 4。推奨は (c) rootshell-vnc)、系統 1 / 2 (B1 の結果次第。795)。
   置き場所は決定済み (B4)。別の private の repo を作る (名前は未定)
3. **段 2 — 芯 (796)**: rootshell-vnc を呼ぶ最小の macOS のアプリで、1 台に高性能で繋ぐ。ここで B3 も確かめる
4. **段 3 — 使い勝手 (798・797)**: 常時接続・ショートカットでの切り替え・ウインドウの大きさを覚える・相手のディスプレイの切り替え。747 から `open` で受け渡す
5. **段 4 — キー (799)**: アプリが前面の間だけ ctrl+→ などを止めて相手へ送る (手元でビルドする版だけ)
6. **段 5 — ssh の副経路 (801)**
7. **段 6 — App Store 版 (795)**: sandbox・entitlements・審査。手元の版が使えるようになってから

## ブロッカー

- **B1 (最大) — 見る側の「高性能は 1 本」を自作で外せるか (800)**: 外せないと「複数台を高性能で」は満たせない。そのときは高性能 1 台 + 標準で常時接続、
  か系統 2 (相手に自作のサーバ。795) を考える。**段 0 で決まるまで、798 の複数台の設計を固めない**
- **B2 — ctrl+→ が相手の側で効くか (799)**: 手元で止めて送れても、相手の画面共有のサービスが通さないなら、送り方では直らない
- **B3 — macOS の版と Apple silicon (796)**: 高性能モードは両方の Mac が Apple silicon・Sonoma 14 以降。rootshell-vnc のパッケージは macOS 15 以降。
  手元は macOS 27.0.1 (2026-10-16 に `sw_vers`)。**相手の各 Mac の版とチップは未確認** (m1prombp は名前から M1 と推定)
- ~~B4 — 置き場所~~ → **ユーザー決定 (2026-10-16): ソースは別の repo にする。private**。dotfiles には issue (この epic) だけを置く。
  repo の名前と、issue を新しい repo へ移すかは未定。**repo は仕様を詰め終えるまで作らない** (ユーザー指示 2026-10-16)
- B5 (軽い) — 747 側の改修: `open vnc://…` をこのアプリの URL scheme に替える (797)。747 の担当と合わせる

## アプリ名の候補 (2026-10-16。repo の名前もこれに合わせる)

決めていない。App Store・GitHub・商標で同じ名前が使われていないかは**未確認** (決める前に確かめる)。

| 候補 | 由来 |
|---|---|
| **Hopscreen** | 画面から画面へ跳ぶ (ショートカットで接続先を切り替える) |
| **Switchyard** | 操車場。何台もの Mac を繋いだまま置いておき、行き先を切り替える |
| **Fleetglass** | 手元の Mac の群れ (fleet) を、ガラス越しに眺めて触る |
| **Mado** | 窓 (日本語)。相手の Mac への窓。短い |
| **Madoguchi** | 窓口。一覧から相手を選んで繋ぐ入口 |
| **Paneshift** | 画面 (pane) をずらして切り替える |
| **Tetherdesk** | 繋ぎっぱなし (tether) の机 (desktop) |
| **Lanview** | 内部の網 (LAN、747) の Mac を見る。747 (mac-lan-manager) と対になる名前 |
| **Multimac** | 複数の Mac を同時に。説明的で分かりやすいが、Mac を名前に入れると App Store の審査で問われうる (未確認) |

## 技術スタック (案。2026-10-16 の調査。確かめたものは出典つき)

| 層 | 案 | 根拠・気になる点 |
|---|---|---|
| 言語 | Swift 6 (language mode v6、厳しい並行性の検査) | rootshell-vnc の全 target が `.swiftLanguageMode(.v6)` (Package.swift) |
| 最低の OS | **macOS 26** (ユーザー決定 2026-10-16。一度 27 にして 26 に直した) | 手元 (見る側) のアプリの条件。相手の Mac は高性能モードの条件 (Apple silicon・Sonoma 14 以降) を満たせばよく、26 である必要は無い。rootshell-vnc は macOS 15 以降なので足りる |
| 画面共有のプロトコル | [rootshell-vnc](https://github.com/kitknox/rootshell-vnc) (MIT) の `RFBProtocol` / `RFBTransport` / `RFBRendering` | 依存を版で固定している: swift-nio `exact: 2.95.0`・swift-nio-ssl `2.36.0`・swift-nio-transport-services `1.26.0`・BigInt `5.3.0` 以上 ([Package.swift](https://raw.githubusercontent.com/kitknox/rootshell-vnc/main/Package.swift))。🚨 同じ swift-nio を使うほかの依存 (下の SSH) と版がぶつかりうる |
| アプリの殻・ウインドウ | AppKit (`NSWindow`) を主に、一覧などは SwiftUI | ウインドウの大きさを覚える (798) は `NSWindow` の frame の保存を相手ごとの鍵で持つ。全画面・キーの扱いも AppKit が素直。rootshell-vnc の SwiftUI の部品は iOS の操作 (タッチ・Pencil) が中心で、Mac でそのまま使えるかは未確認 |
| 映像の復号と描画 | VideoToolbox の HEVC の復号 (rootshell-vnc の `RFBRendering`) → `AVSampleBufferDisplayLayer` か Metal | どちらが遅れが少ないかの比較の資料は見つからなかった。VideoToolbox の復号器は別プロセスで動く ([WWDC20 10090](https://developer.apple.com/videos/play/wwdc2020/10090/))。動いてから測る (794 の 10) |
| 網・探索 | Network.framework (NIO transport services 経由)・`NWBrowser` で `_rfb._tcp` | ローカルネットワークのプライバシー: Info.plist に `NSBonjourServices` と説明文が要る ([TN3179](https://developer.apple.com/documentation/technotes/tn3179-understanding-local-network-privacy)) |
| キーの入力 | 前面の間: `NSEvent` / 手元の版だけ: `CGEventTap` (`defaultTap`、アクセシビリティ) / 全体のホットキー: `RegisterEventHotKey` | sandbox で `defaultTap` は作れない見込み (795)。`RegisterEventHotKey` が sandbox で効くかは未確認 (798) |
| SSH (801) | [Citadel](https://github.com/orlandos-nl/Citadel) (MIT。swift-nio-ssh の上。コマンドの実行・出力の逐次受け取り・ポートの転送・SFTP) か、手元の版だけ `/usr/bin/ssh` | Citadel の RSA の認証は swift-nio-ssh の fork に依る。🚨 swift-nio の版が rootshell-vnc の固定 (2.95.0) と合うかを先に確かめる |
| 秘密 | Keychain (相手のパスワード・SSH の鍵) | iShareScreen はパスワードを保存しない。こちらは常時接続なので保存が要る |
| 受け渡し | URL scheme (`CFBundleURLTypes`) | 747 から `open` で起こす (797) |
| ビルド | XcodeGen (`project.yml`) で Xcode のプロジェクトを作る | 手元の他の Mac / iOS のアプリと同じ作法 (`ios-app-developer` の skill)。Xcode のプロジェクトを手で持たない |
| テスト・lint | Swift Testing・SwiftLint | rootshell-vnc は XCTest か Swift Testing かを未確認 |
| 署名・配布 | 手元: 手元の署名でビルドして使う / 後で: Developer ID か Mac App Store | 手元の署名ではビルドのたびに権限 (アクセシビリティ・入力監視) を付け直す手間が出うる (未確認。795) |

## 反証レビュー

省いた (2026-10-16)。表の事実は Package.swift と Citadel の README を直に開いて取り、そのほかは各子 issue の反証レビュー済みの記述を指している。
未確認のものは未確認と書いた。
