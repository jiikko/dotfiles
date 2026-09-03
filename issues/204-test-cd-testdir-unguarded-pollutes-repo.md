# 204 test: `cd "$TEST_DIR"` が失敗したとき fixture が repo root に落ちる (318 箇所)

起票日: 2026-09-03
重要度: P3 (汚染だけ。**注入は実行されていない**)
出典: issue 188 の所要時間計測中に実際に踏んだ (2026-09-03)

## 何が起きたか

`make test` の内訳を測るため、`tests/zshrc/**/test_*.sh` を**ランナー経由でなく `bash <file>` で
直接**回した。これらは `#!/usr/bin/env zsh` の zsh スクリプトなので `source test_helper.sh` が
失敗し、`TEST_TMP` が空のまま先へ進んだ。その結果:

```
TEST_DIR="$TEST_TMP/inj2"   # → "/inj2"
mkdir -p "$TEST_DIR"        # → 権限が無く失敗
cd "$TEST_DIR"              # → 失敗するが **rc を見ていない**
echo "dummy video" > "./$EVIL"   # → CWD (= repo root) に書かれる
```

repo root に fixture 3 件が残った (名前に `$(touch pwned_*)` を含む。中身は 12 バイトの
`dummy video`)。**`pwned_*` は生成されていないので、注入は実行されていない** (`ls pwned_*` で確認)。
掃除済み。

## 何が問題か

- `cd "$TEST_DIR"` の失敗を見ていない箇所が **318 箇所** (`grep -rn 'cd "$TEST_DIR"' tests/ | wc -l`)。
  どれも「cd に失敗したら CWD のまま書く」ので、呼び方を間違えると repo が汚れる
- 名前に `$(...)` を含むファイルが repo root に残るのは、**次に誰かが `*` を展開したときの
  地雷**でもある (av1ify の injection テストは「実行されないこと」を確かめる側だが、
  残骸そのものは無防備なまま置かれる)
- 直接実行が誤りである (ランナー経由が正) のは事実だが、**誤った呼び方が静かに repo を汚す**
  形は直せる

## 直し方の候補

| 案 | 変更量 | 効き方 |
|---|---|---|
| A. 各 `cd` に `\|\| exit 1` を足す | 318 箇所 | 確実だが機械的置換の量が多い。`cd ... \|\| { print ...; exit 1 }` の形を lint で固定する必要もある |
| B. helper が `TEST_TMP` を作った直後に `cd "$TEST_TMP"` する | helper 数箇所 | **1 箇所で class ごと消える**。ただし CWD = repo root を前提にしているテストがあると壊れる (av1ify 124s + concat 56s を回して確認が必要) |
| C. lint で「`cd` の rc を見ていないテスト」を落とす | 検査 1 本 + 既存の修正 | 再発も止まる。既存 318 箇所を直すまで赤が続くので A か B と併用 |

推し: **B を試して、壊れないなら B。壊れるなら A + C**。B は「そもそも repo root を CWD に
しない」形なので、`cd` の rc を見ていない箇所が残っても被害が出ない。

## 受け入れ条件

- [ ] 呼び方を間違えても repo root に fixture が落ちないこと (`bash tests/zshrc/av1ify/test_av1ify_injection.sh`
      を repo root で実行して `git status` が clean のまま)
- [ ] `tests/zshrc/av1ify` と `tests/zshrc/concat` をランナー経由で回して緑 (B を採るなら必須)
- [ ] 変異で red を見る (guard を外す / helper の cd を消す)

## レビュー状態

反証レビュー未実施。踏んだ事実と 318 件のカウントは実測 (上記コマンド)。
