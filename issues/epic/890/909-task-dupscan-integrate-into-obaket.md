# 909 (task): obaket の make test と CI に dupscan gate を組み込む

起票日: 2026-10-25

## 概要

obaket に `dupscan gate` ([905](905-feat-dupscan-ci-check-gate.md)) を組み込み、既にある関数の写しを足した変更が `make test` と CI で落ちるようにする (ユーザー 2026-10-25: obaket に組み込みたい)。
epic 890 の起点は obaket の PR 24 (同じ `StorageLocation` を組むヘルパーが 6 つ。経緯は obaket issue 1298) なので、
obaket で効くことが epic の受け入れでもある。dotfiles の他の issue に残っている「obaket が要る」確かめも、ここでまとめて行う。

**このマシンには obaket の checkout が無い**。作業する session は、clone の前にユーザーの許可を取る (`~/.claude/CLAUDE.md`「無断で `git clone` しない」)。
obaket の規約 (obaket の CLAUDE.md・`.claude/rules/`・issues/README.md) は、触る前に Read する。

## 手順

1. **前提を測る** (組み込む前。obaket には何も書かない)
   - obaket の root で `dupscan` (report) と `dupscan gate -base <既定のブランチ>` を回し、所要 (wall) と組の数を記録する。
     parse だけなら Swift 1626 ファイルで 5.0 秒 ([891](891-design-dupscan-requirements.md) の実測)。通しは未実測
   - 所要で `make test` に入れるかを決める: 増分が数秒を超えるなら、base のキャッシュを dupscan に足すか、`make test` でなく lint の gate に置くかをユーザーに諮る (905 の trigger)
2. **dotfiles 側の受け入れ** (PR 24 の前の木 = obaket の commit `45cc3b208^`。obaket の作業中の checkout を動かさないよう、その木を `git worktree add --detach` で別に出して使う。
   untracked の確かめは作業ツリーが要るので `-head <commit>` では代えられない。worktree を作る前にユーザーに諮る)
   - [905](905-feat-dupscan-ci-check-gate.md): その worktree で base を `45cc3b208^` にし、6 つのうち 1 つを新しく足した作業ツリーで `gate` が非 0 になり、足した単位を寄せる側として名指しする (未コミット・untracked の新しいファイルの場合も)
   - 905: 誤って止める割合を obaket の過去の commit の流し直し (`-base <親> -head <commit>`。DualNoteApp では 905 の進捗にある流し直しで測った) で測り、止める段の既定 (`-fail-levels`) を obaket でも保てるか決める
   - [904](904-feat-dupscan-trial-subcommands.md): 6 つのうち 1 つを `near` で指すと、残りの 5 つが段つきで出る
   - [897](897-feat-dupscan-index-subcommand.md): `SharedTestSupport` に `bucketLocation(path:)` を含む top-level の関数が載り、名前の一部・戻り値の型 (`StorageLocation`)・doc の語のそれぞれで PR 24 の共通のヘルパーが引ける。1 回の引きの所要。
     `SharedTestSupport` は obaket の `.dupscan.json` の `"shared"` に glob で指す (設定は 2026-10-25 に足した。897)
   - [894](894-feat-dupscan-swift-adapter.md): 6 つの `location` の定義を含むファイルを切り出して adapter のテストの固定の入力にする (dotfiles に置く。obaket のコードの一部を写すので、置いてよいかをユーザーに確かめる)。
     所属ターゲットの解決 (`Package.swift`・`project.yml`) を obaket の構成で確かめる
   - [898](898-test-dupscan-obaket-calibration.md): 全体の期待値 (PR 24 の前の木の全体に既定の設定で当て、6 つが出る・同じ組に入ってよいもの / 入ってはいけないもの) と、
     肯定と否定を対にした変異 (文を 1 つ足す・1 つ消す・段を外す・閾値を緩める)。組の数・誤検知の割合・ノード数の分布・所要の実測
   - [895](895-feat-dupscan-type3-candidates.md)・[901](901-research-dupscan-existing-tools.md): obaket の全体での所要、CPD / jscpd を PR 24 の前の木に当てた比べ、ast-grep で 6 つを形で引けるかの試し
   - [891](891-design-dupscan-requirements.md) の「効果」: 組み込む前の段ごとの組の数 (test / prod 別) を `dupscan -format json` で残す (導入の前と後で比べるため。`gate --record` も CI で使う)
   - 結果は各 issue の本文に書き戻す (この issue には要約と、どこに書いたかだけ)。894〜907 には 2026-10-25 時点で別の session の着手の印 (`next/` と担当バナー) が付いている。
     書き戻す前に `git fetch` して印の持ち主を見て、生きている session なら調整する
3. **組み込む** (obaket の変更。obaket の issue 1298 で行う)
   - `src/dupscan/README.md` の「使う側の repo の `make test` と CI に組み込む」の Makefile と GitHub Actions の例を写す。obaket の既存の lint の gate (lint-gates) に載せるか `make test` に載せるかは、obaket の構成を読んで決める
   - 既にある `macOS/bin/check-test-helper-copies` (字句の検査。6 つのうち 4 つを見ていなかった) と登録簿をどうするか (残す・dupscan に置き換える) を決めて issue 1298 に書く
   - 既にある写し (gate は増えた写しだけを止めるので、今ある組は止めない) のうち誤検知のものは、理由つきで `.dupscan.json` か `// dupscan:allow` で外す
   - CI は `-fail-new-exclusions` を付ける (除外を足した PR を人の目に届ける)
   - YAML の `DOTFILES_SHA` は、公開の `jiikko/dotfiles` に push 済みの commit にする。build は `src/dupscan` と `src/subproc` (go.mod の replace) を取り、cgo で文法の C のソースを build できる runner が要る
4. **CI で 1 回確かめる**: 写しを 1 つ足した PR (か branch の push) で CI の gate が落ち、消すと通ることを見る。README の YAML はまだどの repo の Actions でも走っていない (905 の「未検証」)
5. obaket の CLAUDE.md の入口から skill `dupscan` (dotfiles の `_claude/skills/dupscan/`) にたどり着けるかを見る (Swift の repo で発火する前提。[903](903-docs-dupscan-search-before-write-procedure.md))

## 受け入れ条件

- [ ] obaket の通しの所要と組の数を実測して書いた (`make test` に入れるかの判断つき)
- [ ] PR 24 の前の木での 905・904・897 の受け入れが通った (各 issue に書き戻した)
- [ ] obaket の `make test` (か lint の gate) と CI に `dupscan gate` が入り、写しを足した PR で CI が落ちた
- [ ] 既存の字句の検査 (`check-test-helper-copies`) の扱いを決めた

## 依存

- [905](905-feat-dupscan-ci-check-gate.md) (gate と組み込みの例)・[906](906-feat-dupscan-exclusion-config.md) (除外)・[903](903-docs-dupscan-search-before-write-procedure.md) (skill)
- 手順 2 で受け入れる側: [894](894-feat-dupscan-swift-adapter.md)・[897](897-feat-dupscan-index-subcommand.md)・[898](898-test-dupscan-obaket-calibration.md)・[904](904-feat-dupscan-trial-subcommands.md)
- 道具の名前を変える (rename) なら、obaket に書く前に変える (後で変えると obaket の Makefile・CI・除外のコメントも直すことになる)

## 進捗

- 2026-10-25 起票 (ユーザー指示: obaket に組み込みたいが repo を clone していないので、dotfiles の sub issue として書いておく)
- 2026-10-25 反証レビュー (sonnet・読み取りのみ) を取り込んだ: untracked の確かめは作業ツリーが要るので worktree に出す (ユーザーに諮る) /
  拾い漏れ 5 つ (obaket の流し直し・898 の全体の期待値と対の変異・894 の所属ターゲット・901 の ast-grep・891 の効果の記録) を手順 2 に足した /
  897 の受け入れは共通の置き場所の設定が未実装 / 依存に受け入れる側の issue / 書き戻す前の着手の印の確認 / YAML の DOTFILES_SHA と build の前提。
  却下: 「`next/` に 909 の目印が無い」(next/ は着手の印で、open な issue を並べる場所ではない。着手する session が claim で置く)
- 2026-10-25 897 の共通の置き場所の設定 (`shared`) ができたので、手順 2 の前提を直した
