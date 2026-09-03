# overlay が「自分の語彙を持っている / 中断できない処理を走らせている」を主張する場所が散っている

種別: design
起票: 2026-09-03 (issue 148 段階 ④ S2 の敵対的レビューから。**起票時に opus の反証レビューを 1 周通した**)

## 何が起きたか

④ S2 で doctor に削除の導線を足したとき、「削除の実行中はキーを飲む」という状態を
`doctorView` の中に正しく実装した。しかし `browseModel` 側でその状態を見る場所が散っていて、
**3 箇所を直し忘れた**。opus の敵対的レビュー 2 体が、独立に同じ P1 として実測してきた:

| 直し忘れた場所 | 実測された症状 | 決着 |
|---|---|---|
| `tui.go:handleKey` の `ctrl+c` / `ctrl+g` の switch | 削除の実行中に **1 回目の Ctrl-C でアプリごと落ちる**。中断が ctx を通らないので記録が `executing` のまま残り、`cli:` の子が孤児になる | `9e3623b9` で修正 |
| `tui.go:restartPromptVisible` | 裏ビルドが完成していると削除中に再起動ダイアログが最前面へ出る。どのキーもそちらに吸われ、`r` は `syscall.Exec` でプロセスを置き換える | `9e3623b9` で修正 |
| `tui.go:cancelAll` → `doctorView.stop()` | 終了・再起動で削除の ctx を切らない (走査の ctx しか切っていなかった) | `9e3623b9` で修正 |

さらに**起票時のレビューが 4 つ目**を実測で出した: `updateKeyReachable` の doctor 節は
**直っていたが、テストが 1 本も守っていなかった** (節を消す変異が全テスト green を通った)。
→ `TestUpdateKeysYieldToDoctorDelete` を足して塞いだ (状態 × キーの直積。変異で red を確認)。

## 何が構造的な問題か

同じ問いが**別の式で複数箇所**に書かれている (2026-09-03 実測)。

**`ownsKeys()` を実装しているのは 3 つ** (`doctorView` / `issuesView` / `statusView`)。
その参照は **5 箇所**:

| file:symbol | 何を聞いているか |
|---|---|
| `tui.go:updateKeyReachable` | 3 overlay を**揃って**見る (ここだけ) |
| `tui.go:handleKey` の issues routing | `issuesOv.ownsKeys()` — U の横取りを譲るか |
| `tui.go:handleKey` の status routing | `statusOv.ownsKeys()` — U / p / b / u の横取りを譲るか |
| `tui.go:handleKey` の `ctrl+c` switch | `actModal` の 3 状態と `doctorOv.del.blocking()` |
| `tui.go:restartPromptVisible` | `actModal.active()` と `doctorOv.del.blocking()` |

加えて overlay を**列挙している**場所が 2 つ (述語ではないが「新しい overlay を足したら直す場所」):

- `tui.go:cancelAll` — `usageOv` / `actModal` / `issuesOv` / `doctorOv` を止める。
  ⚠️ **`statusOv` は 1 つも止めていない** (`statusView.fetchDiff` の `git diff` が終了時に残る)
- `tui.go` の全画面判定 (`statusOv.visible() || rlDash.visible() || doctorOv.visible()`) —
  「全画面は同時に 1 枚」という**第 3 の軸**

### 🚨 `actModal` が既に答えを持っている

`action_modal.go` の **`active()` / `running()`** が、この issue が作ろうとしている 2 つの述語
そのもの。doc コメントは「`active()` は描画とキー消費を兼ねる 1 つの述語」「`running()` は
中断すると壊れる処理」と明示し、**2 つに分けるな**とまで書いてある。さらに
`action_modal_test.go` の `TestActionModalActiveMatchesHandleKey` が
**state × key の直積**で `active()` と `handleKey` の consumed の一致を pin している。

したがって:

- `ctrl+c` と `restartPromptVisible` の**主項は overlay ではなく `actModal`**。
  overlay 3 つだけを列挙する設計は、いちばん状態の多い参加者を落とす
- **新しい語彙を発明しない**。`active()` / `running()` へ overlay 側を寄せる方が、
  doc も一致テストの前例も再利用できる

### 2 つの軸を混ぜない

- **(a) 自分の語彙を持っているか** — 5 箇所すべてに関係する
- **(b) 中断できない処理を走らせているか** — `ctrl+c` / `restartPromptVisible` に関係する

`issuesOv` / `statusOv` は (a) は持つが (b) は持たない。⚠️ **理由は「壊れないから」ではない**:
`statusView` の破壊的操作 (`runGitRestoreWorktree` / `runGitCleanUntracked`) は
**`Update` の中で同期に**走るので、そもそも「実行中」という相を跨がない。
(非同期は `fetchDiff` があり、そちらは読み取り専用。ただし上記のとおり `cancelAll` に居ない。)
**status に非同期の破壊的操作を足した瞬間に (b) の側へ移る**ので、
「壊れないから (b) を持たない」と書くと次の人が同じ穴を掘る。

## 対応案 (どれを採るかは着手時に判断する)

1. **`actModal` の語彙へ寄せる**。overlay 側に `active()` / `running()` 相当を持たせ、
   `browseModel` は「いま語彙を持っている参加者」「いま中断できない参加者」を 1 箇所で数える
2. **`reflect` で自動列挙する** (起票時レビューの提案)。`browseModel` のフィールドを走査し、
   `reflect.PointerTo(f.Type)` が `interface{ ownsKeys() bool }` を満たす型を集めて、
   挙動テーブルに載っていることをテストで要求する。同一 package なら非公開メソッドの
   interface で実装判定できる。**登録漏れが red になる**
3. **AST 検査**。`ownsKeys()` を実装した型が、参照すべき全経路から参照されていることを
   `parser.ParseDir` で確かめる (前例: `clock_rollback_test.go` / `vs16_literal_test.go`)

⚠️ **interface を足しても「新しい overlay を足したら compile error」は原理的に満たせない**
(リストに書かなければ何も起きない)。compile error になるのは「リストに書いたのにメソッドが無い」
場合だけで、防ぎたい失敗モードではない。値フィールドである点自体は障害にならない
(全メソッドがポインタレシーバなので `&m.field` で満たす。alloc 予算はキー経路を測っていない)。

⚠️ `~/.claude/rules/verify-design-intent-before-refactor.md`: **集約は複雑性が実際に下がる
ときだけ**。「複数箇所を 1 箇所にした」だけで (a)(b) の区別が消えると悪化する。

## 受け入れ条件

- [x] (a) 語彙の所有と (b) 中断できない処理の 2 軸が、**`actModal` を含めた**参加者ごとに
      表で書かれている → `src/glogx/overlay_ownership_test.go` の `overlayOwnershipTable`
      (4 参加者 × owns / uninterruptible / 理由 / cancelAll が止めるか)
- [x] 参加者に新しい状態を足したとき、**参照すべき経路のどれかを直し忘れたら気づける**
      → 表の主張と実装の一致を `browseModel` 経由で pin する 2 本 +
      **reflect で `ownsKeys()` の実装を数え上げ、表に無ければ red** にする 1 本 +
      「(b) を持たないなら理由が空でない」1 本
- [x] その仕掛けが効くことを**変異で確認**した (5 本すべて red。適用の証拠つき):
      `updateKeyReachable` の doctor / issues / status 節をそれぞれ外す /
      `ctrl+c` の switch から doctor 節を外す / `restartPromptVisible` から doctor 節を外す
- [x] **悪化を作らない**: **production は 1 行も変えていない** (検査を足しただけ)。
      したがって issues / status の Ctrl-C と `restartPromptVisible` の挙動は今と同一

## 発火条件 (再発したときの見え方)

新しい overlay に「実行中はキーを飲む状態」を足し、参照経路の一部だけを直した場合。
症状は「実行中に Ctrl-C を押すとアプリが落ちる」「実行中に再起動ダイアログが出る」
「終了しても子プロセスが残る」。⚠️ どれも**テストが overlay の `handleKey` を直叩きしていると
検出できない**。回帰テストは `browseModel.handleKey` 経由で書く。

具体的に次に踏むのは **status に非同期の破壊的操作 (進捗つきの discard 等) を足したとき**。
`updateKeyReachable` は既に status を見ているので通ってしまい、`ctrl+c` /
`restartPromptVisible` / `cancelAll` (statusOv の stop が存在しない) の 3 つを落とす。

## 出典

- issue 148 段階 ④ S2 の敵対的レビュー (opus 2 体、2026-09-03)。`9e3623b9` が doctor のぶんを
  塞いだが、**構造は残っている**
- 本 issue の反証レビュー (opus 1 体、2026-09-03)。5 件の事実誤認を訂正し、
  `updateKeyReachable` の未防備を実測で出した (その場で `TestUpdateKeysYieldToDoctorDelete` を追加)
- `issues/209-retro-doctor-delete-engine-2026-09-03.md` 項目 7

## 適用ログ (2026-09-03)

### 採った案: 「1 (actModal の語彙へ寄せる)」ではなく **検査だけを足す**

対応案 1 (overlay 側に `active()` / `running()` 相当を持たせて `browseModel` で 1 箇所に数える) は
**採らなかった**。理由は 2 つ。

- ⚠️ `verify-design-intent-before-refactor.md`: 集約は複雑性が実際に下がるときだけ。
  5 つの参照経路は**聞いている問いが違う** (`updateKeyReachable` は「X を横取りするか」、
  `ctrl+c` は「1 回目を飲むか」、`restartPromptVisible` は「ダイアログを出すか」、
  `cancelAll` は「止めるか」)。1 つの述語に畳むと (a)(b) の区別が消え、issue 本文が
  警告している悪化になる
- 受け入れ条件が要求しているのは「**直し忘れたら気づける**」であって「1 箇所にする」ではない。
  production を触らなければ**悪化を作らないことが自明**になる (条件 4 が無条件で満たされる)

### 入れたもの (`src/glogx/overlay_ownership_test.go`)

- `overlayOwnershipTable`: 参加者 4 つ (`actModal` / `doctorOv` / `issuesOv` / `statusOv`) の
  2 軸と、(b) を持たない理由、`cancelAll` が止めるか
- `TestOverlayOwnsKeysMatchesUpdateKeyReachable`: (a) の主張と `updateKeyReachable` の一致。
  **`browseModel` 経由**で見る (overlay の handleKey 直叩きでは経路の落ちを検出できない)
- `TestOverlayUninterruptibleMatchesCtrlCAndRestartPrompt`: (b) の主張と `ctrl+c` /
  `restartPromptVisible` の一致
- `TestOverlayOwnershipTableCoversAllParticipants`: **reflect で `browseModel` のフィールドを
  走査**し、`ownsKeys()` を実装した型が表に在ることを要求する。逆向き (表にあって実体が無い) も見る
- `TestOverlayNonUninterruptibleHasReason`: (b) を持たない参加者に理由が書かれていること

状態の作り方は `showOverlayOwning` / `showOverlayUninterruptible` に集約した
(各テストで別々に作ると、片方だけ実装に追従して「別の状態を検査している」ことに気づけない)。

### 表に記録した既知の穴 (このセッションでは直さない)

- **`cancelAll` が `statusOv` を止めていない** (`stoppedByCancelAll: false`)。
  `statusView.fetchDiff` の `git diff` が終了時に残りうる。`statusView` に `stop()` が無いので
  実装が要る = 別 issue の範囲。表に false として**明示**したので、次に status へ非同期を足す人が
  ここを見る

### 着手時に見つけた障害物

`TestUpdateKeysYieldToDoctorDelete` が HEAD で赤かった。原因は 213 ではなく
**`doctor/disk` のデータ競合** (テスト専用カウンタ `collectVisits` が package 変数)。
issue 214 として起票し、`atomic.Int64` へ直して並行走査の回帰テストを足した (`f3bd400a`)。
これを先に潰さないと 213 の変異検証が「赤の中の赤」で読めなかった。
