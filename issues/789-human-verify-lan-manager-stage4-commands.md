# 789 (human): lan-manager の段 4 の前に、固定 IP・画面共有・切断後も走るかを手で 1 回確かめる

起票日: 2026-10-16
期限: 2026-10-23

## なぜ人が要るか

issue 747 の段 4 (相手の Mac の有線を固定 IP にする・鍵を配る) は、本物の `networksetup` / `sudo` を相手の Mac で動かす。setup package のテストは
偽物のコマンドで命令列を走らせているだけで、次の 3 つは偽物では確かめられない。

- 2 台目の Mac が要る
- 網の設定を実際に書き換える
- GUI (画面共有) が開くかは目で見るしかない

機械で測れないかは問うた。この Mac 1 台の中で `-setmanual` を試すと自分の網を壊すおそれがあり、ssh の切断は相手が要る。

## 2026-10-16 の決定と途中の結果

- **帯は `192.168.100.0/24` のまま** (2026-10-16 に 169.254 の中へ移しかけて戻した。ユーザー決定は A)。理由はこの Mac での実測:
  en0・en13・en18 の 3 つに 169.254 があり、まだやり取りしていない 169.254 の番号 (`route -n get 169.254.100.12`) は en0 (Wi-Fi) へ
  流れた。固定した相手へ新しい IP で入り直す確認が Wi-Fi に出て届かず、`open vnc://` も出る I/F を選べない。`192.168.100.x` 宛ては
  まだやり取りしていない番号も en13 へ出る (`route -n get 192.168.100.12` / `.250`)
- **1 の「ルーター無しの固定」はこの Mac で確かめた** (初期設定の x から): `networksetup -getinfo "Thunderbolt Ethernet"` が
  `Manual Configuration` / `IP address: 192.168.100.10` / `Subnet mask: 255.255.255.0` / `Router: (null)`、`route -n get default` が
  `gateway: 192.168.50.1` / `interface: en0` (Wi-Fi のまま)
- 残り: 相手の Mac 側 (固定した後に新しい IP で入り直す確認)、2 と 3

## 手順

相手の Mac (以下「相手」) に、この Mac から ssh で入れる状態で行う。サービス名は相手の `networksetup -listnetworkserviceorder` で見る (例: `Thunderbolt Ethernet`)。

### 1. ルーターを空にした `-setmanual` で「ルーター無しの固定 IP」になるか

相手で:

```sh
networksetup -getinfo "Thunderbolt Ethernet" > ~/before.txt     # 元に戻すための控え
sudo networksetup -setmanual "Thunderbolt Ethernet" 192.168.100.250 255.255.255.0
networksetup -getinfo "Thunderbolt Ethernet"                     # ← この出力を全部 issue に貼る (Manual Configuration の本物の出力)
route -n get default | grep interface                            # 既定の経路が Wi-Fi (en0 など) のままか
```

- **期待**: getinfo が `Manual Configuration` / `IP address: 192.168.100.250` / `Router: (null)` (またはルーターの行が無い)。既定の経路は Wi-Fi のまま
- **元に戻す**: `sudo networksetup -setdhcp "Thunderbolt Ethernet"` (before.txt が手動だったなら、その値で `-setmanual` し直す)

### 2. `open vnc://<IP>` で画面共有が開くか

この Mac で、相手の有線の IP を渡して:

```sh
open vnc://192.168.100.250
```

- **期待**: 画面共有のアプリが開き、相手のユーザー名とパスワードを聞く (相手の「画面共有」がオンのとき)

### 3. ssh が切れても、切り離した変更が最後まで走るか (sudo の use_pty を含む)

この Mac で (相手で sudo のパスワードを 1 回聞かれる):

```sh
ssh -t <相手> 'sudo -v && sudo -n -b nohup sh -c "sleep 20; date > /tmp/mlm-detach-test" >/dev/null 2>&1'
```

返ってきたら**すぐに** ssh を切る (ターミナルを閉じる、または `~.`)。30 秒待ってから:

```sh
ssh <相手> 'cat /tmp/mlm-detach-test; sudo -l | grep -i use_pty; sudo rm -f /tmp/mlm-detach-test'
```

- **期待**: 日時が 1 行出る (切断を越えて最後まで走った)。`use_pty` の行の有無も控える

## 結果の書き方

各手順の出力をこの issue に貼る。1 の getinfo の出力は、setup / netinfo のテストの fixture にする (今は作った値で「Manual Configuration」を見ている)。

## 期待と違ったら

- 1 が `Router` を持つ・既定の経路が有線に移る → 747 の段 4 の `-setmanual` の形を見直す (`setup/staticip.go` の `Script`)。747 に書く
- 2 が開かない → 747 の `o` (画面共有) の形を見直す (`lanmgr/model.go` の `case "o"`)
- 3 で日時が出ない → 切り離し (`sudo -n -b nohup`) が切断で止まる。`setup/staticip.go` の `RemoteCommand` を見直す (747 に書く)

確認が済んだら done へ移す。

## 結果 (2026-10-17, air5 / macOS 26.7 / sudo 1.9.17p2)

### 3: 切り離しが切断で止まった → 直した (人の確認は 1 回だけ残る)

- 相手の固定 IP の手順を air5 で y → 実行は rc=0 で戻ったが、有線 (en5) は DHCP の 169.254.113.65 のまま。確認の ssh は
  192.168.100.11 に届かず exit 255 (操作のログ `~/Library/Logs/mac-lan-manager.log` 07:24:57 / 07:25:10)
- air5 の `/var/db/mac-lan-manager` は空で mtime が前の回 (00:34) のまま = 背景の本体は lock を一度も取っていない (走り出していない)
- sudo 無しの同じ形で再現 (air5 へ `ssh -tt` で `nohup sh -c "sleep 3; echo ok > f" </dev/null >/dev/null 2>&1 &` を打ってすぐ抜ける):
  すぐ抜けると f ができず、抜ける前に `sleep 1` を挟むとできた。ssh の端末が閉じた SIGHUP が、nohup が SIGHUP を捨てる前の子に
  届く競争。`sudo -b` はすぐ戻るので、毎回この負け側に入っていた
- 修正: 背景の本体が lock を取った直後に印 (`started-<token>`) を書き、前面はそれを待ってから ssh を終える。15 秒で現れなければ
  「背景の切り替えが始まらなかった (何も変えていない)」を出して失敗にする (commit「相手の固定 IP は背景の切り替えが走り出したのを
  確かめてから ssh を終える」)
- 残る人の確認: 直した版で air5 の相手の固定 IP → 180 秒以内に確認、が通るか (印を待った後に ssh が閉じても背景が生き残るか。
  sudo の use_pty の下の挙動は sudo 無しの再現では見ていない)
- 本物の sudo で確かめた (2026-10-17 07:57, air5): 画面と同じ `sudo -n -b nohup sh -c ...` で 20 秒のダミーを 2 通り起こした。
  A (背景に回してすぐ ssh を終える = 修正前) は `/var/tmp/mlm-probe-A.log` が作られず、1 行目を書く前に死んだ。
  B (走り出すのを待ってから終える = 修正後) は ssh が閉じた後も tick-20 / done まで走り、シグナルも届かなかった
  (pgid は自分、tty は sudo の pty)。air5 に root の `/var/tmp/mlm-probe-B.log` が残っている (害は無い。消すなら `sudo rm`)
- opus の敵対レビュー: P1 (sudo の後始末で印の後にも死ぬのでは) は上の B で否定。P3 の 2 件 (印を lock より前 / 変更の後に
  書く退行をテストが見ない) はテストで固定した (変異 M1 / M2 で red)。受容 3 件 (印の残骸・多重化の鍵が HostKeyAlias を含まない・
  相手の IP を変えた直後の観測が死んだ接続で待つ) は理由をコードのコメントに残した
- 残る人の確認は上と同じ: 直した版で air5 の相手の固定 IP → 180 秒以内の確認が通るか
