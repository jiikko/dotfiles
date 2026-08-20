# 共通ルール

## 作業開始前の準備

- コードを書き始める前に、必ず `git pull` を実行して最新の状態に更新すること

## Git 禁止操作

- **無断で `git clone` しない。必要ならユーザーに許可を取ること**
- `git stash` を使用しない。ステージ済みの変更を退避したい場合は、別ブランチにコミットするか、ユーザーに確認すること
- サブモジュール内でコミットしたら、**必ずそのサブモジュールのリモートにも push する**。親リポジトリの push だけでは不十分。CI がサブモジュールの参照コミットを取得できず失敗する
- **コミット & push 前に `git status` で dirty なサブモジュールがないか確認すること**。dirty なサブモジュールがあれば、その中に入って差分を確認し、必要ならコミット & push してから親リポジトリの参照を更新すること。dirty を残したまま作業を終えない
- **commit / push 後は、成功を報告する前に実際の git state（`git log -1 --stat` / `git status` / push 出力）を確認すること**。ヘルパー関数やツール出力の「成功」表示を鵜呑みにしない（push 失敗や heredoc 破損を成功と誤報した実例がある）

## 並行作業者がいるときの worktree 退避

- **自分が作業を開始した後に、他の作業者（並行セッション・人間）によるファイル変更を確認できた場合**（例: 作業開始時点には無かった untracked ファイルが増えた、自分が触っていないファイルに新しい差分・ステージが現れた、自分の知らないコミットが積まれた）、**git worktree を作成してそこへ移動して作業してよい**（共有 working tree の index 競合・変更巻き込みを構造的に回避するため）
- **作業開始時点から存在する** dirty / untracked はこの条件に含めない（過去の作業の残骸かもしれず「今まさに並行作業中」の証拠ではない）。それらは従来どおり「触らない・巻き込まない」で共有 working tree のまま続行してよい
- worktree で作った**コミットを master ブランチへ移動できた時点で、作成した worktree は必ず削除する**（`git worktree remove`）。worktree を残したまま作業を終えない（放置 worktree は「どこに何があるか分からない」状態と stale ブランチを量産する）
- worktree を使わず共有 working tree に留まる場合は、pathspec 明示 commit の規律に従う（[`commit-with-pathspec.md`](rules/commit-with-pathspec.md)）
- **自分が書き込み権限のエージェント（`-s workspace-write` の codex 等）を 2 体以上並行させるときも worktree を分ける**。「担当ディレクトリが重ならない」を理由に同一 working tree で走らせない（想定外のファイルに手が伸びる / 並行中の build・test が何を検証したのか分からなくなる）。詳細は [`parallel-write-agents-need-worktree-isolation.md`](rules/parallel-write-agents-need-worktree-isolation.md)

## 応答・成果物の長さとスコープ

Opus 5 は既定で「よく喋り・よく書き・スコープを広げ・よく委譲する」方向に寄る（reasoning effort は思考量を制御するだけで、目に見える出力の長さは制御しない）。ここは明示指示でしか効かないので言語化しておく。出典: [公式ガイド](https://platform.claude.com/docs/ja/build-with-claude/prompt-engineering/prompting-claude-opus-5)（乖離に気づいたら同じ commit で直す）。

- **応答は本題に紙面を使う**。前置き・免責・注意書きは短く。「説明して」に対しては要点のサマリを返し、詳細な解説は明示的に求められたときだけ書く
- **成果物ドキュメント（レポート・md・要約）の長さはタスクの必要量に合わせる**。中身は尽くすが、埋め草セクション・重複したサマリ・定型文で嵩上げしない
- **進捗更新のペース**: 最初のツール呼び出し前に何をするかを一文で言う。作業中は重要な発見か方針転換があったときだけ短く挟む。完了時は結論（何が起きたか / 何が分かったか）から書き、根拠はその後
- **依頼されたスコープをそのまま完遂する**。ルーチンな判断は自分で下し、解釈が分かれると成果物が実質的に変わるときだけ確認する。依頼が誤っている / もっと良い方法があると思ったら一文で述べてから依頼どおり進める。黙って狭める・広げる・別物にすり替えるのはしない
  - 例外は「コード変更時の自律改善」が実行を義務付けている範囲だけ（境界の正本はその節と [`verify-design-intent-before-refactor.md`](rules/verify-design-intent-before-refactor.md) の「自律改善との境界」表。ここで再掲しない）。範囲外の気づきは「ぼやきポイント」に回す
- **訂正のナレーションは重要なものだけ**。ユーザーのコード・結論・判断が変わる誤りは端的に訂正して続ける。何も変わらない言い間違いは黙って直して進む（過去の誤りの列挙・自己批判はしない）
- **自分の判断で自己再チェックの手順を足さない**。Opus 5 は指示なしでも自分の作業を見直すため、その場の思いつきで「もう一度読み返すパス」「確認用サブエージェント」を積むのはトークンを増やすだけで品質を上げない。自発的にやる検証は具体的な外部事実の確認（実コマンド・テスト実行・実出力・git state）に寄せる
  - **これは既定のルールや明示起動した skill が定める検証・レビュー工程を省略する根拠にはならない**。「レビュー方針」の codex レビュー・敵対的レビュー、[`escalate-to-forge-after-failed-tries.md`](rules/escalate-to-forge-after-failed-tries.md) のエスカレーション、forge / cross-review / review-loop の各 Phase は要求どおり全て通す（これらはユーザーが選んだ工程であり、モデルの自己再チェックではない）

## 一時ファイルの配置

- **`/tmp` の使用は禁止. `./tmp` を使うこと。絶対に。**

## Issue管理

- `issue/*.md` の内容に対応した後、作業が完了したら対応するissueファイルを `issue/done/` ディレクトリに移動すること
- **issue の記述を鵜呑みにしない**。実際のコードと git 履歴に照らして検証してから着手する（既に修正済み・false positive を着手前に弾く）。関連: [`verify-design-intent-before-refactor.md`](rules/verify-design-intent-before-refactor.md)（refactor 提案の事前確認）/ [`issue-creation-codex-review.md`](rules/issue-creation-codex-review.md)（issue 作成時の codex レビュー）
- **人にやってほしい動作確認は応答本文に書いて流さず、issue に起こす**（chat は流れて存在自体が忘れられる）。`NNN-verify-<スラッグ>.md` で起票し、本文に `期限: YYYY-MM-DD` を書く。**既読はファイルの位置で表す**（未読 = `issues/`、確認済み = `issues/done/`。既読ヘッダーは本文の書き換え忘れで嘘が残るので使わない）。期限切れは `issue-sync` skill が最初に報告する

## 設計方針

- Godクラスを避けること。クラスが肥大化しそうな場合は、意味のある単位（責務ごと）でクラスを分割できないか検討すること
- 変更したファイルにGodクラス/Godファイルの予兆（責務の混在、過度な行数など）を見つけたら、リファクタリングを提案すること（ただし行数だけで判断せず、下記のとおり複雑性が実際に下がるかで判断する）
- **リファクタリングの目的は「複雑性を下げる」こと。行数が多いだけで単純にファイル/クラスを分けるのはリファクタではない**（分割は複雑性を移動するだけで削減しない）。「何をもって複雑性が下がるか」の判断基準と着手前の確認手順は [`verify-design-intent-before-refactor.md`](rules/verify-design-intent-before-refactor.md)
- バグフィックス後、そのプロジェクトに導入されているlinterのカスタムルールやpresetルールで再発防止できないか検討し、提案すること
- **zsh の hook (precmd/preexec) 経路から呼ぶ関数は `$(...)` でなく `REPLY` で返す**（fork が毎操作の体感レイテンシになる）。詳細は dotfiles repo の `rules/zsh-hook-return-via-reply.md`
- **カバレッジ向上を要求されても、対象が「テスト困難 かつ 低価値」の両方を満たすなら拒否する**（数値のための水増しテストを書かない）。判断は「テスト容易性 × 価値」の 2 軸で行い、困難×高価値は逃げずにテスタブルへ直してから書く。詳細は [`refuse-low-value-coverage.md`](rules/refuse-low-value-coverage.md)
- **新規テストは「壊す変更を 1 つ当てて red を見る」まで確認してから commit する**（green は「正しい」ではなく「その書き方では壊せなかった」）。変異させても green のままのテストは主張を何も守っていないので書き直す。詳細は [`mutation-verify-new-tests.md`](rules/mutation-verify-new-tests.md)
- **再利用される道具（スクリプト / CLI / Makefile target / lint ルール / ヘルパー）を新設したら、同じ変更で「入口のドキュメント」を更新する**（その作業手順を持つ skill・領域の CLAUDE.md / README・既存ツールの一覧表）。**ツールのヘッダコメントは入口に数えない**（そのファイルを開く動機は存在を知っている人にしかない）。既存ツールと使い分けが要るなら判断基準を 1 行で書く。詳細は [`new-tool-requires-entrypoint-docs.md`](rules/new-tool-requires-entrypoint-docs.md)


## コード変更時の自律改善

変更を入れる前に、変更先の周辺コードを読み、以下に該当するものがあれば **変更と同時に** 改善すること（提案ではなく実行）。

- **重複コード**: 同じロジックが複数箇所にある場合、まずヘルパー関数に抽出してから変更を入れる。同じ変更を2箇所にコピペするのは禁止
- **死んでいる分岐・未使用コード**: 変更対象の関数内に到達不能なコードがあれば削除する
- **不自然な責務の混在**: 変更を入れようとしている関数が明らかに複数の責務を持っていたら、まず分離してから変更を入れる

「依頼された変更だけ入れて終わり」ではなく、触ったコードを前より良い状態にして返すこと。ただし、依頼と無関係なファイルまで手を広げる必要はない。

## ぼやきポイント推奨

作業中に「依頼範囲外だが将来直したくなりそうな違和感」を見つけたら、応答の最後に **ぼやき（短い気づき）** として一言添えること。判断材料の提供であり、勝手に修正してはならない（依頼範囲外）。

- 対象例: 二重に実装されている規約・未統一のスタイル混在・ハードコード・マジックナンバー・テスト漏れの予兆・依存方向の歪み・命名の食い違い 等
- 形式: 「**なお、ぼやきポイント**: 〜」の一行〜数行。長文の分析にはしない。issue 化が妥当そうなら「issue 化しますか？」と一言添える
- 「タスクと無関係だから黙る」のではなく「無関係だが伝える価値があるなら一行ぼやく」
- 確信が低いもの・好みの問題・ユーザーが既知のものはぼやかなくてよい。ノイズになる

## 不具合対応の原則

**パッチワーク（症状への対処）ではなく、構造的な根本改修を行うこと。** これは最も重要な原則の一つである。

不具合対応は「ログを足して現象を追う」より先に、設計上の前提（契約）を見直して構造で潰す。

- まず **不変条件（Invariant）** を言語化する（例：deep link は失われない／同一ファイルの同一性は一意／UI失敗で再生は止まらない）
- **失敗モード**（順序競合・再送・二重実行・部分失敗・再起動）を列挙し、設計で吸収する
- **境界（main/renderer、UI/Domain、外部API）** ごとに責務を分離し、手続きの連鎖ではなく「コマンド＋結果」の形にする
- 同一性は **安定キー（id/path_lower 等）** に統一し、表示用文字列に依存しない
- 追加ログは最後の手段。必要なら「イベント／状態遷移」が観測できる設計にする
- **「この if 文を足せば直る」と思ったら立ち止まる** — その条件分岐が必要になった設計上の前提を疑うこと
- 修正が「症状への対処」ではなく「前提の是正」になっているかを必ず確認する。場当たり的な条件分岐の追加や、特定ケースだけを救うワークアラウンドは原則禁止
- **直したバグは「同じ間違いが別の場所にもある」前提で grep する** — 同じ API・同じイディオムの使用箇所を横断確認する。特にテスト側で見つけた不具合は production 側を、production で見つけたらテスト・別モジュール側を必ず見る（実例: `SecItemDelete` の単発呼び出しを test helper で直した後、production の同一バグが残っていた）
- **効果がなかった修正は必ず revert する** — バグ修正を入れて検証した結果、効果がなかった（的外れだった）場合、その修正をコードに残さず元に戻すこと。効果のない変更が積み重なるとコードの意図が不明瞭になり、将来の改修を妨げる
- **UI / デバイス / 環境に関わる問題は、修正を提案する前に実際の環境制約（入力手段・ツールのバージョン・他 platform の参照実装）を確認する**。詳細は [`check-other-platform-reference.md`](rules/check-other-platform-reference.md) / [`no-osascript-for-ui-verification.md`](rules/no-osascript-for-ui-verification.md) / [`no-ios-simulator-verification.md`](rules/no-ios-simulator-verification.md)

## レビュー方針

- **重要なコード変更・バグ修正は、設計と実装の両方を codex レビューに通すことを基本とする**（設計 → codex レビュー → 実装 → テスト → codex レビュー）。codex の指摘は無視せず、根拠の弱い断定・false positive を訂正してから commit する
- 起動は skill 経由（`codex-review` / `cross-review` / `review-loop` / `codex-lead` / `codex-drive`、下表参照）。typo・数行の chore など軽微な変更は対象外
- **レビューは「探す」だけで閉じない。壊しにいくパス（敵対的レビュー / red team）を 1 本混ぜる**。判断ロジック・境界・状態遷移・外部 I/O が動いた変更では、commit / PR クローズ前の最終ゲートとして通す（機械的置換・設定値変更だけなら省略してよいが、省略したことは一言明示する）
- **「指摘なし」は「正しい」ではなく「その探し方では壊せなかった」**として扱う。不変条件は「壊す方法が見つからなかった」ではなく、テスト・型・設計で固定して初めて閉じる
- **主張は証拠ではない**。自分/codex/エージェントの「対応済み」「検証済み」「テスト green」は、diff・実行結果・外部基準（spec の test vector / 実サーバ / CI）で裏を取ってから受け入れる。裏の取れない主張は「未検証」として報告に残す
- **自分で新設した「安全機構」(破壊的操作・後始末・検査/ゲート) は自己レビューで閉じない**。異常系 (対象 0 件 / 依存コマンド失敗 / 権限なし / 並行) を実験で作り、観点を分けた敵対的レビューを最終ゲートにする。詳細は [`adversarial-review-own-safeguards.md`](rules/adversarial-review-own-safeguards.md)
- **敵対レビューの出力こそ無検閲で採用しない**。発火条件（入力・順序・環境）が具体的で再現できたものだけ修正し、再現しないものは記録、示せないものは「未確認リスク」として issue / 観測ポイントに落とす。推測に基づく防御コードを足さない（作法の正本は `~/.claude/skills/codex-review/SKILL.md` の「敵対的レビューの作法」）

## スキルファイル参照

`~/.claude/skills/` に専門知識スキルが格納されている。以下のキーワードに関連するタスクでは、対応する SKILL.md を作業前に Read すること。

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
| fable, fableっぽく, fable流, Fable の働き方, /fable | `~/.claude/skills/fable/SKILL.md` |
| クラッシュ, crash, .ips, DiagnosticReports, SIGSEGV, SIGABRT | `~/.claude/skills/crash-log-analyzer/SKILL.md` |
| codex-review, Codexレビュー, コードレビュー依頼 | `~/.claude/skills/codex-review/SKILL.md` |
| codexにリード, codex主導で着手, 設計から codex に任せて（実装は Claude）, codex-lead | `~/.claude/skills/codex-lead/SKILL.md` |
| codexに書かせて, codexメインで実装, codexに作らせて, 設計から実装まで codex に丸投げ, codex-drive | `~/.claude/skills/codex-drive/SKILL.md` |
| cross-review, クロスレビュー, 複数視点レビュー | `~/.claude/skills/cross-review/SKILL.md` |
| レビューループ, review-loop, make review | `~/.claude/skills/review-loop/SKILL.md` |
| 視認性, 色被り, UXレビュー | `~/.claude/skills/ux-visibility-review/SKILL.md` |
