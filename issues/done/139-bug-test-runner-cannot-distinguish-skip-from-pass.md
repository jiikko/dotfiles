# 139 bug: `make test` の runner が skip を `[ok]` と区別できない

起票日: 2026-08-29 / 種別: bug / 優先度: **P1** (検査が消えても気づけない構造)

## 事実

`Makefile:139` の runner は **exit 0 なら `[ok]` を出すだけ**で、skip を集計していない:

```make
'out=$$(mktemp); if "$$0" >"$$out" 2>&1; then echo "[ok] $$0"; rm -f "$$out"; else echo "[FAIL] $$0"; cat "$$out"; rm -f "$$out"; exit 1; fi'
```

テストが「依存が無いので何も検査せず `exit 0`」しても、出力は合格と同じ `[ok]` になる。

## 実害 (2026-08-29 に発生済み)

CI を macOS へ移した (issue 133) 直後、`tests/claude/test_deny_bare_tmux_kill.sh` が
`timeout(1)` 不在で 16 行目の skip に落ち、**60 件の assert が消えたのに `[ok]` と集計されていた**。

- 守っていたのは 2026-07-30 の**本番 tmux サーバ誤 kill の再発防止ゲート**
- 消えた中には issue 072 の「入力長で hook が timeout に殺されて deny が消える」回帰テストも含まれる
- 気づけたのは敵対的レビューが **ubuntu run と macOS run の出力を全行 diff** したから。
  テストファイル数は **96 → 96 で同数**、全 workflow green だったので、緑の側からは観測できない
- 直近の `make test` ログには skip 系の行が **109 件**ある (大半は正当だが、区別が付かない)

## なぜ運用では閉じないか

`tests/CLAUDE.md` は「環境依存で走らせないときは**テスト自身の stdout に理由を出す**」と定めて
いる。それは守られていた (`SKIP: timeout(1) が無い環境` と出ていた) が、**runner がそれを数えて
いない**ので、人がログを読むまで誰も気づかない。規律は機能していて、集計が無いことが穴。

## 対応案

**runner が skip を第 3 の結果として集計する**。`[ok]` / `[FAIL]` の 2 値をやめる。

- テストが skip したことを機械可読な形で出す (既存の `skipped:` / `SKIP:` を拾うか、
  専用の終了コード。既存出力を拾う方が全テストの書き換えが要らない)
- runner は末尾に `[skip] N 件` を出す。**0 件でないことは失敗ではない** (正当な skip はある)
- ⚠️ 本命は「前回との差」。skip が**増えた**ことが分かる形にする。CI なら前回 run との比較、
  手元なら件数の表示だけでも「あれ、増えてる」に気づける

## 受け入れ条件

- [ ] 依存不在で skip するテストを 1 本作り、runner の出力が `[ok]` **ではない**ことを確認する
- [ ] 正当な skip があっても `make test` は緑のまま (skip は失敗ではない)
- [ ] **変異検証**: 既存テストを「何も検査せず exit 0」に変えると、集計に現れる
- [ ] 件数を出す (`verify-execution-not-just-exit-code.md` の「件数を出す設計が効く」)

## 関連

- `_claude/rules/verify-execution-not-just-exit-code.md` — 「実行環境を変えたら出力を全行 diff」を
  2026-08-29 に追記した。本 issue はその**自動化**にあたる (人が diff しなくても分かる形)
- [133](133-chore-drop-linux-support-and-move-ci-to-macos.md) 手順 3 — 60 件消失の発生源

---

## 対応 (2026-08-29): `exit 77` = ファイル全体 skip の規約を入れた

### 採らなかった案

- **出力から `SKIP:` を正規表現で拾う** — 表記が `SKIP:` / `skipped:` / `⚠ … skip` の 3 種類に
  割れており脆い。全体 skip と部分 skip も区別できない
- **`✓` が 0 件のファイルを skip とみなす** — 実測すると **97 本中 21 本**が別の出力形式
  (`OK:` / `All … passed` / `[test-x] すべて成功`) で、誤検出だらけになる

→ **終了コードなら出力形式に依存しない**。automake の慣例に合わせて 77 を採った。

### 入れたもの

- 直列 runner: 77 を `[skip]` に分類し、末尾に**件数と一覧**を出す (skip は失敗ではないので緑のまま)
- 並列 runner: `[ok]` / `[skip]` / `[FAIL]` の 3 値
- 全体 skip する 7 箇所を 77 に変更。⚠️ `test_retro_open.sh:145` は date スタブ内の `exit 0` なので対象外
- 受け側を先に洗った: テストファイルを直接実行するのは runner 2 本だけで、
  `scripts/test_changed.sh` と `Makefile:97` は make target 経由なので 77 は漏れない

### 効果 (実際に見えるようになったもの)

| 環境 | 丸ごと skip していたテスト |
|---|---|
| 開発機 | `tests/tmux/test_fork_scratch.sh` (実サーバ誤 kill 防止で skip する設計) |
| CI | `tests/claude/test_claude_links_complete.sh` (runner に `~/dotfiles` が無い。設計どおり) |

**どちらも従来は `[ok]` に埋もれていた**。環境によって skip するものが違うことも、これで見える。

### 受け入れ条件

- [x] skip するテストが `[ok]` にならない (プローブで直列・並列とも確認)
- [x] 正当な skip があっても `make test` は緑
- [x] 変異検証 (「何も検査せず exit 77」のプローブが集計に現れる)
- [x] 件数を出す

### 残課題

なし。
