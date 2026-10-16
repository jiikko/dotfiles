# 793 (feat): macOS 専用の画面共有クライアント (Apple の高性能モードのプロトコルを自前で話す)

起票日: 2026-10-16

## 概要

ユーザー依頼 (2026-10-16): macOS 標準の「画面共有」(Screen Sharing.app) と同じことを、自作の macOS ネイティブアプリでやりたい。
標準アプリの**高性能モード** (High Performance) と同じプロトコルを、リバースエンジニアリングした公開実装 (Python) を参考に自前で話す。

自作したい理由 (ユーザーの言)。どちらも標準アプリの不満で、まだ実測していない:

- **修飾キーが手元の Mac と被る**。繋いだ先の Mac でデスクトップ (Mission Control の操作スペース) を切り替えられない
- **高性能モードで繋げる Mac が 1 台だけ**。ほかにも制限が多い

## 要件 (ユーザーの言から)

- macOS 専用のネイティブアプリ。手元 (見る側) も相手 (見られる側) も Mac だけを対象にする
- 画面・入力の性能は標準アプリの高性能モードと同じ水準にする。プロトコルは高性能モードのものを自前で話す (下の「わかっていること」)
- 接続先の Mac を一覧から選んで繋ぐ
- **常時接続**: アイドル中も繋ぎっぱなしにしておき、選べばすぐその画面に切り替わる
- 修飾キーとデスクトップの切り替えは、相手の Mac へ送るか手元の Mac に残すかを自分で決められる
- 複数の Mac に同時に繋げる (標準アプリの「1 台だけ」を外したい)

## わかっていること (2026-10-16 の Web 調査。出典は各項。手元ではまだ何も実測していない)

- **Apple の公式の制約** ([Apple: Use High Performance screen sharing](https://support.apple.com/guide/remote-desktop/apdf8e09f5a9/mac)):
  両方の Mac が Apple silicon で macOS Sonoma 14 以降。「高性能の画面共有のセッションは各 Mac で同時に 1 本まで」。
  4K の画面 1 枚で 75 Mbps を推奨、有線を推奨。UDP 5900〜5902 が通ること。仮想ディスプレイは最大 4K
- **参考にする Python 実装は [iShareScreen](https://github.com/renegadelink/iShareScreen)** (README の記述。コードはまだ読んでいない):
  - ライセンスは **AGPL-3.0-or-later**。Python 3.10 以降。macOS / Windows / Linux で動くビューア
  - 制御は TCP 5900、音声と RTCP は UDP 5900、映像は UDP 5901。暗号化は SRTP。映像は HEVC RExt 4:4:4、音声は AAC-ELD-SBR
  - 認証は相手の Mac のユーザー名とパスワード。クリップボードの同期は Linux でだけ書かれている
  - キー・マウスの送り方と、複数の画面・複数のセッションについては README に書かれていない
- **同じ仕組みの既存の実装** (どれも作る前に見比べる。ライセンスは未確認):
  [rootshell-vnc](https://github.com/dave-atx/rootshell-vnc) (Swift で書いたネイティブのクライアントで、高性能モードを UDP で話す) /
  [remotex](https://github.com/aleonnet/remotex) (ブラウザ向けのゲートウェイ。標準と高性能の両方を話す) /
  [libvncserver issue 696](https://github.com/LibVNC/libvncserver/issues/696) (高性能モードを入れるかの議論)
- 高性能モードで共有している間は相手の Mac の画面が暗くなる、と書く記事がある
  ([Six Colors](https://sixcolors.com/post/2026/06/high-performance-mode-allows-sharing-another-macs-display-as-if-your-own/))。
  常時接続にすると、相手の Mac の前に座っても使えない時間が続くかもしれない (未確認)
- screensharingd に認証を素通りできる脆弱性 (CVE-2026-65400) が報告されている
  ([Resecurity](https://www.resecurity.com/jp/blog/article/cve-2026-65400-macos-screen-sharing-authentication-bypass-under-active-exploitation))。
  画面共有を常時開けておくなら、繋ぐのは手元の網 (issue 747 の内部の網) の中だけに絞る

## 着手前に決めること・確かめること

1. **「1 台だけ」の制限がどちら側にあるか** (自作する意味がここで決まる): Apple の文言「各 Mac で同時に 1 本まで」は、見る側と
   見られる側のどちらの制限かが読み取れない。**見られる側の制限なら、1 台の相手に 2 本は繋げないが、別々の Mac に 1 本ずつなら自作でも標準
   アプリでも足りるかもしれない**。**見る側 (Screen Sharing.app) の制限なら、自作で外せる**。
   確かめ方: 手元の Mac から 2 台の相手へ、標準アプリで高性能のまま同時に繋げるかを試す。繋げなければ iShareScreen を 2 つ起こして試す
2. **修飾キーの問題が自作で直るか**: 標準アプリの設定で直らないかを先に確かめる。直らないなら、何がキーを取っているか
   (標準アプリか、手元の macOS のショートカットか) を確かめる。自作のアプリでも、手元の macOS がアプリより先に処理するキー
   (Mission Control の ctrl+←→ など) は CGEventTap などで横取りしないと相手へ送れない。アクセシビリティの許可が要る
3. **ライセンス**: iShareScreen は AGPL。コードを写したり、別の言語へ訳して移植したりすると派生物になり、AGPL がかかる。
   取れる道は 2 つで、ユーザーの判断が要る:
   - (a) AGPL を受け入れて移植する
   - (b) 仕様だけを文書に起こし (ポート・パケットの形・鍵の交換の手順)、その文書から書き直す
4. **作る前に既存の実装で足りないかを見る**: rootshell-vnc は既に Swift のネイティブのクライアントで、高性能モードを話す。
   上の 1・2 に答えられて、一覧・常時接続を足すだけで済むなら、自作より安い
5. **置き場所と言語**: 「ネイティブアプリ」なら Swift (HEVC の復号は VideoToolbox、UDP は Network.framework、入力は CGEvent)。
   dotfiles の中に置くか、別の repo にするか
6. **一覧の出どころ**: issue 747 (mac-lan-manager) の台帳と Bonjour の一覧を使えるか。
   747 の画面共有は今、標準アプリを開いている
7. **常時接続の重さ**: 画面が変わらない間の帯域・CPU・電池は未実測。繋いでおく台数 × 推奨 75 Mbps (4K) が網に収まるか。
   アイドルの間は映像を止めて繋がりだけを保つ、という形を取れるか
8. **パスワード**: Keychain に置く (iShareScreen は保存しない)

## 対応方針

未定。上の 1 と 2 の答えで、自作するか・どこまで作るかが決まる。最初の一歩は 1 の実測 (標準アプリで 2 台に同時に繋ぐ)。

## 関連

- issue 747 — 手元の Mac 同士を内部の網で繋ぐ道具 (一覧・台帳・画面共有の起動)

## 進捗

- [ ] 1: 「1 台だけ」の制限がどちら側にあるかを実測する
- [ ] 2: 修飾キーの問題の原因を確かめる
- [ ] 3: ライセンスの道 (a / b) をユーザーが決める
- [ ] 4: 既存の実装 (rootshell-vnc ほか) で足りないかを見る
