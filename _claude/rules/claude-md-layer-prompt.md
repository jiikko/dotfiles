# レイヤーディレクトリに CLAUDE.md が無ければ問いかけるルール

## ルール

- **「レイヤーディレクトリ」で非自明な編集 (= ロジック追加 / 構造変更 / 規約付与) を行う前に、そのディレクトリに `CLAUDE.md` が存在するか確認する**
- **存在しない場合は、勝手に作成しない / 黙って続行しない。ユーザーに作成可否を一度だけ問いかける**
- ユーザーが「作る」と言ったら作成し、「作らない」と言ったらその **セッション中はそのディレクトリについて再度問わない** (繰り返し聞かない)
- 問いかけは「なぜ作成を提案するのか (= 該当ディレクトリの責務 / ファイル数 / 推測される local convention)」を 2〜3 行で添える。yes/no だけ聞かない

## 「レイヤーディレクトリ」の定義

責務が明確で、ファイルが意味的にまとまっているディレクトリ。具体的には以下のいずれかを満たすもの。

- 名前そのものが責務を示している (`Models/`, `Views/`, `ViewModels/`, `Services/`, `Storage/`, `Repositories/`, `Controllers/`, `Components/`, `Tests/`, `Resources/`, `Migrations/`, `cmd/`, `internal/<feature>/`, `app/<feature>/`, `pkg/<feature>/` 等)
- ある機能 / タブ / モジュールに対応するディレクトリ (`Diary/`, `Memo/`, `Settings/`, `Auth/`, `Billing/` 等)
- Swift / Go / Rails の Package / Module ルート (`Package.swift` / `go.mod` / `Gemfile` がある or それに準ずるディレクトリ)
- そのディレクトリ配下に **5 個以上の source file** がある or **2 階層以上のサブディレクトリ** がある

逆に「レイヤーディレクトリではない」もの (= 問いかけ対象外):

- ビルド成果物 (`build/`, `tmp/`, `.build/`, `DerivedData/`, `node_modules/`, `vendor/`, `dist/`, `target/`)
- 自動生成ディレクトリ (`*.xcodeproj/`, `.swiftpm/`, `__pycache__/`, `.gradle/`)
- 設定ファイル単独の置き場 (`signing/`, `.github/workflows/` 等、ファイル 1〜2 個でレビューポイントが無い)
- バイナリ / asset のみのディレクトリ (`*.xcassets/`, `images/`, `fonts/`)

判断に迷う場合は **「新しい人がここに何があるか把握するのに 5 分以上かかりそうか」** で判定する。かかりそうならレイヤーディレクトリ扱い。

## 「非自明な編集」の定義

問いかけのトリガになる作業:

- ✓ そのディレクトリに **新規ファイル** を作る
- ✓ そのディレクトリ内の **既存ファイルを 30 行以上変更** する (typo 修正 / リネーム以外)
- ✓ そのディレクトリの **構造を変える** (サブディレクトリ作成、ファイル移動)
- ✓ そのディレクトリの **責務 / 規約 / 制約に関わる変更** をする (例: lifecycle 制御の追加、validation の追加、外部依存の差し替え)

問いかけ不要 (= 軽微):

- ✗ typo / format の修正
- ✗ コメントの追加 / 修正
- ✗ import 文の追加
- ✗ 1 ファイル内の 30 行未満の bug fix
- ✗ ユーザーが「これだけ直して」と明示的に scope を絞っている軽微なタスク

## 問いかけの形式

雛形:

```
このディレクトリ (`<path>`) に CLAUDE.md がありません。以下の理由から作成を提案します。

- 配置されているファイル: <N> 個 (主なもの: <file1>, <file2>, <file3>)
- 推測される責務: <一行>
- 触りそうな local convention の候補: <一行>

作成しますか?
1. 作る (内容案を提示してから書く)
2. 作らない (今は不要)
3. あとで判断する (このセッションでは聞かない)
```

3 択にする理由: 「あとで」を許容しないと「念のため作る」or「拒否」に二極化しがちで、ユーザーの判断負荷が高い。「あとで」を選ばれたら本ルールの問いかけはそのセッション中スキップする。

作成すると返答が来たら、まず **内容案 (= section 構成と各 section の bullet 案)** を 5〜10 行で提示し、合意を取ってから `Write` する。いきなりフルテキストを書かない (= 後戻りコストが大きい)。

## 作成判定の最終ガード

問いかけ前に **`claude-md-maintenance.md` の「作るタイミング」基準** を満たすか自己確認する。満たさないなら **問いかけ自体をスキップして良い** (ユーザーに無駄な選択を強いない)。

満たす基準 (= 問いかける条件):

- 固有規約 / gotcha / 制約 が 3 個以上ある (実コードを読んで判断)
- 親 CLAUDE.md から `<dir>/CLAUDE.md` 参照と書かれている / 書かれそう
- 新しい人が読まないと必ず事故る規約がある
- ファイル 5 個以上 + 責務が複数で入口の地図が必要

これらが **どれも当てはまらない** (= 中身が単純で local convention が無い) なら、無理に問いかけない。「とりあえず CLAUDE.md を置く」は禁止 (空 / 形骸化を増やすだけ)。

## やること / やらないこと

- ✓ レイヤーディレクトリで非自明な編集をする **前** にチェック
- ✓ 不在時は問いかける (3 択: 作る / 作らない / あとで)
- ✓ 「作る」となったら内容案 → 合意 → 書く、の順を守る
- ✓ `claude-md-maintenance.md` の「作るタイミング」基準を自己ガードに使う
- ✗ 無断で作成する (= ユーザーが望まない CLAUDE.md が増える)
- ✗ 黙って続行する (= 後から「このディレクトリ規約あった?」と聞かれて答えられない)
- ✗ 軽微な変更でも毎回聞く (= ノイズ。typo 修正で問わない)
- ✗ 一度「作らない」と言われたものを同セッションで再度問う
- ✗ 中身が単純で local convention が無いディレクトリで問いかける (= ユーザー時間の浪費)

## 例

### 例 1: 問いかけるケース

```
ユーザー: Sources/Services/ に新しい AuthService.swift を追加して

Claude: このディレクトリ (`Sources/Services/`) に CLAUDE.md がありません。
以下の理由から作成を提案します。

- 配置されているファイル: 6 個 (KeychainService, NetworkService, AnalyticsService, ...)
- 推測される責務: 外部 IO を伴うアプリケーションサービス層
- 触りそうな local convention 候補: actor 隔離方針 / error 型の統一 / DI の入口

作成しますか?
  1. 作る (内容案を提示してから書く)
  2. 作らない (今は不要)
  3. あとで判断する (このセッションでは聞かない)
```

### 例 2: 問いかけないケース (軽微)

```
ユーザー: Sources/Services/KeychainService.swift の typo を直して

Claude: (修正のみ実施。CLAUDE.md の有無に関する問いかけはしない)
```

### 例 3: 問いかけないケース (レイヤーディレクトリでない)

```
ユーザー: signing/ExportOptions-AppStore.plist の uploadBitcode を YES に変えて

Claude: (signing/ は設定ファイル単独置き場 = レイヤーディレクトリではない。
       問いかけスキップして修正のみ実施)
```

### 例 4: 問いかけないケース (作成基準を満たさない)

```
ユーザー: Sources/Helpers/ に NumberFormatter+Currency.swift を追加して

Claude: (Helpers/ にファイルが 2 個しかなく、local convention も無い。
       `claude-md-maintenance.md` の作成基準を満たさないので問いかけスキップ。
       追加のみ実施)
```

## 関連

- 姉妹ルール: [`claude-md-maintenance.md`](claude-md-maintenance.md) — CLAUDE.md の保守 (touch base で乖離を直す) と作成 / 削除タイミング基準。本ルールの自己ガードに使う
- 関連global rule: [`pending-issue-rationale-in-code.md`](pending-issue-rationale-in-code.md) — 「対応しない判断はコード側にコメント」と同じく「Why を残す」思想
- Anthropic blog: [How Claude Code works in large codebases — best practices](https://claude.com/blog/how-claude-code-works-in-large-codebases-best-practices-and-where-to-start) — "lean and layered" CLAUDE.md の出典
