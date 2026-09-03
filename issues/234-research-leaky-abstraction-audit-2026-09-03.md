# 234 research: 抽象境界の具象漏れ 監査 (2026-09-03) の全数勘定と却下理由

起票日: 2026-09-04
種別: research (監査記録)
状態: **記録として残す**。着手する残債は 228 / 229 / 230 へ切り出し済み

対象: `src/glogx` (+ `replace` で取り込む `src/doctor` module)
方式: forge-Minimum (4 体: go-architecture-designer / architecture-reviewer / test-coverage-advisor / refactoring-patterns)
→ **31 件を 12 クラスタに畳み、各クラスタを 2 体に「反証せよ」と指示して検証**

🚨 **以降の audit はこの記録を先に読むこと** (却下済みの指摘を再生成しないため)。
`issues/done/071` の「反証で崩れた (却下)」節と同じ役割を持つ。

## 全数勘定

| クラスタ | 判定 | 行き先 |
|---|---|---|
| C1 doctor の live 経路が termsafe を通らない | **成立** (影響の主張は要訂正) | [228](228-bug-glogx-doctor-live-path-skips-termsafe.md) |
| C1 の索引 [7] 復元経路が `Entry` を再束縛しない | **成立** (別物として分離) | [229](229-bug-glogx-doctor-snapshot-entry-not-rebound.md) |
| C9 usage の codex 枠 Label が無害化されない | **半分成立** | [230](230-bug-glogx-usage-codex-label-not-sanitized.md) |
| C5 未知 Guard が 2 つの switch を素通り | **レビュー中に修正済み** | issue 222 / `ecf285a7` |
| C2 `CommandRunner` が exit code を持たない | 却下 (下記) | — |
| C3 表示意味論の 2 実装 | 却下 (下記) | — |
| C4 `diskVerifyCommands` の ID 分岐 | 却下 (下記) | — |
| C6 削除手段の語彙の再実装 | 却下 (下記) | — |
| C7 GitHub の生 enum が描画層へ | 却下 (下記) | — |
| C8 `subproc` にプロセスグループ kill が無い | 却下 (下記。**提案は有害**) | — |
| C10 `src/doctor` に lint 設定が無い | 却下 (下記。stale) | — |
| C11 `usage/dial.go` が表示名で分岐 | 却下 (下記) | — |
| C12 `action_modal` の `target == "codex"` 3 箇所 | 却下 (下記) | — |

**31 件 → 起票 3 件 / 修正済み 1 件 / 却下 8 件。**

## 反証で崩れた (却下) — 再提案しないこと

| 指摘 | 崩れた理由 |
|---|---|
| **C4** `diskVerifyCommands` に `chrome-tmp` の case が無い = 今日すでにギャップ | `diskVerifyCommands` を新設した **issue 183 が「(足さない)」と理由つきで明示決定した意図的な除外**。主張は「183 を探したが記述なし」と書いており事実誤認。付随被害 (「なぜ対象外かを確かめる手段が消える」) も、`blocked` の `Reason` がプロセス名を名指しし `diskCopyText` が「理由:」行として必ず出すので成立しない |
| **C7** PR の未知 enum を「(レビュー必須ではない)」と断定する = false green | (1) 「意図の記述は無い」が誤り — default 分岐の文言は **issue 021 と README.md と関数コメントに仕様として明記**。(2) GitHub の現行スキーマを introspection で実測すると 3 enum の値集合が case と完全一致し、未知値を今日構成できない。到達する `""` (ブランチ保護なし) こそ文書化された意図どおりのケース。部分応答・権限不足は `gh` が rc=1 を返す error 経路に落ちて `reviewRow` に到達しない |
| **C3** 表示意味論が doctor 側と glogx 側に 2 実装 | load-bearing にしていた「突き合わせるテストは 0 件」が誤り。**`TestDoctorUnverifiedEntryMatchesCLI`** (同一 fixture を `disk.Format` と UI の `lines()` に流す parity テスト) が実在・green で、**issue 169 に変異 5 本の red 記録**まである。現在の入力で CLI と UI の出力が食い違う状態を構成できない (5 語彙が完全一致) |
| **C10** `src/doctor` に `.golangci.yml` が無い | **主張が書かれた時点で既に修正済み**。`ecf285a7` (23:30:01) で追加され、しかも主張の suggestion が求めた内容 (exhaustive + `default-signifies-exhaustive: true` + 変異で red) がそのまま実装され issue 222 に記録されている。レポート (23:32:37) は**修正の 2 分半後に書かれた stale なツリーを読んでいた** |
| **C11** `usage/dial.go` が表示名を分岐キーに使う = silent | 構造は事実だが「silent」が実測で誤り。主張が自ら提案した変異 (`cli = "codex"` を変える) を当てると **4 テストが red**。うち `banner_test.go: TestRenderDashboardBannerVersion` は主張の failure mode そのもの (「codex の段に Claude Code の版が黙って付く」) を捕まえる。主張は `dial_test.go` しか見ておらず、**同じ fixture を使う `banner_test.go` を見落とした** |
| **C12** `target == "codex"` の分岐が 3 箇所 = 改名で silent に壊れる | 3 箇所すべて既存テストに pin されており、各分岐を落とす変異は**ビルドが通ったうえで red** (`TestUpdateKeysReachableFromOverlays` / `TestCodexUpdateThatDidNotUpdateIsNotReportedAsLatest`)。加えて `startUpdate` が `updateMsg{target: "claude"}` と**リテラルで target を書き換える**ため、未知の target は 1 箇所目で洗浄され「3 箇所すべてで claude 側へ落ちる」は成立しない |
| **C6** 削除手段の定数が非公開で glogx が語彙を再実装 | 構造は事実 (非公開定数 / 素の string / glogx 側 9 箇所のリテラル / 突合テスト不在)。しかし**現行コードから到達不能** — (a) カタログに `propose` エントリが 0 件、(b) `propose` の `EntryOutcome` は `BeforeSize=0` なので「freeing に加算」の実害はゼロ加算、(c) 「綴りを変える」は disk 側のテスト 2 本がリテラルで pin しており silent でない、(d) 未知の `DeleteVia` は `Failed` になる。**trigger**: disk 側が削除の作法を増やしたとき |
| **C2** `CommandRunner` が exit code を持たず `*exec.ExitError` を掘る | 事実誤認は無い (4 件とも実コードどおり)。しかし harm を今日のどの入力・状態・順序からも構成できない (production の実装は `ExecRunner` 1 本のみ)。さらに主張が「今日すでに発火している証拠」に挙げた**テストの `sh` 起動は issue 100 §6 で codex 敵対レビューが既に指摘し、理由つきで却下されコード側の doc にも残っている既決事項**。実在する発火条件は「`cliHealthRunner` に差した実装が `*exec.ExitError` の同一性を失う場合」に限られる (`%w` の wrap は通ることを実測) |
| **C8** `subproc` にプロセスグループ kill が無い | 構造は事実 (`subproc.CommandContext` は `WaitDelay` のみ / `doctor/runner.Exec` は `Setpgid` + `Kill(-pgid)` / 2 秒定数の重複)。しかし発火条件が 1 つも成立せず、**さらに主提案が退行を作る** (下記の 🚨) |

### 🚨 C8 の提案は採らないこと (実装すると機能を壊す)

C8 は「`subproc` に `Setpgid` + pgid kill を取り込む」を主提案にしているが、
**`bin/lib/go_autobuild.zsh: _go_autobuild_spawn` は裏ビルドを意図的にデタッチしている**:

```zsh
( trap '' HUP TERM INT
  ... ) >>"$log" 2>&1 </dev/null &!
```

コメントが理由と過去事故まで書いている — 「popup を閉じたときに process group へ飛ぶ HUP で
巻き添えにされないため (**これが実際に起きていた経路**)」。
**`SIGKILL` は trap できない**ので、提案どおり pgid kill を入れると `localCmdTimeout` (5 秒)
超過時にこの裏ビルドを殺す。C8 が発火例に挙げた `autobuild.go` は**偽陽性**。

C8 の他の前提も陳腐化していた: 「doctor 側に等価な強制が無い」は `fdf51bb2` (00:02) の
depguard `exec-via-runner` (runner/ とテスト以外での `os/exec` import を CI で禁止) で偽。
「変異を当てても緑のまま」も誤りで、root `make test` は `GO_PROJECT_DIRS` の自動発見で
`src/doctor` を含むため `runner_test.go` の 2 本が red になる。

## 反証させたことで得られたもの (手順の記録)

**「レビューして」ではなく「この主張は誤りだという前提で反証せよ。不確かなら refuted に倒せ」**と
指示し、観点を①事実関係 ②影響 に分けて 2 体に当てた。結果、**12 クラスタ中 8 つが崩れた**。
肯定形で聞いていれば 31 件がそのまま issue になっていた。

### 反証者が見つけた「主張側の典型的な失敗」

1. **issue を「探した」と書きながら決定的な行を見落とす** (C4 = 183 の「(足さない)」)。
   → 関連 issue は**全文を読ませる**指示が要る
2. **stale なツリーを読む** (C10 = 修正の 2 分半後に書かれたレポート)。
   → 並行セッションのある repo では「主張の基準時刻」を明示し、`git log` で差分を確認させる
3. **追跡を 1 段手前で打ち切る** (C1 の影響面 = `ansi.Truncate` で止め、bubbletea v2 の
   セル分解を見ていなかった)。→ **sink まで追い切ったか**を明示的に問う
4. **同じ fixture を使う隣のテストファイルを見ない** (C11 = `dial_test.go` だけ見て
   `banner_test.go` を見落とし「テストは存在しない」と書いた)

**3 は自分 (main agent) も踏んだ** — 228 を P1 で起票してから実測で P2 へ訂正した。

## 関連

- `issues/done/071-research-design-audit-2026-08-20.md` — 同じ形式の先行記録 (却下一覧の正本)
- `issues/226-retro-glogx-audit-2026-09-03.md` — 別セッションの retro。項目 1 が
  「テストの**射程**を測らずに却下の根拠にした」で、本監査の C3 / C11 / C12 は**逆に射程を測って却下できた**
