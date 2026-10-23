# 884 (refactor): serialrun と runtimeout の写しを共有にする

起票日: 2026-10-23

## 概要

serialrun の監査で見つかった、グループ G の 2 項目。

- 監査: 2026-10-22〜23 に audit skill の 22 観点を 1 体ずつ直列で回した (sonnet・読み取りのみ。直接実行)。全数勘定と、直さないと決めたものの理由は [886](886-research-serialrun-audit-2026-10-23.md)
- 設計の正本: [869](../869-feat-serial-run-machine-wide-heavy-command-queue.md)。下の `869:NNN` は 869 の行番号 (2026-10-23 時点)
- 各項目の「確認済み / 推測」は監査の報告の申告。直す前に実コードで裏を取る (`~/.claude/CLAUDE.md`「主張は証拠ではない」)
- 出典の「観点名 P2-1」は監査の報告の番号 (報告そのものは tmp に置いた使い捨てで残らない。要点は各項目に写した)

## 項目

### G1 runtimeout と serialrun の「子を起こして rc を写す」一式 (`exitAs` / `waitStatus` / `startErrorRC` / `parseSeconds` + rc 定数 126/127) が写しで、片方だけ直った実害がもう出ている
- 重要度: P2 / 確認済み (3 関数は逐語一致。`exitAs` だけ serialrun 側に `got == syscall.SIGQUIT` の除外があり、コメントは「runtimeout の exitAs と同じ」のまま。runtimeout に `runtimeout 30 sleep 20` の pid へ SIGQUIT → rc=2 + goroutine ダンプ。期待は 131)
- 根拠: src/serialrun/main.go と src/runtimeout/main.go。本体一致の組は 3 組 (`waitStatus` 13 行・`startErrorRC` 6 行・`parseSeconds` 7 行) で全部この 2 module の組。serialrun は既に `replace proctree => ../proctree` で兄弟 module を持つ前例がある。module に出すと良くなるかは推測 (Makefile / CI の module 数が増える)
- 直し方の案: 小さな module (子の起動・rc 変換・死に直し) に出す。runtimeout の QUIT は serialrun の版へ揃える (別 issue)。出さないなら両 README に「写し。片方を直したらもう片方も見る」と書き、runtimeout の QUIT の修正を先に入れる。同じ commit か直後に、runtimeout 側にも要る修正を揃える (C1 の Ignored / A4 の Stop の窓 / E4 の `--help` / G2)。クロスモジュールの同型関数の検査 script は 2 組目が出るまで足さない (I14)
- 出典: duplication P2-1 / lint-from-done P2-2

### G2 `parseSeconds` が 0 より大きいが 1ns 未満の値を 0 に切り捨て、`--max-wait 1e-10` が「上限なし」になる
- 重要度: P3 / 確認済み (error-handling: 他が 6 秒走っている間に `--max-wait 1e-10 -- echo ran` を起こすと 6 秒待って「実行 (待ち 6 秒)」で走った。broken-code は読んだだけ)。そう渡す呼び出し元は現状無い
- 根拠: main.go `parseSeconds` (`time.Duration(f * 1e9)`) と `runQueued` の `if opt.maxWait > 0`。runtimeout にも同じ関数がある (G1)
- 直し方の案: `f > 0` なのに結果が 0 なら 1ns に切り上げる (または拒否)。G1 の共有化と同時なら 1 か所
- 出典: error-handling P3-1 / broken-code U-3

---

## 進捗

- 2026-10-23 (commit「refactor: runtimeout と serialrun の子の rc・死に直し・秒の引数を proctree/childrun に寄せる (issue 884)」):
  - [x] G1 `exitAs` / `waitStatus` / `startErrorRC` / `parseSeconds` と rc 126/127 を、新しい module ではなく proctree の package `childrun` に置いた
    (runtimeout も serialrun も既に proctree を replace で持ち、CI の paths にも proctree が入っている。module を増やすと Makefile・CI・一覧の配線が増える)。
    両方の写しは消し、呼び出しを `childrun.*` に。寄せる前に呼び出し元の前提を突き合わせた: runtimeout の「無視していた合図は Reset で無視に戻る」と
    serialrun の「QUIT では死に直さない」はどちらも共通の版で表せる。
    🚨 **runtimeout の QUIT の不具合はこれで直った** (QUIT で rc=2 とダンプ → 131)。`TestQuitToToolKeepsSignalRC` (runtimeout)。
    runtimeout 側に揃える修正の残り: E4 (`--help`) は 882 で入れた / G2 はこの共有で入った / C1 (起動時に無視されていた INT・QUIT を保つ) は runtimeout が意図して外している
    (`src/runtimeout/main.go` の注記。mutate-verify の Ctrl-C のため) ので揃えない / A4 は serialrun の待ち列の回収 (`reclaimStopped`) で、runtimeout には該当する仕組みが無い
  - [x] G2 `ParseSeconds` は 0 より大きいのに 1ns 未満になる値を 1ns に切り上げる (`1e-10` → 1ns)。`TestParseSeconds` (childrun。runtimeout から移した)
- 変異 (全部 red): QUIT でも死に直す / 1ns 未満を切り捨てる / 死に直さない
- 敵対的レビュー 1 周 (Opus): P1 / P2 なし。挙動の変更は意図した 2 つだけ (QUIT・1ns) と突き合わせで確認。QUIT の経路は直す前より良いか同じ
  (子が TERM で死んだ: 2 → 143 / 呼び出し元が QUIT を無視: 131 のまま / INT の後に QUIT: 2 → 130)
  - P3 子が pid を書かないまま待ちが尽きてもテストが QUIT を撃ちに進む → 落とすように直した
  - P3 **記録 (前からある形)**: ExitAs が覚えるのは最後のシグナルだけ (INT の後に QUIT を受けると INT で死に直さない) /
    ParseSeconds の上限は float→int64 の変換を arm64 の挙動に頼る (amd64 では負になりうる。このリポジトリは macOS の arm64 だけ) /
    runtimeout の `rcCannotExec` / `rcNotFound` の別名はテストだけが使う
