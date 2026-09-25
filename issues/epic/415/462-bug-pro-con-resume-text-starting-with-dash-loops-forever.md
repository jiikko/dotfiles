# 462 (bug): 回答が「-」で始まると claude がオプションと読み、再開の失敗を上限なく繰り返して枠を占める

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

もろい作りの監査 (460) の観点 2。再開・起動がすぐ失敗する形に回数の上限が無く、カードは分解済みのまま枠を 1 つ占めて回り続ける。

## 詳細

- 該当: `src/pro-con/dispatcher/launcher.go` の `ExecLauncher.Resume` (`--bg --resume <id> --setting-sources project,local <text>` の最後に本文を位置引数で渡す) /
  `store` の answer (`c.Resume = r.Answer` を生のまま) / `dispatch` (失敗 → 印を残す → launchGrace の後に起動し直す。回数の上限が無い)
- 発火条件 (実測): 回答の本文が「-」で始まる (箇条書きの回答)。`claude … "- …"` は `error: unknown option` で rc=1 (監査の係が隔離した cwd の `claude -p` で確認。2.1.282)
- 壊れ方: 再開の前に `claude stop` するので PG は止まったまま。カードは分解済みに居るので回答し直せない (`Answerable` が偽)。印が残ったまま launchGrace ごとに
  同じ失敗を繰り返し、枠を 1 つ占め続ける。落ちた回数の上限 (3c-2a) はこの形には効かない
- 同じループに入る形 (読んだだけ): trust していない repo (415 の実測 rc=1) / 消した worktree への `--resume` (chdir の失敗) / 未ログイン / 464 の古い claude
- 偽物の `Resume` はどんな文でも受けるので、テストから見えない (460 の偽物とのずれ)

## 対応方針 (候補)

- 本文を渡す前に `--` で位置引数を区切るか (claude が `--` を受けるかを先に実測)、固定の前置き (差し戻しの `ReworkPrefix` と同じ形) を付ける
- rc≠0 がすぐ返る起動・再開の失敗を分類し、同じ失敗が N 回続いたら分解済みで回し続けず、人の番 (質問待ち) へ回して理由を書く。偽物に「すぐ失敗する」を足してテストで固定する

## 対応 (2026-09-25 / カード C-017)

- 本文: `ExecLauncher` が位置引数を渡す 1 か所 (`positionalArg`) で、「-」で始まる文に `pro-con から:\n` を前置きする。
  起動・再開・PM の知らせ・追加オーダー (`ordersText` は「- 追記」で始まりうる) のどれから来ても効く。`--` は claude が受けるか未実測なので使わない
- 回数: `runClaude` が「起動できなかった (chdir の失敗・claude が無い)」「時間内に rc≠0 で返った」を `ErrRejected` で包む (何も立っていない)。
  時間切れ・`--bg` の出力が読めないものは立っているかもしれないので含めない。dispatch は `ErrRejected` を `card.Rejects` で数え、
  `launchRejectLimit` (3) 回続いたら印を外して人の番 (WaitCrashed) へ回し、理由 (最後の stderr) を書く。回答すると起動・再開し直す。成功と他の失敗で 0 に戻す
- テスト: PATH の先頭に偽の `claude` (commander と同じく知らない「-」始まりで rc=1) を置いて red を見てから直した
  (`TestResumeTextStartingWithDashIsNotReadAsOption` / `TestLauncherClassifiesRejected`)。回数は `TestRepeatedRejectedLaunchGoesToHuman`。
  変異 6 本 (前置きを外す / 分類を外す / chdir の分類を外す / 上限を外す / 立っているかもしれない失敗も数える / 人の番へ回すときに印を残す) がすべて red (bin/mutate-verify)
- 敵対レビューで直したもの: ①人の番へ回したあとの回答 (store の answer) が、まだ渡せていない差し戻し・テストの結果の文を上書きして消していた
  → 残っていれば前に残して「回答: 」を続ける (`TestRejectedResumeKeepsUndeliveredTextThroughAnswer`) ②ctx の取り消し (dispatcher の終了) も拒否に数えていた
  → ctx が生きているときだけ ③回数の 0 戻しが未検査 → `TestRejectCountResetsOnOtherOutcomes`。この 4 本も変異で red を確かめた (計 10 本)
- `make test` rc=0 (af6a64cf。1 回目は呼び出しの無くなった `Dispatcher.note` を unused で落とした → 消した)
- 分かっていて受けるもの: 起動 (session 無し) の拒否から人の番へ回したときの回答は、きっかけにしかならず本文は PG に渡らない (最初の指示で起動し直す。
  理由の文にそう書く) / 再開の前の `claude stop` が rc≠0 を返し続けた形も拒否に数えるので、前の session が生きたまま人の番へ回りうる (質問待ちで
  idle のまま残るのと同じ形) / `claude --bg` が立ててから rc≠0 で返す形があれば誤判定になる (未実測。人の番で止まるのでループは閉じる)
- 範囲外で残るもの: PM の起動・再開 (pm.go) は数えていない (本文の前置きは効く。記録に PM の行が無いときの起動の拒否は上限なく繰り返すように読める。未確認)

## 関連

- 460 (監査の記録) / 464 (古い claude でも同じループに入る) / 452 (人の番)
