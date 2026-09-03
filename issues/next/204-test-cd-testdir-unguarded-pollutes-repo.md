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
| A. 各 `cd` に `\|\| exit 1` を足す | 318 箇所 | **この症状に効く**。`cd /inj1` の失敗で即 exit する |
| B. helper が `TEST_TMP` を作った直後に `cd "$TEST_TMP"` する | helper 7 本 | **効かない (却下)**。下記 |
| C. lint で「`cd` / `mkdir` の rc を見ていないテスト」を落とす | 検査 1 本 + 既存の修正 | 再発も止まる。A と併用 |

推し: **A + C**。

### 案 B を却下した理由 (敵対的レビューが実験で示した)

失敗の起点は `TEST_TMP` が空になることではなく、**その手前で helper の source が死ぬ**こと:

```
$ bash test_av1ify_injection.sh
test_av1ify_injection.sh: 行 11: /test_helper.sh: No such file or directory
test_av1ify_injection.sh: 行 12: setopt: command not found
```

`source "${0:A:h}/test_helper.sh"` の `${0:A:h}` は zsh 拡張で、bash では空に潰れて
`/test_helper.sh` になる。**helper は 1 行も実行されない**ので `TEST_TMP="$(mktemp -d)"` にも
到達せず、helper に `cd "$TEST_TMP"` を足しても走らない。B を模して helper に挿入した状態で
再実行しても、fixture 3 件はそのまま CWD に残った (レビュワーが実験済み)。

**`TEST_TMP=` を作るのは helper 2 本ではなく 7 本** (`av1ify/test_helper.sh` /
`concat/test_helper.sh` / `av1ify/test_av1ify_clipboard.sh` / `repair_mp4/test_repair_mp4.sh` /
`repair_mp4/test_repair.sh` / `test_video_health.sh` / `validate-mp4/test_validate_mp4.sh`) なので、
「1 箇所で class ごと消える」も誤りだった。

### 「CWD = repo root を前提にしたテストがある」も否定された

レビュワーが **CWD を repo 外の空ディレクトリにして av1ify + concat の全 29 本を zsh で実行**し、
すべて rc=0 / 残骸なし / repo は clean を確認した。両 helper は `ROOT_DIR` を `${0:A:h}` から
解決しており CWD に依存しない。B の懸念は空だったが、B 自体が上記のとおり無効。

### class は 318 より広い

同じ「rc を見ない cd」は他の形にもある (`cd "$TMP_ROOT/r"` ×3 / `cd "$ROOT_DIR"` ×3 /
`cd "$TMP_ROOT"` ×2 / `cd r` / `cd deep/nest`)。さらに**実害の主因は cd だけではない**:
今回のログでは `mkdir -p "/inj1"` も `Read-only file system` で失敗しており、その rc も見ていない。
C の lint は「`cd` 単独」ではなく **「TEST_DIR を作って入る 2 行 1 組」**を対象にする方が class に合う。

### 「`*` の展開で地雷」は根拠が弱い (訂正)

`$(...)` を含むファイル名は glob 展開では実行されない (eval される経路が必要)。残骸が
望ましくないのは事実だが、「地雷」という書き方は裏付けが無い。

## 受け入れ条件

- [ ] 呼び方を間違えても repo root に fixture が落ちないこと (`bash tests/zshrc/av1ify/test_av1ify_injection.sh`
      を repo root で実行して `git status` が clean のまま)
- [x] `tests/zshrc/av1ify` と `tests/zshrc/concat` を repo 外の CWD から全 29 本実行して緑
      (レビューで実施済み。B の懸念の否定になっている)
- [ ] 変異で red を見る (guard を外す / helper の cd を消す)

## レビュー状態

**敵対的レビュー済み (opus、2026-09-03)。** 318 件のカウント / fixture 3 件 / 注入が実行されて
いないことは再現された。一方 **推していた案 B は実験で否定された**ので上のとおり差し替えた。
⚠️ カウントは system grep で取ること: Claude Code の `grep` は ugrep 経由で `$` を行末アンカーと
解釈し、同じパターンで **0 件**を返す (レビューの指摘)。
