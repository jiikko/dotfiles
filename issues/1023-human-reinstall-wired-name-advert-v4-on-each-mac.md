# 1023 (human): 各 Mac の有線の名前の広告を v4 (SMB も広告する形) に入れ直し、obaket の「近くのホスト」を目視する

起票日: 2026-11-04
期限: 2026-11-18

## 概要

1011 で、有線の名前を広告するループが、ファイル共有の有効な間は `_smb._tcp` も「<コンピュータ名> (有線)」で広告するようになり、
形の目印 `AdvertTag` が `mac-lan-manager-wired-name-v4` に上がった。既に入っている Mac は v3 のままなので、

- SMB の行は出ない (obaket・Finder の「近くのホスト」に有線の名前が出ない)
- lan-manager の初期設定を開くと、名前の手順が済みにならない (済みの判定 `setup.NameState.Advertises` が目印も見るため)。
  一覧の `!` は名前の解決だけを見るので付かない (967 と同じ)。自動では入れ直されない

入れ直したのは kojiM3MBP だけ (2026-11-04、`bin/mac-lan-manager upgrade-advert`)。残りは air5・m2macmini・ms・m1prombp。

## 人が要る理由

相手の Mac の LaunchAgent を書き換える操作で、Claude が断りなく打つとユーザーの Mac の設定を変えることになる (967 と同じ。967 では
ユーザーの許可を得て Claude が ssh で流した)。obaket の「近くのホスト」の表示は実機のアプリの画面を見るしかない。

## 手順

1. 各 Mac で入れ直す。どちらか:
   - dotfiles のある Mac: `~/dotfiles` を pull して `GO_AUTOBUILD_SYNC=1 bin/mac-lan-manager upgrade-advert` (`setup.sh` も同じ形で呼ぶ)。画面にログインしている間に打つ。
     `GO_AUTOBUILD_SYNC=1` を付けないと古いバイナリが走り、v3 を「今の形」と読んで何もしない
   - それ以外: lan-manager の初期設定を開き、「相手の有線の名前 (…) を広告する」を実行する
2. 確かめ方: その Mac で `launchctl print gui/$(id -u)/com.jiikko.mac-lan-manager.wired-name` の arguments の 4 番目が `mac-lan-manager-wired-name-v4`、
   9・10 番目が `/bin/launchctl`・`/usr/sbin/scutil`
3. ファイル共有が有効な Mac (2026-11-04 時点で ms・air5) について、別の Mac で `dns-sd -B _smb._tcp local.` に「<コンピュータ名> (有線)」の行が出る。
   共有が無効な Mac (m2macmini・kojiM3MBP。m1prombp も 2026-11-04 の `_smb._tcp` の一覧に出ていなかったので無効と見ているが、`launchctl` では未確認) は出ない
4. obaket の SMB の接続画面の「近くのホスト」に `ms.wired.local` (表示名「kojiのMac Studio (有線)」) が出る (目視)
5. 期待と違ったら: 1011 に書き戻す (done へ送った後なら open に戻す)。3 で行が出ないなら、その Mac で `launchctl print system/com.apple.smbd` の rc を見る (0 で出ないならループの不具合)

## 進捗

- [ ] air5・m2macmini・ms・m1prombp を v4 に入れ直した
- [ ] ms・air5 の「(有線)」の行が `_smb._tcp` に出た
- [ ] obaket の「近くのホスト」に `ms.wired.local` が出た
