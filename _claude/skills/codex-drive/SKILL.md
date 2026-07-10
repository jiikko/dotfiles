---
name: codex-drive
version: 1.0.0
description: codex をメイン実装者として実コードを書かせ (codex exec -s workspace-write)、Claude はオーケストレーション (スコープ分割・成果物検証・観測駆動デバッグ・commit/push・反復) に徹するワークフロー。大きめの実装/移植/プロトコル実装で、余っている codex トークンを使い切りたい時に使う。「codex に書かせて」「codex メインで実装」「codex に作らせて」「codex-drive」「/codex-drive」で発火。typo・数行修正には使わない (それは Claude が直接やる)。
---

# Codex Drive（Claude が操縦、codex がメイン実装）

**codex に実コードを書かせ (write 権限)、Claude は「指示・検証・観測・反復・commit」に徹する**ワークフロー。
codex-lead が「codex に設計をリードさせ Claude が実装」なのに対し、本 skill は逆で **実装の主役が codex**、
Claude は **orchestrator/verifier**。新規ライブラリ・大きめ機能・プロトコル実装・大量移植など、コード量が多く
codex トークンを積極消費したいタスク向け。

## 役割分担（崩さない）

| 主体 | 担当 |
|---|---|
| **codex** (`codex exec -s workspace-write`) | 実ファイルの作成/編集、自分でプロジェクト標準の build/test を回して反復、spec 準拠の実装 |
| **Claude (main agent)** | タスクのスコープ分割 / codex への指示作文 / **成果物の検証 (build・test・diff 精読)** / **観測駆動デバッグ (実行・ログ・CI で事実を集め、次の指示に翻訳)** / commit & push / 次の一手の判断 |

**Claude は重い実装を自分で書かない**。ただし **trivial な 1-2 行の確定的修正** (観測で原因が確定した typo・定数・オフセット) は
codex 往復より速いので Claude が直接やってよい (`subagent-model-tiering.md` の例外と同じ判断)。

## 大原則

- **codex の出力を無検閲で commit しない** (`subagent-model-tiering.md`)。Claude が必ず `build`/`test`/diff を見てから commit。
- **codex に git commit させない**。commit/push は Claude が行う (検閲後)。codex には「commit しない」と毎回明示。
- **1 タスク = 1 検証可能なマイルストーン**にスコープする。codex に「全部」を投げない (15 分制約・精度低下)。
- **観測駆動** (`instrument-before-second-fix.md`): 修正が外れたら次の blind fix を投げず、まず観測 (ログ/実行/CI) を増やして
  事実を取り、それを codex への次の指示に翻訳する。CI でしか出ない移植/プロトコルバグはこの往復で 1 段ずつ潰す。
- **モデルはスキル側で明示する**: `-m gpt-5.6-luna -c model_reasoning_effort="low"`。省略すると `~/.codex/config.toml` の
  既定 (対話 TUI 側の都合で変わる) を拾い、実行ごとにモデルが変わってしまう。

## Phase 0: codex 適性評価（着手前に必ず・最初にやる）

codex-drive を回す前に、**そのタスクが codex 実装に向いているか**を Claude が評価する。向いていない領域を
無評価で codex に投げると、もっともらしく動かない/見た目が破綻したコードを量産し検証で空回りする。

### codex に向く（そのまま codex-drive で進めてよい）

- **プロトコル/wire 実装・codec・パーサ** (バイト構造・spec 準拠・エンディアン)
- **アルゴリズム/暗号プリミティブ** (test vector で正誤が機械判定できる)
- **大量の同型変換・移植・rename・boilerplate**
- **CLI / ライブラリ境界の純ロジック** (入出力が決定的で unit/CI で検証できる)
- 仕様書 (RFC/MS-* 等) と test vector があり、**正解が客観的に確定できる**もの

### codex が苦手（一度ユーザーに確認を入れてから進める）

- **UI / UX / 視覚レイアウト** (SwiftUI/AppKit/CSS の見た目・余白・色・アニメーション)。
  「動く」と「見た目/操作感が正しい」が乖離しやすく、スクショ/実機/人間の目視確認を受け入れ条件に入れる必要がある領域。
  **今回ユーザー明言: UI は苦手**
- **AppKit/SwiftUI のランタイム挙動** (focus/responder/AttributeGraph 等、実行時にしか分からない振る舞い)
- **デバイス/実機/GUI 操作に依存する検証** (権限・GUI・ネットワーク・実機アクセスに制約されやすく、自走確認を信用しにくい)
- **主観的・美的判断、プロダクトの方向性、曖昧仕様** (正解が客観確定できない)
- **大きなアーキテクチャ設計判断を単独で丸投げすること** (前提・制約・受け入れ条件は人間/Claude が握る。設計リードから任せたい場合は codex-lead や forge も検討)

### その他の既知の弱点（向く領域でも検証で必ず潰す。SMBee セッション等で実観測）

これらは「codex に任せない」ほどではないが、**codex の自己申告/自己テストを信用せず Claude が外部基準で検証**する。

- **循環テストになりがち**: codex は自分のエンコードを自分でデコードして通る round-trip テストを書きやすい。
  実サーバ/spec の公式 test vector で照合しないと「テスト green なのに実機で wire 拒否」が起きる
  (実例: NEGOTIATE round-trip は green だが Samba が INVALID_PARAMETER)。**外部 vector / 実通信で必ず裏取り**。
- **バージョン差・実在しない API/設定キーの捏造**: 学習時点と環境のズレで、存在しない設定オプションや
  古い API をもっともらしく書く (実例: Samba の `smb3 signing algorithms` は当該版に存在せず無視された)。
  **設定キー/API は実環境・実ドキュメントで存在確認**。
- **クロスプラットフォーム/別 toolchain のコンパイル不能を見落とす**: codex は自分が動く環境 (例 macOS) で
  しか確かめられず、他ターゲット (Linux/別 Swift 版) の差異を踏む (実例: glibc の addrinfo メンバ順 /
  SOCK_STREAM 型 / IPPROTO_TCP 型)。**ターゲット環境 (CI 等) で必ずビルド**。
- **実環境の観測が環境制約に左右される**: Docker/実機/実サーバ/CI は、権限・ネットワーク・GUI・認証の制約で
  codex の自己完結確認が失敗/不完全になりやすい。実行依存の正否は Claude が CI/実機/実サーバで確認し、
  結果を codex に渡す (本 skill の観測駆動ループ [6])。
- **過大申告 (overclaim)**: 「確認済み」「対応済み」と未検証で書くことがある。サマリの主張は diff/実行で裏取り。
- **自分のサンドボックス制約への場当たり回避**: `--disable-sandbox` 等で「通った」と報告しがち。
  Claude は**素の環境で**ビルド/テストし直す。
- **広域の一貫性 (多数ファイル横断)**: 大きな sweep で命名/規約の一貫性を崩すことがある。範囲を区切り diff を精読。

### 判定と分岐

1. タスクを上記で分類する。**向く** → そのまま `[1]` へ。
2. **苦手領域に該当 / 判断に迷う**場合は、**実装に入る前に一度ユーザーに確認する**:
   - 「このタスクは codex が苦手な〈UI/実機/主観判断〉を含むので、(a) それでも codex-drive で進める / (b) Claude が
     直接やる / (c) forge 等別アプローチ、のどれにしますか？」と選択肢を添えて聞く。
   - UI を含む複合タスクなら、**苦手部分を切り出して**「ロジック/wire は codex-drive、UI は Claude/別途」と分割提案する。
3. ユーザーが「それでも codex で」と言えば進める。確認なしに苦手タスクを codex に丸投げしない。

## ワークフロー

```
[0] 適性評価         … 上記。苦手領域なら一度ユーザーに確認 (UI 等)
[1] スコープ確定     … Claude が「1 マイルストーン」を切り、受け入れ条件 (検証方法) を決める
[2] codex 実装       … codex exec -s workspace-write に指示。codex がファイルを書きプロジェクト標準の build/test まで回す
[3] 検証 (検閲)      … Claude が build/test を自分で実行 + diff 精読。根拠なき断定・spec 取り違え・指示逸脱を弾く
[4] commit & push    … 自分の差分のみ commit (commit-policy)。push は必要時
[5] 実地検証 (任意)   … 実行 / 実機 / CI で動かす。失敗したら [6]
[6] 観測 → 次の指示   … blind fix しない。観測を増やし事実を取り、codex への次の的確な指示にする → [2]
```

各マイルストーンが green になったら次のマイルストーンへ。マイルストーン間で Claude が状況を要約し、必要なら人間に判断を仰ぐ。

## 手順詳細

### 1. スコープ確定（Claude）

- 今回の **1 マイルストーン**を決める (例: 「probe が NEGOTIATE して交渉結果を出す」)。大機能は複数マイルストーンに割る。
- **受け入れ条件 = どう検証するか**を先に決める (プロジェクト標準 build/test green / CLI 出力 / CI E2E green / 実機ログ。
  Swift プロジェクトなら `swift build` / `swift test`)。
- 触ってよい範囲・触らない範囲・既存方針 (設計 doc 等) を明示する。

### 2. codex に実装させる（write 権限）

```bash
# 最終応答の保存先は ./tmp ではなくセッション scratchpad 等の一意パスに。`</dev/null` 必須 (codex-review ルール)。
last_message="<scratchpad>/codex-drive.$(date +%Y%m%d-%H%M%S).$$.last-message.txt"
command codex exec -s workspace-write -m gpt-5.6-luna -c model_reasoning_effort="low" \
  --ephemeral -o "$last_message" </dev/null "$(cat <<'EOF'
<タスク>。git commit はしない (人間が検証して commit する)。ファイルを書き、プロジェクト標準の build/test が green に
なるまで自分で反復すること。Swift プロジェクトなら swift build / swift test を使う。

## ゴール / 受け入れ条件
- <1 マイルストーンの完了条件と検証方法>

## やること
- <具体的な実装項目。spec があれば節番号で参照>

## 制約
- <触らない領域 / 既存方針 / 依存方針 / Linux ビルド維持 等>
- 確証が持てない点は決め打ちせず ⓥ コメントを残し保守的に実装する。
終わったら、変更点・検証結果・未完部分を要約。
EOF
)" 2>&1 | tail -40
```

- `command codex` プレフィックス / `</dev/null` / `--ephemeral -o` は **codex-review スキルのルールが正本**（理由・実測根拠はそちら）。
  `-o` は実行ログ全体ではなく最終応答 (`--output-last-message`) の保存先。標準出力/標準エラーは必要に応じて呼び出し側で保存する。
- **`--full-auto` は使わない**。実装は `-s workspace-write` を明示 (review の `-s read-only` とは別)。
- 大きいタスクは codex が時間内に終わらないことがある。プロンプトに「時間内に終わらなければ最小で動く形を優先し、
  残りは TODO で残す」と書く。
- コマンド実行ツールの timeout は 900000ms。

### 3. 検証（Claude が必ず検閲）

- **自分の環境でプロジェクト標準の build/test を実行**して green を確認 (Swift プロジェクトなら `swift build` / `swift test`。
  codex 環境の sandbox 差で codex 報告が `--disable-sandbox` 等になっていても、Claude は素の環境で確認する)。
- **diff を精読**: 根拠なき断定 (「実装済み」「動作確認済み」)・spec 取り違え・指示外ファイルへの変更・半端な編集を弾く。
- 軽微な確定的問題 (typo・命名) は Claude が直接直してよい。設計に関わる修正は codex に戻す。

### 4. commit & push（Claude）

- **自分が触った差分のみ** `git add <path>` (commit-policy)。並行する他作業/WIP を巻き込まない。
- 1 マイルストーン = 1 commit を基本に。commit message に「codex 実装 + main agent 検証」と何をやったかを書く。
- push は必要時のみ (CI で実地検証したい時など)。push 可否ルールは各リポジトリに従う。
- submodule なら commit 後に即 push し親参照を bump (submodule-workflow)。

### 5. 実地検証（任意・該当時）

- ユニットで担保できない部分 (実通信・実機・統合) は、実行 / CI / 実機で動かす。
- 例: CI E2E を push でトリガし `gh run watch <id> --exit-status` で結果を待つ。GUI/実機など自走確認が難しい検証は
  スクショ・ログ・人間確認を受け入れ条件に含める。

### 6. 観測 → 次の指示（blind fix 禁止）

- 失敗したら **2 発目の blind fix を打たない** (`instrument-before-second-fix.md`)。
- まず **観測を増やす**: ログ/hex dump/実 status/成功経路との差分。「何が見えていないか」を可視化する診断を
  (Claude が小さく入れるか codex に入れさせて) 仕込み、実行/CI で**事実**を取る。
- 取れた事実 (実 status・実バイト・差分) を **codex への次の的確な指示**に翻訳して [2] に戻る。
- これにより、CI/実機でしか再現しない移植・プロトコル・wire バグを 1 往復 1 バグで確実に収束させる。

## codex-lead / forge との使い分け

| skill | 主役 | 使う場面 |
|---|---|---|
| **codex-drive** (本 skill) | **codex が実装**、Claude が検証/観測/反復 | コード量が多い実装・移植・プロトコル実装。codex トークンを積極消費 |
| codex-lead | codex が設計リード、Claude が実装 | 設計判断から codex に任せたい。実装の主役は Claude |
| forge | 専門家エージェント並行 + クロスレビュー | 高品質・多視点が要る実装/レビュー。バグ修正の自前試行が 1-2 回失敗した escalation 先 |
| codex-review / cross-review | レビューのみ | コードは変えない |

## やること / やらないこと

- ✓ 着手前に codex 適性を評価し、苦手領域 (UI/実機/主観判断) なら一度ユーザーに確認する
- ✓ codex に実装の主役を任せ、Claude は検証・観測・commit・反復に徹する
- ✓ 1 マイルストーンに絞り、受け入れ条件 (検証方法) を先に決める
- ✓ codex 出力は必ず build/test/diff で検閲してから commit
- ✓ 失敗時は観測を増やしてから次の指示を出す (blind fix しない)
- ✗ codex に git commit させる / 無検閲で commit する
- ✗ 重い実装を Claude が自分で書く (trivial な確定修正は例外)
- ✗ 「全部まとめて」を 1 回の codex 実行に投げる

## 関連

- `~/.claude/skills/codex-review/SKILL.md` — `command codex` / `</dev/null` / `--full-auto` 禁止などコマンド作法の正本。検証フェーズのレビュー委譲先
- `~/.claude/skills/codex-lead/SKILL.md` — 逆の分担 (codex 設計リード + Claude 実装)
- `~/dotfiles/_claude/rules/subagent-model-tiering.md` — 下位主体の出力は main が必ず検閲
- `~/dotfiles/_claude/rules/instrument-before-second-fix.md` — 観測駆動デバッグ (本 skill の [6] の正本)
- `~/dotfiles/_claude/rules/escalate-to-forge-after-failed-tries.md` — 収束しない時の escalation
