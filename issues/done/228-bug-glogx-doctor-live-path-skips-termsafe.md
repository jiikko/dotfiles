# doctor の live 経路が termsafe を通らず、外部由来のファイル名が生のまま表示とクリップボードへ載る

起票日: 2026-09-04
種別: bug (security)
優先度: **P2** (規約が明文で禁じている形。ただし**表示 sink での端末乗っ取りは実測で崩れた** — 下記 §5)
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

### 5. 🚨 描画層の被害は実測で崩れた (当初の主張の訂正)

当初この issue は「`ansi.Truncate` がエスケープを保存する」ところで追跡を打ち切り、
**表示しただけで OSC52・タイトル書き換え・画面消去が起きる**と書いていた。**これは誤り。**

追跡には **1 段残っていた**。`View.Content` は bubbletea v2 の `cursedRenderer.flush` で
`uv.NewStyledString(view.Content).Draw(cellbuf, …)` に渡り、ultraviolet の `printString` が
**セル単位に分解し直す**。非 SGR / 非 OSC8 のシーケンスは `cell.Content += string(seq)` に溜まるだけで、
**次の可視文字が `cell.Content = string(seq)` で上書きするため消える**。

反証レビューが、同じパッケージ・同じバージョンを隔離モジュールから呼び、doctor の 1 フレームを
40x4 の ScreenBuffer に Draw → TerminalRenderer で Flush して実測した:

| 注入したもの | 端末へ出た結果 |
|---|---|
| OSC52 (クリップボード書き込み) | **落ちる** |
| OSC0 (タイトル書き換え) | **落ちる** |
| CSI `ESC[2J` (画面消去) | **落ちる** |
| DCS | **落ちる** |
| **OSC8 ハイパーリンク** | **素通り** (攻撃者の URI が端末へ出る) |
| **SGR (色 / 点滅)** | **素通り + 次の行へ滲む** (hint 行まで赤背景になる) |
| **改行 `\n`** | **行が割れて偽の行ができ、固定高フレームの最下行が押し出される** |

つまり**表示 sink で実際に起きるのは端末制御の乗っ取りではなく、整合性・可読性の破壊**
(偽の行・行数偽装・色の滲み・リンク偽装)。当初の P1 の根拠はここで失われる。

### 5b. 実際に危険な sink は 2 つ

**(a) クリップボード — 本物。**
`y` / `Y` は `copyWithToast` → `copyToClipboard` → `pbcopy` へ**無害化なしで生のまま**渡る。
本 repo は `copyJobContextLines` で「生のままシステムクリップボードへ流すと、**ペースト先の端末で
OSC52 等が発火しうる**(レビュー確定)」と**自ら同じ脅威モデルを明文化している**。
ただし発火は「表示しただけ」ではなく「コピーして端末に貼った瞬間」= `y` を押す能動的操作が要る。
→ **優先すべきは `copyPath` / `copyText` 側であって `doctorRow.text` 側ではない。**

**(b) `bin/diskdoctor` の stdout — 「表示しただけで発火」が本当に成立する経路。**
`src/doctor/disk/report.go: Format` は bubbletea の sink を通らず**stdout へ直接書く**ため、
上の表の「落ちる」列が当てはまらない。当初の主張はここを名指ししていなかった。

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

**着手順は §5b に従う** — 表示 (`doctorRow.text`) より **クリップボード (`copyPath` / `copyText`) と
`bin/diskdoctor` の `report.go: Format`** が先。入口 1 箇所で通せば結果的に全部閉じるが、
「まず表示を直した」で終わらせると**危険な方の 2 つが残る**。

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
- `issues/done/193` — **前例**。snapshot の `Item.Path` と disk キャッシュの未検証を塞いだ issue。
  193 自身が「178 の『壊せなかった』にこの 2 件は含まれていない」と探索の非網羅性を明記している
- `issues/done/178` — snapshot の信頼境界。**脅威モデルが保存ファイルに閉じており、live 経路は対象外だった**

  🚨 **切り分け (これを書かないと「178 で直したはず」で誤って閉じられる)**: 178 は敵対レビューの
  2〜3 周目で「実走査でも成立する」インジェクションを 2 件直している (`svc.ShellQuote` の allowlist 化と
  `manualCommands` の `Label` 引用)。したがって **plist `Label` の*シェル*インジェクションは live 経路でも
  既に閉じている**。本 issue が指すのは `svcSection` が `" ⛔ "+f.Label` を生連結する
  **表示側の制御文字**で、そちらは未対応
- `src/glogx/worktree_status.go: dispPath` — 同種の値 (ファイル名) を正しく通している実装の見本

---

## 対応 (2026-09-04, dotfiles-c2)

commit: `77915873` (本体) → `149a8079` (敵対的レビューの指摘)。

### やったこと

1. **`termsafe` を `src/termsafe` の独立 module へ出した**。CLI (`bin/diskdoctor` / `bin/svcdoctor`)
   は `doctor` module にあり、glogx → doctor の依存があるので `glogx/termsafe` を引けなかった
   (循環)。3 点セット (Makefile / go.mod / `src_termsafe.yml`) を揃え、`src_glogx.yml` /
   `src_doctor.yml` の paths にも `src/termsafe/**` を足した (`scripts/check_go_project_lanes.sh`
   は 7 件で緑。`bin/lib/go_autobuild.zsh` は replace 先を指紋に含めるので旧バイナリの取り違えも
   起きない — `src/termsafe` が入力集合に入ることを実測で確認)
2. **関門を 1 つにした**。`disk.SanitizeForDisplay` / `svc.SanitizeForDisplay` を新設し、
   **CLI の `Format` の先頭**と **glogx の Msg 受け口** (`receiveDisk` / `receiveSvc` /
   `receiveBrew` / `receiveDelete`) の両方が同じ関数を通る
3. 🚨 **同一性を持つ値は書き換えず落とす**。`disk` の `Item.Path` と `svc` の
   Label / PlistPath / Domain がこれ。書き換えると「画面に出ているものと、実際に消す /
   案内するものが違う」を作る (svc は敵対レビューが実際に「攻撃者が選んだファイルを `rm` しろ」
   と案内させた)。落とした件数は `Failures` / `DirErrs` に残し、**CLI の終了コードにも出す**
4. **復元経路の二重実装を termsafe へ寄せた** (§2)。`cleanOneLine` / `cleanBrewText` /
   `svc.cleanText` はどれも `unicode.IsPrint` を自前で回しており、ESC だけ落として payload
   (`]52;c;…`) を本文に残す形だった。残したのは長さの上限だけ (復元固有の関心)
5. **テスト** (§3 / §4): `untrusted_display_test.go` に doctor を 6 本追加 (それまで 0 本
   だったのが漏れの直接の原因)。ディスクの fixture は**実際に ESC / OSC52 入りの名前の
   ディレクトリを作って走査させる**。CLI 側は `Format` と終了コードのテストを追加。
   変異検証は 1 周目 11 本 + 2 周目 8 本

### 敵対的レビュー (opus / read-only) の結果

**live 経路の素通りは 1 本も見つからなかった**。壊れたのは復元経路と svc の同一性で、
P1 2 件 / P2 3 件 / P3 2 件を修正した (詳細は `149a8079` の commit message)。
特に効いた指摘: **テストの約半分が無検査**だったこと (brew の fixture が `Unavailable` と
`Warnings` を同時に立てており、`brewSection` の早期 return で警告に触れる row が 0 個だった)。

### 直さなかったもの (再提案しないこと)

- **`-json` は生の Report を出したまま**。encoder が制御文字を `\u001b` へ escape するので
  安全で、機械の読み手から情報を落とす方が害が大きい。**終了コードだけ**無害化後に合わせた
  (「隠したものがある」を緑にしないため)
- **`flattenDoctorRows` (row の改行ガード) は現時点で到達経路が無い**。外部由来の値は 1 行の
  場所では `PlainLine` を通るため。それでも置いたのは、無害化の使い分けを間違えた 1 行が
  入った瞬間に固定高の契約が壊れ、行数を数えないテストでは気づけないから。到達経路が無い以上、
  検査は直接テスト (`TestFlattenDoctorRowsEnforcesSingleLine`) が持つ
- **`replace` 先が dependent の workflow paths に入っているかは機械が見ていない**
  (`check_go_project_lanes.sh` は `src/<name>/` が `src_<name>.yml` にあるかだけを見る)。
  今日の配線は正しいので、止めているのは `src_termsafe.yml` のコメントだけ。
  次に replace を足す人が漏らす形なので、**issue 251 の中に候補として記録**した

### 積み残し (別 issue)

- §4 の「termsafe が `string -> string` で型の関門を持たない」→ **issue 251**
- 復元した `Entry` をカタログへ束ね直す件は **issue 229** のまま (今回入れた
  `SanitizeEntryForDisplay` は制御文字しか直さない。意味のずれは残る)
