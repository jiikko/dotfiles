# 共通ルール

`rules/*.md` は `~/.claude/rules/` に link され、毎セッション全文読まれる。この文書は rule に
書いていない規範と、rule への索引だけを持つ (rule の中身をここで要約し直さない)。

## 作業開始前の準備

- コードを書き始める前に、必ず `git pull` を実行して最新の状態に更新すること
- 変更を入れる前に、依頼が名指ししていない関連ソース (その領域の issue・`docs/`・周辺の CLAUDE.md) も開いて、見つけた制約を使う。すぐ手を動かす方へ寄りやすく、依頼文にない前提を取りこぼす
  - 🚨 **Bash (python / sed) でファイルを編集するなら、先にそのディレクトリの CLAUDE.md を Read する**。下位の CLAUDE.md は Read ツールで触れたときにしか読み込まれないので、スクリプトだけで編集するとそのディレクトリの規約 (GUI 必須の作業など) を素通りする
- **起動した cwd の外の repo を触るなら、編集の前にその repo の CLAUDE.md と `.claude/rules/` を Read する**。repo の規約は cwd がその repo に入るまで注入されない

## Git 禁止操作

- **無断で `git clone` しない**。必要ならユーザーに許可を取る
- `git stash` を使わない。ステージ済みの変更を退避したいなら、別ブランチにコミットするかユーザーに確認する
- サブモジュール内でコミットしたら、**そのサブモジュールのリモートにも push する** (親の push だけでは CI が参照コミットを取得できない)
- **コミット & push 前に `git status` で dirty なサブモジュールがないか確認する**。あれば中で差分を確認し、必要ならコミット & push してから親の参照を更新する。dirty を残したまま作業を終えない
- **commit / push 後は、成功を報告する前に実際の git state (`git log -1 --stat` / `git status` / push 出力) を確認する**。ツール出力の「成功」表示を鵜呑みにしない

## 並行作業者がいるときの worktree 退避

- **作業開始後に**他の作業者 (並行セッション・人間) の変更を確認できたら (untracked の増加 / 自分が触っていないファイルの新しい差分 / 知らないコミット)、**git worktree を作ってそこで作業してよい**
- **作業開始時点から在る** dirty / untracked はこの条件に含めない (過去の残骸かもしれない)。触らず・巻き込まずに共有 working tree のまま続行してよい
- **worktree で作業したら、元のブランチ (worktree を切った起点のブランチ) の remote へ push するまでが担当範囲**。
  commit して「統合はお任せ」で止めない。`git push origin HEAD:<元のブランチ>` が non-fast-forward で弾かれたら
  worktree 内で `git fetch` + rebase して検証をやり直し、push し直す
- **worktree を残さない**。push の成功を確認してから `git worktree remove` する (`&&` で繋ぐ。push が弾かれたまま消すと未 push の commit を失う)。
  本体の checkout が同じブランチなら、そちらも `git -C <本体> pull --rebase` して追い付かせる
- 共有 working tree に留まるなら [`commit-with-pathspec.md`](rules/commit-with-pathspec.md) に従う。書き込み権限のエージェントを 2 体以上並行させるときは [`parallel-write-agents-need-worktree-isolation.md`](rules/parallel-write-agents-need-worktree-isolation.md)

## 他セッションからの問い合わせに答えるとき

- **調べないと答えられない問い合わせは、背景のサブエージェント (sonnet) に調べさせて返信の下書きを作らせる**。自分の会話に残るのを「受信 / 委譲 / 返信」の数行に抑え、作業の文脈を問い合わせで埋めない
  - サブエージェントへの指示は「読み取りのみ・返信は 3 行以内の下書きで返す」。**送信は自分で行う** (下書きを検閲してから `SendMessage`。[`subagent-model-tiering.md`](rules/subagent-model-tiering.md))
  - 手元の記憶だけで答えられる 1〜2 行の質問 (今どの issue / どのファイルを触っているか) は委譲せずその場で返す

## 応答・成果物の長さとスコープ

出力の長さ・スコープ・委譲の量は reasoning effort では制御できず、明示指示でしか効かない (モデルごとの傾向と出典は `~/dotfiles/_claude/rules-rationale/global-claude-md.md`)。

- **応答は本題に紙面を使う**。前置き・免責は短く。「説明して」には要点のサマリを返し、詳細は求められたときだけ書く
- **成果物ドキュメントの長さはタスクの必要量に合わせる**。埋め草セクション・重複したサマリ・定型文で嵩上げしない
- **進捗更新**: 最初のツール呼び出し前に何をするかを一文で言う。作業中は重要な発見か方針転換のときだけ挟む。完了時は結論から書き、根拠はその後
- 🚨 **「次はこれをやります」でターンを終えない**。次の行動を書くなら同じターンで 1 手目を実行する。長い完了報告の直後ほど踏みやすい。着手できないなら**やらないと書く**
  - 依頼した作業が残っているのにターンを終える形は次の 4 つ。どれもやらない: ①やったことの長い要約を次の手の予告で締める ②「よければ続けます」と申し出て返事を待つ ③残りの作業を妨げない判断事項を並べて止まる ④「長くなった / 区切りがいい」を理由に報告で止まる。状況メモや判断事項への推奨は、次のツール呼び出しと同じメッセージに書いて進める
  - 止まってよいのは、ユーザーの入力なしには何も進まないときと、行く手を意図的に守られたもの (権限・hook) に阻まれたときだけ。破壊的・外部公開の操作の確認と、ユーザーが事前の提案を求めている作業はこの項で省かない
- **依頼されたスコープをそのまま完遂する**。ルーチンな判断は自分で下し、解釈次第で成果物が実質的に変わるときだけ確認する。依頼が誤っている / もっと良い方法があると思ったら一文で述べてから依頼どおり進める。黙って狭める・広げる・すり替えない
  - **確認を求めるときは、選択肢ごとに「何が起きるか」と「今の挙動から失われるもの」を 1 行ずつ書く**。新しい挙動だけを並べると、暗黙に消える挙動を承認者が見落とす。**消す・止める対象は種類 (ファイル / ディレクトリ / プロセス) を分けて列挙する**。「ファイルを消してよいか」の承認はディレクトリに及ばない。**使い心地が変わる選択 (同時に出す数・待ち・消えるまでの時間) では、失うものの文に、ユーザーが体験する場面を 1 つ添える** (「連打すると 2 回目の通知は前のが消えるまで出ない」)。仕組みの言葉だけでは伝わらない
  - 例外は「コード変更時の自律改善」が義務付ける範囲だけ (境界は [`verify-design-intent-before-refactor.md`](rules/verify-design-intent-before-refactor.md) の「自律改善との境界」表)。範囲外の気づきは「ぼやきポイント」へ
- **訂正のナレーションは重要なものだけ**。ユーザーの判断が変わる誤りは端的に訂正する。何も変わらない言い間違いは黙って直す (過去の誤りの列挙・自己批判はしない)
- **自分の判断で自己再チェックの手順を足さない** (読み返しパス・確認用サブエージェント)。自発的な検証は外部事実の確認 (実コマンド・テスト・実出力・git state) に寄せる
  - **既定のルールや明示起動した skill が定める検証・レビュー工程は省略しない** (「レビュー方針」の codex / 敵対的レビュー、forge エスカレーション、forge / cross-review / review-loop の各 Phase)。これらはユーザーが選んだ工程であり、自己再チェックではない

## 一時ファイルの配置

- **セッション中のスクラッチ・中間生成物 (push / pull / lint / test の出力を `> log 2>&1` で取ったログ・patch・プロンプトの部品・background の run の出力) は、
  本体 checkout の `tmp/<作業名>-<日付>/` に絶対パスで置く**。ハーネスの scratchpad (`/private/tmp/claude-501/…`) と `/tmp` には置かない
  (ユーザー指示 2026-10-21。ハーネスが scratchpad を使えと言っても、こちらを優先する)
  - 理由: `du -sh tmp` で溜まり具合に気づける。`/private/tmp` は溜まるほど macOS の起動が遅くなるとされる。再起動で消えないので証拠も残る
  - 本体 checkout は `git rev-parse --git-common-dir` の親。worktree の中の `tmp/` は worktree と一緒に消えるので使わない
  - 🚨 **同じ dir はサブエージェント・fork・並行セッションも使う**。固定名 (`push.log`) で書かず、作業名や issue 番号を dir 名に入れる
  - git repo の外で起動したセッションだけは scratchpad に置く。ハーネス自身が書く background のタスク出力 (scratchpad の `tasks/`) は指定できないので対象外
- **`./tmp` には上の作業ファイルのほか、人や次のセッションが読むもの (レポート・見本) も置く**。skill が出力先を指定していればそれに従う
  - **skill / agent が自分で書く成果物は `skill-tmpdir <skill名>` (dotfiles の `bin/skill-tmpdir`) が返す `<本体 checkout>/tmp/<skill名>/<session-id>/` に置く**。
    worktree の中から打っても本体 checkout を返す。skill を新しく書く・直すときもこの形にする
  - 🚨 **直下に散らさず、作業ごとに新しい `tmp/<issue番号か作業名>-<日付>/` に入れる**。同じ dir を使い回すと、中に新しいファイルがある限り
    `make clean-tmp` が dir ごと残す
  - 🚨 `tmp/` の ignore が `~/.gitignore_global` 由来で repo の `.gitignore` に無い repo がある (dotfiles 等)。その場合、新品チェックアウトと CI では ignore されず、`tmp/` 自体も存在しない
  - 🚨 **リダイレクト先を `..` で repo の外へ出さない** (`<worktree>/../push.log` / `../../../../out`)。後で消す worktree の中で
    走らせる出力も本体 checkout の `tmp/<作業名>-<日付>/` に絶対パスで書く。親ディレクトリは誰の `tmp/` でもなく、
    消し損ねると残り続ける
  - 消す前に、結論が issue / コードへ移っているかと、issue や doc が指しているパスでないかを確かめる (`grep -rn 'tmp/' issues/ _claude/`)。dotfiles では `make clean-tmp` (既定は 30 日より古いもの。`DRY_RUN=1` で一覧のみ、`DAYS=7` で期間を変える)。`/private/tmp`・`$TMPDIR` を片付けるなら `cleanup-tmp-dir` skill (`bin/tmpclean`。直接 rm しない)
- **スクリプト / テストが実行時に作る隔離ディレクトリは対象外**。既定は OS の一時領域 (`mktemp -d` / `t.TempDir()`)。`./tmp` に置くなら理由をコード直近に残す
- 線引きは置き場所ではなく **終了時に消す責任が実装されているか**
  - 🚨 パスで判断しない (`mktemp -d` は macOS では `/var/folders/…`、Linux では `/tmp` 配下になりうる)
  - 🚨 `trap` は中断では走らず、dir を消してもそこで起こしたプロセスは残る

## Issue管理

- **`issues/` (または `issue/`) を持つ repo では、SessionStart hook (`_claude/hooks/issue-rules-inject.sh`) が issue 運用規約を注入する。この CLAUDE.md と同じ拘束力で従う**。正本は `~/dotfiles/_claude/issue-rules.md`、repo 固有の事項は各 repo の `issues/README.md`。issues/ を持たない repo には適用しない
- issues/ がある repo なのに注入が見当たらないときは、issue を触る前に正本を Read する
- claim の手順と issue の反証レビューは `issue-rules.d/` の rule が正本で、同じ hook が 1 本ずつ注入する。検証レポートを issue へ移す手順 ([`move-report-conclusions-to-issues.md`](rules/move-report-conclusions-to-issues.md)) は `paths:` で tmp の下に触れたときにどの repo でも読み込まれ、issues/ のある repo では同じ hook が起動時にも注入する。注入が見当たらなければ `~/dotfiles/_claude/issue-rules.d/` を Read する

## 設計方針

- God クラスを避ける。肥大化しそうなら責務ごとに分割できないか検討する
- 変更したファイルに God クラス / God ファイルの予兆 (責務の混在など) を見つけたらリファクタを提案する。ただし**目的は複雑性を下げること**で、行数だけを理由にファイルを分けるのはリファクタではない (分割は複雑性を移動するだけ)。判断基準は [`verify-design-intent-before-refactor.md`](rules/verify-design-intent-before-refactor.md)
- バグフィックス後、その project の linter のカスタムルール / preset で再発防止できないか検討し、提案する
- 設計・検証の rule (`rules/*.md`) は毎セッション全文読まれ、各 rule の冒頭に発動点がある。ここで索引し直さない。例外は `paths:` で条件ロードされるもの: zsh の precmd / preexec から関数を呼ぶなら `~/dotfiles/.claude/rules/zsh-hook-return-via-reply.md` (`REPLY` で返す。dotfiles では zsh のファイルを Read すると読み込まれる。dotfiles 固有の規範の置き場は `~/dotfiles/docs/README.md` の「ここに置かないもの」)

## コード変更時の自律改善

変更を入れる前に変更先の周辺コードを読み、以下に該当するものがあれば **変更と同時に** 改善する (提案ではなく実行)。

- **重複コード**: 同じロジックが複数箇所にあるなら、まずヘルパーに抽出してから変更を入れる。同じ変更を 2 箇所にコピペしない
  - 文書・規約も同じ。同じ段落を 2 箇所目へ書くよう頼まれたら、単一の正本から読ませる案を一文添える
- **死んでいる分岐・未使用コード**: 変更対象の関数内の到達不能コードは削除する
- **不自然な責務の混在**: 変更対象の関数が明らかに複数の責務を持っていたら、まず分離してから変更を入れる

触ったコードを前より良い状態にして返す。ただし依頼と無関係なファイルまで手を広げない。

## ぼやきポイント推奨

依頼範囲外だが将来直したくなりそうな違和感を見つけたら、応答の最後に一言添える。判断材料の提供であり、勝手に修正しない。

- 対象例: 二重実装の規約・スタイル混在・ハードコード・マジックナンバー・テスト漏れの予兆・依存方向の歪み・命名の食い違い
- 形式: 「**なお、ぼやきポイント**: 〜」を一行〜数行 (長文の分析にはしない)。issue 化が妥当なら「issue 化しますか？」と添える
- 「タスクと無関係だから黙る」のではなく「無関係だが伝える価値があるなら一行ぼやく」
- 確信が低いもの・好みの問題・ユーザーが既知のものはぼやかない
- **ぼやきも事実の主張なら裏を取る**。特に「〜は検査されていない」のような不在の主張。取っていないなら「**未確認だが**」と明示する

## 不具合対応の原則

**パッチワーク (症状への対処) ではなく、構造的な根本改修を行う。** 最も重要な原則の一つ。
「ログを足して現象を追う」より先に、設計上の前提 (契約) を見直して構造で潰す。

- まず **不変条件 (Invariant)** を言語化する (例: deep link は失われない / 同一ファイルの同一性は一意 / UI 失敗で再生は止まらない)
- **失敗モード** (順序競合・再送・二重実行・部分失敗・再起動) を列挙し、設計で吸収する
- **境界 (main/renderer、UI/Domain、外部 API)** ごとに責務を分離し、手続きの連鎖ではなく「コマンド + 結果」の形にする
- 同一性は **安定キー (id / path_lower 等)** に統一し、表示用文字列に依存しない
- 追加ログは最後の手段。必要なら「イベント / 状態遷移」が観測できる設計にする
- **「この if 文を足せば直る」と思ったら立ち止まる**。その分岐が必要になった前提を疑う。特定ケースだけを救うワークアラウンドは原則禁止
- **直したバグは「同じ間違いが別の場所にもある」前提で grep する**。テストで見つけたら production を、production で見つけたらテスト・別モジュールを見る。関数の契約変更 (返し方・シグネチャ) の呼び残しも同じ扱い
  - grep は直した行の字面ではなく**失敗の仕組み**で書く (SIGPIPE なら生産側を問わず `| grep -q` を探す)。字面で探すと、引数や生産側が違う同じ仕組みの変種を取りこぼす
- **効果がなかった修正は必ず revert する**
- 新しい値・フラグ・経路を既存の呼び出しに通すなら [`survey-receiver-guards-before-passing-new-values.md`](rules/survey-receiver-guards-before-passing-new-values.md)。UI / デバイス / 環境の問題は [`check-other-platform-reference.md`](rules/check-other-platform-reference.md) / [`no-ios-simulator-verification.md`](rules/no-ios-simulator-verification.md)。Apple のプロジェクトの次の rule は `paths:` で Swift / Xcode のファイルを Read したときだけ読み込まれる: UI 確認 ([`no-osascript-for-ui-verification.md`](rules/no-osascript-for-ui-verification.md))・xcodebuild 中の `swift build` / `swift test` ([`no-concurrent-spm-build-during-xcodebuild.md`](rules/no-concurrent-spm-build-during-xcodebuild.md))・別の thread から呼ばれる callback ([`swift-off-actor-callback-outside-actor.md`](rules/swift-off-actor-callback-outside-actor.md))・id だけで等しくなる型を SwiftUI の差分に使う ([`swiftui-id-only-equatable-skips-redraw.md`](rules/swiftui-id-only-equatable-skips-redraw.md))。**読み込まれていなければ、その作業の前に直接 Read する**

## レビュー方針

- **重要なコード変更・バグ修正は、設計と実装の両方を外部レビューに通す** (設計 → レビュー → 実装 → テスト → レビュー)。codex が許可されている環境では codex (`codex-review` / `cross-review` / `review-loop` / `codex-lead` / `codex-drive`)、それ以外では観点を分けた read-only サブエージェント (1 体に全部見せず、①壊す ②素通り (false green) ③回帰 のように観点を分けて**反証**させる。「レビューして」と頼むと追認が返る)。typo・数行の chore は対象外。codex を使わない環境では観点を分けたサブエージェントを直接起動する (`cross-review` skill は codex を含むため丸ごとは使えない)
- 指摘は無視せず、根拠の弱い断定・false positive を訂正してから commit する
- **壊しにいくパス (敵対的レビュー / red team) を 1 本混ぜる**。判断ロジック・境界・状態遷移・外部 I/O が動いた変更では commit 前の最終ゲートにする (機械的置換・設定値変更だけなら省略してよいが、省略したと明示する)
- **skill (`SKILL.md`) を改修したら、終える前に全体の整合チェックを観点を変えて 3 回回す** (ユーザー指示 2026-10-21)。同じ見方で 2 回目を回さない。観点の例: ①skill の中の整合 (番号・相互参照・用語・節どうしの矛盾・書き換えで消えた記述) ②skill の外との整合 (repo の実物 = ルート・列名・パス・コマンド、他の skill / rule / CLAUDE.md、正本が別の場所 (esa など) にあるならその版) ③使う人の目で頭から辿る (書かれた手順どおりに進めて詰まる所・出力の形と書き方の食い違い・初回にしか成り立たない前提)。直したら、直した版でもう一度回す。dotfiles の `_claude/skills/` では `bin/skill-eval` (`.claude/rules/skill-eval-after-edit.md`) も別に回す
- **「指摘なし」は「その探し方では壊せなかった」**。不変条件はテスト・型・設計で固定して初めて閉じる
- **主張は証拠ではない**。自分 / codex / エージェントの「対応済み」「テスト green」は、diff・実行結果・外部基準で裏を取ってから受け入れる。取れないものは「未検証」と報告する
  - 🚨 **「N 箇所すべてに対応した」と書くなら N を機械で数えてから** (`git show --stat` / `grep -c`)
  - 🚨 **「等価」「挙動は変わらない」も書く前に compiler / テストで確かめる**。自分の書き換えは疑われにくい
- 自分で新設した安全機構は [`adversarial-review-own-safeguards.md`](rules/adversarial-review-own-safeguards.md)、防御を外すときは [`list-masked-failure-modes-before-removing-guard.md`](rules/list-masked-failure-modes-before-removing-guard.md)
- **敵対レビューの出力こそ無検閲で採用しない**。発火条件が具体的で再現できたものだけ直し、再現しないものは記録、示せないものは「未確認リスク」として issue / 観測ポイントに落とす。推測で防御コードを足さない (作法の正本は `~/.claude/skills/codex-review/SKILL.md` の「敵対的レビューの作法」)
  - **「A より B が悪い」という指摘は、A が現状 (status quo) かを確かめてから採る**。レビュワーは理想と比べることがあり、事実が正しく再現できても、現状より悪化するとは限らない
  - **レビューの推奨を設計・要件へ取り込む前に、関係する skill / rule / issue と会話の既存ユーザー決定を照合する**。
    「ユーザー決定」「ユーザー指示」などを検索し、印のない制約も本文で確認する。推奨が既存の決定と食い違うなら、
    ユーザーが変更を指示するまでその決定を維持し、推奨を採らない理由を記録する

## シンボル検索の使い分け (grep と dupscan)

Swift / Go の repo で関数・メソッド・型を探すとき。使い方は skill dupscan。

- **dupscan** (`index -name` / `callers`。Swift は `def` / `refs` も): 名前が広くヒットが多いとき・大小や表記が不確かなとき・「似た関数が既にあるか」を知りたいとき (広い名前では rg より桁違いに小さく返る)
- **grep / rg**: 定義 1 件を狙い撃ちするとき (`func .*Name`。dupscan は毎回の注意書きの分だけ重い)・文字列・コメント・設定・ログ。dupscan は構文木だけを読む
- dupscan は型を解決しない。**0 件は不存在の証明ではない**。出力が大きい・曖昧なら `-limit` で絞るか rg に切り替えてよい。Go は `def` 非対応なので、`index` の行の `file:開始-終了` を Read の範囲 (offset / limit) にして該当の単位だけ読む。打ち切られたら先頭の件数と `rg` の提示を見て絞る

## スキルファイル参照

以下のキーワードに関連するタスクでは、対応する SKILL.md を作業前に Read する。
agent はこの表に一部しか載っていないので、**agent を探すときは [`agents/README.md`](agents/README.md) を見る**
(乖離は `tests/claude/test_agents_index.sh` が検出する)。

| キーワード | 参照先 |
|-----------|-------|
| 監査, audit, コードレビュー全体 | `~/.claude/skills/audit/SKILL.md` |
| コミット, commit, git commit | `~/.claude/skills/c/SKILL.md` |
| forge, 専門家実装, 専門家エージェントで実装/修正（修正・実装まで任せる） | `~/.claude/skills/forge/SKILL.md` |
| CSS, Node.js, Electron, フロントエンド, デスクトップアプリ | agent: `css-expert` / `nodejs-expert` / `electron-expert` |
| iOS, iPhone, XcodeGen, SPM, code signing, AVFoundation, @rpath | `~/.claude/skills/ios-app-developer/SKILL.md` |
| perf.log 分析, ボトルネック（ThumbnailThumb 専用 / bin/tt-client 前提） | `~/.claude/skills/perf-analysis/SKILL.md` |
| WCAG, アクセシビリティ, ダークモード, スタイルレビュー | `~/.claude/skills/style-review/SKILL.md` |
| AVFoundation, AVPlayer, 動画再生, seek, scrub, frame stepping | `~/.claude/skills/avfoundation-reference/SKILL.md` |
| watchOS, Apple Watch, WatchKit, WatchConnectivity, HealthKit, コンプリケーション | `~/.claude/skills/watchos-expert/SKILL.md` |
| App Store, TestFlight, 審査, リジェクト, App Store Connect | agent: `appstore-submission-expert` |
| issue-sync, issue同期, 完了漏れ, done移動 | `~/.claude/skills/issue-sync/SKILL.md` |
| issue-writeback, issue更新漏れ, 書き戻し漏れ, issue更新した? | `~/.claude/skills/issue-writeback/SKILL.md` (本文の追記漏れ。done 移動は issue-sync) |
| dupscan, 重複したメソッド, 既にあるヘルパー, 書く前に探す, セルフレビューで写し, 呼び出し元 (Swift / Go) | `~/.claude/skills/dupscan/SKILL.md` (Swift と Go。def / refs は Swift だけ) |
| start-issues-ops, issue運用を始める, issuesディレクトリを作る | `~/.claude/skills/start-issues-ops/SKILL.md` |
| cleanup-tmp-dir, /tmp を掃除, 一時ファイルを片付け, TMPDIR が溜まっている | `~/.claude/skills/cleanup-tmp-dir/SKILL.md` (消す前に承認を取る。dotfiles の tmp/ は `make clean-tmp`) |
| claude-md-refresh, CLAUDE.md の refresh, README が古い, ディレクトリの文書の乖離 | `~/.claude/skills/claude-md-refresh/SKILL.md` (引数で対象ディレクトリを指定。issues/ は対象外) |
| claude-md-compact, CLAUDE.md を圧縮, rules を縮める, 常時読み込みを減らす, instruction files の上限 | `~/.claude/skills/claude-md-compact/SKILL.md` (規範を残して実例・重複を外へ移す。ずれを直すのは claude-md-refresh) |
| refresh-on-session, 差分全体の見直し, 用語の揺れ, コメントの更新漏れ, 呼び出し元の追従漏れ | `~/.claude/skills/refresh-on-session/SKILL.md` (差分だけを見る。バグ探しは `/code-review`) |
| events.yml, 審査が通った, バージョン公開を記録, product-analytics, my-products-update-apps-event | `~/.claude/skills/my-products-update-apps-event/SKILL.md` |
| クラッシュ, crash, .ips, DiagnosticReports, SIGSEGV, SIGABRT | `~/.claude/skills/crash-log-analyzer/SKILL.md` |
| dupscan-dogfood, dupscan を使って開発, dupscan のフィードバック, dupscan を使い込む | `~/.claude/skills/dupscan-dogfood/SKILL.md` |
| codex-review, Codexレビュー, コードレビュー依頼 | `~/.claude/skills/codex-review/SKILL.md` |
| codexにリード, codex主導で着手, 設計から codex に任せて（実装は Claude）, codex-lead | `~/.claude/skills/codex-lead/SKILL.md` |
| codexに書かせて, codexメインで実装, codexに作らせて, 設計から実装まで codex に丸投げ, codex-drive | `~/.claude/skills/codex-drive/SKILL.md` |
| cross-review, クロスレビュー, 複数視点レビュー | `~/.claude/skills/cross-review/SKILL.md` |
| レビューループ, review-loop, make review | `~/.claude/skills/review-loop/SKILL.md` |
| pro-con, カード, PM, PG, レーン, dispatcher, dogfooding (pro-con を外から動かす・調べる) | `~/.claude/skills/pro-con/SKILL.md` (正本は `pro-con help <話題>`) |
| 視認性, 色被り, UXレビュー | `~/.claude/skills/ux-visibility-review/SKILL.md` |
| ずんだもん解説, VOICEVOX, ずんだもん, キャラ音声の掛け合い動画, 読み上げ動画 | `~/.claude/skills/zundamon-kaisetsu/SKILL.md` |
