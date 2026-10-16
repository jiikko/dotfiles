# 802 (task): epic 793 の目次 — 着手する順番・ブロッカー・技術スタック

起票日: 2026-10-16

> epic 793 の子。子 issue の目次と段取りの正本 (目次はここ 1 か所だけに置く)。各 issue の中身はそれぞれの本文が正本で、ここには写さない。
> 子 issue を足したら、下の目次の着手する段に 1 行足す。

## 今やること

**段 0 の実測** (ユーザーの手。コードは書かない) と、別のマシンでの spike (repo `jiikko/screen-share-app` の `HANDOFF.md`。
rootshell-vnc の CLI で 1 台、次に 2 台へ同時に高性能で繋ぐ。800 の B1 と 806 の残った問いに答える)。

## 目次 (着手する順)

段の順に進める。同じ段の中は並べてよい。「着手の条件」が揃うまで、その段の設計を固めない。

| 段 | issue | やること | 誰が | 着手の条件 |
|---|---|---|---|---|
| 前提 | [794](794-research-macos-screen-sharing-client-feasibility.md) | 要件・なぜ自作するのか・わかっていること・決めることの正本 (読む) | — | — |
| 前提 | [795](795-research-app-store-distribution-and-third-party-apps.md) | App Store の扱い (決定済み: 自分で使うのが第一、出せるなら出す) と他社アプリの仕組み (読む) | — | — |
| **0** | [800](800-research-screen-sharing-multiple-hp-sessions.md) | 実測: 2 台に同時に高性能で繋げるか / 高性能に繋ぐまでの秒数と、標準と行き来したときに相手のウインドウが動くか / 標準と高性能の体感の遅延 | ユーザー | なし (今すぐ) |
| **0** | [799](799-feat-screen-sharing-modifier-keys-and-spaces.md) | 実測: 標準アプリで ctrl+→ を押し、手元・相手・どちらも動かない、のどれかを見る (全画面でも) | ユーザー | なし (今すぐ) |
| **1** | [794](794-research-macos-screen-sharing-client-feasibility.md) の 4 / [795](795-research-app-store-distribution-and-third-party-apps.md) | 決める: ライセンスの道 (推奨は rootshell-vnc)・相手に自作のサーバを置くか (系統 1 / 2)・アプリ名 (下の候補)・repo を作る時期 | ユーザー | 段 0 の 800 の結果 |
| **1** | [806](806-research-rootshell-vnc-internals.md) | 調べる: rootshell-vnc の中身 (公開 API・Mac で使えるか・複数セッション・負荷の下げ方) | Claude | なし (2026-10-16 に着手) |
| **1** | [805](805-design-screen-sharing-claude-pdca-debug-loop.md) | 決める: Claude が確かめる経路 (CLI・ログ・デバッグの口)・試験に使う Mac・verify の入口の名前 | ユーザー + Claude | なし |
| **2** | [796](796-feat-screen-sharing-hp-protocol-client.md) | 作る: 1 台に高性能で繋ぐ芯。805 の 1・2 (画面を出さない CLI・ログ) を同時に入れる。B3 (相手の版とチップ) を確かめる | Claude | 段 1 の決定・別の repo |
| **3** | [798](798-feat-screen-sharing-always-connected.md) | 作る: 常時接続・ショートカットでの切り替え・ウインドウの大きさを覚える・相手のディスプレイの切り替え・別ウインドウ | Claude | 796。複数台の設計は 800 の結果次第 |
| **3** | [797](797-feat-screen-sharing-host-list.md) | 作る: 747 から `open` で受け渡す URL scheme (747 側の改修と合わせる) | Claude | 796 |
| **3** | [803](803-feat-screen-sharing-throttle-hidden-sessions.md) | 作る: 見えていない画面の負荷を下げる | Claude | 798 |
| **4** | [799](799-feat-screen-sharing-modifier-keys-and-spaces.md) | 作る: アプリが前面の間だけ ctrl+→ などを止めて相手へ送る (手元でビルドする版だけ) | Claude | 796。段 0 の 799 で相手側で効くと分かっていること |
| **5** | [801](801-feat-screen-sharing-ssh-side-channel-reactive.md) | 作る: ssh の副経路 (ユースケースの 2・3 から) | Claude | 798 |
| 別線 | [804](804-feat-multi-mac-claude-code-dashboard.md) | 複数の Mac の Claude Code を 1 台から見渡す。**Swift のアプリを待たずに mac-lan-manager の Go の TUI で先に試せる**。試すならいつでも着手できる | Claude | ユーザーが「Go の TUI で先に試す」と決めたら |
| **6** | [795](795-research-app-store-distribution-and-third-party-apps.md) | App Store 版: sandbox・entitlements・審査 | Claude + ユーザー | 手元の版が使えるようになってから |

## ブロッカー

- **B1 (最大) — 見る側の「高性能は 1 本」を自作で外せるか (800)**: 外せないと「複数台を高性能で」は満たせない。そのときは高性能 1 台 + 標準で常時接続、
  か系統 2 (相手に自作のサーバ。795) を考える。**段 0 で決まるまで、798 の複数台の設計を固めない**
- **B2 — ctrl+→ が相手の側で効くか (799)**: 手元で止めて送れても、相手の画面共有のサービスが通さないなら、送り方では直らない
- **B3 — macOS の版と Apple silicon (796)**: 高性能モードは両方の Mac が Apple silicon・Sonoma 14 以降。rootshell-vnc のパッケージは macOS 15 以降。
  手元は macOS 27.0.1 (2026-10-16 に `sw_vers`)。**相手の各 Mac の版とチップは未確認** (m1prombp は名前から M1 と推定)
- ~~B4 — 置き場所~~ → **ユーザー決定 (2026-10-16): ソースは別の repo にする。private**。dotfiles には issue (この epic) だけを置く。
  **2026-10-16 に作った: private repo `jiikko/screen-share-app` (`~/src/screen-share-app`)**。repo 名は仮で、製品名が決まったら揃える (ユーザー)。
  別のマシンで開発を続けるため、引き継ぎのプロンプトを repo の `HANDOFF.md` に置き、issues/ の運用を入れた。中身は SwiftPM の骨組み
  (rootshell-vnc 0.1.6 に依存する実行ファイル `ssa-probe`) だけで、spike はまだ書いていない (`swift build` も未実行)。
  この Mac には Xcode が無い。epic 793 の issue は dotfiles に残している (移すかは未定)
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
