# doctor: 削除テストのサンドボックス・ハーネスに 3 つの穴 (段の 1 つが変異で守られていない)

起票日: 2026-09-04
種別: bug (テストハーネス / 安全機構)
優先度: **P2** (production は無影響。壊れるのは「fixture の書き間違いから実データを守る」唯一の機構)
出典: audit (security / error-handling + broken-code) 2026-09-04 / forge-Standard。main agent が裏取り済み

## 前提

`src/doctor/disk/main_test.go` の冒頭は **「4 段構え (どれも「効いていること」を自己テストで
固定してある)」** と宣言している。実際に固定されているかを段ごとに見たら 3 箇所が外れていた。
`~/.claude/rules/adversarial-review-own-safeguards.md` §1.5 (段数を宣言したら段ごとに変異を当てる) の
適用対象。

## (a) 1 段目の「os.TempDir() の外は登録できない」が、判定式のコピーでしか検査されていない

該当: `main_test.go: trySandboxAllow` / `sandboxAllow`

`TestSandboxAllowRejectsPathsOutsideTempDir` が呼ぶのは `trySandboxAllow` で、これは
`sandboxAllow` の判定式を**別に書き写したもの**。`sandboxAllow` 側の検査を外す変異を当てても
このテストは緑のまま = **本走査の破損を検出しない**
(`~/.claude/rules/verify-execution-not-just-exit-code.md` の「canary と本走査は同じ関数を通す」)。
付随: `fatalRecorder.fatal` はどのテストも読んでいない (書くだけのフィールド)。

修正方向: 判定部を `sandboxAllow` と共有する述語 (例 `sandboxAllowable(root) error`) に切り出し、
`sandboxAllow` は「述語 + `t.Fatalf`」だけにする。テストは述語を直接叩く。

## (b) 移動 (trash) の**宛先**が検査点を通らない

該当: `src/doctor/disk/delete.go: trashMove` の `allowDestructive("trash", src)`

検査点に渡すのは `src` だけ。**登録済みの src から未登録のディレクトリへの移動が `err == nil` で
成功し、違反記録も 0 件**になる (自己テストは src が未登録の向きだけを見ている)。
`TrashDir` はテストが自由に渡せるので、`opt.TrashDir` を 1 行間違えると実データの場所へ
ファイルが移る。

修正方向: `allowDestructive("trash-dest", trashDir)` を `MkdirAll` の**前**に足す
(op 名を分けると、拒否の記録から「どちら側で止まったか」が読める)。

## (c) 作成系 (`MkdirAll` / `O_CREATE`) が検査点より前にある

該当: `delete.go: newHistory` (`os.MkdirAll` → `os.OpenFile(O_CREATE|O_EXCL)`) /
`trashMove` の `os.MkdirAll` / `main_test.go: TestDestructiveCallsGoThroughHook` の `destructive` 表

AST ゲートは「消す・動かす」だけを見る設計 (意図的)。その結果 `newHistory` は sandbox 判定なしに
実ディレクトリと 0 バイトの JSON を作る。しかもその後 `write` が拒否されると
`discard` も拒否されるので **0 バイトの記録ファイルが残る** (次に記録を読む処理がパースエラー)。
`XDG_CACHE_HOME` の差し替え (3 段目) が既定経路を守っているので実害は
「`HistoryDir` を明示的に変なパスへ渡したテスト」に限られるが、宣言している段数とは合っていない。

修正方向: 「作成系は対象外」という脅威モデルを維持するなら**その旨をヘッダに書く**
(今は「4 段構え」と読める)。守るなら `newHistory` の先頭に `allowDestructive("history-create", dir)` を置く。
併せて `destructive` 表に `unix.Unlink` / `unix.Rmdir` が無い (`Unlinkat` はある) 点も埋める:
`unix.Unlink(p)` と書くだけでゲートが無音で素通りする。

## silent か

3 つとも **silent**。(a) は変異で緑、(b)(c) は違反記録が 0 件のまま通るので、
`TestMain` の最終判定 (4 段目) にも出ない。

## 変異検証の形

- (a): 述語へ寄せた後、`sandboxAllow` の検査を allow-all に変異させ、
  `TestSandboxAllowRejectsPathsOutsideTempDir` が red になることを見る
- (b): 登録済み src → 未登録 dst の `trashMove` が error になることを assert し、
  `allowDestructive("trash-dest", …)` を外す変異で red を見る
- (c): `HistoryDir` に未登録のパスを渡した `Delete` が**ディレクトリを作らずに**中止することを assert

## 決着 (2026-09-04)

3 つとも塞いだ。

- **(a) 判定式のコピー** → `sandboxAllowable(root) error` へ切り出し、`sandboxAllow` と自己テストが
  **同じ関数を通る**形にした（`trySandboxAllow` / `fatalRecorder` は削除）。
  自己テストには「登録してよい側」も足した（判定が deny-all に化けても気づけるように）
- **(b) 移動の宛先** → `trashMove` の `MkdirAll` の**前**に `allowDestructive("trash-dest", trashDir)` を置いた。
  op 名を分けたので、拒否の記録から「どちら側で止まったか」が読める
- **(c) 作成系** → `newHistory` の先頭に `allowDestructive("history-create", dir)` を置いた
  （脅威モデルを書くだけで済ませず、守る側を選んだ）。あわせて AST ゲートの `destructive` 表に
  **`Unlink` / `Rmdir`** を足した（`unix.Unlink(p)` と書くだけで無音で素通りする形だった）

### 検証

- 新規テスト 2 本 + 既存 1 本の書き換え:
  `TestTrashDestGoesThroughHook`（未登録の宛先への移動が `OutcomeFailed` になり、**宛先ディレクトリを
  作らず**、違反記録が残る）/ `TestHistoryCreateGoesThroughHook`（未登録の置き場で中止し、
  ディレクトリを作らず、元も消さない）/ `TestSandboxAllowRejectsPathsOutsideTempDir`（述語を直接叩く）
- 変異 3 本とも red: 述語を allow-all にする / `trash-dest` の検査点を外す / `history-create` の検査点を外す
- 🚨 新しい検査点が**既存テストの未登録パスを 1 件捕まえた**
  （`TestDeleteFailsClosedWhenHistoryUnwritable` の `HistoryDir`）。これは (c) が予測していた
  「`HistoryDir` を明示的に変なパスへ渡したテスト」そのもので、テスト側に登録を足した
