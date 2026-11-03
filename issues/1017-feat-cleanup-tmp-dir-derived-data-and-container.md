# 1017 (feat): cleanup-tmp-dir で Xcode の DerivedData と Apple container も掃除するかを聞く

> 🚨 **担当中: air5 (glogx)**（2026-11-03〜）

起票日: 2026-11-03

## 依頼 (原文)

> tmp clean skillもあるけど、  derived-data, apple containerも対象にしてクリーンアップするかを聞くように改修して

## 対応

- skill `cleanup-tmp-dir` (2.1.0): 数えた後に、何を対象にするかを AskUserQuestion (複数選択) で聞く手順 3 を足した。選択肢は一時領域・Xcode の DerivedData (持ち主の消えたもの)・
  Apple container (停止中の container・使われていない image)・dotfiles の `tmp/`。無いもの (DerivedData の dir が無い・container が無い・service が止まっている) は出さない
- tmpclean に種類 `derived-data` を足した (`src/tmpclean/derived_data.go`)。`~/Library/Developer/Xcode/DerivedData` の直下の `<名前>-<英小文字 28 字>` のうち、
  持ち主 (info.plist の WorkspacePath。`plutil` で読む) が無いと言い切れるものだけを候補にし、既存と同じ plan → 承認の ID → apply (直前に `lsof +D` を取り直す) → ゴミ箱の経路で移す。
  古さでは選ばない。info.plist の無い `<ディレクトリ名>-<hash>` (SwiftPM の package のビルドが作る) は、同じ hash で info.plist を持つ entry の持ち主が全部消えているときだけ候補にする。
  検査点 (trashGuard) は DerivedData の根の直下だけを通す
- Apple container は tmpclean に載せず、skill で別の質問にした (ファイルを動かす操作でなく CLI の操作で、ゴミ箱を経由しない)。`container prune` / `container image prune` /
  `container image prune --all` を選ばせ、打つ直前に一覧を取り直して増えていないかを見る。**volume は消さない** (名前と容量を見せるだけ)
- 入口: skill の表と手順、`tmpclean -h`、`src/tmpclean/README.md` の derived-data 節

## 実測 (2026-11-03)

- この機械の container の volume に `monolink-mysql-data` があった。volume は cache でなくデータのことがあり、`container volume prune` は container から参照されていない volume を全部消す
  → volume は skill の選択肢に出さない
- obaket のテスト用 container は止めるときに `container rm -f` で消し、起動のたびに作り直す (`bin/lib/container-test-server.bash`)。停止中の container の prune で壊れない
- SwiftPM の package を xcodebuild でビルドすると、hash が同じ 2 つの dir (info.plist を持つ `<package 名>-<hash>` と、Build を持ち info.plist の無い `<ディレクトリ名>-<hash>`) ができる
  (my-products issue 041 で実測)
- `lsof +D` は 2.5 万ファイル (1.4G) の DerivedData で 0.54 秒
- 本物の機械で `tmpclean plan -kinds derived-data` (読み取りのみ): 候補 0、残した理由は「形でない」4 (ModuleCache.noindex など)・「持ち主が在る」1 (obaket 本体)

## 検証

- `go test ./...` (src/tmpclean) と `make -C src/tmpclean lint` (0 issues)、`dupscan gate` (新しい重複なし)
- derived-data のテスト: 持ち主の消えたもの・package の 2 つを移す / 持ち主が在る・同じ hash に持ち主の在る info.plist がある兄弟・info.plist の無い単独・形の違う dir・
  key の無い / 相対の / 外れた volume の WorkspacePath・開かれている・symlink を残す / 移す直前に開かれた・持ち主が戻ったら残す / 根が無ければ 0 件 / 根が symlink なら止める /
  検査点の深さ / 残す理由の表示 / 移す直前に持ち主が戻ったら残す / info.plist が symlink なら兄弟ごと残す / 本物の plutil で XML と binary の plist を読む /
  外付けの volume の下・祖先が symlink なら残す / plutil の失敗はその entry を残すだけで止めない / plan の種類の書き換えで session の取り直しを飛ばせない
- 変異 (どれも狙ったテストが赤): 持ち主の存在を見ない / 祖先の深さを見ない / 同じ hash の持ち主を見ない / 開かれているかを見ない / 形を見ない (残す理由のテストで赤) /
  兄弟の判定を AND から OR へ / 自分の持ち主を見ない / 検査点の DerivedData の節を外す / 移す直前の lsof を飛ばす /
  祖先の symlink を見ない / 外付けの根を見ない / 種類の照合を外す / 移す直前の持ち主の取り直しを外す / 先に移した hash を数えない /
  symlink の info.plist を info.plist の無い dir として数える
- `bin/skill-eval cleanup-tmp-dir`: PASS score=1 delta=1 cases=2/2 (発火と、DerivedData・container を対象の候補に挙げるかの新しいケース)
- skill の整合チェック 3 回 (skill の中・外の実物・使う人の目): 手順の番号の参照のずれ、plan の無い経路 (container だけを選んだとき)、選択肢と別の質問の結び付けを直した
- 敵対的レビュー (codex が 11/4 まで使用上限のため opus のサブエージェント) 2 周:
  - 1 周目 (P1 なし): 採用 = symlink の祖先・外付けの volume の下を「持ち主が消えた」と読む / 移す直前に持ち主を取り直していない /
    plan の種類を照合しない / plutil に時間の上限が無い / 本物の plutil と symlink の info.plist のテストが無い / container の文言 (手元でビルドした image・container の中に書いたもの)
  - 2 周目 (P1 なし): 採用 = plutil の時間切れで apply 全体が止まる (その entry を残すだけに) / 取り直しで全部の plist を読む (同じ hash だけに) /
    先に移した hash を移す前に数えていた (移せた後に) / `/Volumes/` の配線のテスト

## 残り (記録のみ)

- `/Volumes` 以外に置いた mount point が外れて空の dir として残っているときの、その下の持ち主は「消えた」と読む
- 存在する最も近い祖先が symlink なら、その先が在って持ち主を消しただけでも残す (取りこぼし)
- 数十万ファイルの DerivedData で `lsof +D` が 60 秒の上限を超えると移せない (rc=1)。大きいものでは未実測
- info.plist の無い dir が先に処理され、その後に同じ hash の info.plist を持つ entry の持ち主が戻ったとき、Build の dir だけがゴミ箱へ行く (戻せる。再ビルドで直る)
- 検査点は根の和で見る (種類ごとに根を分けない。session・tmpdir と同じ)
- skill の最後の文言の手直し (container の注記・手順の結び付け) の後は `bin/skill-eval` を回していない (振る舞いは変えていない)

## 評価

一時領域を片付けるときに、DerivedData と Apple container も対象にするかを選べるようになった。DerivedData は持ち主の消えたものだけを、既存の守り
(plan の ID・移す直前の取り直し・ゴミ箱) に載せて移す。container は volume を消さない形に絞った。足したものは tmpclean の種類 1 つ (約 200 行とテスト) と skill の手順。
本物の機械では今は候補 0 件で、持ち主の消えた DerivedData を本物で移すところまでは確かめていない (テストの fixture と、読み取りのみの plan まで)。
