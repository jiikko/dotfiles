# 1011 (feat): mac-lan-manager の有線の名前でも SMB を広告し、SMB クライアントの「近くのホスト」に出す

起票日: 2026-10-30

## 概要

obaket の SMB の接続画面にある「近くのホスト」に、`ms.wired.local` が出ない。原因は、mac-lan-manager が有線の名前を
**目印のサービスの種類 `_mac-lan-manager._tcp` でしか広告していない**ことにある。SMB クライアント (obaket・Finder) が探すのは
`_smb._tcp` なので、有線の名前は SMB の候補として現れない。ファイル共有を有効にしている Mac について、有線の名前でも `_smb._tcp` を広告する。

ユーザーの言葉 (2026-10-30): 「smb の接続画面で近くのホストっていうリストを出すのだが、そのなかに、ms.wired.local がないのはどうして？
dotfiles mac-lan-manager から広告したホストは見れないのかな？」

## 現状 (2026-10-30 に確認)

- 有線の名前の広告は `src/mac-lan-manager/setup/wiredname.go`。LaunchAgent のループが `/usr/bin/dns-sd -P` を起こし、
  名前と一緒に登録するサービスの種類は `wiredService = "_mac-lan-manager._tcp"` (コメント: 「名前だけは登録できない」ので付けている目印)
- obaket の「近くのホスト」は `BonjourSMBHostSuggestionService` (obaket の `shared/Sources/ObaketPlatform/`) が
  `NWBrowser(for: .bonjour(type: "_smb._tcp", domain: nil))` で探すだけ
- 実測 (`timeout 5 /usr/bin/dns-sd -B <type> local.`):
  - `_smb._tcp`: 「kojiのMacBookAirm5」「kojiのMac Studio」など、macOS のファイル共有が広告する名前だけ。ホスト名は `.local` の側
  - `_mac-lan-manager._tcp`: `ms.wired` / `m1prombp.wired` / `m2macmini.wired` / `kojim3macbook-pro.wired`
- obaket は候補を正規化したホスト名でまとめる (`HostSuggestion.swift` の `outputIndexByHostKey`)。`ms.local` と `ms.wired.local` は
  別の行になるので、`_smb._tcp` を有線の名前でも広告すれば 2 行目として出る見込み (未実測)
- 今すぐの回避: 接続画面のホスト欄に `ms.wired.local` を手で入れれば繋がる (名前は `_mac-lan-manager._tcp` の登録で引ける)

## 対応方針 (候補)

1. ファイル共有 (SMB) を有効にしている Mac では、有線の名前でも `_smb._tcp` を登録する。
   例: `dns-sd -P "<コンピュータ名> (有線)" _smb._tcp local. 445 <host>.wired.local <内部 IP>`
2. ファイル共有が有効かの判定を決める (自分の `_smb._tcp` の広告が在るか / `sharing -l` / smbd の状態など。どれが確かかを実測で選ぶ)。
   無効にした後も広告が残らないようにする
3. **同じホスト名を 2 本の `dns-sd -P` で登録したとき、mDNSResponder が名前の衝突として扱わないかを先に実験で確かめる**。
   衝突するなら、1 本の登録の中で両方を出す形 (`dns-sd` 以外の登録手段を含む) を検討する

採らない案: obaket が `_mac-lan-manager._tcp` も探す。製品のアプリが個人用の道具の目印に依存し、Finder など他の SMB クライアントには効かない。

## 注意点

- `dns-sd -P` は広告する I/F を選べないので、有線の名前は Wi-Fi 側にも見える (issue 747 で受容済み)。SMB の候補も Wi-Fi 側に出る
- 広告は、その IP を持つ I/F が無くなった後も残る (issue 847)。候補に出ても届くとは限らない
- 表示名を変えると、obaket の候補の名前 (serviceName) も変わる。「(有線)」のように `.local` の行と見分けが付く名前にする

## 受け入れ条件

- [ ] 同じホスト名の 2 本の `dns-sd -P` が衝突しないことを実験で確かめた (または衝突しない形に決めた)
- [ ] ファイル共有を有効にした Mac で、`dns-sd -B _smb._tcp local.` に有線の名前の行が出る
- [ ] ファイル共有を無効にすると、その行が消える
- [ ] obaket の「近くのホスト」に `ms.wired.local` が出る (実機の目視)
- [ ] `make test` が通る

## 関連ファイル

- `src/mac-lan-manager/setup/wiredname.go` (`wiredService` / advertLoop)
- `src/mac-lan-manager/discover/scan.go` (`Services`。`_smb._tcp` を読む側)
- obaket: `shared/Sources/ObaketPlatform/BonjourSMBHostSuggestionService.swift` / `shared/Sources/ObaketPlatformContracts/HostSuggestion.swift`
- 関連 issue: [747](done/747-feat-lan-remote-login-launcher-tui.md)

## 進捗

- 2026-10-30: 起票。反証レビューは省略した (現状の節は上の grep と `dns-sd -B` の実測で確かめた範囲。反証されていない仕様として扱う)
