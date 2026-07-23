# 019 feat(glogx): CI 失敗 job の再実行キー (`r`)

## 背景

CI が ✗ のとき、現状は「glogx で失敗を見る → ブラウザで Actions を開いて Re-run → glogx に戻って眺める」の往復が必要。glogx は既に write 操作 (`b` push / `u` pull) と push 後の CI ポーリング機構を持つため、再実行も TUI 内 1 キーで完結させたい (2026-07-23 ユーザー承認)。

## やること

- job パネル / job 詳細ポップアップで、フォーカス中の **失敗 job** に `r` キーを割り当てる
- 実行コマンドは `gh run rerun --job <CheckID> -R <owner>/<repo>` (CheckRun の `databaseId` = Actions job id。既に `CheckDetail.CheckID` に保持済み)
- push/pull と同じ **y/N 確認モーダル** (actionModal) を経由する。write 操作の語彙を揃える
- 成功したらトースト通知 + その SHA のパネルリフレッシュ。job が pending になれば既存の `panelPollMsg` 定期リフレッシュが自然に追従する
- ガード:
  - `CheckID == 0` (StatusContext = 外部 CI) → 「GitHub Actions の job ではないため再実行できません」を notice
  - 失敗状態 (`StateFailure`) 以外の job → 再実行対象外の旨を notice (成功 job の rerun は誤爆リスクの方が大きい)
- テスト: `CommandRunner`/差し替え点の fake で `gh run rerun` の引数と、確認 → 実行 → トースト → リフレッシュの状態遷移を検証。実 API は叩かない

## 設計メモ

- rerun 直後の GraphQL は古い conclusion を返す期間がある。push ポーリングと同様「pending が見えるまで数回取り直す」までは初回はやらず、panelPoll (3s) の既存追従に任せる。体感が悪ければ後続で pushPoll 相当を足す
- `gh run rerun <run-id> --failed` (run 単位) でなく job 単位を採用: パネルのフォーカス単位が job であり、run id は現状保持していない (追加取得が要る)

## 関連

- README「未対応」リストには無い新規提案 (2026-07-23 の会話由来)
- [020](020-feat-glogx-copy-failure-context.md) と合わせて「✗ → TUI 内で完結」のループを閉じる
