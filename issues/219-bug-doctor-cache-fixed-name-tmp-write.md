# 219 bug: doctor キャッシュの書き込みだけ `writeAtomic` を通っておらず、固定名 tmp + `os.WriteFile` の複製実装が残っている

起票日: 2026-09-03
出典: glogx 監査 (resource-leaks / dead-code、direct 実行) + 反証レビュー (opus, read-only)
重要度: **P3**（残骸はディレクトリエントリ 1 個。データ破損経路は無い。理由は「実害の範囲」節）
対象: `src/glogx/doctor_cache.go` の `saveDoctorDiskCache` / `saveDoctorSnapshot`
関連: `src/glogx/cache.go` の `writeAtomic`（同 package の正しい実装。`cache_test.go:164` が rename 分岐を pin）、
      `src/doctor/disk/delete.go` の `history.write`（同じ書き方の穴を敵対レビューで塞いだ側）

## 症状

`~/.cache/glog` へ書く経路は 4 本あり、**3 本は `writeAtomic` を通っている**
（`cache.go:116` CI キャッシュ / `claude_version.go:144` / `usage_cache.go:90`）。
doctor の 2 経路だけが独自の atomic 書き込みを持っている（`doctor_cache.go:225-231` / `:412-418`）:

```go
tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
if err := os.WriteFile(tmp, data, 0o600); err != nil {
    return err                       // ← tmp を消さない
}
if err := os.Rename(tmp, path); err != nil {
    _ = os.Remove(tmp)               // ← rename 失敗だけは掃除する
    return err
}
```

`writeAtomic` は `CreateTemp` + 3 分岐の掃除（`cache.go:152` write 失敗 / `:156` Close 失敗 /
`:160` rename 失敗）を持つ。**doctor 側は rename 分岐だけを写して、write / Close 分岐を落としている。**

### (a) write / Close 失敗で tmp が残る（実測）

`os.WriteFile` は `O_CREATE|O_TRUNC` で開いてから書くので、**書き込みが失敗してもファイルは残る**。
pid が毎回変わるので後続の実行で上書きもされず、`~/.cache/glog` の `*.tmp.*` を掃く経路は
コードにも Makefile にも無い（`src/` を掃いた結果、glob/remove はテストの自前 temp dir 内だけ）。

**実測 2026-09-03**（同じ 8MB を書かせて A/B。**production の関数ではなく「形の複製」で測った**ので、
等価性はコードの読み合わせに依っている）:

| 形 | 結果 |
|---|---|
| `doctor_cache.go` の形（固定名 + `os.WriteFile`） | `no space left on device` / **leftover 1 件**（0 バイト） |
| `cache.go` の `writeAtomic` の形（`CreateTemp`→`Write`→`Close`→`Rename`） | 同じ err / **leftover 0 件** |

再現手順（`tmp/` は gitignore で消えるのでここに残す。**RAM ディスクなので実ディスクは触らない**）:

```sh
dev=$(hdiutil attach -nomount ram://2048 | awk 'NR==1{print $1}')   # 1MB
diskutil eraseVolume HFS+ glogxaudit "$dev"
# /Volumes/glogxaudit に 8MB を os.WriteFile する Go を 1 本書いて実行し、err と残骸を見る
hdiutil detach "$dev"                                               # 後始末は必ず自分の device を明示
```

発火条件は ENOSPC / EDQUOT / EIO。**doctor は「ディスクが足りない」ときに開く画面**なので、
ENOSPC はこの機能の想定シナリオそのもの（一番残骸が出てほしくない状況で出る）。

### (b) 固定名なので symlink を辿る（ただし動機の主ではない）

`<path>.tmp.<pid>` は完全に予測可能（pid の候補は有限で事前に全部撒ける）。`os.WriteFile` は
`O_EXCL` を付けないので **symlink を辿って truncate + 上書き**し、続く `os.Rename` は
**symlink 自体**を改名するのでキャッシュのパスが被害ファイルへの symlink になる。
`os.CreateTemp` は `O_EXCL` + ランダム名なのでこの経路が閉じる。

🚨 **`delete.go` と同じ重さで扱わないこと（反証レビューの指摘を採用）**。`delete.go:1093-` の 🚨 が
「現実的な穴」と判定した根拠は、**被害者が破壊的操作の fail-closed 保証そのもの**だったこと
（記録が実体を持たないまま「書けた」ことになり、`delete.go` の「書けなくなったら残りを触らずに止める」が
無音で破れる）。doctor キャッシュにはその保証が無く、読み側は破損・欠損を「結果なし」に畳む
（`loadDoctorDiskCache` / `loadDoctorSnapshot`）ので、逸れて失うのは起動トーストと再利用だけ。
DAC でも攻撃者の得るものは無い: 実測で `~/.cache/glog` は `drwxr-xr-x koji staff` なので、
symlink を置けるのは同一 uid = truncate が届く先には元から書ける。

**唯一の増幅は macOS TCC**（Full Disk Access を持たない同一 uid のプロセスが glogx を
confused deputy にして TCC 保護下のファイルを truncate する）。**これは未実測**で、
実測しない限り (b) を動機に数えない。**直す動機は「同じ置き場への書き込みで二重実装が
1 つだけ残っている」ことで足りる。**

## 実害の範囲（P3 の根拠）

- write 失敗時は `os.Rename` に到達しないので、**前の正しいキャッシュは生き残る**（データ破損は無い）
- 残骸は 0 バイト（ENOSPC の場合）のディレクトリエントリ 1 個 × 失敗した pid の数
- 逆に過小でもない: 掃く経路が無いので減らない

## 直し方

`writeAtomic`（`cache.go`）へ寄せる。**二重実装をやめるのが根治**で、(a) の error-return 経路が閉じる。

- パーミッション: `os.CreateTemp` は 0600（`os/tempfile.go` の `O_RDWR|O_CREATE|O_EXCL, 0600`）で
  現状の `0o600` と一致し、rename でも保たれる
- `MkdirAll(0o755)` も `writeAtomic` が持っているので呼び出し側から落とせる（現状と同じ mode）
- **tmp の prefix は引数にする（任意ではなく必要）**: `.glog-cache-*` 固定のままだと、残骸が出たときに
  「隠しファイルで出所不明」になる（今は `doctor-snapshot.json.tmp.<pid>` = 何が漏れたか読める）。
  `.glog-cache-*` を掃く経路も同じく無いので、名前は読める側に倒す
- 🚨 **閉じるのは error-return 経路だけ**。`CreateTemp` と `Remove` の間で SIGKILL / panic した残骸は
  どちらの実装でも残る（「(a)(b) 両方が構造的に閉じる」とは書けない）

## 受け入れ条件

- [x] `saveDoctorDiskCache` / `saveDoctorSnapshot` が `writeAtomic` 経由になる（2026-09-04。**tmp 名は引数化せず writeAtomic 内で導出**。理由は下記 P1）
- [x] **write 失敗**で tmp が残らない回帰テストを doctor 側に足した (2026-09-04)。rename 失敗は
      `cache_test.go:164` と同じ手（rename 先をディレクトリにする）で作れるが、**write 失敗の再現には
      別の手が要る**（RAM ディスク / 書き込みを差せる seam）。難しければ
      「`os.WriteFile` を使わないことの静的 pin」でもよいが、その場合は**満たせていない主張**
      （write / Close 分岐）を issue に明記して残す
- [x] 変異検証で**分岐を名指しした** (2026-09-04): `cache.go:152` の `os.Remove(tmpName)` を外したとき、
      **今回足した doctor のテストが** red になること。🚨 `cache.go:160`（rename 分岐）を外すのは
      既存の `TestSaveCacheCleansTempOnRenameFailure` が拾うので、**doctor 側のテストを 1 本も
      足さずに「変異で red」を名乗れてしまう**（`mutation-verify-new-tests.md` の
      「スイートの red を効いていると読まない」）
- [ ] （任意・trigger 待ち）「この置き場への書き込みは `writeAtomic` を通す」を ruleguard / depguard で強制する。**第 1 段では入れない**（下記「入れなかったもの」）。
      既に `gorules/rules.go` がある repo なので、静的 pin をその形で入れると下の
      `issues_state.go:71` も同時に射程に入る

## 同型の横展開（grep 済み。掃いた軸と掃いていない軸）

掃いた軸は **「固定名 tmp」**（`src/` 全体を `\.tmp` で grep）。同じ形は他に 1 箇所:

- `src/parallel-each/result_log.go:96` — `tmp := w.path + ".tmp"`。**pid すら付かないのでプロセス間で
  共有**され、同じログに対する parallel-each 2 プロセスが同一 tmp を踏み合う（予測可能性より
  こちらが先に効く）。rename 失敗だけ掃除する形まで同じ。**glogx の範囲外なので本 issue では直さない**

掃いていない軸は **「tmp を使わない書き込み」**。`\.tmp` の grep では原理的に出ない例:

- `src/glogx/issues_state.go:71` — 同じ置き場の `issues-last-screen.json` を tmp も rename も無しに
  `os.WriteFile(path, ...)` で書く。実害は無い（読み側が unmarshal 失敗を「状態なし」に畳む）ので
  直す必要はないが、**掃いた軸の外**なので記録に残す

## 同時に走らせた dead-code 掃きの結果（0 件。次の監査の起点として残す）

`golang.org/x/tools/cmd/deadcode -test=false ./...`（main からの到達可能性）で **3 件、いずれも却下**:

| 検出 | 却下理由 |
|---|---|
| `usage/render.go:38 RenderLine` | 「単独コマンド切り出し用の公開面」の意図が `usage/usage.go:4` に明記。`issues/done/030:66` で同じ検出を既に却下 |
| `usage/render.go:77 RenderTable` | 同上（🚨 コメントは**条件付きの削除許可**「単独コマンド案を捨てるならテストを `RenderTableGroups` へ寄せてから削除してよい」。**trigger = 単独コマンド案を捨てたとき**） |
| `issues/parse.go:28 BytesReadForTest` | テスト専用の観測点（`issues_view_test.go:1761/1763` が使用中）。doc に「production からは読まないこと」 |

🚨 **この 0 件は「この道具では出なかった」**であって「無い」ではない。次の監査への引き継ぎ:

- `src/glogx/go.mod` は `replace doctor => ../doctor` の**別モジュール**なので、上のコマンドは
  `src/doctor` を一切見ていない。`deadcode -test=false ./... doctor/...` まで広げても同じ 3 件だった（実測）
- deadcode の RTA は interface / func 値経由の dispatch を保守的に「到達可能」とみなす。
  glogx は seam を `var f = func(...)` で持つ設計なので、**死んだ seam は原理的に出ない**

補助的な掃きの結果: `make -C src/glogx lint` は 0 issues（`unused` / `errcheck` は既定セット、
`bodyclose` は `.golangci.yml:11`、`contextcheck` は `:13`、`lostcancel` は govet 既定）。
書かれるだけで読まれない struct フィールドも 279 個を粗く掃いたが、候補 7 件は全部**行末での読み**を
取りこぼした偽陽性だった（`bRun := v.brewRun` の形）。

## 攻めて見つからなかった範囲（resource-leaks）

- production の `go func` 9 箇所: 送信先チャネルは全て buffer ≥ 1 か latch 管理。`doctor_view.go` の
  `ch := make(chan doctorDiskEvent, catalogN+1)` は `src/doctor/disk/scan.go` の「`OnResult` は
  catalog 1 件につき 1 回」と一致していることを確認（超えると閉じた後の走査 goroutine が詰まる。
  この不変条件を pin するテストは**無い** = モジュール境界を跨ぐ契約がコメントだけで保たれている）
- timer/ticker: production は `drainWatchEvents` の 1 本だけ。`defer timer.Stop()` + Stop→drain→Reset の作法
- fsnotify: `stopWatch` / `stopGitLogWatch` / `cancelAll` で Close、世代 (gen) で古いチェーンを弾く規律
- ファイル: `probe.go` は全分岐で Close、`writeAtomic` は掃除済み、`issues/parse.go` / `status_view.go` は defer Close
- 外部プロセス: `subproc.CommandContext` の WaitDelay 規律は `waitdelay_discipline_test.go` が強制

## 対応 (2026-09-04)

`writeAtomic` へ寄せた。**設計は敵対的レビュー (opus) が上乗せ 3 点を落とした結果の別案**で、
最初の案 (新関数 `writeAtomicPrefix` + production seam + AST 静的 pin) は採らなかった。

### 入れたもの

- `writeAtomic(path, data []byte, pattern string)` に**シグネチャ変更**（関数は 1 本のまま）。
  委譲で新関数を足す案を却下した根拠: 呼び出し元は 3 箇所 (`cache.go:116` /
  `claude_version.go:144` / `usage_cache.go:90`) だけで、`cache_test.go` は `SaveCache` を
  呼んでいて `writeAtomic` を直接呼んでいない = **編集は 3 行 + テストの glob 1 箇所**。
  API 保存に実需要が無く、関数を 2 本にすると読む jump と touch 箇所が増えるだけ
  (`verify-design-intent-before-refactor.md`: 複雑性は下がらず移動する)
- **全経路の temp 名を `<元のファイル名>.tmp.<乱数>` に統一**した (旧 `.glog-cache-*` は消滅)。
  doctor だけ可読名にすると、残り 3 経路に出所不明の既定が残って同じ軸で不整合を作る。
  🚨 区切りは**ドットのまま**にした: `doctor_view_test.go:491` の残骸チェックが `.tmp.` を
  見ており、`.tmp-` にすると**成功パス上の assert が無言で vacuous になる** (変異でも
  気づけない。敵対レビューの指摘)
- doctor 2 経路から `MkdirAll` + 固定名 tmp + `os.WriteFile` + `os.Rename` を削除

### 入れなかったもの (理由つき。再提案しないため)

- **production の seam** (`var createTempFile = os.CreateTemp`): 不要。`RLIMIT_FSIZE` を 0 に
  すると `(*os.File).Write` が EFBIG (`file too large`) を返し、Go は SIGXFSZ で死なない
  (実測 2026-09-04)。root も RAM ディスクも要らず 0.2 秒。テストのために production へ
  seam を足す必要が無い
- **AST 静的 pin** (`doctor_cache.go` に `os.WriteFile` が出ないことを検査): 不要。
  固定名 `os.WriteFile` を書き戻す変異で**振る舞いテストが red になる** (実測。下の変異 C)。
  静的 pin が追加で捕まえるのは「掃除を正しく書いた重複実装」だけで、射程も 1 ファイルなので
  別ファイルへ移す / import alias で素通りする (`adversarial-review-own-safeguards.md` §8)
- **ruleguard / depguard による強制**: 第 1 段では入れない。配線自体は既に在る
  (`.golangci.yml` の `gocritic.enabled-checks: [ruleguard]` + `gorules/rules.go`) が、
  ruleguard も構文しか見ないので「どの置き場への書き込みか」は表現できず、package 全体の
  `os.WriteFile` 禁止 + 例外リストになる。**trigger: 3 本目の複製が出たとき、または
  `issues_state.go:71` を直す判断が出たとき**

### 変異検証 (ケース名ごとの pass/fail で判定)

| 変異 | 新テスト (write 分岐) | 既存 `TestSaveCacheCleansTempOnRenameFailure` |
|---|---|---|
| A: write 分岐の `os.Remove` を外す | **FAIL** (disk cache / snapshot 両方) | PASS |
| B: rename 分岐の `os.Remove` を外す | PASS | **FAIL** |
| C: doctor に固定名 `os.WriteFile` を書き戻す | **FAIL** (disk cache) | PASS |

いずれも `go build` が通ることを確認してから判定した。復元後は全ケース PASS。
`go test -race ./...` EXIT=0 (glogx 34.1s ほか全パッケージ ok)、`make -C src/glogx lint` 0 issues。

### 満たせていない主張 (issue の要求どおり明記)

- **閉じたのは error-return 経路だけ**。`CreateTemp` と `Remove` の間で SIGKILL / panic した
  残骸は、どちらの実装でも残る (`writeAtomic` の doc コメントにも書いた)
- **(b) の TCC 増幅は未実測**のまま。動機に数えていない
- **`RLIMIT_FSIZE` 手法の CI (macOS runner) 上の挙動は未実測**。手元 macOS のみ
- **射程外**: `src/parallel-each/result_log.go:96` (pid すら付かない固定名 tmp) /
  `src/glogx/issues_state.go:71` (同じ置き場へ tmp も rename も無しに `os.WriteFile`)

### 副産物 (issue の範囲外)

全経路の temp 名が `<base>.tmp.<乱数>` に揃ったので、残骸を掃く道具が書きやすくなった。
🚨 **ただし「1 glob で掃ける」は誤りだった** (実装レビューで訂正):

- `writeAtomic` 経由の 5 経路は **再帰の `**/*.tmp.*`** が要る。CI キャッシュは
  `<base>/github.com/<owner>/<name>.json` なので、top level の `*.tmp.*` では**1 件も拾えない**
- `doctor-history` の temp は `.<乱数>.tmp` (`src/doctor/disk/delete.go`) で命名が別。
  `parallel-each` も別。どちらも別の glob が要る

doctor は元々 `~/.cache` を掃除する画面なので、そちらへ回収するのが上位の解かもしれない。

## 実装フェーズの敵対的レビュー (opus, 2026-09-04) — P1 を 1 件採用

**最初の実装 (pattern を引数にする形) に P1 が出たので直した。**

### P1: `pattern` 引数が knob になり、既存テストが呼び出し側 1 行で vacuous になる

引数化すると「production が作る名前」と「テストが glob する名前」が**別々のリテラル**になる。
レビューが作った変異 (M-G) を**こちらでも再現した**:

- `cache.go` の呼び出しの pattern を `".glog-cache-*"` に戻す (掃除は健全) → 全テスト PASS
- **上に加えて rename 分岐の `os.Remove` を削除** (= rename 失敗で temp が実際に残る)
  → **スイート全体が PASS**。issue 219 が塞いだはずの穴と同型のリークが緑で通る

引数化の目的は「残骸の出所が読める」ことで、**`writeAtomic` 内で導出しても同じだけ満たせる**。
引数は「コンパイル時の不変条件」を「何も強制しない 5 箇所の慣習」に格下げしていた。

対応: **`pattern` 引数を削除**し `writeAtomic` 内で `filepath.Base(path)+".tmp.*"` を導出。
`cache_test.go` の glob も `filepath.Base(path)+".tmp.*"` と **path から導出**する形にして、
リテラルの二重管理をやめた。修正後の変異 (当て直した実測):

| 変異 | 新テスト (write 分岐) | 既存 (rename 分岐) |
|---|---|---|
| write 分岐の `os.Remove` を外す | **FAIL** (2 ケース) | PASS |
| rename 分岐の `os.Remove` を外す | PASS | **FAIL** |
| 名前の導出を旧名 `.glog-cache-*` に変える | **FAIL** (2 ケース) | PASS |

呼び出し側に knob が無くなったので M-G は**構造的に作れない**。

### 満たせていない主張 (実装レビューの指摘で追記)

- 🚨 **Close 分岐の掃除は変異検証の射程外**。`RLIMIT_FSIZE` では Close 失敗を作れないため、
  `cache.go` の Close 分岐の `os.Remove` を外しても**どのテストも赤くならない** (実測)。
  「3 分岐すべて掃除する」は実装の事実だが、**pin されているのは write と rename の 2 分岐だけ**。
  `writeAtomic` の doc にも書いた
- **`RLIMIT_FSIZE=0` は子プロセスに継承され、非 Go の子は SIGXFSZ で即死する** (機構は実測:
  `/bin/sh -c 'echo > file'` が `signal: filesize limit exceeded`)。この package は
  `runner.Exec` 経由で `du` / `brew` / `launchctl` を起こし、issue 216 の「テストが実走査の
  goroutine を漏らす」と組み合わさると誤判定しうる。**発生は再現できなかった**
  (`-shuffle` 3 seed / coverage 付きフル走で EXIT=0。窓が JSON 1 個の Write 1 回なので確率が極小)。
  doc に「窓の中でプロセスを起こさないこと」を明記した

### レビューが崩せなかった点

5 経路の命名統一 / `RLIMIT_FSIZE` 手法と production seam 不要 / 変異 A・B・C の分岐名指し /
race・lint・gofmt / glob のパスが実際の書き込み先であること / `filepath.Base` の実用上の安全性 /
旧 `.glog-cache-*` 残骸は 0 件で失うものが無いこと。**新テストが vacuous でないことも独立に再現された**。
