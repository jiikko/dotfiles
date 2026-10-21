# 851 (research): $TMPDIR に TemporaryDirectory.* が 1 日数千件溜まる出どころを調べる

> 🚨 **担当中: Mac (glogx)**（2026-10-21〜）

起票日: 2026-10-21

## 概要

`$TMPDIR` (`/private/var/folders/1s/…/T`) の直下に `TemporaryDirectory.*` が溜まり続けている。2026-10-21 に数えると 104,546 件で、
前回の起動 (2026-09-17 12:49) からの 34 日で溜まった (起動より前に作られたものは 0 件。起動時の削除で一度空になっている)。
ほぼ全部が空で、中身のあるものも `.keep-directory` だけ (一部は `inputs-1.LinkFileList` も) なので容量は小さいが、件数が
起動時の削除と glogx の doctor の「一時領域」タブ・`bin/tmpclean` の所要に効く (下の「影響」)。作っているプロセスを突き止め、出どころで止める。

325 / 305 (2026-09-06) では「dotfiles の repo を grep して 0 件・8/25 から継続・1 日 45 個」として**スコープ外**にした。
今は 1 日 2,900〜8,400 件で、2 桁増えている。

## 実測 (2026-10-21)

作成日 (birthtime) ごとの件数の上位:

| 日 | 件数 |
|---|---|
| 2026-10-12 | 8,362 (うち 10 時台 2,470・11 時台 1,019) |
| 2026-10-04 | 7,867 |
| 2026-09-21 | 6,048 |
| 2026-10-03 | 6,034 |
| 2026-10-15 | 5,839 |

直近: 1 時間で 446 件 / 6 時間で 1,573 件 / 24 時間で 2,891 件 (21:50 時点)。数秒の間に数件ずつまとまって作られる
(例: 21:34:24〜21:34:35 に 25 件)。

測った手順: `find "$TMPDIR" -mindepth 1 -maxdepth 1 -name 'TemporaryDirectory.*' -exec stat -f '%SB' -t '%Y-%m-%d' {} + | sort | uniq -c`。
中身 (深さ 2 の名前、全件): `.keep-directory` 9,287 件・`inputs-1.LinkFileList` 402 件・`main.swift` 1 件。`main.swift` は `swift -e` の
一時スクリプト (`#sourceLocation(file: "-e")`。10/21 08:45) で、ビルドとは別の作り手がいる証拠。
件数は同じ日に `bin/tmpclean apply` で 5,610 件をゴミ箱へ移した後の値 (移したものは日ごとの表に入っていない)。

## 手がかり (仮説。未確認)

- **名前の形**: SwiftPM / swift-tools-support-core 系の一時 dir の接頭辞の可能性がある (未確認。一次情報で確かめる)。
  Xcode の `swift-build` のバイナリには `TemporaryDirectory` の文字列がある。Foundation の `itemReplacementDirectory` 説は
  確信が低い (`sourcekit-lsp` のバイナリに `itemReplacement` は 0 件。Darwin の itemReplacement は `TemporaryItems` の下に作るとされ、
  `$TMPDIR/TemporaryItems` は別に在る)。作り手の候補は SwiftPM 系のツール (swift-build・swift-package・swift-driver・`swift -e`・sourcekit-lsp)
- **作り手の候補 (どれも確定していない)**:
  - Claude Code の LSP 連携が起こした `sourcekit-lsp` (PID 40846、11:59 起動。親は `claude` PID 7150) の background indexing。
    21:50 の時点では xcodebuild・swift-build・xctest は居らず、直近 1 時間で 446 件増えていた
  - Xcode のビルド・テスト (`make test` から起動された xcodebuild・SWBBuildService)。21:52 には obaket のテストで動いており、
    20:56〜21:18 の増分 (毎分 2〜9 件) もその時間帯と重なる。`inputs-1.LinkFileList` (リンカに渡すファイル一覧の形) はビルド由来と合う
  - `swift -e` のスクリプト実行 (`main.swift` の 1 件)
  - 10/12 10 時台の山 (2,470 件) がどれに当たるかは見ていない。発生源は 1 つとは限らない

## 調べ方 (案)

1. **相関 (対照つき)**: 一定の間隔 (例: 5 分) ごとに、増分 (`-Btime -5m` の件数) とその時点で動いているプロセス
   (`ps -Ao pid,etime,comm` の xcodebuild・SWBBuildService・swift-build・swift-frontend・sourcekit-lsp・swift) を同じ行に記録する。
   増分を「xcodebuild が居る / sourcekit-lsp だけが居る / どちらも居ない」で分けて比べる。sourcekit-lsp を止めた 1 時間だけを見ると、
   その間の Xcode ビルドの有無で結果が変わり、仮説を棄却できない
2. **作った本人の特定**: `sudo fs_usage -w -f filesys | grep TemporaryDirectory` で、mkdir したプロセス名を直接見る (sudo が要る。
   人に頼むか、検証の間だけ権限を得る)。`eslogger create` でも取れる (同じく権限が要る)。権限なしで試せるのは、動いている候補に
   `lsof -p <pid>` を打って `TemporaryDirectory.*` を開いているかを見ることだが、空の dir は作ってすぐ手放すので捕まらないことが多い
   (捕まらなくても候補を外す根拠にはならない)
3. 出どころが分かったら、設定で止められるか (sourcekit-lsp の background indexing を切る・置き場を変える)、upstream の既知の不具合かを調べる。
   dotfiles の中で止められないなら、定期的な片付け (`bin/tmpclean`) で受けるかを決める

## 影響

- `bin/tmpclean apply` は 1 件ごとに `lsof` を取り直すので 1 件約 0.25 秒かかり、12 万件の片付けに約 8 時間かかる (2026-10-21 に実測し、途中で止めた)
- 起動時の削除の件数が増える (`~/.claude/CLAUDE.md`「一時ファイルの配置」)。前回 (9/17) の起動から 34 日で約 15 万件
- `dirhelper` の毎日の掃除は、3 日より古いこれらを消していない (起動より後のものが 34 日分残っている。理由は未確認。850 でも同じ観測)

## 関連

- 850 — 同じ `$TMPDIR` の `treefiler-config-*` の出どころ (treefiler のテスト)。2026-10-21 に解消
- 325 / 305 — 9/6 の時点で `TemporaryDirectory.*` を repo 由来でないとしてスコープ外にした
- `src/tmpclean/README.md` / `_claude/skills/cleanup-tmp-dir/SKILL.md` — 溜まったものの片付け

## 進捗

- [x] 件数・増え方・中身の実測と、出どころの候補の列挙 (2026-10-21)
- [x] 反証レビュー (codex の枠切れのため sonnet の read-only 1 体で代替。2026-10-21)。数字・起動前 0 件・プロセスの親子・関連 issue の要約は
  反証されなかった。採った指摘: itemReplacement 説の根拠が弱い (SwiftPM 系の接頭辞の可能性へ書き換え) / sourcekit-lsp を最有力とする根拠が足りない
  (xcodebuild と時間帯が重なる。候補の 1 つへ下げた) / 相関の手順に対照が無い (プロセスと増分を同じ行に記録する形へ) / 中身に `swift -e` の
  `main.swift` があった (作り手の候補に追加)
- [ ] 調べ方 1 (相関)
- [ ] 調べ方 2 (作った本人の特定)
- [ ] 出どころで止める、または受け方を決める
