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

### 2026-09-03 に再確認 (issue 207 の残作業として。**被害の成立は今回も未確認のまま**)

| 確かめたこと | 結果 |
|---|---|
| `/private/var/tmp` の SimDevice 作業ディレクトリ | **0 件** (並列テストを走らせていないので当然) |
| 照合先 `~/Library/Developer/CoreSimulator/Devices` | 在り (1 件) |
| 照合先 `~/Library/Developer/XCTestDevices` | **在り** (0 件。ディレクトリは実在する) |
| 照合先 `~/Library/Developer/Xcode/UserData/Previews/Simulator Devices` | 無し (存在しないセットは `os.Stat` で skip する仕様どおり) |

**修正が照合する 3 セットのうち 2 つが実機に実在する**ことは確認できた。一方、
**「並列テストの clone が `/private/var/tmp` に dir を作る」の裏取りは今回もできていない**。
観測には `xcodebuild test -parallel-testing-enabled YES` の実行中である必要があり、
[`no-ios-simulator-verification.md`](../../_claude/rules/no-ios-simulator-verification.md) により
シミュレータを使う確認は自発的に行わない (ユーザーの明示指示があるときだけ)。

**trigger は据え置き**: iOS / macOS アプリ側で並列テストを回す作業が入ったとき、その最中に
`ls /private/var/tmp | grep SimDevice` と
`xcrun simctl --set ~/Library/Developer/XCTestDevices list devices -j` を採る。
前者に出る UDID が後者に在り、既定セットに無ければ、この issue が塞いだ穴が実在したことになる。

🚨 **現時点で誤判定は起こりえない** (作業ディレクトリが 0 件なので候補が生成されない)。
急いで確かめる必要は無く、機会が来たときでよい。

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

## 対応 (2026-09-03)

**修正した。** デバイスセットを**名前の直書きではなく列挙**する形にした (`src/doctor/disk/guard.go`)。

- `simDeviceSetDirs(env)` を新設し、`~/Library/Developer` 直下の **`*Devices` で終わる実在ディレクトリ**
  (symlink も `os.Stat` で辿る) と Xcode Previews の固定パスを集める。並び順は `sort.Strings` で決定論
- `simDeviceUDIDs` はそれぞれに `xcrun simctl --set <dir> list devices -j` を投げ、**どれか 1 つでも
  失敗したら fail-closed** (孤児判定をしない)
- `~/Library/Developer` 自体が読めないときも fail-closed。無視して空を返すと「セットが無い」と
  同じ結果になり、この関数が防いでいる失敗モードそのものを、診断の痕跡を出さずに再現する

これで XCTestDevices (並列テストの clone) と XCPGDevices (Playground) の両方が拾われ、
将来セットが増えても名前を足す必要がない。

### 変異検証

| 変異 | 結果 |
|---|---|
| 列挙をやめて Previews だけに戻す (旧挙動) | red (`XCTestDevices セットのデバイス (並列テストの clone) を孤児にした`) |
| セット取得の失敗を無視する | red (2 テスト) |
| `*Devices` のサフィックス判定を外す | red (`DVTDownloads` に問い合わせて failed) |
| `~/Library/Developer` の `ReadDir` エラーを握り潰す | red |
| サフィックスを `TestDevices` に狭める (XCPGDevices だけ落とす) | red (`セット XCPGDevices に問い合わせていない` のみ) |
| Previews の error だけ握り潰す | red (`Simulator Devices の取得失敗を fail-closed にしていない` のみ) |

敵対レビューがケース名ごとの pass/fail まで確認し、**意図した 1 ケースだけ**が落ちることを実測した。

### 敵対的レビュー (sonnet / read-only / 2 周)

1 周目 5 観点: 採用 1 / 却下 0 / 記録 2。

- **採用 (P2)**: `simDeviceSetDirs` の `os.ReadDir` エラーが握り潰され、doc コメントが謳う fail-closed が
  破れていた (`chmod 000` で dirs が黙って空になることを実測) → error を返す形に変更
- **記録 (未解決の構造的リスク)**: `simctl --set` は**任意のパス**を受けるので、`~/Library/Developer` の
  外に作られたデバイスセットはこの列挙では発見できない。**しかも「見つからない」ので fail-closed も
  発火しない**。`xcodebuild` には clone セットの置き場を変える CLI フラグが無く、実機にもそういう
  セットは無いので理論上のリスクに留まる (旧実装の「名前 2 つ直書き」よりは狭まっている)
- **記録 (テスト基盤の脆さ)**: `fakeRunner` はトークン境界でなく生の `HasPrefix` で応答を選ぶので、
  将来 `.../FooDevices` と `.../FooDevicesExtra` のような prefix 関係にある fixture を足すと誤マッチする。
  現状の 3 パスは互いに prefix 関係に無いので今は問題にならない
- **壊せなかった**: `*Devices` で終わるがデバイスセットでないディレクトリへの問い合わせ
  (`simctl --set` は任意の実在ディレクトリに rc=0 と空リストを返すので fail-closed に落ちない) /
  壊れた symlink・権限なし・大量エントリ / `sort.Strings` と fakeRunner の噛み合わせ

2 周目 5 観点: 採用 1 / 却下 0。

- **採用**: `chmod 000` のケースを同じテスト関数の中に置いていたため、**復元行を 1 行消すだけで
  後続の per-set fail-closed assert 3 本が恒常的に green になる** (「別の理由で fail-closed」に
  なるため)。実測で確認された → 独立したテスト `TestSimDeviceSetDirUnreadableFailsClosed` に
  切り出し、per-set ループの手前に「この時点で dev が読める」という前提 assert を置いた。
  切り出したテストにも「読める状態なら候補に出る」という前提 assert を入れて、
  「読めない」以外の理由で failed になっていないことを固定した
- **壊せなかった**: `!os.IsNotExist(err)` の線引き (ENOTDIR / ELOOP / 親が読めない / 壊れた symlink の
  6 パターンを実測し、取りこぼしも過剰な escalate も無し) / TCC との一貫性 (`installedBundleIDs` が
  同じ「安全リストの root は fail-closed、葉は握り潰す」形をしており、`sizePaths` が候補リスト側で
  per-item を許容するのとは役割が違う) / `chmod 000` テストが root や CI で vacuous にならないか
  (CI の macOS runner は非 root)
