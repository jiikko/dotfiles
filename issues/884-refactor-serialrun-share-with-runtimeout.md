# 884 (refactor): serialrun と runtimeout の写しを共有にする

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-23〜）

起票日: 2026-10-23

## 概要

serialrun の監査で見つかった、グループ G の 2 項目。

- 監査: 2026-10-22〜23 に audit skill の 22 観点を 1 体ずつ直列で回した (sonnet・読み取りのみ。直接実行)。全数勘定と、直さないと決めたものの理由は [886](886-research-serialrun-audit-2026-10-23.md)
- 設計の正本: [869](869-feat-serial-run-machine-wide-heavy-command-queue.md)。下の `869:NNN` は 869 の行番号 (2026-10-23 時点)
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

- [ ] 未着手 (項目ごとに、直した commit の subject・当てた変異・実測を書く)
