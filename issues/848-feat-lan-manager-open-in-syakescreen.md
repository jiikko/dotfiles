# 848 (feat): lan-manager で、画面共有を SyakeScreen の URL で開く (キー a)

起票日: 2026-10-20

## 概要

my-products の screen-share-app (SyakeScreen) の issue 812 で、URL scheme `syakescreen://connect?host=<宛先>&user=<ユーザー名>[&port=<番号>]` を入れた (2026-10-20)。
lan-manager の `o` (外のアプリで開く) は今 `open vnc://<宛先>` で Apple の画面共有を開く (`src/mac-lan-manager/lanmgr/model.go` の `case "o"`)。
これを SyakeScreen の URL にすると、有線の宛先で高性能の接続を 1 手で開ける。

## アプリ側で決まっていること (812 の実装。2026-10-20)

- `user` は必須 (アプリの一覧の鍵が `user@host[:port]` なので、省くと同じ Mac の 2 本目になる)。無い・不正なら開かずに理由を出す
- パスワードは渡さない (URL に載せても捨てる)
- 一覧に同じ相手が居れば選ぶ。宛先の書き方が違っても、ユーザー名とポートが同じで名前を引いたアドレスが重なれば同じ相手 (ユーザー決定 2026-10-20)。
  一覧に居なければ宛先を埋めたフォームを開くだけで、人が押すまで繋がない
- 宛先は ASCII の名前か 4 つの 10 進の組の IPv4 (数字だけ・0x の組・空の組は断る)。IPv6 は渡せない

## やること

- `o` の宛先を、SyakeScreen が入っていれば `syakescreen://connect?host=<m.target(r)>&user=<userFor(r)>`、無ければ従来の `vnc://` にする (または別のキーにする。どちらにするかは着手のときにユーザーに聞く)
  - ユーザー名は `userFor(r)` (台帳に無ければ推測の名前)。推測の名前で開いたときは、アプリ側でフォームか「パスワードが未登録」になる
  - 入っているかの判定: `open -Ra SyakeScreen` か LaunchServices に scheme を聞く (`/usr/bin/open` の終了コードだけで判定しない。未登録の scheme でも開くダイアログが出る)
- 確かめる: 有線の名前 (`<ホスト名>.wired.local`) と IP の両方で、SyakeScreen の一覧の同じ Mac が選ばれる (812 の受け入れ条件の lan-manager 側)。
  一覧の相手が Wi-Fi の IP で覚えてあり、URL が有線のアドレスだと、アドレスが重ならずフォームに回る (812 の照合の限界)

## 関連

- my-products: apps/screen-share-app の issue 812・docs/manual.md の「URL で開く」
- issue 747 (lan-manager の要件)

## 進捗

### 2026-10-20: a で SyakeScreen を開くキーを入れた

ユーザー決定 (2026-10-20): **新しいキー `a`** (`o` は Apple の画面共有 `vnc://` のまま)。上の「やること」の「`o` を差し替える」案は採らなかった。

入れたもの (commit「mac-lan-manager: a で SyakeScreen (syakescreen://connect) を開く」と続き 1 つ):
- 一覧の行と詳細のパネルで `a` → `open syakescreen://connect?host=<m.target(r)>&user=<userFor(r)>` (`lanmgr/model.go` の `case "a"` と `syakeScreenURL`。値は `url.Values` で符号にする)
- 断る条件は `o` と同じ (画面共有を広告していない・繋ぐ IP が分からない・この Mac の行・ネットワークを読む前) に、ユーザー名が分からない (`noUserNote`) を足した。推測のユーザー名なら「SyakeScreen で開く  (ユーザー名は推測)」と知らせる
- 案内: 最下行の候補 (優先度 11。幅が足りなければ落ちる)・詳細のカードの画面共有の行 (`o 開く  a SyakeScreen`)・`?` の板。glogx の `L` の板でも `Binds` に入れたので届く
- README と `docs/glogx-ui-guide.md` の動作層の表に `a` を足した

確かめたこと:
- `make -C src/mac-lan-manager test` / `lint`、`make test-lint` が通る (テストのコメントに書いた issue 番号を test-comment-provenance が落としたので外した)
- 変異 (どれも狙ったテストが赤): ユーザー名の確認を外す / 詳細のパネルのキーから外す / query を符号にしない / この Mac の行で断らない / 案内を常に出す / 案内を出さない
- SyakeScreen が入っていない Mac: 登録の無い scheme で `open` を打つと、ダイアログは出ず rc=1 と stderr `No application knows how to open URL … (-10814)` (2026-10-20、macOS 27.0.1)。`ExitIsFailure: true` なので知らせに出る。上の「やること」の `open -Ra` で入っているかを先に見る判定は要らなかった
- レビュー (読み取りのみ、sonnet): 受け側が断る形 (IPv6・末尾のドット・使えないユーザー名) を渡す経路は壊せなかった (宛先は Bonjour の v4・台帳の ValidLAN・ifconfig の inet・`.wired.local` の名前、ユーザー名は ValidUser を通る)。P3 4 件のうち、肯定側の案内のテストを足した。
  残り (記録だけ): `!vnc`・awayReason・推測のユーザー名の知らせ・カードの user 空のテストは無い / 詳細のパネルでは自動リピートのガードが外れ、押しっぱなしで open を繰り返す (`o` も同じ。この変更より前から)

## 残り

- 実機で `a` を押す確かめ (人)。`bin/mac-lan-manager` で管理対象の Mac を選び、一覧の行と詳細のパネル (`enter`) の両方で `a` を押す。期待: SyakeScreen が前に出て、その Mac を選ぶ (一覧に居なければ宛先を埋めた「接続先を追加」が開く)。
  2026-10-20 時点で SyakeScreen の一覧の m2macmini は古い IP `192.168.100.12` で覚えてあり、今は有線のアダプタを替えて `192.168.50.7` なので、アドレスが重ならずフォームに回る (screen-share-app 812 の照合の限界)。一覧の相手を足し直してから確かめる
- 有線の名前と IP の両方で同じ Mac が選ばれるか (上の確かめのときに、宛先が名前の相手と IP の相手で見る)
