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
  - Apple の DTS の回答 ([forums 820594](https://developer.apple.com/forums/thread/820594)・[789896](https://developer.apple.com/forums/thread/789896)・
    [707680](https://developer.apple.com/forums/thread/707680)): 権限は 3 つある
    - Accessibility (アクセシビリティ): 「not compatible with the App Sandbox」
    - ListenEvent (入力監視。CGEventTap でキーを**読む**): sandbox で使える
    - PostEvent (CGEvent.post でキーを**起こす**): sandbox で使える。設定の画面ではアクセシビリティの欄に出るが、別の権限
      (「it doesn't give you complete accessibility access. It's just limited to posting events.」)
  - リモートのクライアントがキーを相手へ送るのはネットワーク越しなので、PostEvent は要らない。問題は、手元の macOS が先に処理する
    ctrl+←→ などを**止めて**手元で効かせないことができるか:
    - 読むだけのタップ (`listenOnly`) はイベントを止められない。DTS の例もこの形 (707680:「We don't replace the event」)
    - 止められるタップ (`defaultTap`) が sandbox の中で作れるかは、DTS も答えていない (未確認)。アクセシビリティの許可が要る、
      sandbox の中では作れなかった、という利用者の報告はある ([forums 122492](https://developer.apple.com/forums/thread/122492)。Apple の人ではない)。
      **否の側に寄る**ので、App Store で出すなら、修飾キーの問題は直せない前提で考える
  - PostEvent を使ったクリップボードのアプリが「アクセシビリティの機能をアクセシビリティ以外の目的に使っている」として 2.4.5 で 2 回
    却下された例がある (820594。却下の文言は開発者の言い換えで、DTS は「I work for DTS, not App Review」)。技術的に使えることと、
    審査が通ることは別
- 🚨 **AGPL の移植は App Store と相性が悪い** (794 の 4 の (a)): FSF は 2010 年に、App Store の利用規約が GPLv2 とぶつかるとして
  GNU Go を取り下げさせた ([FSF](https://www.fsf.org/news/2010-05-app-store-compliance)。VLC も同じ理由で外され、2013 年に MPL 2.0 との
  二重ライセンスで戻った。[Computerworld](https://www.computerworld.com/article/1347653/gplv2-blocks-vlc-from-apple-s-app-store.html))。
  AGPLv3 について FSF が明示した声明は見つからなかった。iShareScreen の権利者が異議を出すかどうかにもよる。
  App Store で出すなら、(b) 書き直すか (c) MIT の rootshell-vnc の道が安全
- **同じプロトコルで App Store に出ている先例 (らしいもの)**: rootshell (bundle ID `com.kk2.rootshell`。rootshell-vnc の作者の端末アプリ。iPhone・iPad・
  Vision Pro・Mac)。第三者の掲載 ([APKPure](https://iphone.apkpure.com/rootshell-local-terminal-ssh/com.kk2.rootshell/amp)) で 1.0.11 に「High Performance mode
  streams up to 4K at 60 FPS, fully encrypted」の画面共有が載り、App Store の評価 (4.64・11 件) を集めたサイトもある
  ([marlvel.ai](https://marlvel.ai/apps/com-kk2-rootshell))。**App Store の公式のページ自体は見ていない**ので、高性能モードの入った版が審査を通って
  並んでいるかは未確認 (確かめるには App Store で rootshell を開き、版の履歴を見る)。Mac 版が Catalyst か、sandbox の中で動いているかも未確認
- Apple はプロトコルを公開していないので、macOS の更新で予告なく変わりうる。審査を通っても、後から壊れる
- **自分の Mac だけで使うなら App Store は要らない**: 自分でビルドして署名すれば、sandbox も審査も外せる (アクセシビリティの権限も使える)。
  App Store で出すかどうかで、取れる設計が変わる。ただし手元のビルドで ad-hoc 署名すると、ビルドし直すたびに権限の許可を付け直す手間が
  出うる (未確認)
- 新しく出すアプリは sandbox が必須。sandbox を採っていない App Store のアプリは、必須になる前から並んでいる古いものだけ (DTS、707680)

## 他社のアプリは何を話しているか (初期の答え: 大きく 2 つの系統。全部が VNC ではない)

1. **Mac に最初から入っている画面共有のサーバ (screensharingd) と話す系統** — こちらは VNC (RFB) 
   - Screens 5 (名前も「VNC Remote Desktop」)、Jump Desktop の VNC の接続など。Edovia の案内は、相手の Mac で標準の画面共有を有効にする手順
     ([Edovia](https://help.edovia.com/en/screens-5/getting-started/prepare-mac-for-screen-sharing))
   - RFB 自体は公開された仕様。Apple 独自の部分は、IANA の登録表で security type 30〜36 が Apple に割り当てられている
     ([IANA](https://www.iana.org/assignments/rfb/rfb.xhtml))。その中身 (Apple Remote Desktop の認証。DH 鍵交換・MD5・AES-128) は
     libvncclient の `rfbclient.c` (`HandleARDAuth`) が実装している。2020 年のフォーラムでも、公開の資料は 2011 年の解説だけだという
     ([forums 654421](https://developer.apple.com/forums/thread/654421))。Apple が仕様を公開していないこと自体は確かめきれない
   - **高性能モード (HEVC・UDP) を話すものはまだ少ない**: rootshell-vnc / iShareScreen / remotex など、どれも解析による実装 (794)。
     Screens と Jump Desktop が高性能モードを話すという記述は見つからなかった
2. **相手の Mac にも自社のアプリ (host agent) を入れてもらい、自社のプロトコルで話す系統** — Apple の仕組みを解析する必要が無い
   - Jump Desktop の Fluid: 相手の Mac に Jump Desktop Connect を入れる (App Store の説明の「asking them to install the free Jump Desktop
     Connect app」はリモートサポートの文脈。Fluid の画面共有に要るという逐語の文はまだ確かめていない)。同じページの VNC の節は
     「Tested with macOS 10.5+」で、標準のサーバとも話す
   - Parsec / AnyDesk / RustDesk も相手の Mac にアプリを入れ、画面収録とアクセシビリティの許可を求める
     ([Parsec](https://support.parsec.app/hc/en-us/articles/4423549357837)・[AnyDesk](https://support.anydesk.com/knowledge/security-permissions-on-macos)・
     [RustDesk](https://rustdesk.com/docs/pt/client/mac/))。自社のプロトコルで話すことの一次資料はまだ取っていない。TeamViewer は未調査
   - 794 の要件に当てると、この系統なら「1 本まで」も「画面の暗転」も Apple の制約ではなくなる。代わりに相手の Mac に自作の
     サーバを常駐させることになり、画面収録とアクセシビリティの権限が要る

## 残っている問い

- [x] App Store で出したいのか、自分の Mac で使えればよいのか → **ユーザー決定 (2026-10-16): 自分の Mac で使えることが第一。出せるなら
  App Store にも出したい**。設計への落とし方:
  - 中身 (接続・復号・描画・一覧・常時接続) は sandbox の中で動く形で書く (公開 API だけ。Network.framework・VideoToolbox・Metal)
  - 修飾キーの横取り (止められるタップ) は sandbox で使えない見込みなので、**手元でビルドする版だけの機能**として切り離す
    (ビルドの設定で外せる 1 か所に閉じる)。App Store 版はキーを止めずに送るだけにする
  - コードは AGPL を持ち込まない ((b) 書き直すか (c) MIT の rootshell-vnc。794 の 4)。(a) を選ぶと App Store の道が閉じる
- [ ] 794 の方針として、系統 1 (Apple のサーバと話す) と系統 2 (自作のサーバを相手に置く) のどちらを取るか。
  系統 2 は 794 の 1 と 3 の制約を外せるが、作る量が倍になる
- [ ] sandbox の中で止められるタップ (`defaultTap`) が作れるかを試す (App Store で出すときだけ要る。否の側に寄る)

## 反証レビュー (2026-10-16、opus のサブエージェント 1 体。出典を開いて照合)

採った指摘 (P1 の 2 件は DTS の原文 789896・820594 を開いて確かめた):
- PostEvent はアクセシビリティとは別の権限で、sandbox で使える。「送る側は sandbox で使えない」と読める書き方を直した
- 読むだけのタップはキーを止められない。止められるタップが sandbox で作れるかは未確認だが否の側に寄る、と書き直した
- AGPL と App Store は FSF の GPLv2 の声明 (2010) で裏を取り、対象が GPLv2 であることを限定した
- Apple の認証方式は IANA の登録と libvncclient の実装で裏を取った。Jump Desktop の引用は逐語で確かめられなかったので書き直した

反証できなかった主張: 2.5.1 / 2.4.5 (i) / 4.2.7 の読み、Screens 5 と Jump Desktop が Mac App Store にあること、Screens が標準の画面共有を使うこと、
両者が高性能モードを話すという一次情報が無いこと
