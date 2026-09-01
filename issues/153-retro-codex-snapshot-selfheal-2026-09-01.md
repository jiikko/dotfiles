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

- 反省点 3 の方針決定 (ユーザー)
- issue 149 の受け入れ条件「実 snapshot からの codex --version」は次セッション起動後に確認
  (snapshot は起動時に再生成。テストが同条件を固定済みなので形式的な確認のみ)
