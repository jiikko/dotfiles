# 222 bug: `src/doctor` に lint 設定が無く、glogx と共有している enum の網羅が片側しか守られていない

起票日: 2026-09-03
出典: glogx 設計監査 (design / responsibility / duplication / polymorphism、direct 実行) + 反証レビュー (opus, read-only)
重要度: **P2**（根拠は「exhaustive の不在」1 本。今のコードに配線漏れは無く、壊れるのは「次に enum を足す人」）
状態: **設定の追加と exhaustive 4 件の解消は完了** (2026-09-03, origin `ecf285a7`)。残件は下の「残っていること」
対象: `src/doctor/.golangci.yml`（新設した）、`src/doctor/disk/scan.go:scanEntry`、
      `src/doctor/disk/delete.go:verifyEntry`、`src/doctor/disk/report.go:riskMark` ↔ `src/glogx/doctor_view.go:doctorRiskMark`

## なぜ glogx の監査で doctor が出てくるか

glogx の doctor 画面は `disk.Risk` / `Status` / `Outcome` / `Guard` を**そのまま**分岐材料に使っている
（`doctorRiskMark` / `deleteResultSize` / `doctorOutcomeWord`）。**同じ enum に対する switch が
2 モジュールに分かれて存在する**のに、網羅の強制は glogx 側だけに掛かっていた
（`deleteResultSize` が「数字を出さない」no-op の case まで列挙しているのがその証拠）。

🚨 **`src/README.md` は「`.golangci.yml` は任意（無ければ既定 linter で運用）」と明記している**ので、
設定の不在それ自体は規約違反ではない（この issue の根拠にしない）。doctor に必要なのは
**glogx と enum を共有していて、しかも破壊的操作を持つ**という個別事情による。

## 実測（2026-09-03。設定を入れる前 = HEAD 時点）

**(1) `src/doctor` は src/ の 6 Go モジュール中ただ 1 つ lint 設定を持たなかった**

```
$ cd src/doctor && golangci-lint config path
level=warning msg="No config file detected"   exit status 6
```

親ディレクトリ探索の抜け道も無い（root にも `$HOME` にも `.golangci*` は無い）。CI
（`src_doctor.yml` → `_go-project.yml` の `make -C src/doctor lint`）と root の `make test-go-lint` は
どちらもこの `make lint` を呼ぶだけなので、**動いていたのは v2.5.0 既定の 5 個**
（errcheck / govet / ineffassign / staticcheck / unused）だけだった。gofmt もゲートになっていなかった
（他 5 モジュールは全て `formatters: enable: [gofmt]` を持つ）。

**(2) glogx と同じ extras を当てると production に 10 件**

| linter | production |
|---|---|
| **exhaustive** | **4**（`disk/delete.go` の `Outcome` 2 本、`disk/scan.go` の `Guard` 2 本） |
| prealloc / perfsprint / intrange / unconvert | 6（テストを含めると 14 件。`unparam` は production 0 件） |

🚨 4 件はいずれも `default:` を持たない switch（目視で確認）。glogx の設定は
`default-signifies-exhaustive: true` = 「default があれば意図表明として網羅を要求しない」なので、
**この 4 件は glogx の設定でもそのまま赤くなる**（設定の緩さで説明できる件数ではない）。

## 何が silent に壊れるか（発火条件）

`scanEntry` は `e.Guard` を**2 本の default 無し switch に分けて**捌いている
（前半 = 専用走査・ブロック判定 / 後半 = パス展開後の Item 絞り込み）。分割自体は正当で、
**適用時点が違う**（展開の前と後）ので 1 本にまとめられない。危険なのは新しい `Guard` 値を
どちらにも書き忘れたときで:

- **絞り込み型**（`Boottime` / `SimDevice` / `OrphanApp` / `BrewOrphan` / `VMRoot` の仲間）と
  **ブロック型**（`ProcessAbsent` の仲間）を書き忘れると、**guard が 1 つも適用されないまま候補になる**
  = fail-open。削除直前の再走査も同じ `scanEntry` を通るので、**両方の走査で同じに漏れる**
  （`delete.go` のコメント自身が「guard こそが『そのパスは今消してよいか』を決めている」
  「guard を通さない突合はエントリの glob の内側しか守らない」と設計として宣言している）
- 🚨 **供給型**（`BrewCleanup` / `SimRuntime` のように paths をコマンド出力から作る型）は例外で、
  書き忘れると `e.Paths` の展開に落ちるので多くは候補 0 件 = fail-closed になる
  （当初この issue は「一律 fail-open」と書いていた。反証レビューの指摘で訂正）
- 下流のガードは代替にならない: `Status != OK` の拒否は同一エントリが OK なら通り、
  `RiskConfirm → trash 強制`は新エントリが Safe/Caution なら効かず、`validateTarget`
  （絶対パス / `..` 無し / HOME 直下の除外 / 深さ下限 / 経路に symlink 無し）は**爆発半径の下限**であって
  guard の代替ではない
- **compile error にならない**（Go は default 無し switch の取りこぼしを許す）
- **テストも赤くならない**: `Guard` の値集合を列挙するテストは production・テストともゼロ件
  （カタログ全体を回すテストが `Guard` に触るのは `TestCatalogRespectsExclusions` だけで、
  「全 `Guard` が配線されている」を主張するテストは無い）

同型は `Outcome`（`verifyEntry` の早期 return と Item 集計）にもあるが、取りこぼしが
「集計に数えない」で止まるので fail-open ではない。

## 入れた修正（origin `ecf285a7`）

- `src/doctor/.golangci.yml` を新設。**exhaustive**（`default-signifies-exhaustive: true`）と、
  当時 0 件だった不変条件系・衛生系を有効化。gofmt を formatter ゲートに入れた
- 🚨 **`Guard` の 2 本は `default:` で閉じず、全 case を並べる形で解消した**。
  `default-signifies-exhaustive: true` の下で `default:` を書くと、**新しい Guard を足しても
  永久に lint が赤くならない**（合成パッケージで実測: default 無し版だけが `missing cases` を出す）。
  つまり `default:` を選ぶと本 issue の主題である fail-open が「lint 済み」の顔で恒久的に不可視になる。
  `Outcome` の 2 本は fail-open ではないので `default:` でもよいが、揃えて全列挙にした
- `GuardNone`（値は `""`）は**元々どちらの switch にも無かった**ので、両方に no-op の case として明記した
- gofmt ゲートを入れたことで `disk/report_test.go` が改行なしで commit されていたのが判明したので直した
- 検証: `golangci-lint config path` が設定を返す / **変異**（case 群を 1 つ外す → `go build` は成功し
  exhaustive が 1 件出る → 復元で 0 issues）/ `make -C src/doctor test` (-race) 全緑。
  いずれも並行セッションの WIP と混ざらないよう使い捨て worktree で実施

## 決着 (2026-09-04)

残件 3 つとも片付いたので done へ送る。

- [x] **未導入の linter を入れた**: `intrange` / `perfsprint` / `prealloc` / `unconvert` / `unparam`。
      潰した指摘は 15 件 (production 6 = prealloc 2 `disk/guard.go` / perfsprint 2 `svc/scan.go` /
      intrange 1 `disk/paths.go` / unconvert 1 `disk/size.go`、テスト 9)。
      🚨 **9 件目はこの作業で新しく現れた**: `svc/scan_test.go` の 3 節 for を `range 60` へ直した瞬間に
      prealloc が同じループを見えるようになった (prealloc は `range` ループしか見ない)。
      `unconvert` は**変換を落とした** (`it.Size += s.Blocks * 512`)。最初は隣の行に倣って
      `//nolint:unconvert // Blocks は platform で型が違う` を付けたが、敵対レビューが Go 1.25.4 の
      `syscall/ztypes_*_*.go` を全数走査して **34 変種すべて `Blocks` は int64**（= 理由が偽）と示した。
      隣の `Dev` の nolint は本物 (int32 ×9 / uint32 ×5 / uint64 ×20)
- [x] **語彙の出典を 1 箇所へ寄せた**: `disk.Mark(Result) string` と `disk.Foldable(Result) bool` を
      export し、`Format` (CLI) と `doctorRiskMark` / `diskSection` (TUI) の両方が呼ぶ形にした。
      glogx 側は**色だけ**を持つ (`doctorRiskColor`) ので、doctor module の「表示幅・色の依存を
      持たない」方針は維持されている
- [x] `src/doctor/Makefile` に「設定はこのディレクトリの `.golangci.yml` を自動で読む」の 1 行を足した

### 🚨 寄せただけでは守られていなかった (変異検証で判明)

一元化の直後に `disk.Mark` の「🚨 注意」を書き換える変異を当てたところ、**doctor 側も glogx 側も
緑のまま**だった (6 語のうち pin されていたのは「✅ 安全」「❓ 走査できず」「🔎 未検証」だけ)。
出典に寄せることと、その出典を固定することは別なので、テストを 2 本足した:

- `disk/report_test.go:TestMarkVocabulary` — 6 語を状態ごとに pin (出典側)
- `disk/report_test.go:TestFoldable` — 畳む条件の 5 ケース (候補あり / 一部失敗 / 未検証 / blocked)
- `glogx/doctor_view_test.go:TestDoctorRiskMarkDelegatesWordAndAddsColor` — **委譲していること**と
  **語と色の対応**を組で pin (語そのものは再掲しない。再掲すると 2 実装に戻る)。
  🚨 敵対レビュー 2026-09-04 の P1: 最初は `color != ""` しか見ておらず、**色を全部 ansiDim へ潰す変異が
  緑のまま通った** (語と色が矛盾した行を作れた)。色は glogx にしか無いので、委譲だけでは守れない。
  さらに「未検証だが候補は在る」ケースが無いと、色側の条件から `len(Items)==0` を落とす変異も素通りする
  (この 2 つを足して両方 red を確認)

変異検証 (いずれも `go build` が通ることを確認してから判定):

| 変異 | 結果 |
|---|---|
| `disk.Mark` の「🚨 注意」を別の語にする | `TestMarkVocabulary` が red |
| glogx が委譲をやめて**違う語**を返す分岐を足す | `TestDoctorRiskMarkDelegatesWordAndAddsColor` が red |
| glogx の色を全部 `ansiDim` に潰す / 色側の未検証条件だけずらす | 同テストが red (色の組を pin したため) |
| `Foldable` から `Unverified == ""` を外す | `TestFoldable` + `TestFormatKeepsUnverifiedEntryWithZeroItems` が red |
| `intrange` 化を戻す / `unconvert` の nolint を外す | それぞれの linter が発火 |

検証: `make -C src/doctor lint` 0 issues / `make -C src/doctor test` (-race) 全緑 /
`make -C src/glogx lint` 0 issues / `make -C src/glogx test` (-race) 全緑。

## 副次: 同じ語彙が 2 箇所に別実装で在る

`Risk` / `Status` → 記号 + 語の写像が `disk.riskMark`（CLI）と `doctorRiskMark`（TUI。色も返す）の
2 実装あり、畳む条件（`Status == StatusOK && len(Items) == 0 && len(Failures) == 0 && Unverified == ""`）も
両方に逐語で在る。どちらのコメントも「UI 側 / CLI 側と同じ語彙を使うこと」と互いを指しているだけ。

**今はズレていない**（6 文字列を byte-exact に比較して完全一致。fallthrough も両方 `string(r.Entry.Risk)`）。
危ないのはテストの pin が**片側だけ**なこと（glogx は glogx の文字列、doctor は doctor の文字列を assert）で、
**片側だけ語を変えるとその側のテストだけ直って通る**。

## 却下した候補（次の監査が同じものを再生成しないために）

| 候補 | 却下理由 |
|---|---|
| `scanEntry` が `Guard` を 2 本の switch に分けているのは責務混在 | 適用時点が違う（パス展開の前と後）。1 本にまとめると順序が崩れる。分割は正当で、問題は網羅の強制が無いことだけ |
| `tui.go` (3,719 行) / `issues_view.go` (1,972 行) の責務集中 | **0 件**。README「glog との共通コード分離について」と `src/glogx/CLAUDE.md`「flat な `package main` は意図的。サブパッケージを切る基準は『実在する第二消費者』か『明示的な分離要望』」が意図を明記。変更理由の数を測っても（2026-07-01 以降 tui.go = 189 commit / 21 issue、issues_view.go = 68 / 12）「状態機械 1 個に対する変更」の範囲で、凝集度の低さ・依存の広がり・状態の絡みのどれも裏付けが取れなかった。行数だけでは違反にしない（`verify-design-intent-before-refactor.md`） |
| コード重複（トークン単位） | **0 件**。`dupl` の閾値を既定の 120 → **60 まで下げて** production を走らせても glogx / doctor ともに 0 issues（テストは設定で対象外 = 「テーブル/フローの相似は仕様」）。残る重複は上の「語彙の 2 実装」のような**意味的**なものだけ |
| ポリモーフィズム置換（種別で分ける if / switch） | 上の enum switch 群が唯一の候補で、置換先は「`Risk` / `Status` にメソッドを持たせる」= 語彙の一元化と同じ。他に「同じ条件が複数箇所に散在」する形は無し（`switch key {}` 19 本はキー入力の dispatch で種別分岐ではない） |

## 却下しなかった（この監査で塞いだ）候補

`doctor_view.go:diskVerifyCommands` がカタログ ID の文字列で分岐している件は、当初
「突合テストが在る」で全面却下したが、**反証レビューが抽出の穴を実測して却下を崩した**:

- `TestDiskVerifyCommandsIDsExistInCatalog` の抽出は `case "([a-z0-9-]+)"` で、
  **`case` 直後の 1 個目のリテラルにしか当たらない**。`case "a", "b":` の 2 個目以降は素通りし、
  **11 ID のうち 7 件しか突合していなかった**（未突合: `brew-cleanup-residue` / `xctest-spindump` /
  `launchd-tmp` / `swiftui-drag-cache`）。canary も `len(ids) < 5` なので 7 → 5 まで壊れても落ちない
- 修正: case 行に出る全リテラルを拾う形へ変え、canary を 10 件へ上げた。
  **変異検証**: grouped case の 2 個目に存在しない ID を混ぜると red（`go vet` は通る = ビルド不能の
  偽陰性ではない）、復元で green
