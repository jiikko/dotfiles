# 共通ルール

`rules/*.md` は `~/.claude/rules/` に link され、毎セッション全文読まれる。この文書は rule に
書いていない規範と、rule への索引だけを持つ (rule の中身をここで要約し直さない)。

## 作業開始前の準備

- コードを書き始める前に、必ず `git pull` を実行して最新の状態に更新すること
- 変更を入れる前に、依頼が名指ししていない関連ソース (その領域の issue・`docs/`・周辺の CLAUDE.md) も開いて、見つけた制約を使う。すぐ手を動かす方へ寄りやすく、依頼文にない前提を取りこぼす

## Git 禁止操作

- **無断で `git clone` しない**。必要ならユーザーに許可を取る
- `git stash` を使わない。ステージ済みの変更を退避したいなら、別ブランチにコミットするかユーザーに確認する
- サブモジュール内でコミットしたら、**そのサブモジュールのリモートにも push する** (親の push だけでは CI が参照コミットを取得できない)
- **コミット & push 前に `git status` で dirty なサブモジュールがないか確認する**。あれば中で差分を確認し、必要ならコミット & push してから親の参照を更新する。dirty を残したまま作業を終えない
- **commit / push 後は、成功を報告する前に実際の git state (`git log -1 --stat` / `git status` / push 出力) を確認する**。ツール出力の「成功」表示を鵜呑みにしない (push 失敗や heredoc 破損を成功と誤報した実例がある)

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

出力の長さ・スコープ・委譲の量は reasoning effort では制御できず、明示指示でしか効かない
(Opus 5.5 は Opus 5 より少ないトークンで終え、すぐ作業に取りかかる側、Fable 5.1 は報告が少なくなる側に寄る。出典:
[Opus 5.5 ガイド](https://platform.claude.com/docs/ja/build-with-claude/prompt-engineering/prompting-claude-opus-5-5)
(Opus 5 ガイドのパターンも引き続き有効とされる) / `claude-api` skill の Fable 5.1 節。乖離に気づいたら同じ commit で直す)。

- **応答は本題に紙面を使う**。前置き・免責は短く。「説明して」には要点のサマリを返し、詳細は求められたときだけ書く
- **成果物ドキュメントの長さはタスクの必要量に合わせる**。埋め草セクション・重複したサマリ・定型文で嵩上げしない
- **進捗更新**: 最初のツール呼び出し前に何をするかを一文で言う。作業中は重要な発見か方針転換のときだけ挟む。完了時は結論から書き、根拠はその後
- 🚨 **「次はこれをやります」でターンを終えない**。次の行動を書くなら同じターンで 1 手目を実行する。長い完了報告の直後ほど踏みやすい (実測 2026-09-09: 1 セッションで 3 回やり、3 回とも「スタックしていませんか？」と聞かれた)。着手できないなら**やらないと書く**
  - 依頼した作業が残っているのにターンを終える形は次の 4 つ。どれもやらない: ①やったことの長い要約を次の手の予告で締める ②「よければ続けます」と申し出て返事を待つ ③残りの作業を妨げない判断事項を並べて止まる ④「長くなった / 区切りがいい」を理由に報告で止まる。状況メモや判断事項への推奨は、次のツール呼び出しと同じメッセージに書いて進める
  - 止まってよいのは、ユーザーの入力なしには何も進まないときと、行く手を意図的に守られたもの (権限・hook) に阻まれたときだけ。破壊的・外部公開の操作の確認と、ユーザーが事前の提案を求めている作業はこの項で省かない
- **依頼されたスコープをそのまま完遂する**。ルーチンな判断は自分で下し、解釈次第で成果物が実質的に変わるときだけ確認する。依頼が誤っている / もっと良い方法があると思ったら一文で述べてから依頼どおり進める。黙って狭める・広げる・すり替えない
  - 例外は「コード変更時の自律改善」が義務付ける範囲だけ (境界は [`verify-design-intent-before-refactor.md`](rules/verify-design-intent-before-refactor.md) の「自律改善との境界」表)。範囲外の気づきは「ぼやきポイント」へ
- **訂正のナレーションは重要なものだけ**。ユーザーの判断が変わる誤りは端的に訂正する。何も変わらない言い間違いは黙って直す (過去の誤りの列挙・自己批判はしない)
- **自分の判断で自己再チェックの手順を足さない** (読み返しパス・確認用サブエージェント)。自発的な検証は外部事実の確認 (実コマンド・テスト・実出力・git state) に寄せる
  - **既定のルールや明示起動した skill が定める検証・レビュー工程は省略しない** (「レビュー方針」の codex / 敵対的レビュー、forge エスカレーション、forge / cross-review / review-loop の各 Phase)。これらはユーザーが選んだ工程であり、自己再チェックではない

## 一時ファイルの配置

- **Claude がセッション中に作る成果物 (レポート・スクラッチ・中間生成物) は `./tmp`**。`/tmp` に置かない
  - 例外: ハーネスが指定する scratchpad (`/private/tmp/claude-501/…`) はそのまま使ってよい
  - 🚨 `tmp/` の ignore が `~/.gitignore_global` 由来で repo の `.gitignore` に無い repo がある (dotfiles 等)。その場合、新品チェックアウトと CI では ignore されず、`tmp/` 自体も存在しない
  - 消す前に、結論が issue / コードへ移っているかと、issue や doc が指しているパスでないかを確かめる (`grep -rn 'tmp/' issues/ _claude/`)。dotfiles では `make clean-tmp` (既定は 30 日より古いもの。`DRY_RUN=1` で一覧のみ、`DAYS=7` で期間を変える)
- **スクリプト / テストが実行時に作る隔離ディレクトリは対象外**。既定は OS の一時領域 (`mktemp -d` / `t.TempDir()`)。`./tmp` に置くなら理由をコード直近に残す
- 線引きは置き場所ではなく **終了時に消す責任が実装されているか**
  - 🚨 パスで判断しない (`mktemp -d` は macOS では `/var/folders/…`、Linux では `/tmp` 配下になりうる)
  - 🚨 `trap` は中断では走らず、dir を消してもそこで起こしたプロセスは残る (実例: `scripts/tmux_reap_orphan_servers.sh` の背景注記。里子化した tmux サーバで自動復元が 17 日間不発)

## Issue管理

- **`issues/` (または `issue/`) を持つ repo では、SessionStart hook (`_claude/hooks/issue-rules-inject.sh`) が issue 運用規約を注入する。この CLAUDE.md と同じ拘束力で従う**。正本は `~/dotfiles/_claude/issue-rules.md`、repo 固有の事項は各 repo の `issues/README.md`。issues/ を持たない repo には適用しない
- issues/ がある repo なのに注入が見当たらないときは、issue を触る前に正本を Read する
- claim の手順は [`claim-issue-in-next-and-push.md`](rules/claim-issue-in-next-and-push.md)、検証レポートを issue へ移す手順は [`move-report-conclusions-to-issues.md`](rules/move-report-conclusions-to-issues.md)

## 設計方針

- God クラスを避ける。肥大化しそうなら責務ごとに分割できないか検討する
- 変更したファイルに God クラス / God ファイルの予兆 (責務の混在など) を見つけたらリファクタを提案する。ただし**目的は複雑性を下げること**で、行数だけを理由にファイルを分けるのはリファクタではない (分割は複雑性を移動するだけ)。判断基準は [`verify-design-intent-before-refactor.md`](rules/verify-design-intent-before-refactor.md)
- バグフィックス後、その project の linter のカスタムルール / preset で再発防止できないか検討し、提案する
- 以下は rule が正本。発動点だけ並べる:

| 発動点 | rule |
|---|---|
| カバレッジ向上を求められた | [`refuse-low-value-coverage.md`](rules/refuse-low-value-coverage.md) |
| 検査・テストを「通った」と判断する | [`verify-execution-not-just-exit-code.md`](rules/verify-execution-not-just-exit-code.md) |
| 新規テストを commit する | [`mutation-verify-new-tests.md`](rules/mutation-verify-new-tests.md) (`paths:` でテストファイルを Read したときだけ読み込まれる。**読み込まれていなければ commit の前に直接 Read する**) |
| 性能を主張する | [`perf-claims-need-measurement.md`](rules/perf-claims-need-measurement.md) |
| shim / wrapper を PATH 先頭に置く | [`path-shim-must-resolve-real-binary.md`](rules/path-shim-must-resolve-real-binary.md) |
| 外部コマンドの出力・終了コードを判定材料にする | [`measure-external-cli-streams-separately.md`](rules/measure-external-cli-streams-separately.md) |
| 再利用される道具を新設する | [`new-tool-requires-entrypoint-docs.md`](rules/new-tool-requires-entrypoint-docs.md) |
| 端末 UI で縦に揃える | [`no-mixed-width-columns-in-terminal-ui.md`](rules/no-mixed-width-columns-in-terminal-ui.md) |
| テストで時間を測る / `sleep` で待つ | [`avoid-wall-clock-assertions.md`](rules/avoid-wall-clock-assertions.md) |
| 見た目が未確定のまま本体を書く | [`decide-layout-in-sample-renderer-first.md`](rules/decide-layout-in-sample-renderer-first.md) |
| 対話プロンプトを自動で確認する | [`verify-interactive-prompt-with-pty-driver.md`](rules/verify-interactive-prompt-with-pty-driver.md) |
| zsh の precmd / preexec から関数を呼ぶ | `~/dotfiles/rules/zsh-hook-return-via-reply.md` (`REPLY` で返す。dotfiles 固有の規範の索引は同 `rules/README.md`) |

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
- **ぼやきも事実の主張なら裏を取る**。特に「〜は検査されていない」のような不在の主張。取っていないなら「**未確認だが**」と明示する (実例 2026-09-02: 「検査対象外」とぼやいた検査が `src/glogx/box_test.go` に在った。Go の検査は `tests/` ではなく `src/<proj>/*_test.go` にある)

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
- **効果がなかった修正は必ず revert する**
- 新しい値・フラグ・経路を既存の呼び出しに通すなら [`survey-receiver-guards-before-passing-new-values.md`](rules/survey-receiver-guards-before-passing-new-values.md)。UI / デバイス / 環境の問題は [`check-other-platform-reference.md`](rules/check-other-platform-reference.md) / [`no-osascript-for-ui-verification.md`](rules/no-osascript-for-ui-verification.md) / [`no-ios-simulator-verification.md`](rules/no-ios-simulator-verification.md)。Apple のプロジェクトでは `no-osascript-for-ui-verification.md` と [`no-concurrent-spm-build-during-xcodebuild.md`](rules/no-concurrent-spm-build-during-xcodebuild.md) (xcodebuild の実行中に同じ checkout で `swift build` / `swift test` を並行させない) が `paths:` で Swift / Xcode のファイルを Read したときだけ読み込まれる。**読み込まれていなければ UI 確認・並行ビルドの前に直接 Read する**

## レビュー方針

- **重要なコード変更・バグ修正は、設計と実装の両方を外部レビューに通す** (設計 → レビュー → 実装 → テスト → レビュー)。codex が許可されている環境では codex (`codex-review` / `cross-review` / `review-loop` / `codex-lead` / `codex-drive`)、それ以外では観点を分けた read-only サブエージェント (作法は [`issue-creation-codex-review.md`](rules/issue-creation-codex-review.md) の代替節)。typo・数行の chore は対象外。codex を使わない環境では観点を分けたサブエージェントを直接起動する (`cross-review` skill は codex を含むため丸ごとは使えない)
- 指摘は無視せず、根拠の弱い断定・false positive を訂正してから commit する
- **壊しにいくパス (敵対的レビュー / red team) を 1 本混ぜる**。判断ロジック・境界・状態遷移・外部 I/O が動いた変更では commit 前の最終ゲートにする (機械的置換・設定値変更だけなら省略してよいが、省略したと明示する)
- **「指摘なし」は「その探し方では壊せなかった」**。不変条件はテスト・型・設計で固定して初めて閉じる
- **主張は証拠ではない**。自分 / codex / エージェントの「対応済み」「テスト green」は、diff・実行結果・外部基準で裏を取ってから受け入れる。取れないものは「未検証」と報告する
  - 🚨 **「N 箇所すべてに対応した」と書くなら N を機械で数えてから** (`git show --stat` / `grep -c`。実測 2026-09-06: 4 と書いて実際は 3)
  - 🚨 **「等価」「挙動は変わらない」も書く前に compiler / テストで確かめる**。自分の書き換えは疑われにくい (実測 2026-09-21: exhaustive switch 化で 2 case を落とし、ビルドを壊したまま「完全に同一」と書いた)
- 自分で新設した安全機構は [`adversarial-review-own-safeguards.md`](rules/adversarial-review-own-safeguards.md)、防御を外すときは [`list-masked-failure-modes-before-removing-guard.md`](rules/list-masked-failure-modes-before-removing-guard.md)
- **敵対レビューの出力こそ無検閲で採用しない**。発火条件が具体的で再現できたものだけ直し、再現しないものは記録、示せないものは「未確認リスク」として issue / 観測ポイントに落とす。推測で防御コードを足さない (作法の正本は `~/.claude/skills/codex-review/SKILL.md` の「敵対的レビューの作法」)

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
| fable, fableっぽく, fable流, Fable の働き方, /fable | `~/.claude/skills/fable/SKILL.md` |
| クラッシュ, crash, .ips, DiagnosticReports, SIGSEGV, SIGABRT | `~/.claude/skills/crash-log-analyzer/SKILL.md` |
| codex-review, Codexレビュー, コードレビュー依頼 | `~/.claude/skills/codex-review/SKILL.md` |
| codexにリード, codex主導で着手, 設計から codex に任せて（実装は Claude）, codex-lead | `~/.claude/skills/codex-lead/SKILL.md` |
| codexに書かせて, codexメインで実装, codexに作らせて, 設計から実装まで codex に丸投げ, codex-drive | `~/.claude/skills/codex-drive/SKILL.md` |
| cross-review, クロスレビュー, 複数視点レビュー | `~/.claude/skills/cross-review/SKILL.md` |
| レビューループ, review-loop, make review | `~/.claude/skills/review-loop/SKILL.md` |
| 視認性, 色被り, UXレビュー | `~/.claude/skills/ux-visibility-review/SKILL.md` |
