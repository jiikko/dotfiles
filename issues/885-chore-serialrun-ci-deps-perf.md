# 885 (chore): serialrun の CI・依存・性能

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-23〜）

起票日: 2026-10-23

## 概要

serialrun の監査で見つかった、グループ H の 2 項目。

- 監査: 2026-10-22〜23 に audit skill の 22 観点を 1 体ずつ直列で回した (sonnet・読み取りのみ。直接実行)。全数勘定と、直さないと決めたものの理由は [886](886-research-serialrun-audit-2026-10-23.md)
- 設計の正本: [869](869-feat-serial-run-machine-wide-heavy-command-queue.md)。下の `869:NNN` は 869 の行番号 (2026-10-23 時点)
- 各項目の「確認済み / 推測」は監査の報告の申告。直す前に実コードで裏を取る (`~/.claude/CLAUDE.md`「主張は証拠ではない」)
- 出典の「観点名 P2-1」は監査の報告の番号 (報告そのものは tmp に置いた使い捨てで残らない。要点は各項目に写した)

## 項目

### H1 依存の無い module 12 本が setup-go のキャッシュ鍵を共有し、保存し直されない
- 重要度: P3 / 鍵の共有は確認済み。効果の大きさは推測 (差の約 12 秒がコンパイル + リンクだが、キャッシュで済んだ分は未測定)
- 根拠: `src/*/go.sum` が空の module 12 本 (atomicfile / codexevents / lockman / mp4box / process_supervisor / proctree / runtimeout / serialrun / subproc / termsafe / userdirs / zundamon-kaisetsu)。`cache-dependency-path: ${{ inputs.dir }}/go.sum` が空ファイルのハッシュになり全部同じ鍵。`Cache hit ... not saving cache` で最初に保存した 1 本 (13 MB) が固定
- 直し方の案: 先に測る (空のキャッシュと復元後で `go test -race` の所要を比べる)。差が数秒なら何もしない。大きければ鍵にプロジェクト名を入れる
- 出典: ci P3-2

### H2 go が古い機械で、hook の同期ビルドが toolchain (~90MB) の取得を待つ。ソース変更後の最初の hook 呼び出しは同期ビルドで約 0.9 秒止まる
- 重要度: P3 / toolchain の取得は推測 (経路は実コードで確認。この dev 機は go 1.26.0 で発火しない。timeout 後の挙動は settings.json の hook timeout 次第で未確認)。0.9 秒は確認済み (実測 904 / 921 ms、冷たい初回 1113 ms) だが performance は「許容、不要」と書く
- 根拠: src/serialrun/go.mod `go 1.25.0` / bin/lib/go_autobuild.zsh の `_go_autobuild_build` (GOTOOLCHAIN 既定のまま) / bin/serial-run の `go_autobuild_exec` が `--async` なし
- 直し方の案: 手元 go が go.mod の要求より古いときは hook 経路 (`serial-run hook`) では再ビルドを諦めて素通し (fail-open)。または go.mod の `go` 行を実際に要る版まで下げる (下がるかは未確認)。0.9 秒の同期ビルドは直さなくてよい (判定だけの hook に限って `--async` にする案は、旧版が「取れた」を返す危険を注記が名指ししており未検証)
- 出典: dependency P3-1 / performance P3-1

---

## 進捗

- 2026-10-23: 2 項目とも測って、コードは変えないと決めた
  - [x] H1 **直さない**: 手元 (go1.26.0 darwin/arm64) で serialrun の `go test -race -count=1 ./...` を、空の GOCACHE で 37.6 秒、温まった後で 32.4 秒
    (差 5.2 秒。所要の大半はテスト自体の約 32 秒。空の GOCACHE は 129 MB)。issue の案の「差が数秒なら何もしない」に当たる。
    CI の runner での差は未実測 (監査の見積もりは約 12 秒)。**再評価の trigger**: CI の 1 job の所要の大半がビルドになったとき (go test のログの ok 行の秒と job の所要の差で見る)
  - [x] H2 **直さない**: 手元の go が go.mod の要求 (1.25.0) より古い機械はいまのところ無い (この機械は go1.26.0)。直す案はどれも今の害より重い:
    hook の入口で手元の go の版を見る判定は `go env` が 1 回 0.2 秒 (3 回測って 0.20〜0.21 秒。goenv の shim 越し) で、重いコマンドの hook のたびに足すことになる /
    入口で `GOTOOLCHAIN=local` にすると、go_autobuild がビルドの失敗を記録して TTL の間は手で打つ `serial-run` まで止める (`bin/lib/go_autobuild.zsh` の注記) /
    hook だけ `--async` にする案は、旧版の判定を返す危険が `bin/serial-run` の注記で名指しされている。
    **再評価の trigger**: go が 1.25 より古い機械でこの dotfiles を使うとき (そのときは hook の初回が toolchain の取得で 20 秒の timeout に掛かりうる。掛かっても hook は fail-open で、包まずに走る)
