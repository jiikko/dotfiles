# subproc

外部プロセス実行の安全弁 (WaitDelay と git の timeout) を 1 箇所に置く module。glogx・dupscan・ratelimit・pro-con・treefiler・
mac-lan-manager・tmpclean・serialrun が replace で取り込む。**新しい外部コマンド実行は `subproc.CommandContext` を使う** (理由と経緯は subproc.go の doc)。

- クリップボードへの書き込みは `subproc/clipboard` の `Copy` (glogx と treefiler の両方が使う。WaitDelay が要る stdin のパイプを持つので subproc に置いた。issue 718)
- git の起動の口 (GitArgs など) と、テストで継承した GIT_DIR などを外す `UnsetGitLocationEnv` の使い分けは CLAUDE.md の「入口」
- ctx を持たない起動 (前景で端末に直結する attach・supervisor が止める子・終わるまで待つだけの ps) は `subproc.Command` (同じく WaitDelay を張る)
- 張り忘れの検出は `subproc/boundarycheck` が本体で、各 module のテスト (glogx `waitdelay_discipline_test.go`、ratelimit / treefiler / pro-con / mac-lan-manager / tmpclean / dupscan の `exec_boundary_test.go`) が
  数行で呼ぶ。既定は「Cmd を作ったら WaitDelay を張る (注記で免除) + Cmd を作るファイルを Allow に固定」、厳しい設定 (`NoOsExec`) は「os/exec を import しない」。
  走査した数は go list とディレクトリごとに突き合わせる (issue 777)
