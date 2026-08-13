# tests/ の lint が 44 本中 1 本しか構文検査していないのに「44 本 OK」と表示して緑になる

起票日: 2026-08-14
種別: bug
優先度: **P2** (新設した安全機構が、検査していないものを検査したと報告する。
ただし `make test` 全体は後続の実行フェーズで赤になる — 下の「影響範囲」参照)

## 何が起きるか

`scripts/lint_test_scripts.sh` (5f4eef9 で新設) は `make test` / `make test-lint-tests` から
呼ばれ、完了時に次を表示して exit 0 する。

```
[lint-tests] zsh -n 44 本 + shellcheck 39 本 OK
```

**実際に zsh が構文解析しているのは sort 順 1 本目の `tests/nvim/bench_nvim.sh` だけ**で、
残り 43 本は一切パースされない。**lint ゲートは、検証していない本数を検証済みとして報告している。**

原因は 37 行目:

```sh
[ -z "$zsh_files" ] || zsh -n $zsh_files
```

`zsh -n file1 file2 ...` は **file1 をスクリプトとしてパースし、file2 以降は
「file1 への位置パラメータ `$1 $2 ...`」として渡す**。「最初のエラーで止まる」のではなく、
**2 本目以降はそもそも読まれない**。40 行目の件数は `wc -w` で引数の個数を数えているだけなので、
検査した本数ではなく「渡した本数」を報告している。

`shellcheck -S warning $sh_files` の側は正常 (shellcheck は複数ファイル引数を受ける)。
Makefile の `test-zsh-syntax` も 1 ファイルずつループしているので無傷。
**本件は 5f4eef9 で新設された経路だけの欠陥。**

## 再現

`zsh -n` の多引数の意味 (単離プローブ):

```console
$ printf '#!/bin/zsh\necho ok\n' > ./tmp/probe/good.zsh
$ printf '#!/bin/zsh\nif [ 1 -eq 1 ]; then\necho x\n' > ./tmp/probe/bad.zsh

$ zsh -n ./tmp/probe/good.zsh ./tmp/probe/bad.zsh; echo "EXIT=$?"
EXIT=0                                    # ← bad.zsh は読まれない

$ zsh -n ./tmp/probe/bad.zsh; echo "EXIT=$?"
./tmp/probe/bad.zsh:4: parse error near `\n'
EXIT=1
```

実機構での false green (43 本目に変異を注入):

```console
$ printf '\nif [ 1 -eq 1 ]; then\n  echo "unterminated"\n' >> tests/zshrc/test_git_prompt.sh
$ make test-lint-tests; echo "EXIT=$?"
[lint-tests] zsh -n 44 本 + shellcheck 39 本 OK
EXIT=0                                    # ← 構文エラーがあるのに緑
$ git checkout -- tests/zshrc/test_git_prompt.sh
```

同じ変異を **1 本目**に入れると赤になる (= 1 本目しか見ていない証拠):

```console
$ printf '\nif [ 1 -eq 1 ]; then\n  echo "unterminated"\n' >> tests/nvim/bench_nvim.sh
$ make test-lint-tests; echo "EXIT=$?"
tests/nvim/bench_nvim.sh:115: parse error near `\n'
make: *** [test-lint-tests] Error 1
EXIT=2
$ git checkout -- tests/nvim/bench_nvim.sh
```

⚠️ 5f4eef9 のコミットメッセージにある検証「zsh テストへ構文エラー → `zsh -n` fail」は、
**変異先が sort 順 1 本目のときにしか再現しない**。この検証が通ったこと自体が、
本件を見逃した理由になっている (`mutation-verify-new-tests.md` の失敗例)。

## 影響範囲 (敵対的検証で範囲を訂正した)

**「43 本が完全に無検査」ではない。`make test` 全体は赤になる。** 実測で確認した内訳:

| 経路 | 本数 | 構文エラーは捕まるか |
|---|---|---|
| `make test` → `test-runtime` → `test-discovered` が**実行**する | 39 | ○ (実行時にパースされる) |
| 各テストから `source` される `test_helper.sh` | 2 | ○ (source 時にパースされる) |
| CI の `.github/workflows/bench.yml` でのみ実行 (`tests/tmux/bench_tmux.sh`, `tests/zshrc/bench_zsh.sh`) | 2 | △ (ローカル `make test` では走らない) |
| `zsh -n` が実際に検査 | 1 | ○ |

```console
$ make test-dir DIR=tests/zshrc/test_git_prompt.sh   # test-discovered と同じ run_tests
[run] tests/zshrc/test_git_prompt.sh
... (全 assert 通過) ...
tests/zshrc/test_git_prompt.sh:114: parse error near `\n'
make: *** [test-dir] Error 1
```

→ **緑のまま素通りするのは `make test-lint-tests` 単体と `make test-lint` 単体を叩いたときだけ。**

### それでも実害が残る理由

1. **lint ゲートが「検証していない本数」を検証済みとして報告する** (安全機構の false green そのもの)。
   `make test-lint` を「速い事前チェック」として単体で回す運用では、構文エラーを緑で通す
2. **実行が拾ってくれるのは設計上の保証ではなく偶然**。zsh は逐次パースするため、
   **早期 exit / skip ガードより後ろにある構文エラーは実行検査でも到達しない**
   (環境依存で skip されるテストほど危ない)
3. **bench 2 本はローカル `make test` では一切走らない**。ここは lint だけが頼り

⚠️ shellcheck 側 (39 本) は全数が正常に検査されている (6 本目に注入して SC1046/SC1072 で赤を確認済み)。

## 同じファイルのもう 1 つの false green (16 行目)

```sh
command -v shellcheck >/dev/null 2>&1 || { echo "[lint-tests] shellcheck not found; skipping"; exit 0; }
```

shellcheck が PATH に無い環境では、**shellcheck と無関係な `zsh -n` の検査ごと** skip して
exit 0 する。CI (`.github/workflows/lint.yml`) はバージョン固定で shellcheck を入れるため
CI では発火しないが、手元やコンテナで shellcheck が無いと `make test` の tests/ lint が
**全部素通りして緑**になる。

このスクリプト自身が 19-21 行目で

```sh
# 発見 0 件は fail (discover_shell_scripts.sh と同じ規律: ディレクトリ改名や find の失敗を
# 「未実行なのに成功」にしない)
```

という規律を明記しているのに、その 3 行上でその規律を破っている。

## 対応方針 (案)

1. **`zsh -n` を 1 ファイルずつ回す** (`test-zsh-syntax` と同じ形)。全件走らせてから
   まとめて落とすなら失敗を集約する:

   ```sh
   rc=0
   for f in $zsh_files; do zsh -n "$f" || rc=1; done
   [ "$rc" -eq 0 ] || exit 1
   ```

2. **件数の表示を「渡した数」でなく「検査した数」にする**。ループでカウンタを回して出せば、
   同じ嘘が構造的に書けなくなる
3. **shellcheck 不在時は zsh 側だけでも回す**。shellcheck を必須にして落とすか、
   最低限「zsh -n N 本 OK / shellcheck: skipped (未導入)」と**何を検査しなかったかを明示**する
   (「skipping」とだけ出して緑にすると、読み飛ばした人には成功に見える)
4. **再発防止のミューテーション検証を規約にする**: 「1 本目ではなく **最後の 1 本**に変異を
   当てて red を見る」。1 本目で検証すると本件と同じ穴を通す

## 未確認

- `sh -n` / `bash -n` にも同じ多引数の性質があるか (本件は zsh のみ実測)。
  `shellcheck` は複数ファイルを受けることを実測済み
- 他リポジトリの同型スクリプトに同じ書き方が無いか (`grep -rn 'zsh -n \$'` の横展開)

## 関連

- `_claude/rules/adversarial-review-own-safeguards.md` — 「検査できなかったときに緑を返さない」
  「沈黙 = 成功になっていないか」。本件は両方に該当する
- `_claude/rules/mutation-verify-new-tests.md` — 「green は『正しい』ではなく
  『その書き方では壊せなかった』」。変異先の選び方まで含めて規約化する必要がある
- issue 052 — 同じ「不変条件を守るテストが無い」系 (あちらは scan の I/O、こちらは lint の網羅)
