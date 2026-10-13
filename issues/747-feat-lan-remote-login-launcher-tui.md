# 747 (feat): 手元の Mac 同士のリモートログインを簡単に設定し、一覧から ssh / 画面共有で繋ぐ (TUI)

起票日: 2026-10-13

## 概要

ユーザー依頼 (2026-10-13):
- 「インターネットに繋がっていない方のネットワーク内のホストへの接続を簡単にしたい。host に名前をつけたい」
- (言い直し)「欲しいのは、**自分の Mac へのリモートログインへの簡単セットアップ**、**一覧で ssh 接続先・画面共有先を選択できる機能**」

hosts を管理する道具ではなく、**手元の Mac 同士を ssh と画面共有で繋ぐための launcher** を作る。実装はまだしない (要件と設計の調査まで)。

## 前提 (2026-10-13 時点の構成)

- この Mac には 2 つのネットワークがある:
  - **Wi-Fi** (en0): インターネットに繋がる (ルーターあり・DHCP)
  - **有線**: Mac mini と周辺の Mac をハブで繋いだ閉じた LAN。**ハブの先にルーターは無い**。IP が付いていたのは `Thunderbolt Ethernet` (en13)。
    有線になりうるサービスは他にも `USB 10/100/1G/2.5G LAN` (en10)・`Thunderbolt Bridge`・`iPhone USB` があり、**どれが有線 LAN かは決め打ちできない**
    (`networksetup -listnetworkserviceorder` でサービス名とデバイス名を対応させる)
- 有線の IP は DHCP が無いので macOS が自分で付けた `169.254.x.x`。**起動のたびに変わりうる**
- 有線のルーター欄は空 (`Router: (null)`)、インターネット宛ての経路は Wi-Fi (`default 192.168.50.1 en0`)。**有線にルーターを入れるとインターネットが切れる**
- 同じ Mac が **Wi-Fi 側にも有線側にも見える** (下の調査)

## やりたいこと

1. **自分の Mac をリモートログインできる状態にする (セットアップ)**
   - リモートログイン (ssh) と画面共有を**オンにする / 状態を見る**
   - ssh の鍵で入れるようにする (相手の公開鍵を自分の `~/.ssh/authorized_keys` へ・自分の公開鍵を相手へ)
2. **一覧から選んで繋ぐ (launcher)**
   - 繋げる Mac を一覧にする (ssh できる相手・画面共有できる相手)。**有線で見えている相手を優先**し、Wi-Fi でだけ見えている相手と見分ける
   - 選んで `Enter` で ssh (端末を明け渡す) / 画面共有 (Screen Sharing.app を開く)
   - 相手に**自分で付けた名前**を出せる (元の依頼「host に名前をつけたい」)

## 設計のための調査 (2026-10-13・読むだけ・設定は変えていない)

この Mac (macOS 27.0.1) の Wi-Fi = en0 (index 14)・有線 = en13 (index 27)。相手の例: Mac mini (`m2macmini.local`。Wi-Fi 側 192.168.50.7・有線側 169.254.217.100)。

### 相手を見つける (launcher の一覧)

- **ssh と画面共有の相手は Bonjour で見つかる**: `dns-sd -B _ssh._tcp local` (ssh = リモートログイン) と `dns-sd -B _rfb._tcp local` (画面共有) が
  `if` 列つきで相手を返す。`_rfb._tcp` では有線 (if 27) に `kojiのMac mini`・`kojiのM3MacBook Pro` (この Mac 自身)、Wi-Fi (if 14) に Mac Studio・MacBook Air 等が出た
- **相手の有線の IP も引ける**: `dns-sd -L "kojiのMac mini" _ssh._tcp local` → `m2macmini.local.:22 (interface 27)`、
  `dns-sd -G v4 m2macmini.local` → if 14 = 192.168.50.7・if 27 = 169.254.217.100
- **一覧には自分自身も出る** (`_rfb._tcp` に `kojiのM3MacBook Pro` が if 1 / 14 / 15 / 27 で。`_ssh._tcp` にはリモートログインがオフなので出ない)。
  別に `KOJI’s MacBook Pro` (if 14) もあり、この Mac の旧名か別の機械か名前からは分からない → **自分の除外は名前の一致では足りない** (自分の IP・ホスト名で照らす)
- **インスタンス名は安定しない**: `kojiのMac mini` と `kojiのMac meeini (2)` が別々に出る (名前の衝突で ` (2)` が付く)。別名のキーには使えない
- **`.local` 名で繋ぐと有線は保証されない**: `dscacheutil -q host -a name m2macmini.local` は 192.168.50.7 と 169.254.217.100 の**両方**を返す
  → launcher は名前でなく**有線側の IP** (か ssh の `BindInterface`) で繋ぐ

### 有線で届くか

- 有線で見えた相手 (169.254.217.100) へは、スコープ無しの `ping` (0.4 ms)・`nc -z` の 22 番 (ssh)・5900 番 (画面共有) が**有線で届いた**。
  `route -n get 169.254.217.100` は `interface: en13`・`flags: <…,LLINFO,…,WASCLONED>` (相手ごとの host 経路がクローンされている)
- 一方、**知らない 169.254 の IP** (`route -n get 169.254.217.101`) は en0 (Wi-Fi) へ出る (`netstat -rn` のスコープ無しの `169.254` は en0)
- 🚨 相手ごとの host 経路が**いつ作られ・いつ消えるか** (mDNS で解決した時か・起動直後の初回の通信はどうなるか) は**未確認**。
  🚨 ssh の `BindInterface` では**縛れない**: man ssh_config は「指定したインターフェースのアドレスを接続の送信元アドレスにする」とだけ書いている。
  送信元を決めるだけで、出ていくインターフェースは経路表が決める (`ssh -G` が受け付けたことは縛れる証拠にならない)。
  有線を通す実質の手段は「**宛先を有線の IP にする + `route -n get <IP>` が有線を向いていることを確かめる**」。画面共有も同じ (IP を直に渡す)

### 自分の Mac のセットアップ

- **今の状態**: この Mac は画面共有がオン (`launchctl print-disabled system` で `"com.apple.screensharing" => enabled`・`nc -z 127.0.0.1 5900` 成功)、
  リモートログインはオフ (`nc -z 127.0.0.1 22` 失敗)。Mac mini は 22・5900 とも開いている
- **状態を見る**: `systemsetup -getremotelogin` は管理者権限が要る (`You need administrator access to run this tool`)。
  権限なしで見るならポートを叩く (`nc -z 127.0.0.1 22` / `5900`) か `launchctl print-disabled system`
- **オンにする**: `sudo systemsetup -setremotelogin on`。man に「Requires Full Disk Access privileges」とある (端末にフルディスクアクセスが要る)。
  画面共有は `/System/Library/CoreServices/RemoteManagement/ARDAgent.app/Contents/Resources/kickstart` が実在する (使い方・sudo と TCC の制約は未調査。設定を変えるので実行していない)。
  どちらも、だめならシステム設定の該当画面を開く (`open x-apple.systempreferences:…`) 形に倒す
- **鍵**: `~/.ssh/` に `id_rsa.pub`・`id_rsa_m1pro.pub` がある。`authorized_keys` は無い (この Mac はまだ誰も鍵で入れない)。
  相手へ公開鍵を送る手段は `ssh-copy-id` (最初の 1 回はパスワードで入る) が素直。ed25519 の鍵は無い (作るかはセットアップの範囲に入れるか未決)
- **ユーザー名は Bonjour から取れない**: `ssh <IP>` はこの Mac のユーザー名で入る。相手のユーザー名が違うと失敗する
- **`~/.ssh/config` は dotfiles の管理外** (3.2 KB・`Host` の手書き・`Include` 無し)。launcher が書くなら**専用のファイル** (`~/.ssh/config.d/lan` 等) に書き、
  `~/.ssh/config` の**先頭**に `Include` を 1 行足す (Include は最初の `Host` より前でないと全部の Host に効かない)。既存の行は触らない

## 要件 (案)

1. **セットアップの画面**: 自分の Mac のリモートログイン・画面共有のオン/オフ・`authorized_keys` の有無を見せ、オンにする手段を出す (sudo は書く瞬間だけ・だめならシステム設定を開く)
2. **一覧**: `_ssh._tcp` と `_rfb._tcp` を合わせて Mac ごとに 1 行。列は「名前 (自分で付けた別名 / Bonjour 名)・ssh 可・画面共有可・有線か Wi-Fi か・有線の IP」。自分自身は除く
3. **繋ぐ**: ssh は有線の IP へ (経路が有線を向いているかを繋ぐ前に確かめ、向いていなければ一覧に出す) 端末を明け渡す (glogx / treefiler の `ExecRequest` と同じ形)。
   画面共有は `open vnc://<有線の IP>` (🚨 IP を渡したときの挙動は GUI を開くので未確認。設計の前に人手で 1 回確かめる)。相手ごとのユーザー名を持てるようにする
4. **別名**: 相手の `.local` のホスト名 (`dns-sd -L` が返す `m2macmini.local.`) をキーに別名を覚える (インスタンス名は ` (2)` が付いて変わるのでキーにしない)。
   設定ファイルは `~/.config/glogx/` の並び。IP は毎回 Bonjour で引き直すので保存しない
6. **見つける時間の打ち切り**: `dns-sd -B` は終わらないので、一覧は数秒で一度出し、その後に見つかった相手は足していく (取りこぼしを固定の待ち時間で決めない)
5. **鍵を配る**: 選んだ相手へ自分の公開鍵を送る (`ssh-copy-id`)

## 制約・注意

- 書き込みに管理者権限が要るもの (リモートログインのオン・ネットワーク設定) は、TUI 全体を sudo で動かさず書く瞬間だけ呼ぶ
- `~/.ssh/config` と `authorized_keys` は自分の書いた範囲の外を触らない。atomic に置き換える (`src/atomicfile`)
- 書き込みのテストは本物のファイルを書くので、一時ディレクトリの外を実行前に拒否する仕掛けを同じ commit で入れる (`_claude/rules/sandbox-real-destructive-test-apis.md`)
- 見た目は本体に入れる前にサンプルレンダラで決める (`_claude/rules/decide-layout-in-sample-renderer-first.md`)
- 置き場所は `src/<名前>/` の新しい Go のパッケージ (`tuikit` + bubbletea v2)。glogx から開ける形にするかは未決

## 未決 (ユーザーに聞く)

- 一覧に Wi-Fi でだけ見えている Mac も出すか (有線の相手だけか)
- ed25519 の鍵を作るところまでセットアップに含めるか
- 有線を固定 IP にするか (169.254 のままでも launcher は毎回 Bonjour で引き直すので動く。host 経路の未確認だけが残る)
- 単体の `bin/<名前>` か、glogx の画面の 1 つか

## 反証レビュー

- 初版 (2026-10-13・read-only のサブエージェント 1 体): P1 169.254 が Wi-Fi へ出る / P1 有線のサービス名の取り違え → 実測で裏を取って直した
  (169.254 は「知らない IP は Wi-Fi・有線で見えた相手は有線の host 経路」とその後の調査で訂正)。P2 `-setmanual` でルーターを空にする書き方は未確認。
  P3 経路の手段 (`ssh -b`・`BindInterface`・scoped route) の見落とし → ssh の `BindInterface` を要件 3 に入れた
- 言い直し後の改訂 (2026-10-13・read-only のサブエージェント 1 体。調査の実測は再現した): P1 `BindInterface` で縛れる → 誤り (送信元を決めるだけ。man で確認) と訂正し、
  要件 3 を「有線の IP + 経路の確認」に変えた / P2 インスタンス名はキーにならない (` (2)`) → 要件 4 を `.local` のホスト名に / P2 自分の除外は名前では足りない・
  ユーザー名が Bonjour から取れない・ed25519 が無い・`systemsetup` はフルディスクアクセスが要る (man)・画面共有の `kickstart` が実在 → 調査と要件に書いた /
  P3 `open vnc://<IP>` は未確認・`dns-sd -B` の打ち切り → 要件 3 と 6 に書いた

## 進捗

- [x] 設計のための調査 (上節)
- [ ] 未決を決める
- [ ] 見た目のサンプル
- [ ] 実装
