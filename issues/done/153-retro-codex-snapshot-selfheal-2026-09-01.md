# retro: codex() の snapshot self-heal (issue 149) (2026-09-01)

issue 149 (codex 関数が snapshot で壊れる) の対応。修正 c460f0e / 横展開の起票は issue 152。
同日の issue 150/151 とは別チャンク。

## 良かった点

1. **横展開 grep が本命級を 2 系統掘り当てた**。snapshot の中身 (定義 78 関数 / `_` は 1 つ)
   を実測してから grep したので、「codex だけの問題」ではなく「snapshot に載る wrapper 全般の
   クラス」として捉え直せた。需要判断が要る分は issue 152 に分離してスコープを守った
2. **worktree merge push が「他者の dirty で rebase 不能」を安全に迂回した**。共有 tree の
   `_claude/settings.json` (生きた設定・所有者は私ではない) に一切触れずに統合できた

## 反省点

### 1. background 実行の成否判定を `| tail` の rc で読んだ (既存ルールの再演)

`make test-changed ... | tail -4` を background で流し、wrapper の exit 0 を「通った」と
読みかけた (実際は make が Error 1)。`verify-execution-not-just-exit-code.md` の
「`cmd | tail` の `$?` はパイプ終端の status」そのもの。rc 直取り + 失敗行 grep で取り直して
回収した。

- 切り出し先候補: **却下** (既存ルールが規定済み。実例としてこの retro に残す)

### 2. 新規テストの実行ビット漏れ

`Write` で作った test_*.sh に +x を付け忘れ、直接 `bash test.sh` の green で「動く」と
判断した。runner は `/bin/sh: Permission denied` で red にしてくれた (検出は機能した) が、
「直接実行の green」と「runner 経由の green」は別物という再確認。

- 切り出し先候補: **却下寄りで提案** — lint に実行ビット検査を足す案はあるが、runner が
  毎回の `make test` で同じタイミングに red を出すため防御としては冗長。実例として残すのみ

### 3. 生きた設定ファイルが共有 tree の ff pull を恒久ブロックする

`_claude/settings.json` はハーネスが随時書く生きたファイルで、ユーザーの手動 commit
(dc94919) の直後にも中身が動いていた。この dirty が共有 tree の rebase / ff-merge を
恒久的に阻み、全セッションが「push は worktree 経由・pull は不能」に落ちる。

- 切り出し先候補 (ユーザー判断待ち):
  - 案 A: settings.json を追跡から外す (gitignore + テンプレート化。設定の共有方法は別途)
  - 案 B: 「settings.json はユーザーがこまめに commit する」運用の明文化 (dirty 放置を
    やめる。ただしハーネスが書き続ける限り根治しない)
  - 案 C: 現状維持を明文化 (共有 tree は behind を許容し、統合は worktree merge を正規手順にする)

## 残課題

- [x] 反省点 3 の方針決定 → **A/B/C ではなく「揮発キーの取りこぼしを直す」で解消** (下記)
- [x] issue 149 の受け入れ条件「実 snapshot からの codex --version」→ **確認済み** (下記)

## 対応 (2026-09-02)

### 反省点 3: 原因は「運用」ではなく normalize-settings の揮発キー漏れだった

案 A (追跡から外す) / B (こまめに commit する運用) / C (現状維持を明文化) はいずれも
**dirty になること自体を前提**にしていたが、実測すると dirty の発生源は特定できた:

- `dc94919` (09/01) が `"model": "fable"` と `modelSettings` (`/effort` の書き込み先) を
  追跡ファイルへ持ち込んでいた
- `normalize-settings.sh` の揮発キー一覧は `["model","effortLevel",...]` で、
  **`modelSettings` が入っていない**。CLI が `/effort` を top-level の `effortLevel` から
  `modelSettings.<model>.effortLevel` のネスト形式へ移したため、一覧が空振りしていた
- つまり `/model` `/effort` を触るたびに追跡ファイルが dirty になり、共有 tree の ff pull を阻む

対応: `VOLATILE` に `modelSettings` を追加した (top-level の `effortLevel` は旧形式の残骸回収の
ために残す)。隔離した `HOME` で実測し、`model` と `modelSettings` が local へ退避され、
共有設定 (env / hooks / language) と local の既存値が保たれることを確認。
変異 (一覧から `modelSettings` を抜く) では `settings.json` に残り続ける = 観測が機構の有無で変わる。

**残る穴 (再開の trigger)**:

- 反映は `make pull` の実行時 (normalize はそこで走る)。**素の `git pull` を使うと依然として
  dirty のまま**なので、共有 tree では `make pull` を使う
- 新しい揮発キーを CLI が書き始めたら同じことが起きる。構造で止めるなら
  「settings.json の top-level キーを allowlist で検査する」ゲートが要る (未実装)
- ⚠️ **未検証の疑問**: `normalize-settings` は `local * extracted` で **settings.json 側を優先**
  して local を上書きする。一方 Claude Code の設定の優先順位は local > settings.json。
  現在 local は `model: opus`、settings.json は `model: fable` で食い違っており、
  「CLI がどちらのファイルへ書くか」を確かめていない。**取り違えると `/model` の選択が
  巻き戻る**ので、live の settings.json に対する normalize の実行は今回は行っていない
  (次の `make pull` で走る)

### issue 149 の受け入れ条件: 実 snapshot で確認済み

新 snapshot (`snapshot-zsh-1788306374630`, 09/02 08:46) を素の zsh (`zsh -f`) で source し、
helper 未定義 (`${+functions[_ensure_cli_with_brew]}` = 0) のまま関数経由で実行:

```
codex-cli 0.152.1   rc=0
```

旧 snapshot (`...1788188722354`, 09/01 00:05) で同じことをすると
`command not found: _ensure_cli_with_brew` / rc=1 になる = この観測は修正の有無で変わる。

**副次の発見**: Claude Code の Bash ツールが使うシェルは**プロセス起動時の snapshot を持ち続ける**。
このセッションのシェルは 09/01 00:05 の snapshot 由来で、`type codex` は今も旧定義を指しており
関数経由の `codex` は失敗する。issue 149 は「次セッション起動後に確認」と書いていたが、
正確には**次の CLI プロセス起動**まで直らない (会話を clear しても引き継がれる)。
既存セッションで使うなら `command codex` で回避する。
