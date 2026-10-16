# 779 (bug): pro-con の presence が screens/ の FIFO の印 1 つで画面の数え上げ全体を失敗させる

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-16〜）

起票日: 2026-10-16

## 概要

`pro-con/presence/presence.go` の `scan` は `screens/` の `<id>.lock` を `heldBySomeone` で開いて flock を試し、エラーが返るとそのまま返す。
`<id>.lock` が FIFO などの普通のファイルでないと、`os.OpenFile(O_RDWR)` は通るが `flock` が EOPNOTSUPP で失敗し、`scan` (= `Count` / `Leave` の数え直し) 全体がエラーになる。
issue 768 の敵対的レビューで見つかった前からの穴 (768 で作りかけ (`.tmp-*`) の掃除は普通のファイルに限ったが、印そのものは見ていない)。

## 詳細

- `heldBySomeone` (presence.go): `os.OpenFile(p, os.O_RDWR, 0)` → `syscall.Flock(LOCK_EX|LOCK_NB)`。EWOULDBLOCK 以外のエラーはそのまま返る
- `scan` は `heldBySomeone` のエラーで `return nil, err` する (1 つの壊れた印で全部の画面が数えられない)
- relay 側は `List` で「普通のファイルでない印は開かずに飛ばす」を持ち、`TestListSkipsNonRegularLock` が固定している。presence には同じ守りもテストも無い
- 実際に FIFO の印ができる経路は見つかっていない (誰かが手で置く・壊れた fs)。起きると最後の持ち主の判定ができず、dispatcher と PG の止め時を誤りうる

## 対応方針

- `scan` で `e.Type().IsRegular()` でない `<id>.lock` を開かずに飛ばす (relay の List と同じ)。テストを relay の `TestListSkipsNonRegularLock` に倣って足す
- 768 のレビューの P3: presence の作りかけの掃除 (`heldBySomeone(.tmp-*)` の呼び出し) にもテストが無い。同じ変更で足す

## 関連ファイル

- `src/pro-con/presence/presence.go` (`scan` / `heldBySomeone`) / `src/pro-con/relay/relay.go` (`List`) / issue 768 (done)

## 進捗

- [x] `scan` で普通のファイルでない `<id>.lock` を開かずに飛ばす (relay の `List` と同じ)。テスト `TestScanSkipsNonRegularLock` (FIFO の印があっても数えられ、`Leave` が最後の持ち主を判定できる)
- [x] 作りかけ (`.tmp-*`) の掃除のテスト `TestScanCleansOnlyStaleUnheldTmp` (古くて誰も持っていないものだけ消し、新しいもの・持たれているものは残す。数えない)
- 変異で red を確認 (`mutate-verify`): IsRegular の飛ばしを外す → `TestScanSkipsNonRegularLock` が `operation not supported` で red / 作りかけの `heldBySomeone` 呼び出しを消す → `TestScanCleansOnlyStaleUnheldTmp` red / `IsStaleTmp` を常に真にする → 同 red (と既存の `TestOpenIsAtomicAgainstConcurrentCount`)
- 範囲の外: 普通のファイルで flock が EWOULDBLOCK 以外 (権限など) で失敗する印は、今までどおり数え上げの誤りとして返す (最後の持ち主の判定を誤るより、数えられないと返す方を残した)
