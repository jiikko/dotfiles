# src-env — ~/src の下で `.src-env` の変数を入れる

`cd` したとき、今のディレクトリから `~/src` (`SRC_ENV_ROOT` で変えられる) まで上へ `.src-env` を探し、書いてある `KEY=パス` を export する。
外へ出たら元の値に戻す。direnv と違い allow が要らない (ファイルを実行せず読むだけ)。実体は `zshlib/_src_env.zsh`、テストは `tests/zshrc/test_src_env.sh`。

## いつ使うか

- repo の root や親の repo のパスを、その下のどこにいても変数で使いたいとき (例: my-products の root の `.src-env` に `UMBRELLA_ROOT=$DIR`)
- 値がパスでない設定 (トークン・フラグ) や、PATH・GIT_*・DYLD_* などコマンドの動きを変える変数には使えない (受け付けない。理由は実体の冒頭)。それが要るなら direnv を使う

`cdu` (同じファイル) は `cd "$UMBRELLA_ROOT"`。my-products の下なら、submodule の worktree の中からでも umbrella の root へ移れる。

## 書式

```
# コメント
UMBRELLA_ROOT=$DIR          # この .src-env があるディレクトリ
UMBRELLA_LIB=${DIR}/lib
SOMEWHERE=/absolute/path
```

- 下のディレクトリの `.src-env` が同じ KEY を書いたら、下が勝つ
- 受け付けない行は stderr に「src-env: … を飛ばした」と 1 回だけ出す
- 対話の zsh の `cd` でだけ動く。make・Xcode・cron から起こされるスクリプトには届かないので、スクリプトの中では変数に頼らず自分で root を探す
