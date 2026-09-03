# doctor 削除経路の監査 (2026-09-04) — P3 の残り / 崩せなかった攻め口 / 却下理由

起票日: 2026-09-04
種別: research (監査の全数勘定。レポート本体は `tmp/` = gitignore なので結論だけをここへ移す)
出典: audit `security` / `error-handling` / `broken-code` / `design` / forge-Standard (opus 2 体を直列)
範囲: `src/doctor/disk/{delete,guard,paths,scan,catalog,report}.go` / `main_test.go` /
`src/doctor/runner/` / `src/doctor/cmd/diskdoctor/` / `src/glogx/doctor_{delete,cleanup,view}.go`

## 全数勘定

| 区分 | 件数 | 行き先 |
|---|---|---|
| 起票した (裏取り済み) | 5 | [231](231-bug-doctor-cli-runs-with-zero-targets.md) P1 / [232](232-bug-doctor-partial-selection-freed-is-wrong.md) P1 / [233](233-bug-doctor-confirm-counts-skipped-items.md) P2 / [234](234-bug-doctor-test-sandbox-harness-gaps.md) P2 / [235](235-bug-doctor-go-modcache-scans-all-generations.md) P2 |
| 既起票と重複 → 落とした | 2 | 「live の Reason / コマンド出力が無害化されていない」= [228](228-bug-glogx-doctor-live-path-skips-termsafe.md) (termsafe。より広い) / 「glogx の責務集中」= 222-224 |
| P3 として本 issue に記録 | 6 | 下記 |
| 反証で崩れた / 到達不能 | 8 | 下記「崩せなかった攻め口」 |

## P3 (単独では起票しない。触るときに一緒に直す)

1. **インベントリ書込失敗で中断した run が `phase: done` で終わりうる** — `disk.Delete` は
   ループ内の `hist.write` 失敗で残りを Skipped にして `break` するが、最後に書く phase は
   `ctx.Err()` だけを見て決める。書込失敗が**一時的**なら (ENOSPC → 削除で空きが戻る、は
   ディスク掃除ツールでは現実的) 最後の write が成功し、記録は「最後まで行った」と言う。
   直し方: `break` した事実を変数で持ち、phase を `aborted` 側へ倒す。
2. **記録の置き場は最終要素の symlink しか見ない** — `newHistory` は `os.Lstat(dir)` で dir 自身だけを
   検査する。ゴミ箱側は `noSymlinkInPath` で経路全体を見ているので**非対称**。
   `write` は `CreateTemp` + `Rename` なので任意ファイルの上書きには直結しないが、規律は揃えるべき。
3. **`removeItem` の staging 名がどこにも残らない** — 改名 (`.glogx-delete-<hex>`) と `RemoveAll` の
   あいだにプロセスが死ぬと、その名前の残骸が親に残る (コメントは残骸に言及しているが、
   **名前を記録していない**ので後から掃除も追跡もできない)。`ItemOutcome` に持たせるか、
   固定 prefix を次回走査で掃除する。
4. **`diskExitCode` が「`Unverified` で 0 件」を rc=0 に丸める** — 人間向け出力は
   「0 件ですが『候補なし』ではありません」と言うのに、終了コードでは「きれい」と同じ。
   `report.go` / `doctor_view.go` が守っている false green の規律が CLI の rc では守られていない。
5. **`trashMove` の `dev == 0 && ino == 0` 検査は production 到達不能** — `planItem` が先に同じ検査で
   skip するため。テストも無いので「段」として数えられない (残すなら、なぜ冗長でも残すかを
   `~/.claude/rules/list-masked-failure-modes-before-removing-guard.md` の形でコメントに書く)。
6. **削除中の `ctrl+g` が `ctrl+c` と同じ扱い** — `tui.go` が
   `handleDeleteKey("ctrl+c")` へ渡す (2 段ガードを共有する意図は読める) が、パネルの案内は
   「Ctrl-C を 2 回押すと中断します」だけ。`ctrl+g` 2 回でも中断する = 案内と非対称。
   案内に足すか、`ctrl+g` は飲むだけにする。

## 崩せなかった攻め口 (再提案しないこと。同じ指摘が再生成されるのを止めるために残す)

- **細工した `Result` で任意パスを削除** → できない。`t.Items` は削除の直前にやり直した走査結果
  (`fresh`) への**選択キー**にしかならず、実際に触る値 (パス / サイズ / dev / ino) は全部 fresh 側から取る
- **snapshot に `FromSnapshot: false` を書いて偽装** → できない。load 時に無条件で立て直す
- **確認画面と実行のあいだの consent TOCTOU** → 実際に消える集合は表示した集合の**部分集合**に
  しかならない (fresh 側との突合で減るだけ、増えない)
- **`validateRef` / `substituteRef` の引数インジェクション** → 崩せず。argv 渡し + 先頭ハイフン禁止 +
  `<id>` が独立した引数でないカタログを `parseDeleteVia` が落とす
- **`removeItem` の rename レース** → 崩せず。予測不能名への改名 + 改名後の (dev, ino) 再照合
- **`trashMove` の上書き** → 崩せず。`RENAME_EXCL` + EXDEV / ENOTSUP を「移さずに失敗」へ倒している
- **同じ Item を 2 回渡して解放量を二重計上** → `dedupeTargets` と `freed = min(…)` が打ち消す
- **`excludedRoots` のランタイム強制が無い** → 指摘としては成立しない。あれはカタログの
  allowlist をテストで固定する番人で、ランタイムの番人は `validateTarget` (深さ / HOME 直下 /
  経路の symlink)。両者は別の層で、片方に他方の役目を求めるのは筋違い

## 補足 (次の監査への申し送り)

- 今回のレンズで**一度も壊せなかったのは「破壊的操作そのもの」** (TOCTOU / staging rename /
  RENAME_EXCL / 記録の fail-closed)。壊れていたのは全部**結末をどう数えてどう見せるか**の層
  (231 / 232 / 233 / 235 が同じクラスタ)。次に監査するなら、この層 —
  `verifyEntry` の数え方と UI の表示契約 — に絞ると効率がよい
- 既存テストが 231 / 232 を踏まないのは共通の理由で、**fixture が全 Item を渡す形しか作らない**こと。
  「部分集合を渡す」「fresh に載らない Item を渡す」の 2 形を fixture の標準にすると、この
  クラスタ全体が構造的に守られる

## P3 の対応 (2026-09-04)

**6 件中 4 件を直した。残り 2 件は範囲と衝突の理由で見送り。**

| P3 | 状態 |
|---|---|
| 1. 中断した run が `phase: done` で終わりうる | **直した**。`aborted` を持ち回り、`ctx.Err()` と or で phase を決める |
| 2. 記録の置き場が最終要素の symlink しか見ない | **直した**。`noSymlinkInPath` に寄せてゴミ箱側と規律を揃えた |
| 3. `removeItem` の staging 名がどこにも残らない | **直した**。`ItemOutcome.Staged` に残す (phase が executing で止まっていたらそれが残骸の候補) |
| 4. `diskExitCode` が「`Unverified` で 0 件」を rc=0 に丸める | **直した**。`Undiagnosed` へ倒す (report.go / doctor_view.go と同じ false green の規律) |
| 5. `trashMove` の `dev == 0 && ino == 0` が到達不能 | **外さずに残した**。理由をコメントに書いた (下記) |
| 6. 削除中の `ctrl+g` が `ctrl+c` と同じ扱い | **見送り**。`src/glogx/tui.go` / `doctor_delete.go` に触るので、issue 242 を進めている別セッションと衝突する。向こうの push 後に別 commit で当てる |

### P3-5 を外さなかった理由

`planItem` が先に同じ検査で skip するので production からは到達しないが、**この関数は破壊的操作の
直前の最後の照合**で、外すと下の `uint64(st.Dev) != dev || st.Ino != ino` が (0,0) と実体を
比較することになり「識別できないまま移動する」経路が開く。呼び出し元が 1 つ増えた瞬間に穴になる
形なので、冗長さと引き換えに残した (`list-masked-failure-modes-before-removing-guard.md`)。
⚠️ 到達しないのでテストで固定できていない = **「段」として数えない**。

### 変異検証 (いずれも go build 成功を確認してから判定)

| 変異 | 結果 |
|---|---|
| `aborted` を常に false にする (P3-1 を戻す) | **red**。`phase = "done"` と記録され、issue の症状がそのまま再現 |
| 置き場の検査を `os.Lstat(dir)` に戻す (P3-2 を戻す) | **red** (経路の途中の symlink を素通し) |
| `Unverified` の分岐を消す (P3-4 を戻す) | **red** (`cmd/diskdoctor` 側だけ。disk 側は緑 = 別の層を見ている証拠) |

⚠️ P3-1 の変異は最初 `aborted` が未使用でビルドできなかったので当て直した
(ビルド不能の緑を「検知できず」と読まない)。

### テストのハーネスについて (P3-1)

**書込失敗を「一時的」にしないと症状を観測できない**。恒久的に書けないと最後の write も失敗して
記録が `executing` のまま残り、phase の誤りが見えない。`h.at()` が `opt.Now` を通ることを使い、
Now の呼び出し回数で窓を作った (実測: #1 StartedAt / #2 write(planned) / #4 write(executing) /
#5 FinishedAt / #6 最終 write)。順序が変わったら **「判定不能」で落ちる**ガードを入れてある。

### 満たせていない主張

- **P3-3 は「名前を記録に残す」だけ**で、残骸を掃く経路は作っていない (次回走査での掃除は未実装)
- **P3-6 は未着手** (上記の衝突理由)
- 実機での目視確認は未実施

