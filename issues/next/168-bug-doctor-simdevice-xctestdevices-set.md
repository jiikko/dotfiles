# 168 bug: `simDeviceUDIDs` が XCTestDevices セットを見ず、並列テスト中の clone デバイスを孤児にしうる

起票日: 2026-09-02
重要度: **P2**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 1) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (「敵対的レビュー 2 回目」の coresimulator-orphan P1 と同型)

## 対象

`src/doctor/disk/scan.go` の `simDeviceUDIDs`

## 何が起きるか

前回の敵対的レビューで「Xcode Previews のセット (`~/Library/Developer/Xcode/UserData/Previews/Simulator Devices`) を
見ていない」P1 を直したが、**同じ形のセットがもう 1 つある**。

- `~/Library/Developer/XCTestDevices/` が実機に存在する (現在は空。実証済み)
- `xcrun simctl --set ~/Library/Developer/XCTestDevices list devices -j` は rc 0 で JSON を返す (実証済み)
- `xcodebuild test -parallel-testing-enabled YES` の clone デバイスはこのセットに作られ、**既定セットには出ない**

したがって並列テスト実行中に `/private/var/tmp/com.apple.CoreSimulator.SimDevice.<clone UDID>` が存在すると、
現存デバイスと突合できず孤児 (`RiskSafe` rm) として候補に出る。

**未確認**: clone デバイスが実際に `/private/var/tmp` に SimDevice dir を作るかは未実測 (計測時点で 0 件)。
セットの存在と simctl が応答することは実測済みで、被害の成立だけが未確認。

## 再現の trigger

`xcodebuild test -parallel-testing-enabled YES` を回している最中に:

```
ls /private/var/tmp | grep SimDevice
xcrun simctl --set ~/Library/Developer/XCTestDevices list devices -j
```

前者に出る UDID が後者に在り、かつ既定セットに無ければ、この issue の条件が成立する。

## 対応案

- セットを列挙する形にする (既定 / Previews / XCTestDevices の 3 つ以上)。将来のセット追加で同じ穴が空くので、
  `~/Library/Developer/**/Devices/<UDID>` の存在でも突合する方が変更耐性が高い
- どのセットの取得も失敗したら fail-closed (現状の Previews と同じ規律) を維持する

## 受け入れ条件

- [ ] XCTestDevices セットのデバイスが孤児にならない (偽 runner で 3 セット分の argv を検証)
- [ ] セット取得の失敗が「診断できず」に倒れる
- [ ] 変異検証: セット列挙から XCTestDevices を外すと候補に戻ることを確認する

## 事前検証 (2026-09-03、実機)

**主張は成立する。さらにセットがもう 1 つある。**

```
$ ls ~/Library/Developer/
CoreSimulator  DVTDownloads  XCPGDevices  XCTestDevices  Xcode
$ find ~/Library/Developer -maxdepth 3 -type d -name '*Devices*'
/Users/koji/Library/Developer/XCTestDevices
/Users/koji/Library/Developer/XCPGDevices
/Users/koji/Library/Developer/CoreSimulator/Devices
```

- `~/Library/Developer/XCTestDevices` と **`~/Library/Developer/XCPGDevices`** (Playground 用) が両方実在する
  (どちらも現在は空)
- `xcrun simctl --set <各セット> list devices -j` は**両方とも rc=0 で JSON を返す** (stdout / stderr / rc を分けて実測)
- 現行の `simDeviceUDIDs` (`src/doctor/disk/guard.go:54`) が見ているのは**既定セットと Xcode Previews の 2 つだけ**

したがって issue の対応案「セットを列挙する形にする」を、`~/Library/Developer/*Devices` の**実在するディレクトリを
全部列挙する**形で実装するのが、将来のセット追加 (XCPGDevices のように増える) に対して変更耐性がある。
