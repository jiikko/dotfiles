# 853 (bug): tmpclean が SIP で守られた $TMPDIR の dir を毎回候補に挙げ、apply が移せずに終了コード 1 になる

起票日: 2026-10-21

## 概要

`$TMPDIR` の直下にある macOS のサービスの dir 6 つ (`AudioConverterService`・`CrashHandlerService`・`ReportCrashService`・`SandboxHelper`・`ptpcamerad`・`talagent`) を、
tmpclean の plan が候補に挙げる。apply はどれも `operation not permitted` で移せず、毎回の終了コードが 1 (移せなかったものがある) になる。
2026-10-21 の cleanup-tmp-dir で 1,161 件のうち 1,155 件を移し、この 6 件だけが失敗した。

## 原因

- 6 つとも `ls -lO@` で `sunlnk` フラグ (SF_NOUNLINK) と `com.apple.rootless` の xattr が付いている (SIP が守る置き場)。持ち主は利用者 (koji / staff・`drwx------`) なので、持ち主では見分けられない
- `candidate.go` の tmpdir の判定は「システム」を名前の頭 `com.apple.` だけで見ていた。このサービスの dir は名前が `com.apple.` で始まらない
- 中が空で mtime も古い (10/11) ので、古さの判定でも残らない

## 対応

名前の一覧に足さず、属性で見る。`Lstat` の `st_flags` に rename を拒むフラグ (UF_IMMUTABLE・UF_APPEND・SF_IMMUTABLE・SF_APPEND・SF_RESTRICTED・SF_NOUNLINK) が
付いていれば、「移せない属性 (immutable / SIP)」として残す (`flags_darwin.go` の `unmovable`)。tmpclean は macOS だけで動く (依存先の `safefs` が darwin 専用。ユーザー決定 2026-10-21: macOS のみ対応でよい)。

session の種類 (`/private/tmp/claude-<uid>/…`) には足していない。Claude Code の scratchpad に属性付きの dir が置かれる経路は観測していない。

## 進捗

- [x] 原因の特定 (2026-10-21)
- [x] 直す — commit「tmpclean: rename を拒む属性 (SIP など) が付いた $TMPDIR の dir を候補にしない」
- [x] 回帰テスト
  - `TestTmpdirKeepsUnmovable`: 実物の dir に、利用者が付けられる UF_IMMUTABLE を付けて plan の経路を通す (SF_* は root でしか付けられない)
  - `TestUnmovableEachFlag` (`flags_darwin_test.go`): st_flags を偽の FileInfo で渡し、マスクの 6 bit を 1 つずつ確かめる (反証レビューの P3:
    UF_IMMUTABLE だけでは、今回の不具合の bit SF_NOUNLINK をマスクから外しても緑だった)
  - 変異: 判定の分岐を `false &&` で殺す → red / マスクから UF_IMMUTABLE を外す → red / マスクから SF_NOUNLINK を外す → `TestUnmovableEachFlag` が red
  - 本物の `$TMPDIR` でも比べた: 修正前の版 (インストール済みの `tmpclean`) の plan は候補 6 件 (上の 6 つ)、
    修正後は候補 0 件・「移せない属性」で残したもの 24 件 (前の 6 件と、別の理由で残していた同じ種類の dir。判定の順で、この理由が先に当たる)
- [x] 入口の文書: `src/tmpclean/README.md` の守りの表に足した
- 反証レビュー (read-only 1 体): P3 1 件 (上のテストで対応)。反証できなかった主張: 6 つの dir の属性・持ち主・件数 24、定数が SDK の `sys/stat.h` と一致、
  判定の順で候補が増える経路が無いこと、中身にだけ属性がある同型の残りが今の `$TMPDIR` に 0 件 (今の観測で、将来の保証ではない)
