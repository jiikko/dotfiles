# 205 audit: doctor 画面の並行・中断・状態機械を red team で攻める (163 の体 3。唯一の未走行)

起票日: 2026-09-03
出典: [issues/163](163-audit-doctor-implementation-red-team.md) の「体 3」(6 観点のうちこれだけ成果ゼロ)
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
  - ⚠️ **2026-09-03 に周辺が動いている**: issue 180 で Failures 行と Undiagnosed 行を selectable にし、
    182 でラベル列を幅に応じて縮める形にした。**選べる行の数と順序が変わったので、当時の攻め口は
    そのまま成立しない**。まず現状のコードを読み直すこと
- **`expanded` のキーのずれ**: brew は `brew:<i>:<summary>`、disk は `disk:<ID>`。再スキャンで順序が
  変わると展開状態がずれる / 別の行が開く。`start` が `map{}` で作り直しているのは意図か
- **nil channel の永久ブロック**: `start(force=false)` で snapshot 復元したとき `diskCh` は nil。
  `waitDiskCmd` が nil channel を読むと**永久にブロック**する。gen 一致で呼ばれる経路が
  無いことを証明するか、あるなら再現する
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
2026-09-03 に done へ入った 167-182 の内容も再提出しない (`ls issues/done/1[6-8]*` でタイトルを確認)。

## 受け入れ条件

- [ ] 攻め口 6 つそれぞれについて「再現した (手順つき)」か「壊せなかった」が明記されている
- [ ] 再現した指摘はトピックごとに issue へ起票する (`NNN-<カテゴリ>-doctor-<スラッグ>.md`)
- [ ] 却下した指摘と壊せなかった攻め口を、理由つきでこの issue の「結果」節に残す
- [ ] 起票した P1 は反証レビューを通す

## 結果 (走らせた後にここへ書く)

- 起票した issue:
- 却下 (理由つき):
- 壊せなかった攻め口:
