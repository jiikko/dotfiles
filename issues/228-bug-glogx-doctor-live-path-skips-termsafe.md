# doctor の live 経路が termsafe を通らず、外部由来のファイル名が生のまま表示とクリップボードへ載る

起票日: 2026-09-04
種別: bug (security)
優先度: **P1** (規約が明文で禁じている形。制御文字を含むファイル名 1 つで発火する)
出典: audit (leaky-abstraction) 2026-09-03 / forge-Minimum → **反証レビューを通過** (下記)

## 規約違反

`src/glogx/CLAUDE.md` が正本として宣言している:

> 外部由来の文字列 (git / CI ログ / issue markdown / **ファイル名**) は表示前に termsafe を入口で
> 1 回通す。出所ごとに書き分けると漏れる (issue markdown と git status のパスが実際に漏れた)

`termsafe` の package doc も同じことを、経緯つきで書いている:

> glogx が表示に出すテキストは全て自分以外が書いたもの (…作業ツリーのファイル名) で、そこに端末制御
> シーケンスが入ると**画面破壊・タイトル書き換え・OSC52 によるクリップボード書き込みが「表示しただけ」で
> 起きる**。出所ごとに書き分けると必ずどこかが漏れる (実際 issue markdown と git status のパスが
> 漏れていた) ため、無害化はこのパッケージに集約し、各パッケージは入口で通すだけにする。

**doctor の live 経路はこの関門を 1 度も通っていない。** 同じ規約が守れず漏れるのは 3 度目にあたる。

## 確認したこと (2026-09-04 / 反証レビューが実測)

### 1. termsafe の呼び出しが無い

```
$ grep -c 'termsafe\.' doctor_view.go doctor_cache.go doctor_brew.go \
                       doctor_delete.go doctor_cleanup.go doctor_rowcursor.go
0 / 0 / 0 / 0 / 0 / 0
$ grep -rln 'termsafe\.' --include='*.go' src/glogx
terminal.go / width.go / worktree_status.go / main.go / issues/*.go / usage/dial.go   ← doctor 系は 1 本も無い
```

### 2. live の値が生のまま行に載る

| 箇所 (`src/glogx/doctor_view.go`) | 生で載るもの |
|---|---|
| `doctorView.start` → `dOpt.OnResult` | `disk.Result` をそのまま channel へ |
| `doctorView.receiveDisk` | `v.diskResults = append(v.diskResults, *msg.ev.r)` (述語を通さない) |
| `diskItemRows` | `fmt.Sprintf("        %s%9s  %s", mark, disk.HumanSize(it.Size), it.Path)` |
| `diskDetail` | `doctorRow{text: "               - " + c}` (c = `r.Contents`) |
| `svcSection` | `doctorColor(o.colored, ansiRed, " ⛔ "+f.Label)` |
| `brewSection` | `copyText: "brew doctor の警告 (macOS Homebrew):\n" + w + "\n"` |
| `doctorRiskMark` | `return string(r.Entry.Risk), ""` |

`it.Path` は **`text` だけでなく `copyPath` (クリップボード) にも生で渡る**。

### 3. 値の出所が外部

- `src/doctor/disk/size.go: duSize` — `Item{Path: root}` (root = glob の結果そのまま)
- `src/doctor/disk/scan.go: listContents` — `filepath.Join(filepath.Base(p), en.Name())` (`os.ReadDir` の名前を素で `Contents` へ)
- `src/doctor/disk/catalog.go` — `$TMPDIR/TemporaryItems/NSIRD_Finder_*` / `~/Library/Caches/com.apple.SwiftUI.Drag-*` /
  `$TMPDIR/.com.google.Chrome.*` はいずれも**ユーザー書き込み可能領域**。macOS のファイル名は `/` と NUL 以外の任意バイトを取れる
- `src/doctor/svc/plist.go: parseJob` — `j.Label, _ = raw["Label"].(string)` → `Finding.Label`
- `src/doctor/disk/paths.go: validateTarget` — 絶対パス / `..` / 深さ / 親 symlink は見るが **制御文字の検査は無い**
  (`grep -rn 'unicode.IsPrint\|IsControl' src/doctor` → `svc/restore.go` の 1 件 = 復元経路のみ)

### 4. 発火条件を実際に構成した (隔離環境で実行。repo は無変更)

ESC / 改行 / BEL を含むディレクトリ名を作って glob させた結果:

```
MATCH ".../NSIRD_Finder_\nsecond"
MATCH ".../NSIRD_Finder_\x1b[2J"                  ← 画面消去
MATCH ".../NSIRD_Finder_\x1b]52;c;cHduZWQ=\a"     ← OSC52 (クリップボード書き込み)
ReadDir name "child\x1b[2Jname" -> Contents entry "…\nFAKE ROW/child\x1b[2Jname"
```

`filepath.Clean(p) == p` かつ `IsAbs` なので **`validateTarget` を通り、`Item.Path` に生で載る**。
改行入りの名前は 1 行を 2 行に割れるので、固定高パネルの行数も崩す。

### 5. 描画層も無害化しない

`doctorView.lines` の唯一の加工は `truncateDisp` → `termwidth.Truncate` → `ansi.Truncate` で、
これは **"is aware of ANSI escape codes and will not break them"** = エスケープを**保存する**。

### 6. 非対称: 保存して復元すると落ちる

`doctor_cache.go: safeDisplayPath` (`unicode.IsPrint` 検査) の呼び出しは **`sanitizeSnapshotResults` の 1 箇所のみ**。
`cleanOneLineList` / `cleanBrewText` / `svc.SanitizeRestored` も復元経路からしか呼ばれない。
つまり **「live では出る / 保存して復元すると消える」**。
対照的に `worktree_status.go: dispPath` は git のパスを `sanitizePlainLine` (= `termsafe.PlainLine`) に通している。

### 7. テストが live を守っていない

- `doctor_view_test.go: TestDoctorSnapshotTrustBoundaryFreeText` の入力は `writeDoctorSnapshot` のみ =
  **復元経路からしか制御文字を入れていない**。live 経路 (`OnResult` / `receiveDisk`) からのテストは 0 件
- `untrusted_display_test.go` の関数は 6 本 (PR 状態 / 破棄 / mark next / usage / toast / panel)。**doctor は 1 本も無い**
- `.golangci.yml` 6 ファイルすべてで termsafe への言及 0 件 = lint 強制も無い

### 反証の試み (「意図的」の証拠を探した結果)

`grep -rn '信頼境界' issues/ docs/ src/` → `done/193` / `done/183` / `148` / `svc/restore.go` /
`doctor_cache.go` / `doctor_view_test.go` のみ。`done/178` と `done/193` の脅威モデルはどちらも
**「`doctor-snapshot.json` / `doctor-disk.json` は一般ユーザー権限で書き換えられる」に閉じており**、
「ライブ走査のパスは信用してよい」という記述はどこにも無い。

## 出典の主張のうち、崩れた / 誇張だった部分 (記録)

反証レビューが訂正した点。**次の audit が同じ誤りを再生成しないために残す**:

1. 「6 ファイルすべてで `sanitize*` が 0 件」は**誤り**。`doctor_cache.go` に 9 件ある (復元経路)。
   正しくは「`termsafe.` は 6 ファイルすべてで 0 件。`sanitize*` は `doctor_cache.go` にだけ在る」
2. 「live 経路に検査が皆無」は**誇張**。`doctor_delete.go` は削除パネルのメモに `cleanOneLine` を流用している
   (`:662` / `:671` / `:695`)。**抜けているのは `doctorRow.text` と `copyText` / `copyPath` 側**
3. `disk/scan.go` / `svc/plist.go` は `src/glogx/` ではなく **`src/doctor/`** 配下 (行番号と内容は正しい)

### 🚨 未確認

**実際に doctor 画面を起動して OSC52 が発火するところまでは確かめていない** (TUI の実起動をしていない)。
確認済みなのは「制御文字を含む名前が glob / ReadDir を通って `Item.Path` / `Contents` に生で載り、
描画層の `ansi.Truncate` がエスケープを保存する」までの**データフローと各段の実コード**。

## 対応方針

### 1. 入口 1 箇所で通す (出所ごとに書き分けない)

`doctorDiskMsg` / `doctorSvcMsg` / `doctorBrewMsg` を受ける側 (または各 Msg を作る Cmd 側) で、
外部由来フィールドを `termsafe` へ 1 回通す:

- `disk.Result`: `Item.Path` / `Contents` / `Failures` / `Reason` / `Entry.Risk` 等の表示に出る文字列
- `svc.Finding`: `Label` / `PlistPath` / `Reasons`
- `brewDoctorResult`: 警告本文 (**brew 本文だけは改行を残す版**を使う)

`copyPath` / `copyText` も同じ値から作るので、入口で通せば両方が閉じる。

### 2. 復元経路の二重実装を termsafe へ寄せる

`cleanOneLine` / `cleanBrewText` / `svc.cleanText` を `termsafe` への委譲にする。
**長さ・件数の上限は復元固有の関心なのでそのまま残す** (落とした件数の注記も残す)。

### 3. 変異検証 (これをやるまで「守られている」と書かない)

「**ESC と改行を含むファイル名を実際に作り**、`doctorText` に `\x1b` と `\n` が現れない」テストを書き、
**sanitize の呼び出しを外す変異で red になる**ことを確認する。

🚨 fixture は「退行したら見えるようになる場所」に置くこと — 復元経路からしか入らないテストにすると、
今回の穴 (live 経路) を**構造的に一度も踏まない**まま緑になる (既存テストがまさにその形)。

### 4. 強制手段 (フォローアップ)

`untrusted_display_test.go` に doctor の関数が 1 本も無いのが、この漏れを許した直接の原因。
同ファイルに doctor を足すのが最小。
**termsafe が `string → string` で型の関門を持たない**ことは別の弱さで、本件を直しても残る
(次に viewer を足す人が同じ漏れを作れる)。型か lint で強制する案は別 issue に切るのが妥当。

## 関連

- issue 229 (doctor の復元経路が `Entry` をカタログへ再束縛しない。**同じクラスタとして起票されたが修正箇所も回帰テストも別物**)
- `issues/done/178` / `issues/done/193` — snapshot の信頼境界。**脅威モデルが保存ファイルに閉じており、live 経路は対象外だった**
- `src/glogx/worktree_status.go: dispPath` — 同種の値 (ファイル名) を正しく通している実装の見本
