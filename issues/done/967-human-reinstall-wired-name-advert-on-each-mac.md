# 967 (human): 各 Mac の有線の名前の広告を、IP を失ったら止まる形に入れ直す

起票日: 2026-10-29
期限: 2026-11-12

## 概要

964 で、有線の名前を広告する LaunchAgent を「広告する IP をどの I/F も持っていない間は広告を止めるループ」に変えた。
既に入っている Mac の LaunchAgent は前の形 (dns-sd を直接起こす) のままで、IP を失っても古い IP を広告し続ける。
lan-manager は前の形を「済み」と読まないので、初期設定の名前の手順を実行し直すと今の形に入れ替わる。

## 人が要る理由

各 Mac の LaunchAgent を書き換える操作で、相手の Mac がログインしている間に、確認の板の `y` で打つ (gui のドメインに入れるため。
setup/wiredname.go の InstallScript)。Claude がこれを代わりに打つと、ユーザーの Mac の設定を断りなく変えることになる。
自動では起きない: 一覧の `!` は名前の解決だけを見るので、前の形でも付かない (964 の敵対的レビュー)。

## 手順

1. `~/dotfiles` を pull 済みの Mac で lan-manager を開く (`GO_AUTOBUILD_SYNC=1 bin/mac-lan-manager`、または glogx の `L`)
2. 有線の名前を広告している Mac ごとに、初期設定を開く。前の形のままなら「この Mac の有線の名前 (…) を広告する」/「相手の有線の名前 (…) を広告する」が
   済みになっていないので、その手順を実行する
3. 確かめ方: その Mac で `launchctl print gui/$(id -u)/com.jiikko.mac-lan-manager.wired-name` の `arguments` の先頭が `/bin/sh`・`-c` で、
   4 番目が `mac-lan-manager-wired-name-v2` になっている。`ps -Ao pid,ppid,command | grep 'dns-sd -P'` で、dns-sd の親がその sh になっている
4. 期待と違ったら (名前の手順が済みのまま出る・arguments が前の形のまま): 964 を open に戻す

## 進捗

- [x] 有線の名前を広告している全 Mac で入れ直した (2026-10-29。ユーザーの許可を得て Claude が打った。lan-manager の UI は通さず、
  `setup.NameAdvert.InstallScript()` が組んだ sh を、この Mac ではそのまま、相手の 3 台には ssh の `sh -s` で流した)
  - 入れ直す前は 4 台とも前の形 (`launchctl print` の arguments の 1 番目が `/usr/bin/dns-sd`): kojiM3MBP (kojim3macbook-pro.wired.local → 192.168.100.10)・
    air5 (→ .11)・m2macmini (→ .15)・ms (→ .13)
  - 入れ直した後は 4 台とも state = running、arguments の 1 番目が `/bin/sh`、4 番目が `mac-lan-manager-wired-name-v2`、7・8 番目が上の名前と IP。
    `dns-sd -P` は各 Mac で 1 本だけで、親は job の sh。この Mac (kojiM3MBP) から `dscacheutil -q host -a name <名前>` で 4 つとも台帳の IP に引けた
  - 「IP を失ったら広告が止まる」は本物の I/F で起こして確かめていない (アダプタを抜く操作が要る)。偽の ifconfig ではテスト
    (`TestAdvertLoopFollowsIP`) が見ている
