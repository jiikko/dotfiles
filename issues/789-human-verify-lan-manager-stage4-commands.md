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
