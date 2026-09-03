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

- [ ] `saveDoctorDiskCache` / `saveDoctorSnapshot` が `writeAtomic` 経由になる（tmp prefix を引数化）
- [ ] **write 失敗**で tmp が残らない回帰テストを doctor 側に足す。rename 失敗は
      `cache_test.go:164` と同じ手（rename 先をディレクトリにする）で作れるが、**write 失敗の再現には
      別の手が要る**（RAM ディスク / 書き込みを差せる seam）。難しければ
      「`os.WriteFile` を使わないことの静的 pin」でもよいが、その場合は**満たせていない主張**
      （write / Close 分岐）を issue に明記して残す
- [ ] 変異検証は**分岐を名指しする**: `cache.go:152` の `os.Remove(tmpName)` を外したとき、
      **今回足した doctor のテストが** red になること。🚨 `cache.go:160`（rename 分岐）を外すのは
      既存の `TestSaveCacheCleansTempOnRenameFailure` が拾うので、**doctor 側のテストを 1 本も
      足さずに「変異で red」を名乗れてしまう**（`mutation-verify-new-tests.md` の
      「スイートの red を効いていると読まない」）
- [ ] （任意）「この置き場への書き込みは `writeAtomic` を通す」を ruleguard / depguard で強制する。
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
