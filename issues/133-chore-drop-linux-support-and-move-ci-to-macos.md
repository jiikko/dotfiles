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
