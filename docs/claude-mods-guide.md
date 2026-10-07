# mods の全体像と変更の仕方 (入門・地図)

Claude Code の mods (関数 hook の plugin) が dotfiles でどう置かれ、どう読み込まれ、どう直すかの地図。
**判断・実測・制約の正本は [`claude-mods.md`](claude-mods.md)** (このマシンで届かないイベント、読み込まれる経路の実測、pro-con に載せない理由)。
ここには「どのファイルが何をするか」と「触るときの手順」だけを置く。

## 公式ドキュメント

API・イベント・部品の一次情報は公式 (英語)。この文書は dotfiles での置き方と手順だけを書く。

- [Mods overview](https://code.claude.com/docs/en/plugins/mods/overview) — mods とは何か・どこで動くか (CLI / desktop の Code タブ / `claude -p` …)・settings の hook / skill / MCP との比較
- [Create a mod](https://code.claude.com/docs/en/plugins/mods/create) — 作り方と、編集 → 読み直しの流れ
- [Draw in the interface](https://code.claude.com/docs/en/plugins/mods/interface) — pane・プロンプトの上の帯・ボタン・状態
- [React to events](https://code.claude.com/docs/en/plugins/mods/events) — tool call・プロンプト・ターン、mod の実行順
- [Use the mods API](https://code.claude.com/docs/en/plugins/mods/api) — コマンド・tool・model の呼び出し・timer・ファイル
- [Test a mod](https://code.claude.com/docs/en/plugins/mods/test) / [Troubleshoot a mod](https://code.claude.com/docs/en/plugins/mods/troubleshoot)
- [Mods reference](https://code.claude.com/docs/en/plugins/mods/reference) — イベント・メソッド・部品・制限の一覧
- [Manage mods for your organization](https://code.claude.com/docs/en/plugins/mods/admin) — managed settings で mod を止める・審査する (このマシンで一部のイベントが届かない理由に関係。`claude-mods.md`)
- 見本: [公式の見本の mod](https://github.com/anthropics/claude-code-playground/tree/main/claude-code/mods) / [Claude Code 組み込みの mod のソース](https://github.com/anthropics/claude-code/tree/main/mods)

2026-10-03 に overview の実在と内容を確かめた (他のページは overview のリンクと検索結果から)。手元の `plugin-authoring` skill の型定義 (`claude-code.d.ts`) は、
動いている版の API そのもので、公式ページより細かい。食い違ったら型定義を信じる。

## mods とは

- Claude Code の**中**で動く TypeScript の小さな plugin。イベント (`session.start` / `turn.complete` / `ui.render` …) に hook を掛けて、画面に帯を足すなどをする
- dotfiles では**画面に足すものだけ**を mod にする (issue-band・desktop-statusline)。守りの hook (deny / block) は settings の hook のまま
  (mod の hook は失敗すると黙ってスキップされるため。epic issue 618)
- API は early access でリリースごとに変わる。正本は `plugin-authoring` skill が書き出す型定義 `claude-code.d.ts`

## 全体像

```
~/.claude/settings.json  (= ~/dotfiles/_claude/settings.json へのリンク。どの repo のセッションにも効く)
  └ env.CLAUDE_CODE_PLUGIN_DIRS = ~/dotfiles/_claude/mods
        │  セッション起動時に 1 回、子のディレクトリ (= mod) を全部読む
        ▼
_claude/mods/<name>/                 1 つの mod = 1 ディレクトリ
  .claude-plugin/plugin.json         名前・版・型の契約の場所 (manifest。これが無いディレクトリは読まれない)
  hooks/hooks.json                   { "modules": ["./register.tsx"] }  どのファイルを読むか
  hooks/register.tsx                 ★本体。register(on) でイベントに hook を掛ける
  hooks/*.ts                         純粋関数 (テストしやすいよう register から分けたもの)
  types/index.d.ts                   $.state に置く値の型の契約 (値を持つ mod だけ)
  tests/*.test.ts(x)                 `claude plugin test` が回すテスト (0 件は失敗扱い)
```

- `.claude-plugin/types/` と `tsconfig.json` は engine が自動で書く生成物で、`.gitignore` 済み (触らない)
- mod を足しても settings は書き換えない (親フォルダを渡してあり、子を全部読む)

## 今ある mod

| mod | 何をするか | 主なファイル |
|---|---|---|
| `canary` | 何もしない。`DOTFILES_MOD_CANARY_DIR` があれば「読み込まれた印」を書く (読まれる経路の実測用) | `hooks/register.ts` |
| `issue-band` | 期限切れ / 期限間近の human と未決着の retro があるときだけ、プロンプトの上の帯に出す | `hooks/register.tsx` (配線) / `hooks/band.ts` (出力の読み取りと文言) |
| `desktop-statusline` | Claude desktop の Code タブに、CLI と同じステータスバーを色つきで出す。CLI には描かない | 下の節 |
| `peer-inbox` | 他セッション / teammate との SendMessage の送受信を、プロンプトの上の帯 (直近 3 件・既読 / 隠す)・toast・status line の未読数に残す。transcript に流れる本文は止めない (流れない写し) | `hooks/register.tsx` |

## desktop-statusline の仕組み

CLI は settings の `statusLine` が `_claude/statusline-command.sh` を実行して描く。desktop の Code タブは `statusLine` を**実行しない**ので (issue 625 で観測)、
mod が**同じ script** を呼んで、出力を desktop の部品に直して描く。**表示内容 (項目・色の閾値) の出典は script の 1 つだけ**で、mod にはロジックを写していない。

```
トリガー ──▶ refresh() ──▶ $.state (lines / error) ──▶ ui.render (AbovePrompt) が読んで描く
  │            │
  │            ├ $.session.cwd / model / usage / id と環境変数 CLAUDE_EFFORT を集める
  │            ├ statusInput()  CLI の statusLine と同じ形の JSON にする          (hooks/input.ts)
  │            ├ $.process.run  _claude/statusline-command.sh を stdin に JSON で実行
  │            └ parseAnsi()    出力の ANSI の色を desktop の Text の属性に置き換える (hooks/ansi.ts)
  │
  ├ session.start        (最初のプロンプトの前に待つ) + 60 秒ごとの timer を開始
  ├ turn.complete        メインのターンの終わり (サブエージェントのターンでは作らない。裏で走らせる)
  ├ 60 秒ごと            CLI の refreshInterval と同じ
  └ /statusline-refresh  手動 (下の節)
```

| ファイル | 役割 |
|---|---|
| `hooks/register.tsx` | 全体の配線。トリガー・`refresh`・描画・コマンド。`isDesktopHosted` で「desktop が起こしたセッションか」を判定 |
| `hooks/input.ts` | `$` から取った値を、statusline script が読む JSON の形にする (リセット時刻は ISO → エポック秒、モデル id → `Opus 5.5` の形) |
| `hooks/ansi.ts` | ANSI → desktop の色。**16 色の hex パレット (`FG` / `BG`) が固定**。script が使う SGR だけを扱い、点滅は捨てる |
| `types/index.d.ts` | `$.state` の `lines` / `error` の型 |
| `tests/pure.test.ts` | `ansi.ts` / `input.ts` のテスト |
| `tests/register.test.tsx` | 配線のテスト (desktop の開始の状態から始める fixture) |

要点 (なぜそうなっているか):

- **描画の中では script を呼ばず、`$.state` を読むだけ**。描画 hook は state を書けず (書くと拒否される)、外部 script を毎回呼ぶと重い。
  書き込みはトリガー側で行い、書くと読んでいる描画が自動で再描画される
- **描くのは desktop の surface だけ** (`e.surface === 'desktop'`)。CLI の `statusLine` と二重に出さない
- **「desktop のセッションか」は環境変数 `CLAUDE_CODE_ENTRYPOINT=claude-desktop`** で決める。名簿 (`$.session.surfaces()`) や `e.isInteractive` は、
  desktop が起こすセッションの開始時には空 / false のため使えない (issue 625。詳細と注意は `claude-mods.md`)
- script が失敗・空出力のときは、黙らず帯に `ステータスバーを作れない: <理由>` を 1 行出す
- 既知の課題: 色のパレットが固定でテーマ (ライト / ダーク) に連動しない。script の桁揃えは等幅フォントが前提で、desktop では崩れうる (未対応。issue 625)

### `/statusline-refresh` とは

**スラッシュコマンド**で、skill でも実行ファイルでもない。desktop が起こしたセッションの入力欄で `/statusline-refresh` と打つと、script を今すぐ呼び直して帯を作り直し、
結果 (「作り直した」または失敗の理由) を出力に返す。返信やタイマーを待ちたくないときに使う。

- ソース: `_claude/mods/desktop-statusline/hooks/register.tsx` だけ。`session.start` の中の `$.command.register({ name: 'statusline-refresh', … })` が登録、
  `on('command.run', { command: 'statusline-refresh' }, …)` が実行。`~/.claude/skills/` や `~/.claude/commands/` には何も置かない
- CLI のセッションには登録しない (CLI は `statusLine` が自動で更新する)

## issue-band の仕組み

`session.start` と `turn.complete` で `_claude/hooks/human-tasks-due.sh --counts` と `retro-open.sh --counts` を呼び、件数を `$.state` に置き、
`ui.render` (AbovePrompt) が件数があるときだけ帯を描く (件数 0 なら何も描かない = 黙る)。terminal と desktop の両方に描く。
数えるロジックは script にあり、`hooks/band.ts` は出力の読み取りと文言だけ。

## 変更の仕方

### 既存の mod を直す

1. **worktree で作業する** (この repo の規約。`.claude/rules/worktree-per-session.md`)
2. 編集する。**実装を変えたらテストも同じ変更で直す** (fixture は実際の初期状態から始める。desktop の開始は `surface: null` / `isInteractive: false` / 名簿空 / 環境変数あり)
3. 検査する

   ```bash
   cd _claude/mods && claude plugin validate desktop-statusline && claude plugin test desktop-statusline
   ```

   repo 全体の検査は `bash tests/claude/test_claude_mods.sh` (`make test` に含まれる。CI は claude が無いので skip し、手元が正本)
4. 新しいテストは**変異を 1 つ当てて red になる**ことを確かめる (`_claude/rules/mutation-verify-new-tests.md`)
5. commit → `git push origin HEAD:master` → **`scripts/pull_main_checkout.sh`** で `~/dotfiles` を追い付かせる。
   mod が読まれるのは `~/dotfiles` の実体なので、**pull するまで効かない**
6. **新しい desktop のセッションを開いて確かめる** (desktop は mod のフォルダを見張らないので、既存のセッションは旧版のまま)

反映のタイミングの違い:

| 直したもの | 反映 |
|---|---|
| `_claude/statusline-command.sh` (表示内容) | pull 後、次の更新 (最大 60 秒 / `/statusline-refresh`) から。セッションの開き直しは不要 |
| mod のコード (`hooks/*`) | 新しいセッションから。公式 (overview) によると、開いているセッションで `/reload-plugins` を打つと読み直す (desktop で効くかは未実測) |

### 新しい mod を足す

1. `_claude/mods/<name>/` に `.claude-plugin/plugin.json`・`hooks/hooks.json`・`hooks/register.tsx`・`tests/*.test.tsx` を作る (既存の mod をひな形にする)
2. 値を `$.state` に置くなら `types/index.d.ts` に型を宣言し、`plugin.json` の `"types"` で指す
3. この文書の「今ある mod」表に 1 行足す (一覧の正本はここ)
4. `bash tests/claude/test_claude_mods.sh` が `_claude/mods/*/` を自動で見つけて検査する (manifest 無し・テスト 0 件は失敗)

### 試すだけ (commit せずに、動いている Claude のセッションで)

`plugin-authoring` skill を読み込んだセッションでは、`~/.claude/dev-mods/<セッション ID>/<mod 名>/` に mod を書くと、
「ホットリロードを有効にするか」の確認が出て、有効にするとそのセッションに**即座に**載る (保存するたびに、そのターンの終わりに読み直される)。
そのセッション限りで dotfiles には残らない。desktop で見た目を何度も確かめるときに使う (issue 625 の切り分けはこれでやった)。

### 型 (API) を調べる

`plugin-authoring` skill を読み込むと、このセッションの `claude-code.d.ts` の場所が出る (セッションごとに変わる)。イベントの入力・`$` の各口・各部品の props がコメント付きで入っている。
`grep -n "'turn.complete'"` のように名前で引く。

## つまずき

| 症状 | 見るところ |
|---|---|
| 直したのに desktop に反映されない | 新しいセッションを開いたか (または `/reload-plugins`。desktop では未実測) / `~/dotfiles` に pull したか (`git -C ~/dotfiles status -sb` の `behind`) |
| desktop に何も出ない | 新しいセッションか / `printenv CLAUDE_CODE_ENTRYPOINT` が `claude-desktop` か / 帯に `ステータスバーを作れない` が出ていないか / `claude plugin validate` が通るか |
| どの repo でも出る? | 出る (設定はユーザー全体)。issue-band は開いた repo に issue と件数があるときだけ |
| テストが「0 件」で落ちる | mod にテストが無い。`tests/` に 1 本以上置く |
| 描画 hook で state を書いたら拒否された | 描画は読むだけ。書き込みはトリガー側 (`session.start` / `turn.complete` / timer) で行う |
| mod の変数が消える | 読み直しで `register` が走り直すため、モジュールの変数は初期化される。値は `$.state` に置く |
| 子の `claude -p` でも mod が走る | 環境変数が継承されるため。起動を待たせる処理は裏で走らせる (issue-band の `void refresh`) |

## 関連

- [`claude-mods.md`](claude-mods.md) — 置き場所・読み込み経路の実測・届かないイベント・テストと CI
- epic issue 618 (どの hook を mod へ移すか) / issue 625 (desktop のステータスバーの経緯と未確認リスク)
