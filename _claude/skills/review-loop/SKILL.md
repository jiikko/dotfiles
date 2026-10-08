---
name: review-loop
version: 1.3.0
description: make review / make review-close があるプロジェクトで、Codex の PR レビューと修正を最大5周回す。「レビューループ」「review-loop」「make review で回して」で発火。各レビューを対象 commit と対応付け、根拠を確認して修正し、完了後にクローズする。
---

# Review Loop

`make review` → Codex レビュー → 指摘の検閲・修正 → 再レビュー → `make review-close`。

## 前提条件

- `make review` / `make review-close` が定義され、GitHub に Codex が設定されている。
- origin/master に未 push の対象コミットがある。
- 設定された Codex bot の login を確認する (通常 `chatgpt-codex-connector[bot]`)。
  他の投稿者や自分の返信を Codex の結果として数えない。

## 1. 初回レビューの記録

`make review` の直前に UTC の開始時刻を控え、実行後に PR URL・番号・head SHA を記録する。
既存 PR を再利用する target なら、開始前に既存コメント・レビュー ID も控える。
記録は `./tmp/review-loop.<一意な stamp>.json` に保存し、呼び出し間はその絶対パスを使う。

```bash
date -u +%Y-%m-%dT%H:%M:%SZ
make review
gh api repos/{owner}/{repo}/pulls/{number} --jq '{head_sha: .head.sha, head_ref: .head.ref, base_ref: .base.ref}'
```

この組を **今回のレビュー要求** とする: PR / target SHA / requested_at / 既存の review・comment ID / bot login。
初回の自動レビューが起動しない場合は、記録を作ってから手順4の明示依頼を行う。

## 2. 対象 SHA のレビューを待つ

10分間隔で確認する。CronCreate 等が使えれば使い、無ければ待機機能で代替する。
以下は候補の取得例。複数ページを取りこぼさない。

```bash
# SHA にひも付く review 記録 (完了判定の第一候補)
gh api --paginate repos/{owner}/{repo}/pulls/{number}/reviews \
  --jq '.[] | {id, author: .user.login, commit_id, submitted_at, state, body}'

# コード上の指摘 (対象 commit_id と今回の要求時刻・既存 ID で絞る)
gh api --paginate repos/{owner}/{repo}/pulls/{number}/comments \
  --jq '.[] | {id, author: .user.login, commit_id, original_commit_id, created_at, path, line, body}'

# 全体コメント (commit_id が無いので、これ単独では完了を判定しない)
gh api --paginate repos/{owner}/{repo}/issues/{number}/comments \
  --jq '.[] | {id, author: .user.login, created_at, body}'
```

採用する結果は **bot の一致 + 要求以降の時刻 + 既存 ID ではない + 対象 SHA の一致** を満たすもの。
秒単位の時刻だけで同じ秒の新規結果を落とさず、ID の記録も使う。
完了前に PR の現在の head SHA を再取得する。対象 SHA と違えば、旧レビューを現行差分の合格根拠にせず新しい要求を作る。

全体コメントの「Didn't find any major issues」だけを拾って閉じない。
SHA 付き review 記録が無い場合は、その環境で Codex が使う完了通知と target SHA の対応を確認する。
通知に対応付けの根拠が無い場合は **完了未確認** として報告する。別 bot の check 成功や単なるコメント不在で代用しない。

## 3. 指摘を検閲して対応する

各指摘は `codex-review` の「敵対的レビューの作法」に従い、コード・契約・再現で裏取りする。

- **採用した P1/P2**: 修正 → プロジェクトの必要なテスト → コミット → レビューブランチに push → 同じスレッドへ返信。
- **誤検出**: 到達不能・契約との不一致など、却下根拠を返信する。重大度だけで修正しない。
- **確定できない指摘**: 未確認として観測・issue 化する。再現しないことだけを却下根拠にしない。
- **P3 / スコープ外**: 採用、認識のみ、別 issue のいずれかを判断し理由を書く。

```bash
git push origin HEAD:review/{branch-name}
# コードコメントへの返信。body は一時ファイルから渡す。
gh api repos/{owner}/{repo}/pulls/{number}/comments -X POST \
  -F body=@{reply_body_file} -F in_reply_to={comment_id}
```

返信には変更内容と検証結果 (テスト名・実行結果・未確認範囲) を含める。
推測に合わせた防御コードや、誤った期待値に合わせた修正を足さない。

## 4. 再レビューを依頼する

修正 push 後、PR の head SHA が修正コミットと一致することを確認する。
新しい target SHA・既存 review/comment ID を控え、依頼直前の UTC 時刻を記録する。
新しい要求レコードを保存してからコメントする。修正 push だけで再レビューが走るとは仮定しない。

依頼文はファイルに書き、次の形で投稿する:

```bash
gh pr comment {number} --body-file {request_body_file}
```

依頼文の例:

> @codex review for 対象 head は {target_sha}。前回の修正が効いていない別経路・境界・並行・部分失敗を探してください。
> 根拠と発火条件を示し、修正が生んだ回帰も確認してください。確定できない懸念は未確認として分けてください。

head が実行中に変われば、旧 SHA の結果を今回の要求の完了として数えない。

## 5. ループ判定

- 今回の要求に対応する新しい指摘がある → 手順3。
- **今回の target SHA に対応するレビューが完了し、採用した重要指摘が解消済み** → 手順6。
  その際、再発を検知するテスト・型・設計上の根拠と未確認範囲を確認する。「指摘ゼロ」だけを安全の証明にしない。
- 依頼から30分たっても対応する完了結果が無い → 状況と未確認の SHA を報告して判断を仰ぐ。
- 最大5周。収束しなければ残指摘・確認済みの根拠・未確認範囲を報告する。完了未確認のまま master に push しない。

## 6. クローズと後始末

```bash
git push origin master && make review-close
```

失敗時は PR 状態・ブランチ・残差分を確認してから復旧する。掃除のために `review/*` 全体を削除しない。
削除するのは今回作成したブランチだけで、未移送のコミットが無いことを先に確認する。
監視ジョブを停止し、今回の一意な一時ファイルだけを削除する。

## ルール

- レビュー中の修正はレビューブランチにだけ push する。master push と review-close は手順5の完了条件を満たした後。
  review-loop を依頼した時点でこの規定の push・返信・再レビュー依頼は含意される。無関係な外部投稿は行わない。
- 修正コミットに対応する issue 番号を含める。issue はリポジトリの規約に従う markdown ファイルで管理する。
- 必要な lint / テストが実際に完了したことを確認してから push する。
- bot の主張や重要度を無検閲で採用しない。合意数や繰り返し回数を正しさの証明にしない。
