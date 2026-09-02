# commit は自分が触ったファイルを pathspec で明示する（並行セッションの混入防止） — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/commit-with-pathspec.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## なぜ

同一 repo・同一 working tree で複数の Claude セッションが並行することがある。git の index（staging area）は working tree に 1 つしかないため、pathspec なしの `git commit` は**他セッションが `git add` 済みの変更を自分のコミットに混入させる**。commit コマンド同士の衝突は git 自身の `index.lock` が直列化してくれるので、混入さえ防げばロックや worktree 分離なしで安全に並行できる。

`git commit -- <pathspec>` は index の状態に関係なく指定ファイルの working tree 内容だけをコミットするため、「変更範囲が被らない」前提が守られている限り混入が構造的に起きない。

## ルール本文から移した実例

本文には規範だけを残し、その根拠になった実例をここへ移した（元の文脈のまま）。

手元は再生成済みで build が通るため気づけない
  (実例 2026-08-11: `APILogging.swift` を `git rm` して `xcodegen generate` したが、pathspec に
  `project.pbxproj` を入れ忘れた。commit された tree は「ファイルは無いが pbxproj は参照したまま」で、
  checkout すればビルド不能。手元は再生成済みなので build/test/lint すべて green だった)

- 実例 (2026-08-21): issue を `issues/done/` へ移す commit で新パスだけを指定し、旧パスの削除が
  取り残された。commit は成功し、`git show --stat` にも新パスの追加だけが載るため、
  **成功した commit の stat を見ても漏れに気づけない** (「無いもの」は stat に出ない)

- 実例 (2026-08-11): 「lint \`applog_via_service_locator\` が \`fixture.live.appLog\` を誤爆」が
  「lint  が  を誤爆」になった。commit は成功しており、`git log` を読み返すまで気づかない

- 実例 (2026-07-16): 並行セッションの `reset HEAD~1` が、直前に積まれていた別セッションのコミット (issue rename の参照更新 14 ファイル) を切り落とし staged に戻した。reflog と `git diff --cached <旧SHA>` の同一性検証で復旧できたが、気づかなければ次の pathspec なし commit に溶けて消えていた

## cwd 相対の pathspec で空振りした実例 (2026-09-02, retro 164 項目 2)

doctor ③ の実装中、`cd src/glogx` した状態から `git add src/doctor/...` /
`git commit -- src/...` を打ち、**同じセッションで 2 回**空振りさせた。

git はエラーを出していた (実測: commit は rc=1 + `error: pathspec '...' did not match any
file(s) known to git`、add は rc=128)。見落としたのはエラーそのものではなく、
**その後の `git push` が `Everything up-to-date` で rc=0 を返した**ことで
「commit も push も成功した」と読んでしまった点。気づいたのは hook が出す git state 表示。

だから対策は「エラーを見る」ではなく **cwd を repo root に固定する**方 (エラーは既に出ていた)。
ツールのシェルは cwd を持ち越すので、サブディレクトリでテストを走らせた直後が一番危ない。
