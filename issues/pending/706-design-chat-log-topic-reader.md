# 706 (design): 時系列の会話ログをトピックごとに組み直して読む道具 (設計メモ)

起票日: 2026-10-10
カテゴリ: design / priority: low
出典: ユーザー構想 (2026-10-10 のセッション)
反証レビュー: 済 (2026-10-10。下の「反証レビュー」節)

> **凍結 (pending)**: MVP は作らない (2026-10-10 ユーザー判断)。この issue は設計・経緯・参考資料・実装時のヒントの置き場。
> **再開の条件**: ユーザーが作ると決めたとき。着手する前に「リスク」節の規約と社内規程の確認を済ませる。

## 概要

他人同士の会話ログ (まず Slack のチャンネル) を後から読んで追いつくのが苦痛。時系列に並んだ発言を
**トピックごとに組み直して**読めるようにする道具の構想。ユーザーの比喩は**フーリエ変換**: 時間軸の信号を、
別の軸 (= 話題軸) に移すと構造が見える。

困りごとは「要約が欲しい」ではなく「**混ざった話題を自分でほどく作業**が苦痛」。なので主役は要約ではなく
**組み直した原文**で、LLM の文章は見出しと数行の要点に留める。

## 経緯

1. **最初の案は Slack クライアントの自作** (読み書きの両方): チャンネル一覧はそのまま、中身は LLM が整理して
   スレッド掲示板風に見せ、ユーザーの発言も LLM が言葉を選んで返信・スレッドへ投稿する。「コアは人間、瑣末な言葉の
   チョイスは AI」(アイアンマンの比喩)
2. **調べて分かったこと**:
   - Slack は 2025-05-29 に API 規約を改定し、取得したデータの一括エクスポート・永続的なコピーや索引・LLM での利用を
     制限した。同日、Marketplace 外で配布する「unlisted」アプリの `conversations.history` / `conversations.replies` は
     1 分 1 回・1 回 15 件まで絞られた (社内向けの internal app と Marketplace のアプリは従来の Tier 3 のまま)。
     **他人に配る製品にするのは事実上難しく、自分用に留めるのが現実的**
   - AI が書いたと分かったメッセージは、書き手が誠実でない・信頼できないと受け取られやすい (感謝・謝罪など
     気持ちを伝える文で強い)。書く側は信頼と責任の論点が重い
   - Slack AI のチャンネル要約・Recap は要約を出すが、**表示の構造 (話題ごとの組み直し) は変えない**
3. **書く側を切り離し、読む側だけを残した**。書く側を作るなら「LLM が下書きを出し、人が差分を見て確定する。
   自動送信しない」が前提になる (別の構想として扱う。この issue の範囲外)
4. **フーリエ変換との対比を整理した** (下の節)。この課題には自然言語処理の研究テーマとして名前があると分かった:
   **conversation disentanglement** (会話のもつれほどき)
5. **MVP は作らず、設計メモに留めることにした** (ユーザー判断)

## 設計

### 芯: LLM には「分類」だけをさせ、本文は原文を出す

| 層 | 誰が作るか | 間違えたときの害 |
|---|---|---|
| どの発言がどのトピックか (分類) | LLM | 発言が別のトピックに入る。**原文はどこかに必ず出る**ので、読み落としにはならない |
| トピックの見出しと要点 (2〜4 行) | LLM。各要点に根拠の発言 ID を付ける | 要約の誤りが読者に伝わる。根拠 ID から原文へ飛べるので確かめられる |
| 本文 (原文の発言を時系列のまま) | **機械的に並べるだけ。LLM の生成文は混ぜない** | なし |

- 時間の情報は捨てない。トピックの中は時系列、トピック同士は「最後の発言が新しい順」に並べる
- **全ての発言がちょうど 1 つのトピックに入る**ことを道具の側で検査する。LLM が落とした ID は「未分類」トピックへ入れ、
  存在しない ID (捏造) は捨てる。これは機械で検査できる不変条件で、要約の正しさ (機械では測れない) とは分けて扱う
- Slack のスレッドは**既に 1 つの話題**なので分割しない。親と返信をひとまとまりで LLM に渡し、1 単位として分類させる
- 範囲は**読む側だけ**。投稿・リアクション・既読の更新はしない
- **自動では走らせない** (常駐・定期実行をしない)。追いつきたいときに人が打つ。費用と、外部へ送る量を人が見える形にする
- **結果を保存しない**。表示して終わり。Slack の規約の永続化の制限に触れないため。再実行のたびに LLM を呼び直す費用は受け入れる

### トピックの形 (LLM に返させる JSON)

```json
{"topics": [
  {"title": "リリース日を来週へずらす件",
   "state": "decided",
   "points": [{"text": "QA が終わらないため 10/17 へ延期", "refs": ["1728540000.123456"]}],
   "members": ["1728540000.123456", "1728541111.000100"]}
]}
```

- `state` は `decided` (決まった) / `open` (議論中・未回答の質問がある) / `info` (共有のみ) の 3 つ。
  追いつく人が知りたいのは「何が決まり、何が自分待ちか」なので、要点の文章より先にこれを見せる
- `members` は発言 ID (Slack の `ts`)。スレッドは親の `ts` で代表させる

### 表示の例

```markdown
# #dev — 10/08 09:00 〜 10/10 18:00 (発言 214 / トピック 9)

## 🟢 決定: リリース日を来週へずらす件 (12 件, 最終 10/10 17:40)
- QA が終わらないため 10/17 へ延期 [↗](permalink)
<details><summary>原文 12 件</summary>

- 10/09 10:02 tanaka: QA まだ半分くらいです
- ...
</details>

## 🟡 議論中: CI のキャッシュが効かない (5 件, 最終 10/10 15:12)
...
## ⚪ 未分類 (3 件)
```

### 構成の案

```
slack-cli (既存。読み取り専用)          新しく作る道具
  slack users   -json  ─┐
  slack history -json  ─┼─> 正規化 ─> LLM で分類 ─> 不変条件の検査 ─> 表示
  slack thread  -json  ─┘   (発言の列)  (claude -p)   (全件ちょうど 1 回)
```

- **取得は既存の [jiikko/slack-cli](https://github.com/jiikko/slack-cli) に任せる**。Chrome のセッションを流用するので、
  Slack アプリの作成・管理者の承認・アプリ向けの rate limit の段階 (Tier) の話を避けられる
- **新しい道具は slack-cli に入れず、別コマンドにする**。slack-cli は「読み取り専用で、取得したものを外へ出さない」という
  安全の約束をテストと変異検証で固めている。会話の本文を LLM へ送るのは**その約束と別の性質の操作**なので、同じバイナリに
  混ぜない。パイプでつなぐ形にしておけば、入力を差し替えて Slack 以外のログ (会議の書き起こし・他人の Claude セッションの
  ログ) にも使える

## フーリエ変換との対比

| | フーリエ変換 | この道具 |
|---|---|---|
| 軸を変えて構造を見る | 時間 → 周波数 | 時間 → 話題 |
| 元に戻せるか | 戻せる (逆変換) | 戻せる (原文を全部残し、ts で並べ直せば元のログ) |
| 成分 (基底) | 事前に決まっている (sin 波) | データを見て初めて決まる (ログごとに違う) |
| 1 点の寄与 | 各時刻の値が全周波数に少しずつ寄与する | 1 発言は 1 話題にだけ入る (分割) |
| 時間の情報 | 結果には残らない | 話題の中に時系列を残す (短時間フーリエ変換 = スペクトログラムに近い) |
| 情報が落ちるところ | 高周波を捨てるとき (圧縮) | 要約 (主成分だけ残すのに相当)。原文で補う |

考え方は似ているが、数学的には**混ざった信号の分離** (カクテルパーティー問題 = 何人もの声が混ざった録音から
一人ずつを取り出す) に近い。自然言語処理では **conversation disentanglement** と呼ばれる。

## 実装時のヒント

### slack-cli から取るとき

- 🚨 **フラグは引数より前に置く**。slack-cli は引数の後ろのフラグを使い方の誤り (rc=2) として止める
  (`cmd/slack/main.go` の `checkNoTrailingFlags`)
- `slack history -json -oldest <ts> -n <件数> <channel>` で期間を切る (`-oldest` / `-latest` は ts で受ける。既定 50 件)
- 親の発言 (`thread_ts == ts`) ごとに `slack thread -json -n <件数> <channel> <ts>` (既定 200 件)。
  🚨 `-n` に達すると**警告なしで切る** (`internal/slack/api.go` の `collectMessages` は `-n` に達すると `out[:limit]` を返すだけ)。
  返ってきた件数が `-n` と同じなら、欠けている可能性を表示に書く
- `slack users -json -bots -deleted` で user ID を表示名に引く (history の JSON の `user` は ID)。
  既定はボットと無効化済みのユーザーを除くので、フラグを付けないと退職者やボットの発言が ID のまま残る
- 🚨 **同じ ts の重複を除く**。スレッドの返信をチャンネルにも送ったもの (「チャンネルにも投稿する」) は history と
  thread の両方に同じ ts で出る。slack-cli の `Message` は `subtype` を持たないので見分けられない。
  ts で重複を除き、スレッド側に寄せる (「全件ちょうど 1 回」の検査の前提)
- `Message` は `text` だけを持ち、`files` / `attachments` / `blocks` / `reply_count` は捨てる
  (`internal/slack/types.go`)。ファイルだけの発言は本文が空になるので「(添付のみ)」と出す
- チャンネルは `#name` でなく ID で渡す。`#name` だとチャンネル一覧を `~/.config/slack-cli/cache/` に保存する
  (チャンネル名とトピックだけで本文は入らないが、「保存しない」の範囲をはっきりさせるため)
- permalink は `search` の結果にしか入らない (`Message.Perma` を埋めるのは search だけ)。history / thread の発言は
  `https://<workspace>.slack.com/archives/<channel>/p<ts から . を除いた値>` で組み立てる
- 429 は slack-cli が `Retry-After` で待つ (1 回 60 秒・1 リクエスト 3 回・合計 180 秒まで)。超えたら取れた分を出して rc=1

### LLM を呼ぶとき (`claude -p` を使う場合)

- `claude -p --output-format json --json-schema <上の形>` で形を固定する。API キーを別に持たなくてよい
- `--no-session-persistence`: 付けないと、送った会話の本文がセッションとして `~/.claude` に残る (「保存しない」に反する)
- `--bare` (hooks を飛ばす): Stop hook が最後の返答を差し替えることがある (`.claude/rules/worktree-per-session.md` の
  headless 実行の注意。issue 573)。JSON を最終応答から読むので、hooks を通さない
- 書き込む道具を外す (`--disallowedTools "Edit,Write,NotebookEdit,Bash"`。prompt の後ろに置く)
- 安いモデルで分類、要点だけ高いモデル、と分けると費用を抑えられる

### 量が多いとき

- 1 回の呼び出しに載らない量なら、期間で切って分類したあと「トピックの統合」をもう 1 回呼ぶ (map-reduce)
- 最初は件数の上限 (例: 500 件) を超えたら止めて、期間を絞るよう案内するだけでよい

### 作ったときに確かめること

- 全発言がちょうど 1 つのトピック (または未分類) に出る (道具の検査で保証できる)
- 要点の `refs` が全て入力に存在する発言 ID である
- 同じ入力を 2 回通したとき、トピックの数と大きな分け方がどれだけ揺れるか (実測して書く)
- 実際のチャンネルで「時系列で読むより速く追いつけたか」はユーザーが判断する (機械では測れない。役に立つかが目的そのもの)

## 🚨 リスク (作ると決めたら、着手前にユーザーが判断する)

- **Slack の規約**: 2025-05-29 の改定で、API から取得したデータの一括エクスポート・永続的なコピーや索引・
  **LLM での利用**が制限された。slack-cli は Slack アプリではなく Web クライアントのセッションで内部 API を叩くが、
  規約の対象から外れるとは言えない。**業務のワークスペースに使うなら、会社のポリシーと規約の両方を確認する**
  (slack-cli の README も「組織のポリシーと Slack の利用規約に反しない範囲で」と書いている)
- **外部への送信**: 会話の本文 (プライベートチャンネルを含みうる) が LLM の提供元へ送られる。会社の情報の扱いの
  規程で、社外の LLM に送ってよいかを確かめる
- **要約の誤り**: 「提案しただけ」を「決定」と書く、発言者を取り違える、の 2 つは必ず起きる。要点に根拠 ID を必須にし、
  原文を同じ画面に置くことで緩和するが、ゼロにはならない

## 未決の論点

- 道具の名前と置き場所 (`src/<name>/` の Go か、まず数十行のスクリプトで試すか)
- 期間の指定: 「自分が最後に読んだ位置から」は slack-cli に既読位置を取る口が無い (呼べるメソッドは allowlist で閉じてある)
- 表示: Markdown を pager で見るか、TUI (glogx の部品でトピック一覧と本文の 2 ペイン) にするか
- 複数チャンネルの横断 (同じ話題が #dev と #release に散っている場合の統合)
- 前回の分類を引き継ぎ、新しい発言だけを既存のトピックへ足す増分更新 (分類の揺れを抑えられるが、保存が要る = 規約の論点が増える)
- 自分宛て (メンション・自分が書いた発言への返信) を最上段へ出す
- スレッドの途中で話が逸れる場合の扱い

## 参考資料

### Slack の規約と API

- [Slack: rate limit changes for non marketplace apps (2025-05-29)](https://docs.slack.dev/changelog/2025/05/29/rate-limit-changes-for-non-marketplace-apps)
- [Slack: 2025-05 terms / rate limit update and FAQ](https://api.slack.com/changelog/2025-05-terms-rate-limit-update-and-faq)
- [Slack: rate limits clarity (2025-06-03)](https://docs.slack.dev/changelog/2025/06/03/rate-limits-clarity) — internal app の扱い
- [Hunton: Salesforce locks down Slack data](https://www.hunton.com/privacy-and-cybersecurity-law-blog/salesforce-locks-down-slack-data-time-to-review-your-slack-api-terms) — 規約改定の解説
- [jiikko/slack-cli](https://github.com/jiikko/slack-cli) — 取得側。読み取り専用・ワークスペース限定・資格情報を残さない
- `src/chromecookie/` — slack-cli が使う Chrome の Cookie の復号 (この repo)

### conversation disentanglement (分類の手法と評価の先行研究)

- Elsner & Charniak (2008) [You Talking to Me? A Corpus and Algorithm for Conversation Disentanglement](https://cs.paperswithcode.com/paper/you-talking-to-me-a-corpus-and-algorithm-for) — 課題の提起。IRC のログを人手でほどいたコーパス
- Kummerfeld et al. (ACL 2019) "A Large-Scale Corpus for Conversation Disentanglement" — Ubuntu IRC を人手で注釈した大規模データ
  ([TFDS の irc_disentanglement](https://www.tensorflow.org/datasets/catalog/irc_disentanglement))。広く使われていた
  Ubuntu IRC 対話コーパス (Lowe ら) のヒューリスティックでは、会話の 10.8% しか正しく取り出せていなかったと報告している
- [DialBERT (2020)](https://arxiv.org/pdf/2004.03760) / [Online Conversation Disentanglement with Pointer Networks (EMNLP 2020)](https://arxiv.org/pdf/2010.11080) — 事前学習モデルでの手法

### AI が書いたメッセージの受け取られ方 (書く側を切り離した根拠)

- [Liu et al. (CHI 2022)](https://nlp.stanford.edu/~diyiy/docs/chi22_perception.pdf) — AI の関与を知らされるとメールの書き手への信頼が下がる
- [Reputational risks of AI-mediated communication (arXiv 2025)](https://arxiv.org/pdf/2509.09645)
- [The AI penalty and disclosure paradox](https://researchers.mq.edu.au/en/publications/the-ai-penalty-and-disclosure-paradox-trust-authenticity-and-know/) — 開示が大事と思いつつ、開示されると評価を下げる

## 反証レビュー (2026-10-10、read-only のサブエージェント 1 体。codex の無い環境のため)

slack-cli のコードと突き合わせて、次を訂正した (訂正は別 commit):

- P1: コマンド例のフラグの位置 (引数の後ろだと rc=2) / `claude -p` が既定でセッションを保存する点 /
  Stop hook が最終応答を差し替える点 (`--bare` を足した)
- P2: 「チャンネルにも投稿する」返信の ts 重複 / `users` の既定がボットと無効化済みを除く点 /
  `thread -n` の黙った打ち切り
- P3: `Message` が添付を捨てる点
- 反証できなかった主張: history / thread の既定件数と引数、`-json` が共通フラグであること、429 の待ちの上限、
  permalink が search にしか入らないこと、既読位置を取るメソッドが allowlist に無いこと、`claude -p` のフラグ、
  slack-cli が `src/chromecookie` を使うこと

## 進捗

- 2026-10-10: 構想を起票。反証レビューの指摘を反映
- 2026-10-10: MVP は作らない方針に変更 (ユーザー判断)。最小構成と受け入れ条件を外し、経緯・フーリエ変換との対比・
  参考資料・実装時のヒントの形に組み直して `pending/` へ移した
