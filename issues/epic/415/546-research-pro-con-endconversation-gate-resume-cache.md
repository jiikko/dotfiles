# 546 (research): PG の再開がキャッシュを外す残りの原因 (system prompt の EndConversation の段が gate の起動時の値で揺れる)

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

## 進捗 (2026-09-27、dotfiles-c2。ユーザーの依頼「2 段目までお願いします」)

### 分かったこと

1. **gate は環境変数 `DISABLE_GROWTHBOOK` で丸ごと既定値に倒せる** (claude 2.1.283 の実体: `isEnabled = !DISABLE_GROWTHBOOK && …`。無効なら
   `getFeatureValueWithSource` は既定値を返し、`~/.claude.json` の `cachedGrowthBookFeatures` も読まない)。値の出どころは「起動後に取った値 (payload) > disk のキャッシュ」の順で、
   環境変数の上書き (`getEnvironmentOverrides`) は配布版では常に null (効かない)。
   `--settings` の `env` に `DISABLE_GROWTHBOOK: "1"` を入れると、起動・再開 (コピー) とも `prompt_snapshot` に `EndConversation` も `SendFeedback` も出なかった (実測)
2. 🚨 **ところが、gate を揃えても再開はキャッシュを外した。`EndConversation` の段は外れの原因ではない** (本物の claude の A-B。本番の PG と同じ `-w` の worktree・同じ引数):

   | | 読み | 書き |
   |---|---|---|
   | 起動 (今の PG と同じ設定。`EndConversation` の段あり) | 24,803 | 21,595 |
   | 再開 A (設定を変えない = 段あり) | 26,396 | 20,045 (外れ) |
   | 再開 B (`DISABLE_GROWTHBOOK` = 段なし) | 26,396 | 20,513 (外れ) |

   `~/dotfiles` (worktree なし) でも同じ形だった: 段なしの組の再開は読み 25,957 / 書き 107,280、段ありの組は 26,393 / 106,963。
   止めた session を flag なしで (同じ session id のまま) 起こしても 26,393 / 106,963 で外れ、直前のコピーが書いたキャッシュにも当たらなかった。
   → 525 の実測 3 (読み 26,318 / 書き 20,349 を「段の違い」と推定) も、この段と関係の無い外れだった見込みが高い
3. 当たっているのは system prompt と tools までの共有部分 (約 2.5 万トークン。どの session でも同じ) だけで、会話の部分 (最初の user のメッセージ以降) が
   **別のプロセスになると必ず書き直されている**。system prompt・tools・`prompt_snapshot` の他の欄は、起動と再開で 1 文字も違わなかった (A-B の 2 つを比べた)。
   食い違いは会話の部分のどこか (最初のメッセージに付く添付の作り直し・cache の区切りの置き方など。**未確認**)

### 採らなかったこと

- `DISABLE_GROWTHBOOK` を PG・PM の `--settings` に入れるのは見送った: 上の 2 のとおり外れは減らない。一方で PG の gate が全部既定値に倒れる副作用がある

### 次に調べること (未着手)

- 本番の再開は大半が当たっている (525 の実測 4: `SendFeedback` 無しの session の再開 42 回のうち外れは 5 回) のに、上の実験では 3 通りとも外れた。
  **実験と本番で何が違うか**を先に詰める: 会話の長さ (実験は 1 往復だけ)、止めてから再開までの時間、再開の直前の出来事など。
  本番の transcript で「当たった再開」と「外れた再開」を並べ、最初の要求の読みの値 (外れはどれも 24,803 前後で止まる) と直前の状況を比べるのが安い (LLM を呼ばない)
- 要求そのもの (API に送った中身) を見られれば 1 回で決まる。見る手段は未確認
- 実験の transcript: `~/.claude/projects/-Users-koji-dotfiles/{31e2a5fc,990fd60b,eded4f72,a9ff553f}-*.jsonl`、
  `~/.claude/projects/-Users-koji-dotfiles--claude-worktrees-gbexp2/{8cdcbfc1,f861ad7b,fd08fa7d}-*.jsonl`
