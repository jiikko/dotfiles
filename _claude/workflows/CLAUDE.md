# workflows/

Workflow ツール（`Workflow({scriptPath, args})`）で起動する決定論的オーケストレーション・スクリプト（`*.js`）の置き場。

## このディレクトリ固有の制約（非自明・踏みやすい落とし穴）

1. **命名: skill と同名を付けない**
   `meta.name` が skill 名と衝突すると一覧表示・名前解決で混乱する。skill から呼ぶ fan-out エンジンは `<skill名>-fanout` 等の別名にする（例: `forge.js` の `meta.name` は `forge-fanout`。skill `forge` と区別）。

2. **起動は scriptPath で行う（`name:` 解決に依存しない）**
   このハーネスでは `~/.claude/workflows/` に置いた自作 workflow を `Workflow({name})` で解決できない（built-in のみ。セッション中のホットリロードも不可）。実測で確実に動くのは **`Workflow({scriptPath: "$HOME/.claude/workflows/<file>.js", args})`**。`~` は `$HOME` に展開した絶対パスで渡すこと。
   - セッション再起動後に `name:` 解決が効く可能性はあるが未検証。scriptPath を正式手段とする。

3. **デプロイは setup.sh が自動 symlink（手動不要）**
   `setup.sh` が `~/dotfiles/_claude/workflows/*` を `~/.claude/workflows/` へ個別 symlink する。新しい workflow を追加したら `setup.sh` を再実行すれば反映される（手で symlink を張る必要はない）。

## 構文チェック

Workflow スクリプトはトップレベル `await`/`return` を使うため通常の `node --check` では弾かれる。**`export` を外して async 関数の本体としてパース**して検証する（実行はしない。構文エラーなら rc=1）:

```sh
node -e 'const s=require("fs").readFileSync(process.argv[1],"utf8").replace(/^export\s+/gm,""); new (Object.getPrototypeOf(async function(){}).constructor)(s); console.log("parse ok")' _claude/workflows/forge.js
```

ロジック検証は agent/parallel/pipeline をスタブ化して制御フローを流す（スタブはその都度書く。`./tmp` に置いた版は残らないので、ここから指さない）。

## 現在のファイル

- `forge.js` — forge skill の fan-out エンジン（investigate / review / ultra）。仕様の出典は `skills/forge/_common/{modes,agents,cross-review}.md`。仕様を変えたら forge.js も同期更新する。
