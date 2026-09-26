# new (research): PG の再開がキャッシュを外す残りの原因 (system prompt の EndConversation の段が gate の起動時の値で揺れる)

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

525 で `SendFeedback` は設定 (`feedbackDrafts: "off"`) で起動・再開とも外したが、実測で再開はまだ外れた
(525 の「進捗」の実測 2・3: tools は揃っていたのに、再開の最初の要求で `EndConversation` が足され、読み 26,318 で会話全体を書き直した)。
`EndConversation` の案内の段 (system prompt の末尾近く) は、claude 2.1.283 の実体では GrowthBook の gate `tengu_umber_kestrel` と ToolSearch の有効判定だけで決まり、
設定・環境変数で外す経路が無い。

- gate の起動時の値は `~/.claude.json` の `cachedGrowthBookFeatures` (全プロセスが共有) から来て、起動後の取得で変わると推定。
  2026-09-27 08:05 ごろに見ると `tengu_juniper_relay` / `tengu_umber_kestrel` とも false で、`cachedGrowthBookFeaturesAt` は数秒おきに書き換わっていた。
  それでも PG の起動の 3/4 は両方在りの側で起動している (449)。**誰がいつ false / true を書くかは未確認**
- 仮説 (未検証): pro-con が再開の直前に打つ `claude stop` や、haiku の `-p` (要約役・btw) など、属性の違う claude のプロセスがキャッシュを書き換え、
  直後に起動・再開した PG がその値で system prompt を組む

## 調べること

- `claude stop` / `claude agents` / haiku の `-p` を打つ前後で `cachedGrowthBookFeatures` の 2 つの gate と `cachedGrowthBookFeaturesAt` がどう変わるか (LLM を呼ばないものから)
- PG の起動・再開の直前のキャッシュの値と、そのプロセスの `prompt_snapshot` の `EndConversation` の段の有無が一致するか
- 揃える手があるか: 例: 再開の前にキャッシュの値を起動時と同じ側に寄せる / 起動と再開の間に別の claude を打たない順にする。
  gate の値を書き換える形は Claude Code の内部に依存するので、採るなら版が変わったら確かめ直す前提を書く

## 損の大きさ (525 の実測 4)

2.1.283 の再開で、起動時に両方在りの session の 74 回中 23 回が外れ (書き直し 25k〜244k)。449 の見積もりでは 2026-09-25 の PG の書き込みの約 36%

## 関連

- 525 (tools の並びを揃える。`SendFeedback` はこれで外した) / 449 (数え方と根拠)
