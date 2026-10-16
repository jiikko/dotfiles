# 794 (research): 自作の画面共有クライアントの要件・わかっていること・着手前に決めること

起票日: 2026-10-16

> epic 793 の子。もとは 793 の本文で、793 を epic にしたときにここへ移した (2026-10-16)。

## 概要

ユーザー依頼 (2026-10-16): macOS 標準の「画面共有」(Screen Sharing.app) と同じことを、自作の macOS ネイティブアプリでやりたい。
標準アプリの**高性能モード** (High Performance) と同じプロトコルを、リバースエンジニアリングした公開実装 (Python) を参考に自前で話す。

## なぜ自作するのか

標準アプリ (Screen Sharing.app) への不満 (ユーザーの言。まだ実測していない):

- **修飾キーが手元の Mac と被る**。繋いだ先の Mac でデスクトップ (Mission Control の操作スペース) を切り替えられない (799)
- **高性能モードで繋げる Mac が 1 台だけ**。ほかにも制限が多い (800)
- **キーボードショートカットで接続先を切り替えられない**。ショートカット一発で、繋いでいる Mac をシュッと替えたい (798。2026-10-16 に追加)
- **ウインドウの大きさを覚えてくれない**。切断されるたびに大きさを直す必要があり、だるい (798。2026-10-16 に追加)

## 要件 (ユーザーの言から)

> 要件ごとの調査と作り方は子 issue に分けた (2026-10-16): 796 プロトコル / 797 一覧 / 798 常時接続 / 799 修飾キー / 800 複数台。

- macOS 専用のネイティブアプリ。手元 (見る側) も相手 (見られる側) も Mac だけを対象にする
- 画面・入力の性能は標準アプリの高性能モードと同じ水準にする。プロトコルは高性能モードのものを自前で話す (下の「わかっていること」)
- 接続先の Mac を一覧から選んで繋ぐ
- **常時接続**: アイドル中も繋ぎっぱなしにしておき、選べばすぐその画面に切り替わる
- 修飾キーとデスクトップの切り替えは、相手の Mac へ送るか手元の Mac に残すかを自分で決められる
- 複数の Mac に同時に繋げる (標準アプリの「1 台だけ」を外したい)

## わかっていること (2026-10-16 の Web 調査。出典は各項。手元ではまだ何も実測していない)

- **Apple の公式の制約** ([Apple: Use High Performance screen sharing](https://support.apple.com/guide/remote-desktop/apdf8e09f5a9/mac)):
  - 両方の Mac が Apple silicon で macOS Sonoma 14 以降
  - 「Only one High Performance screen sharing session can be active on each Mac at a time.」
    「各 Mac」なので、見る側にも見られる側にもかかる。**標準アプリでは、2 台に同時に高性能で繋げない**ことが文書から読める
  - 4K の画面 1 枚で 75 Mbps を推奨、有線を推奨。仮想ディスプレイは最大 4K
  - 「Both Mac computers must be able to communicate with each other over UDP ports 5900, 5901, and 5902.」(制御は TCP 5900。下の iShareScreen)
  - 🚨 **相手の Mac に、そこでログインしているユーザーの名前で入ると、相手の画面は暗くなる**:
    「the hardware displays connected to that Mac are blanked to give you privacy」「no one can use that Mac while you're using
    High Performance screen sharing」。**常時接続と真っ向からぶつかる** (下の「決めること」の 3)
- **参考にする Python 実装は [iShareScreen](https://github.com/renegadelink/iShareScreen)** (README の記述。コードはまだ読んでいない):
  - ライセンスは **AGPL-3.0-or-later**。Python 3.10 以降。macOS / Windows / Linux で動くビューア
  - 制御は TCP 5900、音声と RTCP は UDP 5900、映像は UDP 5901。暗号化は SRTP。映像は HEVC RExt 4:4:4、音声は AAC-ELD-SBR
  - 認証は相手の Mac のユーザー名とパスワード。クリップボードの同期は Linux でだけ書かれている
  - 起動例に `--no-curtain` がある (`iss --host mac.local -u me --advertise 1920x1080 --no-curtain`)。相手の画面を暗くしないかを
    クライアント側で選べるのかもしれない (README に説明は無く、未確認)
  - キー・マウスの送り方と、複数の画面・複数のセッションについては README に書かれていない
- **同じ仕組みの既存の実装** (作る前に見比べる):
  - **rootshell-vnc** ([上流 kitknox/rootshell-vnc](https://github.com/kitknox/rootshell-vnc)。調べたのは fork の
    [dave-atx/rootshell-vnc](https://github.com/dave-atx/rootshell-vnc) の README): **MIT License**。
    Swift のパッケージ (macOS 15 以降・iOS 18 以降) で、単体のビューアは `Examples/` にある。ただしビューアは
    「iOS and iPadOS 26.2 deployment target and also supports Mac Catalyst」で、macOS のアプリではない (796)。
    「High Performance mode with low-latency HEVC video over UDP」。custom transport provider を入れると高性能モードは使えない
  - [remotex](https://github.com/aleonnet/remotex) (MIT。ブラウザ向けのゲートウェイ): README には Apple の高性能モードへの言及が無い
    (Web 検索の要約にはそう出たが、裏が取れない)
  - [libvncserver issue 696](https://github.com/LibVNC/libvncserver/issues/696): 高性能モードを入れるかの議論
- screensharingd に認証を素通りできる脆弱性 (CVE-2026-65400) が報告されている
  ([Resecurity](https://www.resecurity.com/jp/blog/article/cve-2026-65400-macos-screen-sharing-authentication-bypass-under-active-exploitation))。
  直った版は Tahoe 26.6.1 / Sequoia 15.7.9 / Sonoma 14.8.9 (同記事)。**穴は相手の Mac の待ち受け側にある**ので、繋ぐ先を絞っても守れない。
  相手の Mac をその版以上に上げ、待ち受ける面を絞る (Wi-Fi 側で 5900 を開けない)。issue 747 の一覧は Wi-Fi 側の相手も出す

## 着手前に決めること・確かめること

1. **見る側の「1 本まで」を自作のクライアントで外せるか** (自作する意味がここで決まる): Apple の文書では、見る側の Mac も 1 本までになる。
   それが Screen Sharing.app の中の制限 (自作なら外せる) か、相手の Mac が「見る側に既に別の 1 本がある」ことを知って断る形 (外せない) かは
   文書から分からない。確かめ方: 手元の Mac から、iShareScreen か rootshell-vnc のビューアを 2 本同時に起こし、別々の 2 台へ高性能で繋ぐ
2. **修飾キーの問題が自作で直るか**: 標準アプリには、システムのキーを相手へ送らず手元に残す隠し設定がある
   (`defaults write com.apple.ScreenSharing DoNotSendSystemKeys -bool YES`。10.6 のころの記事
   [computerwoche](https://www.computerwoche.de/a/don-t-send-special-keys-in-10-6-s-screen-sharing,1926509) の「the "special" keys will be
   executed locally」。今の macOS と高性能モードで効くかは未確認)。まず `defaults read com.apple.ScreenSharing` で今の値を読み、
   標準アプリの設定で直らないかを確かめる。直らないなら、何がキーを取っているか (標準アプリか、手元の macOS のショートカットか) を確かめる。
   自作のアプリでも、手元の macOS がアプリより先に処理するキー (Mission Control の ctrl+←→ など) は CGEventTap などで横取りしないと
   相手へ送れない。アクセシビリティの許可が要る
3. **常時接続と相手の画面の暗転**: ログイン中のユーザーで入ると、相手の Mac は誰も使えなくなる (Apple の文書)。
   → **ユーザー決定 (2026-10-16): 繋いでいる間、相手の Mac が暗くなってよい**。暗くしない形を探す必要は無くなった (798)
4. **ライセンス**: 取れる道は 3 つで、ユーザーの判断が要る (App Store で出すなら (a) は相性が悪い。795):
   - (a) iShareScreen (AGPL-3.0) を移植する。コードを写したり別の言語へ訳したりすると派生物になり、AGPL がかかる。
     義務 (ソースの公開) が生じるのは配布するとき・ネットワーク越しに他人へ使わせるときで、自分の Mac だけで使うなら実質の負担は小さい
   - (b) 仕様だけを文書に起こし (ポート・パケットの形・鍵の交換の手順)、その文書から書き直す。ただし AGPL のコードを読んだ同じ主体
     (同じ Claude のセッション) が仕様を書いて実装も書くと、クリーンルームとしては弱い
   - (c) MIT の rootshell-vnc を使う・参考にする。ライセンスの縛りは著作権表示を残すことだけ
5. **作る前に既存の実装で足りないかを見る**: rootshell-vnc は Swift のパッケージで、高性能モードを話し、単体のビューアもある。
   上の 1〜3 に答えられて、一覧・常時接続を足すだけで済むなら、それを使うのが一番安い
6. **置き場所と言語** → **ユーザー決定 (2026-10-16)**: ソースは別の private の repo。言語は Swift、手元のアプリの最低の OS は macOS 26 (技術スタックは 802)
7. **一覧の出どころ**: issue 747 (mac-lan-manager) の台帳と Bonjour の一覧を使えるか。
   747 の画面共有は今、標準アプリを開いている
8. **常時接続の重さ**: 画面が変わらない間の帯域・CPU・電池は未実測。繋いでおく台数 × 推奨 75 Mbps (4K) が網に収まるか。
   アイドルの間は映像を止めて繋がりだけを保つ、という形を取れるか
9. **パスワード**: Keychain に置く (iShareScreen は保存しない)

10. **速さの余地 (アセンブラで速くできるか)**: ユーザーの問い (2026-10-16)。**見積もり (未実測)**: 余地はほぼ無い。
    遅れの大半は、相手の Mac での撮影と符号化 (Apple のサーバの中。手を出せない)・網・手元の復号と描画にあり、手元の重い処理は
    どれも専用の回路に載る: HEVC の復号は VideoToolbox (ハードウェアの復号器)、色の変換と描画は Metal (GPU)、SRTP の AES は
    CryptoKit / CommonCrypto (CPU の AES 命令を既に使う)。手で書いたアセンブラが勝てるのは CPU で回す処理だけで、それはパケットの
    組み立て・入力の送出のような小さい処理しか残らない。効くのは、復号した画面をコピーせずに描く (IOSurface をそのまま Metal へ渡す)・
    受信から描画までの待ちを減らす、の方。**trigger**: 動くものができたら Instruments で、受信から描画までの各段の時間を測ってから決める
    - 補足 (2026-10-16、再度の問い): 2 台目以降を標準モード (VNC) で繋ぐ形 (800) を取ると、標準モードの画面の復号 (zlib の展開・ZRLE などの
      画素の変換) は CPU で回る。CPU に残る処理としてはここが一番重い。ただし流れている量は実測で 0.4〜0.7 Mbps (800) と小さく、zlib は
      Apple の libz が、画素の変換は Accelerate (vImage) か Swift の SIMD が既に NEON の命令を使えるので、手書きのアセンブラの出番はまだ無い見込み

## 対応方針

未定。上の 1〜3 の答えで、自作するか・どこまで作るかが決まる。最初の一歩は 2 の `defaults read` (安い) と 1 の実測。

## 関連

- issue 747 — 手元の Mac 同士を内部の網で繋ぐ道具 (一覧・台帳・画面共有の起動)

## 進捗

- [ ] 1: 自作のクライアント 2 本で、別々の 2 台へ高性能で同時に繋げるかを実測する
- [ ] 2: 修飾キーの問題の原因を確かめる (まず `DoNotSendSystemKeys`)
- [ ] 3: 相手の画面の暗転と常時接続をどう両立させるかを決める
- [ ] 4: ライセンスの道 (a / b / c) をユーザーが決める
- [ ] 5: 既存の実装 (rootshell-vnc) で足りないかを見る

## 反証レビュー (2026-10-16、opus のサブエージェント 1 体。出典を開いて照合)

採った指摘 (裏は Apple のページと rootshell-vnc の README を開いて取った):
- 「1 本まで」は「各 Mac」なので見る側にもかかる。標準アプリで 2 台に同時に繋げないかを試す実測は要らない。決めることの 1 を「自作で外せるか」に直した
- 相手の画面の暗転は Apple の公式文書にあり、ログイン中のユーザーで入ったときという条件が付く。注記から決めることの 3 へ上げた
- rootshell-vnc は MIT で、調べたのは fork。ライセンスの道に (c) を足した
- remotex の README に高性能モードの記述が無い。CVE は待ち受け側の穴で、繋ぐ先を絞っても守れない。修飾キーには `DoNotSendSystemKeys` がある。AGPL の重さは配布するかで変わり、(b) はクリーンルームとして弱い

反証できなかった主張: Apple の要件 (Apple silicon・Sonoma 14・75 Mbps・有線・4K)、iShareScreen の README の記述、issue 747 への言及
