# 205 audit: doctor 画面の並行・中断・状態機械を red team で攻める (163 の体 3。唯一の未走行)

起票日: 2026-09-03
出典: [issues/163](done/163-audit-doctor-implementation-red-team.md) の「体 3」(6 観点のうちこれだけ成果ゼロ)
重要度: P2 (④ 削除の土台になる状態機械。指摘が出れば P1 になりうる)
対象: `src/glogx/doctor_view.go` 全文、`src/glogx/tui.go` の doctor 配線、`src/glogx/main.go` の再起動と
Ctrl-C 経路、`src/doctor/runner/runner.go`、`src/doctor/disk/scan.go` の goroutine と `OnResult`

## なぜ独立した issue になったか

163 は 6 観点の red team を並行で走らせる手順書だったが、**体 3 だけ 2 回とも成果を残せなかった**。

- 1 回目 (2026-09-02 17:05): 6 体を並行起動し、**全体が session limit (429) で途中終了**。
  他の 5 体は報告や実測ログを残していたので main agent が拾えたが、体 3 は成果ゼロ
- 2 回目 (2026-09-02 深夜): 単独で起動したが、トークン消費を抑えるためユーザー判断で起動直後に中断

163 は残る 5 体分を消化し終えたので**索引としては役目を終えている**。体 3 だけを引き継ぐ。

## 攻め口 (163 の「体 3」節から移設。実コードで未確認のものを含む)

- **`rows` とカーソル index のずれ**: `rows` は `lines()` (描画) が作り直し、`handleKey` はその `rows` を見る。
  **描画とキーの間で結果が届く** (disk の Msg で `diskResults` が伸びる) と `cursor` が別の行を指す。
  Enter / y / Y が意図と違う行に効く再現手順を作る (bubbletea は Update → View の順だが、Msg 2 つが
  連続すると View を挟まない)
  - **2026-09-03 の変更でこの攻め口は当たりやすくなった** (最初は「成立しない」と書いたが逆だった。
    敵対的レビューで訂正): issue 180 が Failures 行を `selectable: true` にし、しかも
    **各 Result の直下に挿入する** (`doctor_view.go` の `diskSection`)。走査中は Msg が届くたびに
    `diskResults` が伸びるので、**選択可能な行が行の途中に増える** = カーソル index のずれの
    再現性は上がっている。
    ⚠️ issue 182 (ラベル列の幅を縮める) は `labelW` の切り詰めだけで `selectable` にも行数にも
    触らないので、**この攻め口とは無関係**
- **`expanded` のキーのずれ**: brew は `brew:<i>:<summary>`、disk は `disk:<ID>`。再スキャンで順序が
  変わると展開状態がずれる / 別の行が開く。`start` が `map{}` で作り直しているのは意図か
- **旧世代のチャネルを新しい gen で待つ**: 163 は「snapshot 復元したとき `diskCh` は nil なので
  `waitDiskCmd` が永久にブロックする」と書いていたが、**これは事実誤認だった** (敵対的レビューで訂正)。
  `diskCh` に nil を代入する箇所は無く (`doctor_view.go` の宣言 / 代入 / 読みの 3 箇所を確認)、
  `start(force=false)` の snapshot 復元は**チャネルを作る前に早期 return する**ので、
  `diskCh` は**前世代のチャネルを保持したまま**になる (nil なのは初回起動時だけ)。
  ⇒ 攻めるべきは「**旧世代のチャネルを新しい gen で待つ**」形。`waitDiskCmd(gen)` が掴むのは
  `v.diskCh` (旧世代) で、そこへ旧世代の走査が書き込むと `receiveDisk` は gen 不一致で捨てる。
  捨てた後に再アームされないなら、その世代の待ち受けが消える。**初回起動 (nil) で
  `waitDiskCmd` が呼ばれる経路が無いこと**も併せて確かめる
- **`Setpgid` と割り込みの相互作用**: 子を別プロセスグループにしたので glogx が受ける SIGINT が子に届かない。
  `cancelAll` が必ず走るか (Ctrl-C 2 回の `quitNow` 経由を含む)。`main.go` の `syscall.Exec` 再起動 (`r`) の
  直前に cancel した子が新イメージの子として残る形 (163 では「記録に留めた」。Setpgid 導入後は
  `Kill(-pgid)` が非同期に走るタイミングを見る)
- **`TestExecKillsGrandchildOnCancel` の時間依存**: `sleep` を使い 300ms で cancel して 2 秒待つ。
  CI の負荷で flaky にならないか ([`avoid-wall-clock-assertions.md`](../_claude/rules/avoid-wall-clock-assertions.md))。
  回数 / 状態で判定する形に書き直せるか
- **`catalogN+1` の容量計算と `Reuse`**: 再利用でも `OnResult` は 1 回呼ばれるか
  (`scan.go` の goroutine は再利用時も `OnResult` を呼ぶ経路か)。呼ばれる回数が容量を超えると詰まる

## 走らせ方

- **read-only のサブエージェント 1 体を単独で**。並行させない (163 で 6 体並行が全滅した)
- **「壊す手順を見つけろ。壊せなければ壊せなかったと明記しろ」**で投げる (追認を返させない)
- テストを足して試すなら**使い捨て worktree**。共有 working tree は編集しない
- 実機で TUI を起こすのはコストが高いので、コード読解と `go test -race -count=N` で決着させる。
  どうしても要るなら隔離 tmux サーバ (`tmux -L <name>`。ソケット未指定の kill は hook が deny する)
- 報告は 2000 字以内

## 再提出しないもの

163 の「## 前回 (2 回目) で「記録に留めた」もの」と「## 結果 (索引)」の却下・壊せなかった攻め口、
[issues/148](next/148-feat-glogx-doctor-disk-diagnosis.md) の「敵対的レビュー 2 回目」の記録済み 5 件。
2026-09-03 に決着した 167-183 の内容も再提出しない。配置はばらけているので
`ls issues/done/1[6-8]* issues/pending/169-* issues/183-*` でタイトルを確認する。

## 受け入れ条件

- [x] 攻め口 6 つそれぞれについて「再現した (手順つき)」か「壊せなかった」が明記されている
- [x] 再現した指摘はトピックごとに issue へ起票する (210 / 211 / 212)
- [x] 却下した指摘と壊せなかった攻め口を、理由つきでこの issue の「結果」節に残す
- [x] 起票した P1 は反証レビューを通す
      → **P1 は 0 件** (再現したのは P2 ×2 / P3 ×1)。条件は空だが、ユーザー指示により
      **3 件とも opus の反証レビューに通した** (結果は各 issue の「レビュー状態」節)

## 結果 (2026-09-03、opus の read-only red team 1 体で走行)

163 の体 3 は 3 回目でようやく成果を残せた。**6 攻め口すべてに決着**が付いた。

### 起票した issue (再現したもの 3 件)

| # | 攻め口 | issue | 要点 |
|---|---|---|---|
| ① | rows とカーソルの index ずれ | **210** (P2) | disk 行は **Size 降順に並べ替えて**描くのに `cursor` は index 保持。走査中に大きい結果が届くと**選択が別エントリへ黙って移り**、`y`/`Y` が別の行をコピーする |
| ④ | Setpgid と割り込み | **211** (P2) | `cancel()` は同期的に殺さない (watchdog goroutine が `Kill(-pgid)`)。`cancelAll()` の次の行が `syscall.Exec` なので**プロセス像ごと消えて孫が孤児化**する。実測 3/3: cancel 直後 alive=true / 2 秒後 alive=false |
| ⑤ | 時間依存のテスト | **212** (P3) | `TestExecKillsGrandchildOnCancel` は「marker が無いこと」で判定するため、**孫が fork される前に cancel が届くと同じ緑**。⚠️ red team は「機構が壊れていても緑」と報告したが、**反証レビューがこれを否定**した (下記) |

### 壊せなかった攻め口 (3 件。次の監査で再生成しないための記録)

- **② `expanded` のキーのずれ**: キーは `disk:<ID>` / `diskfail:<ID>:<f>` /
  `brew:<i>:<summary>` で **ID・本文ベース**。並べ替えでキーは動かないので別行は開かない。
  `start` の `expanded = map[string]bool{}` (`doctor_view.go:131`) が毎回リセットするので
  世代を越えたずれも無い (`r` で展開が畳まれるのは仕様寄り。P3 としても起票しない)
- **③ 旧世代のチャネルを新 gen で待つ**: `waitDiskCmd` は Cmd 構築時に `v.diskCh` を捕まえ、
  `diskCh` の代入は必ず `gen++` と同じ `start` 本体にある。snapshot 復元は代入前に早期 return
  するので、**gen が進んでも旧チャネルへの待ち受けは張られない**。旧世代イベントは
  `receiveDisk` の gen 不一致で捨てられ、再アームもしない。**初回 nil で `waitDiskCmd` に
  入る経路も無い** (gen 一致条件が start 後にしか成立しない)
- **⑥ `catalogN+1` の容量と Reuse**: `scan.go` の `OnResult` は Reuse でも ctx キャンセル済みでも
  **無条件に 1 回**。実測 (plain / reuse / cancelled / reuse+cancelled) すべて
  `OnResult 3 回 = カタログ数`。送信は catalogN + 完了 1 = 容量ぴったりで溢れない。
  `Catalog` が非 nil の空スライスなら `catalogN` が**過大**になるだけで、undersize 側の経路は無い

### 訂正 (issue 205 本文の攻め口①に書いていた前提)

「bubbletea は Update → View の順だが、Msg 2 つが連続すると View を挟まない」は**誤り**。
bubbletea v2.0.9 は **Update ごとに `p.render(model)` を呼ぶ** (`tea.go:886`)。したがって
`rows` は常に最新で、壊れるのは「ユーザーが見た行」と「index が今指す行」のずれの方
(210 に訂正を書いた)。

### 反証レビュー (opus) で訂正した 3 点

**起票の質を保つために回した反証が、red team の指摘 1 件を実際に崩した。**

- **⑤ は「false green」ではなかった**。production 側の変異 (`Kill(-pgid)` → `Kill(pid)` /
  `Setpgid` の削除) を当てると**テストは FAIL する** (私も再現: baseline `ok 2.491s` →
  変異で `FAIL`)。red team が緑にしたのは**テスト側の遅延を 0 にした**結果で、
  「機構が壊れても緑」の証明ではない = **変異の当て方が偽物だった**
  (`mutation-verify-new-tests.md` の「変異は production の機構を戻す形にする」)。
  212 は「タイミング依存で vacuous になりうる」に書き直した (P3 は据え置き)
- **① の実害は disk セクションに留まらない**。`buildRows` は disk → svc → brew の順なので、
  走査中の disk 行の挿入は **svc / brew に置いたカーソルも**ずらす
- **④ は「孤児」ではなく「ゾンビ」**。`syscall.Exec` は pid を保つので子は新しいプロセス像の
  子のまま。さらに restart 経路は `defer waitPullCleanup()` も走らせないので、同じ窓は
  **pull の後始末にも開いている**。直し場所は `cancelAll` の後段ではなく
  `cancelAll()` と `restartSelf()` の間。`usageOv.stop()` / `actModal.stop()` は
  **同型ではない** (Setpgid を持たず、既に latch がある) ので、ぼやきの推測も否定された

### 併せて記録

- `cancelAll` は quitNow / quit / restart / defer の**全経路から呼ばれている**。
  「呼ばれない経路」は探して見つからなかった (問題は呼ぶかどうかではなく、戻ってから
  殺されるまでの窓)
- ぼやき (未確認): 211 を直すなら `usageOv.stop()` / `actModal.stop()` も同じ非同期 kill なので、
  看取りは doctor 専用ではなく `cancelAll` の後段に 1 箇所置く形が素直そう
