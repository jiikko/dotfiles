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

## 結果 (2026-10-21): 作り手は swift-driver の `ArgsResolver`。Swift のツールチェーン全体が残す

調べ方 1・2 (相関・fs_usage) の代わりに、候補ごとに使い捨ての `TMPDIR` を渡して 1 回動かし、残った `TemporaryDirectory.*` を数えた
(候補を 1 つずつ動かすので、他のプロセスの混入が無い)。Swift 6.4 (swift-driver 1.168.6) / Xcode 27.1 (27A9275)。

| 動かしたもの | 残った数 (3 回) |
|---|---|
| `swiftc -c main.swift` (job 1 つ) | 1 / 1 / 1 |
| `swiftc main.swift -o a.out` (コンパイル + リンク) | 0 / 0 / 0 |
| `swift -e 'print(1)'` | 2 / 2 / 2 |
| `swift-frontend -version` (ドライバを通らない) | 0 / 0 / 0 |
| `swift package dump-package` (manifest を読むだけ) | 1 / 1 / 1 |
| `swift build` (1 ファイルのパッケージ、変更なしの 2 回目) | 5 / 5 / 5 (初回は 6) |
| `xcodebuild -scheme Tiny build` (同じパッケージ) | 2 (1 回) |
| `sourcekit-lsp` (initialize → didOpen → 30 秒待つ → shutdown) | 18 (`.build` なし) / 10 (`.build` あり) |

LSP の手順は Python で stdio の JSON-RPC を送るだけのもの (`Content-Length` 付きで `initialize`・`initialized`・`textDocument/didOpen`・
`shutdown`・`exit` を順に送る)。実際に動いていた sourcekit-lsp (PID 40846) は `~/src/my-products/apps/obaket/macOS` を cwd にした、
Claude Code の swift-lsp plugin (`_claude/settings.json` の `enabledPlugins` の `swift-lsp@claude-plugins-official`) が起動したもの。

仕組み (upstream のソースを `gh api` で読んだ。main の版で、手元のバイナリの版と同じかは未確認):

- swift-driver の `Sources/SwiftDriver/Execution/ArgsResolver.swift` の `init` は、一時 dir を渡されないと
  `withTemporaryDirectory(removeTreeOnDeinit: false)` で `$TMPDIR/TemporaryDirectory.XXXXXX` を作り、`.keep-directory` を書く
  (TSC が空の dir を消してしまうのを防ぐため、と FIXME にある)。消すのは `removeTemporaryDirectory()` を呼んだときだけ
- `swiftc` のドライバ (`Driver.swift` の `run`) は、片付けを `defer` で登録するが、job が 1 つのとき (`swiftc -c` の 1 ファイル・`swift -e` 等)
  はドライバ自身をその job に `exec` で置き換える (in-place 実行)。プロセスが置き換わるので `defer` が走らず、dir が残る。
  `-###` などの表示だけのモードは `defer` の登録より前に return するので、これも残る
- SwiftPM (`Sources/Build/BuildOperation.swift`・`BuildManifest/LLBuildManifestBuilder+Swift.swift`・`DriverSupport/DriverSupportUtils.swift`) は
  ビルドの計画のたびに `ArgsResolver(fileSystem:)` を一時 dir なしで作り、`removeTemporaryDirectory()` を呼ばない (`gh search code` で 0 件)。
  sourcekit-lsp はパッケージを開くと SwiftPM を内部で使って計画・準備ビルドをするので、編集や再インデックスのたびに増える
- Xcode のビルドの本体 (swift-build の `SWBCore/LibSwiftDriver/LibSwiftDriver.swift`) は一時 dir をビルド dir の下に明示して渡すので残さない。
  xcodebuild で残った 2 件はパッケージの manifest を読む段 (SwiftPM) のもの

つまり、作り手は 1 つではなく「swift-driver の `ArgsResolver` を一時 dir なしで使い、`removeTemporaryDirectory()` まで届かない経路」全部。
`.keep-directory` だけの dir はこれで説明できる。`inputs-1.LinkFileList` は、リンクの job が途中で死んだ (片付けの条件 `anyJobHadAbnormalExit`) か、
同じ in-place の経路でリンクした回のもの (未確認)。

どれが件数を占めるかは測っていないが、sourcekit-lsp は 1 ファイルのパッケージでも 30 秒で 10〜18 件で、Claude Code のセッションが開いている間ずっと
動くので、直近 1 時間の 446 件 (xcodebuild が居ない時間帯) の主な出どころとして説明が付く (推定)。

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
- [x] 作り手の特定 (調べ方 1・2 は採らず、候補ごとに隔離した `TMPDIR` で動かす A/B に替えた。上の「結果」)。2026-10-21
- [x] 受け方を決めた: 案 C (ユーザー決定 2026-10-21。消し方は rmdir・起動は SessionStart hook・同時起動は lockman で排他)。
  A (upstream に報告)・B (sourcekit-lsp の TMPDIR を隔離)・D (swift-lsp plugin を切る) は採らなかった
  - 実装: commit「feat(tmpclean): sweep で $TMPDIR の空の TemporaryDirectory.* を承認なしで消し、SessionStart hook で 1 日 1 回走らせる」
    (`safefs.RemoveEmptyDir`・`tmpclean sweep`・`_claude/hooks/tmpdir-swift-sweep.sh`。守りと検出しないものは `src/tmpclean/README.md` の sweep の節)
  - 変異 24 本すべて red、opus の敵対レビュー 2 体 + 直した差分に 1 周 (内訳は commit message)
  - 本物の環境で 1 回 (2026-10-21 23:09、hook の入口から): removed=62,599・failed=0・rc=0。`TemporaryDirectory.*` は 104,880 → 42,281。
    hook は 0 秒で返った
- [x] 残った 42,281 件: 全部「動いている Swift のツールの起動より後に作られた」で残った。範囲を決めたのは 10/10 から動き続けている
  `/Applications/Xcode.app/Contents/Developer/usr/bin/make dev-fg-loop` (`swiftToolCommand` の `/Xcode[^/]*\.app/` が Xcode の中の make まで拾う。
  敵対レビューが P3 で挙げた形)。拾う範囲を絞るかは 854 へ引き継いだ (ユーザーが make を一度止める予定。止めた後の sweep の確認も 854)
