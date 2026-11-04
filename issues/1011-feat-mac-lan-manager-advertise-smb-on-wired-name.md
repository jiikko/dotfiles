# 1011 (feat): mac-lan-manager の有線の名前でも SMB を広告し、SMB クライアントの「近くのホスト」に出す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-04〜）

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

- [x] 同じホスト名の 2 本の `dns-sd -P` が衝突しないことを実験で確かめた (または衝突しない形に決めた)
- [ ] ファイル共有を有効にした Mac で、`dns-sd -B _smb._tcp local.` に有線の名前の行が出る (新しいループを入れた Mac で未確認。偽の launchctl で「有効」にした実機の確認は済み → 進捗)
- [ ] ファイル共有を無効にすると、その行が消える (同上)
- [ ] obaket の「近くのホスト」に `ms.wired.local` が出る (実機の目視)
- [ ] `make test` が通る

## 関連ファイル

- `src/mac-lan-manager/setup/wiredname.go` (`wiredService` / advertLoop)
- `src/mac-lan-manager/discover/scan.go` (`Services`。`_smb._tcp` を読む側)
- obaket: `shared/Sources/ObaketPlatform/BonjourSMBHostSuggestionService.swift` / `shared/Sources/ObaketPlatformContracts/HostSuggestion.swift`
- 関連 issue: [747](done/747-feat-lan-remote-login-launcher-tui.md)

## 進捗

- 2026-10-30: 起票。反証レビューは省略した (現状の節は上の grep と `dns-sd -B` の実測で確かめた範囲。反証されていない仕様として扱う)
- 2026-11-04: 実装 (「mac-lan-manager: ファイル共有が有効な Mac は有線の名前でも _smb._tcp を広告する」)
  - 実験 (この Mac、使っていない名前 `zz1011.wired.local`): `_mac-lan-manager._tcp` と `_smb._tcp` の 2 本の `dns-sd -P` を同じホスト名・IP で
    登録 → 両方 `Name now registered and active`、`dns-sd -G v4` は同じ IP、`-B _smb._tcp` に行が出て `-L` は `zz1011.wired.local.:445`。衝突しない
  - ファイル共有の判定は `launchctl print system/com.apple.smbd` の rc (管理者は要らない)。実測: 有効の ms (27.2)・air5 (26.7) で rc 0
    (`launchctl print-disabled system` は `"com.apple.smbd" => enabled`)、切った m2macmini (15.7.5。`=> disabled`) と未設定のこの M3 (27.0.1) で rc 113。
    `_smb._tcp` の広告の有無 (ms・air5 だけ) とも一致。`pgrep smbd` は共有が有効でも走っていない (起動は要求時) ので使えない
  - ループ (`setup/wiredname.go` の `advertScript`): IP を持っていて共有が有効な間だけ `dns-sd -P "<コンピュータ名> (有線)" _smb._tcp local 445 <有線の名前> <IP>`
    を走らせ、共有を切る・IP を失う・TERM で止める。コンピュータ名が読めなければ `<host>.wired (有線)`。plist の引数の末尾に `/bin/launchctl` と
    `/usr/sbin/scutil` を足し (前の形の位置 4・7・8 を動かさない)、目印 `AdvertTag` を v4 に上げた
  - テスト: `TestAdvertLoopFollowsIP` を 2 本の dns-sd を種類で分けて見る形にした (3 回連続 green)。変異 2 本で red を確認 (`bin/mutate-verify`):
    IP を失っても SMB の判定を残す → 「IP を失ったら dns-sd を 2 本とも止める」/ 共有を見ない → 「ファイル共有を切ったら SMB の dns-sd を止める」
  - 実機 (この M3、本物の dns-sd / ifconfig / scutil、launchctl だけ偽物で「有効」): `_smb._tcp` に「kojiのM3MacBook Pro (有線)」が出て、
    偽物を「無効」にすると消え、`_mac-lan-manager._tcp` の行は残った。TERM 後に dns-sd は残らない
  - 敵対的レビュー (opus、読み取りのみ): ループの状態遷移・引数の番号・前の形 (v3) の job との整合は壊せなかった。指摘 8 件の扱い:
    - 採用 2 件 (テストの穴): ①偽の dns-sd が `$*` で書いていて、表示名が空白で 2 引数に割れても緑 → 引数を `|` で区切って 1 つずつ書き、
      表示名が 1 引数であることを見る ②SMB の dns-sd が自分で落ちたときの起こし直しと、目印の側を起こし直しても SMB の側は起こし直さないことが
      未検査 → 足した。変異 3 本 (表示名の引用を外す / `kill -0` の枝を外す / 目印の側の起こし直しで q も空にする) で red を確認
    - 再現しなかった 1 件: SMB の側を止めると同じ名前の A レコードの goodbye が出て有線の名前まで消える → ms から `dns-sd -G v4` で引くと、
      SMB の側だけを止めた後も引けた (目印の行も残った)
    - 受容してコメントに書いた 2 件: Wi-Fi 側の端末にも「(有線)」の SMB の行が出て、繋ぐと届かない (この issue の注意点どおり) /
      コンピュータ名を変えても SMB の dns-sd を起こし直すまで前の名前
    - 記録だけの 3 件 (低・未確認): `launchctl print` が一時的に失敗するとその周で SMB の行が消えて次の周で戻る / q が落ちた後の pid の再利用
      (p にも前からある型) / TERM が `& q=$!` の間に届いたときの取りこぼし (launchd の bootout はプロセスグループごと止める見込み。p と同じ型)
  - この M3 は `bin/mac-lan-manager upgrade-advert` で v4 に入れ直した (rc 0。launchctl の arguments の 4 番目が v4、9・10 番目が launchctl と scutil。共有が無効なので `_smb._tcp` に行は出ない = 期待どおり)
  - セルフレビュー: 初期設定の確認の板が SMB の広告を説明していなかったので 1 行足した。各 Mac の入れ直しと目視は 1023 に起こしたが、ユーザーが自分でやるとのことで 1023 は消した (2026-11-04)
  - 残り: 各 Mac で入れ直す (dotfiles のある Mac は `setup.sh` が `bin/mac-lan-manager upgrade-advert` を呼ぶ。相手の Mac は初期設定の名前の手順)。
    入れ直した後に、共有が有効な ms / air5 で `dns-sd -B _smb._tcp local.` に「(有線)」の行が出ること、obaket の「近くのホスト」に `ms.wired.local` が
    出ること (実機の目視) は未確認
