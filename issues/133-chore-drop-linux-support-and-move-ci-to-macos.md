# 133 chore: Linux をサポート対象外として明文化し、CI を macOS runner へ寄せる

起票日: 2026-08-28 / 種別: chore / 優先度: **P2**

## 決定 (2026-08-28、ユーザー)

**この dotfiles の対象は macOS のみ。Linux はサポート対象外。**

## 根拠 (実コードを確認したもの)

「Linux も一応動く」状態ではなく、**既に macOS 専用**だった:

- `scripts/tmux_extract_popup.sh:71` — `pbcopy` を無条件で呼ぶ (Linux ではコピーが黙って効かない)
- `zshlib/_fs_helpers.zsh:16` — 「Linux 対応が必要になったら type 形式の分岐を追加すること」
  = **今は対応していない**と本人が書いている
- 他の `Linux` 言及もほぼ CI の都合の注記
  (例 `scripts/tmux_reap_orphan_servers.sh:177`「/bin/sh が dash の環境 (Linux CI) で構文エラー」)

つまり CI (ubuntu) は**誰も使わない環境**を検査していた。2026-08-28 の CI 赤 4 件のうち 2 件
(`stat -f %m` / `date -r <epoch>`) は、その環境に合わせるためだけに払ったコストだった。

## なぜ「検出を強化する」ではなく「対象を変える」か

issue 132 は「手元と CI の差を手元で出す」方向の話だったが、**差そのものが目的を持たない**なら、
検出を足すのは症状への対処になる。CI を macOS に寄せれば方言の軸は消える (CLAUDE.md
「不具合対応の原則」の「前提の是正」側)。

## 手順 (順序が重要 — 防御を先に外さないこと)

1. **課金と同時実行の確認 (人間)**。public repo は標準 runner が無料という理解だが、料金体系は
   変わりうるので billing で確認する。macOS は**同時実行数の上限が低い**ため、6 workflow が
   並走するこの repo では待ちが増える可能性がある
2. **before を測る**。現行の Tests / Lint / Bench の所要時間を記録する (macOS runner は VM 起動が
   遅く、今の Tests は 2〜3 分で終わっている。速くなるとは限らない)
3. **Tests だけ `macos-latest` へ切り替えて after を測る**。一度に全部変えない
4. **緑を確認してから**、Linux 専用の防御を外す (下表)。⚠️ 逆順にすると CI が壊れる
5. **明文化**: README か CLAUDE.md に「対象は macOS のみ。Linux は非対応」を書く。
   `zshlib/_fs_helpers.zsh:16` の「必要になったら対応する」は「対応しない」へ直す

## 移行後に外す候補と、それがマスクしていた failure mode

[`list-masked-failure-modes-before-removing-guard.md`](../_claude/rules/list-masked-failure-modes-before-removing-guard.md)
に従い、**外す前に**「本来の目的以外に何を守っていたか」を埋めること。現時点の下書き:

| 外す候補 | 本来の目的 | 副次的に守っていたもの (要精査) |
|---|---|---|
| `scripts/check_platform_dialect.sh` + `make test-platform-dialect` + そのテスト | BSD 専用の stat / date が Linux で壊れるのを止める | フォールバック無しの `date -r` は**書き方として脆い**という指摘は残る (macOS 単一なら実害なし) |
| `make test-gnu` + `scripts/with_gnu_grep.sh` | GNU grep の方言差 (`\t` 等) を手元で出す | 正規表現を移植可能に保つ規律。macOS 単一なら不要 |
| 各所の BSD/GNU コメント (`grep -rn 'GNU' scripts/ zshlib/ tests/`) | 方言差の注意喚起 | **一部は方言以外の理由**を持つ (例: `ps -o lstart=` の末尾パディングは同じ macOS でも版差がありうる)。一律削除しない |
| `CI_PACKAGES_*` / apt install / `make test-ci-group-deps` | CI の依存を宣言し heavy/rest の整合を検査 | macOS runner では preinstall か brew になるため**作り替え**が要る。単純削除ではない |

⚠️ **「Linux で壊れる」以外の理由で存在する防御を巻き添えにしない**。表の 3・4 行目は
特に危ない (方言以外の目的を持つ / 削除ではなく作り替え)。

## リスク (「環境が揃う」を過信しない)

- GitHub の macOS イメージは大量のツールが preinstall されており、**`/bin/bash` は 3.2**。
  手元で homebrew の bash 5 が PATH 先頭にいると、**逆向きの新しい乖離**が生まれる
  (今度は「CI だけ古い bash」)。切り替え後に最初に疑うべきはここ
- macOS runner の起動待ち・同時実行上限で、体感の CI 時間が伸びうる (手順 2・3 で測る)
- **`tmp/` の件 (issue 132 の #3) は OS と無関係なので残る**。あちらの Phase 1 は引き続き有効

## 受け入れ条件

- [ ] billing と同時実行上限を確認した (人間)
- [ ] before / after の所要時間を数字で残した (`perf-claims-need-measurement.md`)
- [ ] Tests → Lint → Bench の順に切り替え、各段で緑を確認した
- [ ] 移行完了後に、上表の各行について「外す / 作り替える / 残す」を**理由つきで**決めた
- [ ] 「対象は macOS のみ」が README か CLAUDE.md に書かれ、`_fs_helpers.zsh` の記述も直した

## 関連

- [132](132-feat-detect-ci-only-preconditions-before-push.md) — こちらが前提になるので、
  132 の #1 / #2 (方言) は本 issue で根本解決される。132 に残るのは #3 (`tmp/`) 系統だけ
- `_claude/rules/list-masked-failure-modes-before-removing-guard.md` — 手順 4 の作法

---

## 手順 3 の結果 (2026-08-28): Tests を macOS へ移し、敵対的レビューを通した

`f3875bb` (移行) → `7b3eb61` (移行で露出した 3 件) → 本コミット (レビュー指摘の反映)。

### 移行で露出した「ubuntu では起きない」3 つの根

いずれも手元で再現してから直した。

1. **runner の `/bin/bash` が 3.2** (開発機は Homebrew の bash 5 が PATH 先頭)。bash 3.2 を PATH
   先頭に見せて再現: `declare -A` が invalid option / statusline の advisor が 7 件不一致 (CI と同数) /
   **bats 1.13 が日本語のテスト名を解決できず 0 件実行して rc=0**
2. **`_zshrc` の未導入案内が stdout に出ていた**。ubuntu には brew が無くブロックごと skip されて
   いた。テストは `zsh -i -c` で起動するので、効くのは interactive gate ではなく **stderr 側**
3. **TERM 未設定/dumb で `print -P '%B'` が空**になる

### 敵対的レビューの全数勘定 (6 確定 / 5 未確認 / 3 は壊せず)

**採用して直したもの**:

| 指摘 | 対応 |
|---|---|
| P1 `test_deny_bare_tmux_kill.sh` が `timeout(1)` 不在で**丸ごと skip** (60 assert 消失)。しかも `[ok]` として集計される | `timeout` / `gtimeout` のフォールバックにし、**両方無ければ fail** (判定不能を緑に畳まない)。CI に `gtimeout` (formula `coreutils`) を要求 |
| P1 (自分で発見) `test_fork_scratch.sh` が `uname == Darwin` で skip し、CI から消えた | 判定を OS から **`$CI` の有無**へ。避けたいのは「開発機の実サーバとの同居」であって macOS そのものではない |
| P1 `TERM` を workflow の env で撒いたのは、テストの前提を別ファイルへ外出ししただけ | `test_print_p_injection.sh` 自身で `export TERM` し、workflow の env は**削除**。TERM 無しで全体を回して他に依存が無いことを確認 (1974 ✓) |
| P2 `macos-latest` は無 pin で、同じ commit の nvim pin の規律と矛盾 | **`macos-15` に pin**。runner の `macos-latest` は darwin25 で開発機 (darwin24) より 1 メジャー先だった。移行の根拠が「mount/ps/stat の出力が macOS 形式」である以上、ここがズレると逆向きの「CI だけ緑」を作る |
| P3 `Makefile` のコメントが `apt install` のまま / issue 132 の `CI_PACKAGES_*` 参照が stale / formula 写像への導線が無い | 3 件とも修正 |
| P3 `zshlib/_ansi_colors.zsh` が**存在しない** `tests/zshrc/test_ansi_colors.sh` を参照 | 実体 (`test_print_p_injection.sh`) に修正 |
| R1 レーステストの `sleep 0.3` は未実測のマジックナンバー | 親が解放を許すまで保持する**マーカー待ち**へ。定数が消え、実行も 6.3 秒に短縮。ガード除去の変異が今も red になることを確認 |
| 攻撃 (a) `__dotfiles_hint` が非対話で **rc=1 を返す** (現時点では悪用不能だが装填された銃) | `if … then … fi` 形にして装填解除 |

**採用しなかった / 保留**:

- **P2 `zsh` も `command -v` では版を見ていない** (CI は system zsh 5.9、開発機は brew zsh)。
  **未対応**。bash と違って今のところ実害が観測されていないので、trigger 待ちにする
  (実害が出たら bash と同じ形で版を要求する)。verify step に `zsh --version` を出す案は、
  次に CI を触るときに入れる
- **P2 `_zshrc` の brew ブロックが CI では常に「未導入」側しか通らない**。成功パス
  (プラグインの source) が CI で一度も走っていない。`bench.yml` は ubuntu のままなので
  ブロックごと skip で、**tests (macOS・未導入) / bench (ubuntu・不在) / 開発機 (導入済み)** の
  3 分裂。**手順 4 で bench を移すときに揃えるか決める** (予算値の再較正が要るのでここでは触らない)
- **R2 `nvim --version | head -1` が composite action の `pipefail` 下** — SIGPIPE で落ちうる。
  未発生。次に action を触るときに直す
- **R3 brew の非決定性** (`HOMEBREW_NO_AUTO_UPDATE` 未設定で日によって版が変わる) / `aws/tap` の
  アノテーション汚染。**未対応**。runner pin で一部は緩和されたが、formula の版は依然として無 pin
- **R4 `#!/bin/bash` が 2 本残っている** (`bin/sync_ratelimit_calendar.sh` /
  `mac/finder-actions/setup-concat-finder-action.sh`) = bash 5 保証の外側。どのテストからも
  実行されていないので実害なし。`#!/usr/bin/env bash` を強制する静的検査は別 issue の候補
- **R5 `test_dangling_symlinks.sh` が対象 0 件でも同じ出力**で常に空回り。移行前からの性質で
  今回の退行ではない。件数を出す設計への修正は別件

**レビューが攻めたが壊せなかった範囲**: bash 5 導入 step の穴 (heavy/rest 両レーンで
`/opt/homebrew/bin/bash` が実際に使われていることを CI ログで確認) / `check_ci_group_deps.sh` の
3 検査は改名後も意味を保つ / 上記 2 本以外に macOS 移行で新たに空振りになったテストは無い
(むしろ `lsof` / `osascript` / `swift` 依存の 3 本が **macOS で初めて実行されるようになり +15 assert**)。
