# 263 bug: issue を `next/` へ claim すると本文の相対リンクが切れ、doc-links gate が push を止める (glogx の `n` も同じ)

起票日: 2026-09-05

## 観測 (obaket、2026-09-05)

`git mv issues/634-*.md issues/next/` で claim した直後の push が、pre-push の `check-doc-links` で落ちた:

```
BROKEN LINK: issues/next/634-bug-per-file-upload-reject-reason-missing-from-logs.md
  link → done/549-bug-readonly-upload-reject-lacks-provider-context.md
  link → 569-bug-finder-drop-rejected-as-nousablefiles.md
```

issue 本文の相対リンク (`done/549-...` / `569-...`) は `issues/` 直下を起点に書かれており、1 段下の `next/` へ動くと全部切れる。
同日 635 でも同じことが起き、どちらも手で `../` を足した。glogx の issues viewer の `n` (`src/glogx/issues/move.go:MoveToSubdir`) は
ファイルを rename するだけで本文を書き換えないので、人が `n` で claim しても同じ壊れ方をする (その場合 gate に落ちるのは次に push する人)。

`done/` への移動でも同じ問題があるが、こちらは done 移動の規律 (`issue-done-on-completion.md`) が「参照パスを grep して同期」を要求しているので
運用で吸収されている。claim は「移動だけを即 push」する規律なので、リンクの書き換えが挟まりにくい。

## 前提 (codex 反証レビュー 2026-09-05 で確認)

- `MoveToSubdir` (`src/glogx/issues/move.go:49`) は `os.Rename` だけで本文を読まない
- obaket の gate `macOS/bin/check-doc-links:38` はリンクを **その md の所在ディレクトリ基準**で解決し、`:18` で `done/` 配下は検査対象から除外している
  (= done 移動で切れたリンクは gate に見えない。next / pending 移動だけが gate で見える)
- `next-claim-push.sh` は Bash の `git mv` を静的検査する hook で、glogx の `n` は通らない。commit / push を案内するだけで doc-links は回さない

## 対応案 (どれか)

- A: `MoveToSubdir` が移動時に本文の相対リンク (`](path.md)` で repo 内を指すもの) を移動先起点に書き換える。
  移動元と移動先の**両方の深さ**から相対パスを再計算する (global `next/` だけでなく `epic/<name>/next/` のような group 配下も対象)
- B: issue 内リンクの起点を「issue ディレクトリ直下」に固定する規約にして、gate 側で状態 dir (`next/` `pending/` `done/`) と group の `next/` 配下は
  親を起点に解決する。書き換え不要だが、gate が done を除外している現状と整合させる方針を先に決める
- C: claim の規律 (`claim-issue-in-next-and-push.md`) に「移動後に `check-doc-links` を回してリンクを直す」を足す (運用で吸収。`n` 経由は hook で
  拾えないので規律のみ)

A (glogx) + C (規律) で Claude / 人の両経路を覆う。B は gate の解決規則を変えるので obaket 側で別判断。

## 関連

- obaket `issues/721-retro-634-635-codex-drive-2026-09-05.md` 反省 7
- `_claude/rules/claim-issue-in-next-and-push.md` / `src/glogx/issues/move.go`

## 決着 (2026-09-05): 案 A/B/C のどれでもなく「symlink の目印」で解決

ユーザー提案で **issue ファイルを動かさず `next/<base> -> ../<base>` の symlink を目印にする**運用へ変えた
(commit 769cb4b0 〜)。リンクの書き換え (A) も gate の解決規則の変更 (B) も要らず、規律 (C) は
「目印を消してから done へ」の 1 行だけになった。

- glogx: 走査は symlink を弾く既存の安全判断 (`isIssueFile`) を保ったまま、`src/glogx/issues/nextlink.go`
  に閉じた例外を置く。採用条件は next/ 直下 (next/ 自体は symlink 不可) / Readlink がちょうど `../<同名>` /
  指す先が直下の通常ファイル / 直下エントリ名と完全一致 / meta ファイルでない。不採用は警告。
  claim (`n`) は symlink 作成、解除は Remove 直前に再検査してから削除。done/pending へ運ぶ経路では
  目印を先に消す (dangling → 再 open で偽 claim が復活する形を構造で塞ぐ)
- hook `next-claim-push.sh` は `ln` の宛先も見る。規律 `claim-issue-in-next-and-push.md` の claim コマンドは
  `ln -s ../NNN.md issues/next/NNN.md`
- CI: `tests/issues/test_next_links_valid.sh` が issues/ 配下の全 symlink を検査 (glogx より厳しい向き)
- 旧運用 (実ファイルが next/ に居る) は読めるまま。dotfiles / obaket の既存 claim は完了まで持ってよい

敵対的レビュー (opus) を 4 周: 1 周目 5 件 (解除が実ファイルを消す P1 / global next/ が watch 外 / next/ 自体の
symlink / 大文字小文字の無言破棄 / dangling の復活)、2 周目 4 件 (meta の偽警告 / bash と Go の照合の割れ /
done/next/ を bash が通す / done 移動時の目印)、3 周目 4 件 (末尾スラッシュ / glob 文字 / 差分の明記 / 文言)、
4 周目 3 件 (epic/next の大文字小文字 / 二重スラッシュ / ヘッダ)。全件採用し、それぞれ再現テストか実測を残した。
**5 周目は回していない** (4 周目の修正は bash の 3 行と comment で、直接の実測で確認した)。

obaket 側 (案 B の gate) は変更不要: 目印は symlink なので本文の相対リンクは切れない。obaket の既存 claim
(next/ の実ファイル) を symlink へ張り直す必要も無い。
