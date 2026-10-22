# 861 (bug): ルーターの無い Wi-Fi で見えた相手を有線 (内部 IP) と数える

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-22〜）

起票日: 2026-10-22

## 概要

lan-manager の全体監査 (865) の P2 (encapsulation E1)。「内部の網に使う有線の I/F か」は `!s.NotWiredLAN() && s.WiredIF(dev)` で決まる
(Hardware Port で無線を除き、ルーターの無い I/F を内部とする)。この組み立てを呼び出し側が手で書いていて、5 か所のうち 2 か所で無線の除外が漏れている。

既出: done の 747 の「残っているもの」に「記録だけ (直していない・未確認)」として、done の 828 に P2 の既知として載っている
(trigger は「ルーターの無い Wi-Fi で lan-manager を開いたとき」で、まだ来ていない)。issue として起こすのは、反証レビューで
**宛先の選び方と断りの理由にまで効く** (下の「何が壊れるか」の 2 と 3) ことが分かり、記録だけの扱いより重いと判断したため。

## 詳細

- `netinfo.Snapshot.WiredIF` / `class` は Hardware Port を見ない。ルーターが無ければ Wi-Fi も `ifWired` になり、`Kind` は KindWired / KindLinkLocal を返す
- 無線を除いている (3 か所): `netinfo.Offline`、`lanmgr/settings.go:lanIFs`、`lanmgr/setup.go:selfWired`
- 除いていない (2 か所):
  - `lanmgr/model.go:rows` の `wiredService := k == netinfo.KindWired || (k == netinfo.KindLinkLocal && m.self.WiredIF(dev))`
  - `selfLan` の I/F で決める側。ただし `pf.LanIF` を設定する経路は `settings.go:lanIFs` の候補 (無線を除く) だけなので、届くのは prefs を手で書いたときだけ。実害は `rows` 側
- 何が壊れるか:
  1. この Mac の Wi-Fi に DHCP が取れず 169.254 になったとき (またはルーターの無い Wi-Fi)、同じ Wi-Fi で見えた相手の IP が「内部 IP」になり
     `r.wired=true` になる。設定の「有線で見えた相手だけ」を通る。この Mac 側の内部の網 (Offline / selfWired) は Wi-Fi を除くので、画面と初期設定で答えが割れる。見えるのは `(無線)` の札だけ
  2. この Mac がルーターのある有線とルーターの無い Wi-Fi の両方に居ると、相手は有線で `KindInternet` (mainIP)、Wi-Fi で `KindWired` (lanIP) に見える。
     `connectIP` は lanIP を優先するので、s と speedtest は有線ではなく Wi-Fi の IP へ繋ぐ
  3. この Mac の内部 IP が帯の中に固定されていて、相手が Wi-Fi の 169.254 にしか見えないと、`lanAway` と `mainIP == ""` が重なり、s は「有線で網が分かれている」という見当違いの理由で断る
- 発火条件: Wi-Fi に DHCP が取れない状態か、ルーターの無い Wi-Fi に繋いだ状態 (実機では未再現。コードの読みで確認)
- `TestLinkIsIndependentOfRouter` は、ルーターの無い Wi-Fi の Kind が Internet でないことしか見ていない

## 対応方針

- 持ち主の側に `netinfo.Snapshot.LanIF(dev) bool` (Port の除外 + ifWired) を作り、5 か所をこれに寄せる。`rows` の wiredService もこれを通す
- ルーターの無い Wi-Fi の上で見えた相手が `wired` にならないテストを足す

## 関連ファイル

- `src/mac-lan-manager/netinfo/netinfo.go` (`WiredIF` / `class` / `NotWiredLAN` / `Offline`)
- `src/mac-lan-manager/lanmgr/model.go` (`rows`)、`lanmgr/settings.go` (`lanIFs`)、`lanmgr/setup.go` (`selfWired` / `selfLan`)

## 進捗

- [x] `LanIF` に寄せる修正とテスト
  - `netinfo.Snapshot.LanIF(dev)` = ルーターの無い I/F (`wiredIF`) で、その I/F のサービスに無線・iPhone USB (`notWiredLAN`) が無い
  - 5 か所 (`Offline`・`lanIFs`・`selfWired`・`selfLan` の I/F で決める側・`rows` の wiredService) を `LanIF` に寄せた。`rows` は Kind を見ずに `LanIF(dev)` だけで決める
  - 迂回を止める: `WiredIF` と `NotWiredLAN` を非公開 (`wiredIF` / `notWiredLAN`) にした。netinfo の外 (dotfiles の全 module) で使っていたのは寄せた 4 か所だけ
  - テスト: `TestLanIF` (Thunderbolt Bridge・USB LAN は入る / ルーターの無い Wi-Fi・iPhone USB・ルーターのある有線・登録の無い I/F・"" は入らない)、
    `TestRowsWifiWithoutRouterIsNotWired` (Wi-Fi が 169.254 の相手は有線と数えない / ルーターのある有線とルーターの無い Wi-Fi の両方で見えた相手は、繋ぐ先が有線のメイン IP)
  - 変異 (`mutate-verify`。どちらも想定のテストが red): `rows` を Kind だけで決める旧の形に戻す → `TestRowsWifiWithoutRouterIsNotWired` /
    `LanIF` から無線の除外を外す → `TestLanIF`
  - 「何が壊れるか」の 3 (lanAway と mainIP == "" が重なる見当違いの理由) は、相手が内部 IP と数えられなくなるので起きない (専用のテストは足していない)
- [x] 敵対的レビュー (opus、読み取りのみ): 本物の有線の相手が内部 IP から外れる形・無線の相手がまだ内部 IP になる形は作れなかった
  (旧の rows の判定は `wiredIF(dev)` と同じで、新しい判定との差は Wi-Fi・AirPort・Bluetooth PAN・iPhone USB の I/F だけ)。指摘への対応:
  - P3 `LanIF` が無効のサービスの Port も見ていた → `Enabled()` だけを見る (`class` と同じ)。`TestLanIF` に「無効の iPhone USB が同じ I/F に残る」を足し、
    `Services` に戻す変異で red を確認
  - P3 保存した I/F が Wi-Fi のときの文言が「読めない」だった → 「(Port) は内部の網に使えない (読めない・ルーターがある・無線か iPhone USB)」
  - P2 内部 IP から外れたルーターの無い Wi-Fi の IP が「その他の IP」で `有線` の札のままだった → ユーザーの決定 (2026-10-22) で別の種類にした:
    `netinfo.KindWirelessNoRouter` (画面「無線 (ルーターなし)」・設定の Key `wireless-no-router`・黄の札)。Hardware Port が電波の I/F の
    ルーターの無い IP は 169.254 も含めてこれになる。iPhone USB は線が USB なので `KindWired` のまま
  - 設定: 既定の「その他の IP に含める種類」に入れた。版 1 の保存の `wired` はルーターの無い Wi-Fi も含んでいたので、`kinds_version` の無い
    古い設定で `wired` が入っていれば新しい種類も入れて読む (今まで見えていた IP を黙って隠さない)。Save は `kinds_version = 2` を書き、
    版 2 の設定は読み替えない (外した選択を戻さない)
  - テスト: `TestKindWirelessNoRouter`・`TestLoadOldKindNames` (版 1 の読み替え)・`TestLoadKindsVersion2KeepsChoice`・
    `TestRowsWifiWithoutRouterIsNotWired` (その他の IP の札)。変異: Kind の無線の判定を外す → `TestKindWirelessNoRouter` /
    版 1 の読み替えを外す → `TestLoadOldKindNames` / Save が版を書かない → `TestLoadKindsVersion2KeepsChoice` (どれも red)
