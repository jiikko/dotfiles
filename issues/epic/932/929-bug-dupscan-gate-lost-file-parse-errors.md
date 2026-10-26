# 929 (bug): dupscan gate が単位を抽出できない解析失敗を通知せず成功する

> 🚨 **担当中: Mac (glogx)**（2026-10-26〜）

起票日: 2026-10-26

## 問題と影響

既存実装を見落として似たメソッドが増えるのを防ぐには、探索・gateが「見つからなかった」と「解析できなかった」を区別する必要がある。現在gateでは、ファイルの解析が失敗して単位が0件になると、失敗ファイルの情報が出力から落ちる。新しい重複が無かったのか、検査対象を読めなかったのか分からない。

9d2e801fで再現。優先度P2。データ破壊の再現ではなく、検査結果の信頼性の問題。再現用の不正なSwiftはコンパイラでも拒否されるため、build検査が併設されていればその例は補える。正しいSwiftでparserだけが失敗する例は今回未確認であり、全CIが壊れたコードを通すとは断定しない。

## 再現手順

dotfiles rootでCLIをbuildし、2つのcommitを持つ使い捨てrepoで試す。HEADとbaseが同じことによる注意表示とは独立して再現する。

```sh
go -C src/dupscan build -o /private/tmp/dupscan-929 .
python3 - /private/tmp/dupscan-929 <<'PY'
import json, pathlib, subprocess, sys, tempfile
exe = str(pathlib.Path(sys.argv[1]).resolve())
root = pathlib.Path(tempfile.mkdtemp(prefix='dupscan-929-'))
def git(*args):
    subprocess.run(['git', '-C', str(root), *args], check=True,
                   capture_output=True)
git('init', '-q')
git('config', 'user.name', 'Fixture')
git('config', 'user.email', 'fixture@example.invalid')
(root / 'Good.swift').write_text('func good(_ x: Int) -> Int { x + 1 }\n')
(root / 'Broken.swift').write_text('func original(_ x: Int) -> Int { x + 2 }\n')
git('add', '.')
git('commit', '-qm', 'baseline')
(root / 'Broken.swift').write_text('func broken(')
git('add', 'Broken.swift')
git('commit', '-qm', 'unparseable change')
for args in (['report', '-format', 'json'],
             ['gate', '-base', 'HEAD~1', '-head', 'HEAD', '-format', 'json']):
    p = subprocess.run([exe, *args], cwd=root, capture_output=True, text=True)
    print(json.dumps({'argv': args, 'rc': p.returncode,
                      'stdout': p.stdout, 'stderr': p.stderr}))
print('fixture:', root)
PY
```

実測: reportは `files_with_errors: ["Broken.swift"]`。gateはrc=0、`changed_units: 0`、`base_is_head: false`、`unreadable_changed_units: []`、stderr空。textも「新しい重複なし」となり解析漏れの注記が無い。

## 原因の裏取り

- `src/dupscan/main.go` のparseAll/scanSourceOptionsはfile単位のHasErrorをerrFilesに保持している。
- `gate_cmd.go` はbase/headのentriesだけをgate.Judgeへ渡し、両側のerrFilesをBuildGateへ伝えていない。
- `internal/gate/gate.go` のUnreadableは抽出できた変更単位の列挙。単位が0件ならこの経路では通知できない。
- `internal/report/gate.go` のGateSummaryはfile単位の解析失敗欄を持たない。[926](926-feat-dupscan-query-diagnostics.md) の探索診断だけではgate側は直らない。

## 対応方針

1. gateのbase/headのfile単位解析失敗を、単位の抽出数に関係なく別々に伝える。全scopeの失敗を保持し、比較相手側の失敗も隠さない。既存unreadable_changed_unitsは単位の情報として維持する。
2. textの先頭行・JSON・xcode・任意のrecord出力に、解析が不完全であることと対象fileを出す。JSONは既存fieldの意味を維持し、追加fieldの互換性を既存consumerと確認する。列挙を省略しても件数/省略を残す。
3. 既存の通常rc契約は黙って変えず、解析失敗で判定不能にする明示option（案 `-strict-analysis`、未実装）を追加する。strictかつbase/headいずれかにfile parse errorがあればrc=2。重複違反も併存する場合は判定不能を優先し、検出済み違反は出力に残す。通常modeも不完全という注記は必ず出す。
4. file読取失敗は現行の実行エラーrc=2を保つ。parse失敗、I/O失敗、意図した除外、非対応言語を同じ理由にまとめない。存在しない単位を捏造せず、違反/出どころ/閾値を変更しない。

## 検証・受け入れ条件

- [ ] 上の0単位の再現をCLI回帰テストにし、変更単位Unreadableの既存テストだけでは検出できないことを確認する。
- [ ] Swift/Go、baseだけ/headだけ/両方、0単位/一部抽出、変更/未変更file、worktree/commit head、I/O失敗の組合せを検査する。
- [ ] 正常入力では既存の違反、除外、base判定、rc、結果順を維持する。
- [ ] text/JSON/xcode/recordで不完全な解析が検知でき、strictのrc=2と重複rc=1の優先順位が一致する。
- [ ] `make -C src/dupscan lint test` 成功。共有fixtureへ依存せず、実行ごとに独立したrepoを使う。

## 進捗

- 2026-10-26: `issues: reserve dupscan coverage and call evidence follow-ups` で番号予約。9d2e801fをbuildしてCLIで再現し、原因と修正・検査の境界を記録した。改善は解析漏れの再現と受け入れ条件を残したこと。実装修正は未着手。

- 2026-10-26: 出力の正確性について読み取り専用Codex subagentで反証レビューし、P1/P2/P3の追加指摘なし。 issue内の相対リンク・JSON/Python構文・diffとBash 5のissue関連テストが成功。929の本文内手順と930の3問い合わせをCLIで再現した。文書の改善は、出力誤認の再現・責務と、既存実装発見/不要な重複追加を測る条件を具体化したこと。機能改善の効果は未検証。
