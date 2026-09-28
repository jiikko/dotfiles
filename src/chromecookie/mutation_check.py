#!/usr/bin/env python3
"""安全装置の変異検証（mutation check）。

chromecookie の「安全装置」— 資格情報のコピーを残さない後始末（3 段構え）/ 復号の検証 /
エラーの分類 — は、テストが**実際に効いている**ことまで確かめないと「在るだけ」になる。
（slack-cli の scripts/mutation_check.py から、ここへ移したコードの分を切り出したもの）green は「正しい」ではなく「その書き方では壊せなかった」。

このスクリプトは、各安全装置を壊す変異を 1 つずつ当てて、対象テストが red になることを
確かめる。安全装置やそのテストを触ったら実行すること。

    python3 mutation_check.py          # 全件
    python3 mutation_check.py --list   # 変異の一覧だけ表示

判定は 4 値で出す（2 値に丸めない）:
  red         期待どおりテストが落ちた = そのテストは効いている
  GREEN       変異したのにテストが緑 = **テストが何も守っていない**（要修正）
  build-error 変異でコンパイルが通らなかった = 判定不能（変異の書き方を直す）
  not-applied 置換対象が見つからない / 1 箇所でない = 判定不能（実装が動いた）

手順として次を必ず通す（どれを飛ばしても誤診する）:
  1. 変異前後でファイルが実際に変わったことを確認する（当たっていない緑を防ぐ）
  2. go build が通ることを確認する（ビルド不能の緑を「検知できなかった」と誤読しない）
  3. 対象テストを名指しで実行し、「--- FAIL: <テスト名>」の有無で判定する
     （rc だけ・件数だけでは「1 本も走らなかった」と区別できない）
  4. 作業ツリーは触らず、リポジトリのコピーに対して変異を当てる

🚨 **red は「その変異でそのテストが落ちた」以上のことを意味しない。**
どの assert が落ちたかまでは見ていないので、「seam を消す正当な整理」のような
挙動中立の変更も red になりうる。red を「そのガードが効いている証拠」として読むなら、
少なくとも 1 度は `-v` で**意図した assert が落ちているか**を目視すること。

## 意図的に載せていない変異（等価変異と判定したもの）

- `openVerifiedChild` の `!ok`（型アサーション失敗）分岐を fail-open にする変異 — **等価変異**。
  実測（darwin / os パッケージ由来の FileInfo）ではこの分岐に到達しない（動的型は常に
  `*syscall.Stat_t`）。テストでは守れないので、コード側にその旨を書いてある。

- `RunAllCleanups` の `filepath.Dir(p) != root` ガードの削除 — **等価変異**。
  到達経路を数え直した結果、削除は `removeVerified` が検証済み root からの相対名で行うため、
  このガードを外しても作業領域の外は消えない（実測でも緑）。単独で担っているのは警告の出力だけ。
  コード側にその旨をコメントしてある。

## 過去にこのスクリプトが見つけたテストの弱さ（再発防止のため記録）

- `TestCookieHostMatches` の fixture が弱く、ドット境界を無視した **素の suffix 比較**でも
  全ケース通った → `slack.com` / `ack.com` / `lpha.slack.com` を追加
"""
import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile

REPO = os.path.dirname(os.path.abspath(__file__))
MUTATIONS = [
    # (名前, ファイル, 置換前, 置換後, テスト対象パッケージ, 期待して red になるテスト名)
    ('v11 を平文として素通しする', 'cookie.go', '\tcase "v10", "v11":', '\tcase "v10":', './', 'TestDecryptValueHandlesV10AndV11'),
    ('meta>=24 のハッシュ 32 バイト除去をやめる', 'cookie.go', '\tif metaVersion >= 24 {', '\tif false {', './', 'TestDecryptValueStripsHashPrefixByMetaVersion'),
    ('PKCS7 を最終バイトだけで判定する', 'cookie.go', '\tfor _, b := range data[len(data)-pad:] {\n\t\tif int(b) != pad {\n\t\t\treturn nil, errors.New("PKCS7: パディングバイトが揃っていません")\n\t\t}\n\t}', '', './', 'TestPKCS7UnpadValidatesWholePadding'),
    ('Cookie ドメイン判定を素の suffix 比較にする', 'cookie.go', '\tif strings.HasPrefix(hostKey, ".") {\n\t\td := hostKey[1:] // domain cookie\n\t\treturn reqHost == d || strings.HasSuffix(reqHost, "."+d)\n\t}\n\treturn hostKey == reqHost // host-only cookie は完全一致のみ', '\treturn strings.HasSuffix(reqHost, strings.TrimPrefix(hostKey, "."))', './', 'TestCookieHostMatches'),
    ('一時ディレクトリをシグナル経路に登録しない', 'workspace.go', '\tw.paths[d] = struct{}{}\n', '', './', 'TestTempDirIsRemovedByBothPaths'),
    ('作業領域の名前を app によらず固定する（他ツールと共有する）', 'workspace.go', '\treturn &Workspace{app: app, paths: map[string]struct{}{}}', '\treturn &Workspace{app: "shared-tool", paths: map[string]struct{}{}}', './', 'TestTempRootIsToolSpecific'),
    ('SIGQUIT(Ctrl-\\) を捕まえるのをやめる', 'workspace.go', 'var cleanupSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}', 'var cleanupSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}', './', 'TestSignalCleanupRemovesTempDirs'),
    ('③の掃除が検証を通らずに root を開く（symlink 先を消す形に戻す）', 'workspace.go', '\tr, err := w.openVerifiedTempRootWith(uid, afterLstat)', '\t_ = uid\n\t_ = afterLstat\n\tr, err := os.OpenRoot(w.tempRoot())', './', 'TestSweepRefusesUnverifiedRoot'),
    ('pid 名の往復一致を外す（掃除の母集合を広げる）', 'workspace.go', '\tif strconv.Itoa(pid) != head {\n\t\treturn 0, false\n\t}', '', './', 'TestPidFromTempDirName'),
    ('掃除が生きているプロセスのものまで消す', 'workspace.go', '\t\tif processAlive(pid) && !isStale(r, e.Name()) {\n\t\t\tcontinue // 生きているプロセスの、新しいものは触らない（並行実行）\n\t\t}', '', './', 'TestSweepRemovesOnlyDeadOwnDirs'),
    ('作業領域の symlink 検査と同一性検査を両方外す', 'workspace.go', '\tif want.Mode()&os.ModeSymlink != 0 {\n\t\treturn nil, fmt.Errorf("%s がシンボリックリンクです（削除してください）: %s", name, display)\n\t}\n\tif afterLstat != nil {\n\t\tafterLstat(name) // テストが「検証中の差し替え」を再現するための窓\n\t}\n\tchild, err := parent.OpenRoot(name)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tgot, err := child.Stat(".")\n\tif err != nil {\n\t\t_ = child.Close()\n\t\treturn nil, err\n\t}\n\tif !os.SameFile(want, got) {\n\t\t_ = child.Close()\n\t\treturn nil, fmt.Errorf("%s が検証中に差し替えられました: %s", name, display)\n\t}\n\t// 🚨 実測（darwin / os パッケージ由来の FileInfo）ではこの分岐に到達しない\n\t// （動的型は常に *syscall.Stat_t）。つまりテストでは守れない。それでも\n\t// 「判定不能なら拒否」を置くのは、別 platform・別実装で型が変わったときに\n\t// 黙って素通りさせないため。テストが無いことを承知で残している。\n', '\t_ = want\n\n\tif afterLstat != nil {\n\t\tafterLstat(name)\n\t}\n\tchild, err := parent.OpenRoot(name)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tgot, err := child.Stat(".")\n\tif err != nil {\n\t\t_ = child.Close()\n\t\treturn nil, err\n\t}\n', './', 'TestSweepRefusesRelativeSymlinkRoot'),
    ('①の後始末をパス文字列の os.RemoveAll に戻す', 'workspace.go', '\treturn d, func() { _, _ = w.removeVerified(filepath.Base(d)) }, nil', '\treturn d, func() { os.RemoveAll(d) }, nil', './', 'TestDeferCleanupRefusesUnverifiedRoot'),
    ('親ディレクトリの検証を省いて末尾だけ検証する', 'workspace.go', '\tr, err := os.OpenRoot(base)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\t// 🚨 display はホップごとに進める。進めないと 2 ホップ目のエラーが\n\t// 実在しないパスを表示し、差し替えを報告する画面が嘘をつく。\n\tpath := base\n\tfor _, name := range []string{w.app, tempRootName} {\n\t\tpath = filepath.Join(path, name)\n\t\tchild, err := openVerifiedChild(r, name, path, uid, afterLstat)\n\t\t_ = r.Close() // 子は自前の fd を持つので、親は閉じてよい\n\t\tif err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tr = child\n\t}\n', '\tr, err := os.OpenRoot(filepath.Join(base, w.app))\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tchild, err := openVerifiedChild(r, tempRootName, w.tempRoot(), uid, afterLstat)\n\t_ = r.Close()\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tr = child\n', './', 'TestParentComponentIsVerified'),
    ('古い残骸の回収をやめる（pid 再利用で永久に残る形へ戻す）', 'workspace.go', '\treturn time.Since(info.ModTime()) > staleAge', '\t_ = info\n\treturn false', './', 'TestSweepRemovesStaleEntriesEvenIfPidAlive'),
    ('作業領域のパーミッションを 0777 にする', 'workspace.go', '\tif err := os.MkdirAll(root, 0o700); err != nil {', '\tif err := os.MkdirAll(root, 0o777); err != nil {', './', 'TestTempRootPermissions'),
    ('相対パスの HOME を受け入れる（cwd 依存になる）', 'workspace.go', '\tif !filepath.IsAbs(dir) {\n\t\treturn "", fmt.Errorf("キャッシュディレクトリが絶対パスではありません（HOME を確認してください）: %s", dir)\n\t}', '', './', 'TestRelativeHomeIsRejected'),
    ('シグナルを受けても終了しない（後始末だけして走り続ける）', 'workspace.go', '\tif s, ok := sig.(syscall.Signal); ok {\n\t\texit(128 + int(s)) // シェルの慣習（SIGINT=130 / SIGTERM=143）\n\t\treturn\n\t}\n\texit(1)', '\t_ = sig\n\t_ = exit', './', 'TestSignalCleanupRemovesTempDirs'),
    ('作業領域を $TMPDIR 基準に戻す', 'workspace.go', '\tdir, err := os.UserCacheDir()\n\tif err != nil {\n\t\treturn "", fmt.Errorf("キャッシュディレクトリを決められません: %w", err)\n\t}\n\t// 🚨 相対パスを弾く。HOME が相対だと作業領域が cwd 依存になり、\n\t// 「カレントディレクトリに一切依存しない」という前提が崩れる（実測: HOME="." で再現）。\n\tif !filepath.IsAbs(dir) {\n\t\treturn "", fmt.Errorf("キャッシュディレクトリが絶対パスではありません（HOME を確認してください）: %s", dir)\n\t}\n\treturn filepath.Join(dir, w.app), nil', '\treturn filepath.Join(os.TempDir(), w.app), nil', './', 'TestTempRootIsToolSpecific'),
    ('所有者(uid)の確認を外す', 'workspace.go', '\tif int(st.Uid) != uid {', '\tif false && int(st.Uid) != uid {', './', 'TestForeignOwnerIsRejected'),
    ('検証中の差し替え検出(SameFile)を外す', 'workspace.go', '\tif !os.SameFile(want, got) {', '\tif false && !os.SameFile(want, got) {', './', 'TestSwapDuringVerificationIsDetected'),
    ('シグナル受信後に後始末してから既定へ戻す（順序を逆にする）', 'workspace.go', '\treset()\n\tcleanup()', '\tcleanup()\n\treset()', './', 'TestSignalHandlerResetsBeforeCleanup'),
    ('シグナル受信後に既定へ戻さない', 'workspace.go', '\treset()\n\tcleanup()', '\t_ = reset\n\tcleanup()', './', 'TestSignalHandlerResetsBeforeCleanup'),
    ('シグナルハンドラに no-op の reset を渡す', 'workspace.go', '\t\t\tresetFunc(func() { signal.Reset(cleanupSignals...) }),', '\t\t\tresetFunc(func() {}),', './', 'TestSignalHandlerPassesRealReset'),
    ('②で「終了中」を立てない', 'workspace.go', '\tw.closing = true\n', '', './', 'TestSignalShutdownRefusesNewTempDirs'),
    ('終了中でも newTempDir が作る', 'workspace.go', '\tif w.closing {\n\t\tw.mu.Unlock()\n\t\treturn "", nil, w.tempRootError(errCleanupClosing)\n\t}', '', './', 'TestSignalShutdownRefusesNewTempDirs'),
    ('作成と登録の間でロックを外す', 'workspace.go', '\tif afterMkdir != nil {\n\t\tafterMkdir()\n\t}\n\tw.paths[d] = struct{}{}\n\tw.mu.Unlock()', '\tw.mu.Unlock()\n\tif afterMkdir != nil {\n\t\tafterMkdir()\n\t}\n\tw.mu.Lock()\n\tw.paths[d] = struct{}{}\n\tw.mu.Unlock()', './', 'TestTempDirCreationAndRegistrationAreAtomic'),
    ('シグナルハンドラへ RunAllCleanups を渡す（終了中を立てない）', 'workspace.go', '\t\t\tcleanupFunc(w.shutdownCleanups),', '\t\t\tcleanupFunc(w.RunAllCleanups),', './', 'TestSignalHandlerPassesShutdownCleanups'),
    ('削除の前に登録簿から消す（失敗したものを再試行しない）', 'workspace.go', '\t\tbyName[filepath.Base(p)] = p\n', '\t\tbyName[filepath.Base(p)] = p\n\t\tdelete(w.paths, p)\n', './', 'TestFailedCleanupIsRetried'),
    ('削除に失敗した名前も removed に入れる', 'workspace.go', '\t\t\t\tfirstErr = err\n\t\t\t}\n\t\t\tcontinue\n\t\t}\n\t\tremoved = append(removed, name)', '\t\t\t\tfirstErr = err\n\t\t\t}\n\t\t}\n\t\tremoved = append(removed, name)', './', 'TestRemoveVerifiedReportsOnlySuccesses'),
    ('Keychain の失敗を EnvError 以外で返す', 'keychain_darwin.go', '\t\treturn nil, &EnvError{', '\t\treturn nil, &ReadError{Kind: ReadDenied, ', './', 'TestKeychainFailureIsEnvError'),
    ('アクセス拒否を EnvError にする（探索全体が止まる形へ戻す）', 'errors.go', '\treturn &ReadError{Kind: ReadDenied, Msg:', '\treturn &EnvError{Msg:', './', 'TestPermissionDeniedIsReadDeniedNotMissing'),
    ('Cookie DB の Stat の ENOENT 以外（アクセス拒否）を「見つからない」にする', 'cookie.go', '\t\tif !errors.Is(err, fs.ErrNotExist) {\n\t\t\treturn "", readFailure("Cookie DB ", c, err)\n\t\t}\n', '\t\tif false && !errors.Is(err, fs.ErrNotExist) {\n\t\t\treturn "", readFailure("Cookie DB ", c, err)\n\t\t}\n', './', 'TestPermissionDeniedIsReadDeniedNotMissing'),
    ('Cookie DB コピー時のアクセス拒否を素のエラーに戻す', 'cookie.go', '\t\t\t\treturn "", nil, nil, readFailure("Cookie DB ", src, err)', '\t\t\t\treturn "", nil, nil, fmt.Errorf("Cookie DB を読み取れませんでした（アクセス拒否）: %s", src)', './', 'TestCopyPermissionDeniedIsReadDenied'),
    ('-wal / -shm の読み取り失敗を黙って捨てる', 'cookie.go', '\t\t\tskipped.Add(err)\n\t\t\tcontinue\n', '\t\t\tcontinue\n', './', 'TestUnreadableWALIsRecorded'),
    ('存在しない -shm まで記録する（ENOENT を除かない）', 'errors.go', '\tif err == nil || errors.Is(err, fs.ErrNotExist) {', '\tif err == nil || (false && errors.Is(err, fs.ErrNotExist)) {', './', 'TestUnreadableWALIsRecorded'),
    ('復号の全件失敗を検出しない（cookie が無いに化ける）', 'cookie.go', 'func (d decryptStats) allFailed() bool { return d.tried > 0 && d.failed == d.tried }', 'func (d decryptStats) allFailed() bool { return false }', './', 'TestDecryptFailuresAreReported'),
    ('1 件の復号失敗で全件失敗扱いにする', 'cookie.go', 'func (d decryptStats) allFailed() bool { return d.tried > 0 && d.failed == d.tried }', 'func (d decryptStats) allFailed() bool { return d.failed > 0 }', './', 'TestDecryptFailuresAreReported'),
    ('目的の Cookie が無いとき、読めなかったファイル（-wal / -shm）を添えない', 'cookie.go', '\treturn r.skipped.AsError(what)\n', '\treturn nil\n', './', 'TestDecryptFailuresAreReported'),
    ('v24 の SHA256(host_key) 照合を外す（鍵違いのゴミが 1/256 で通る）', 'cookie.go', '\t\tif subtle.ConstantTimeCompare(plain[:sha256.Size], want[:]) != 1 {', '\t\tif false && subtle.ConstantTimeCompare(plain[:sha256.Size], want[:]) != 1 {', './', 'TestV24HashPrefixDetectsWrongKeyAtScale'),
    ('照合の入力の host_key から先頭ドットを落とす（正常な cookie が全部失敗する）', 'cookie.go', '\t\twant := sha256.Sum256([]byte(hostKey))', '\t\twant := sha256.Sum256([]byte(strings.TrimPrefix(hostKey, ".")))', './', 'TestV24HashPrefixDetectsWrongKeyAtScale'),
    ('作業領域の異常を素のエラーで返す（setup が「ログインしてから」に化ける）', 'workspace.go', '\t\treturn "", nil, w.tempRootError(err)\n\t}\n\tw.mu.Lock()', '\t\treturn "", nil, err\n\t}\n\tw.mu.Lock()', './', 'TestTempRootFailureIsEnvError'),
    ('終了処理中を素のエラーで返す', 'workspace.go', '\t\treturn "", nil, w.tempRootError(errCleanupClosing)', '\t\treturn "", nil, errCleanupClosing', './', 'TestTempRootFailureIsEnvError'),
    ('壊れた DB を素のエラーで返す（自動検出が未知のエラーとして止まる）', 'cookie.go', '\t\treturn Result{}, &ReadError{Kind: ReadBroken, Msg: "Cookie DB を読めません", Err: err}', '\t\treturn Result{}, err', './', 'TestReadCookiesClassification'),
    ('names の絞り込みを外す（露出面が全 Cookie に広がる）', 'cookie.go', '\tif len(names) > 0 {', '\tif false && len(names) > 0 {', './', 'TestReadCookiesClassification'),
    ('既存の作業領域の group / other 権限を見逃す', 'workspace.go', '\tif got.Mode().Perm()&0o077 != 0 {', '\tif false && got.Mode().Perm()&0o077 != 0 {', './', 'TestPermissiveExistingRootIsRejected'),
]

# COMPILE_GUARDED は「型で閉じている」ことを確かめる変異。
#
# 🚨 こちらは red ではなく **build-error が期待値**。テストで守るのではなく
# コンパイラで守っている性質なので、判定の向きが逆になる。
# （名前: ファイル, 置換前, 置換後）
COMPILE_GUARDED = [
    ('シグナルハンドラの reset と cleanup を入れ替える', 'workspace.go', '\t\t\tresetFunc(func() { signal.Reset(cleanupSignals...) }),\n\t\t\tcleanupFunc(w.shutdownCleanups),', '\t\t\tcleanupFunc(w.shutdownCleanups),\n\t\t\tresetFunc(func() { signal.Reset(cleanupSignals...) }),'),
]


def run(cmd, cwd):
    p = subprocess.run(cmd, cwd=cwd, shell=True, capture_output=True, text=True)
    return p.returncode, p.stdout, p.stderr


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--list", action="store_true", help="変異の一覧だけ表示する")
    ap.add_argument("--json", action="store_true", help="結果を JSON で出す")
    args = ap.parse_args()

    if args.list:
        for i, m in enumerate(MUTATIONS, 1):
            print(f"{i:2d}. {m[0]}  ({m[1]} -> {m[5]})")
        return 0

    # 🚨 作業ツリーには当てない。コピーへ当てる（変異が残ったまま commit される事故を防ぐ）。
    work = tempfile.mkdtemp(prefix="chromecookie-mutation-")
    root = os.path.join(work, "repo")
    try:
        def ignore(dirpath, names):
            if os.path.abspath(dirpath) != REPO:
                return set()
            return {n for n in names if n in {".git", "tmp"}}

        shutil.copytree(REPO, root, ignore=ignore)

        rc, out, err = run("go test -count=1 ./...", root)
        if rc != 0:
            print("baseline が緑ではない。先にテストを通すこと:\n" + (out or err), file=sys.stderr)
            return 2

        results = []
        for name, path, old, new, pkg, testname in MUTATIONS:
            full = os.path.join(root, path)
            src = open(full, encoding="utf-8").read()
            if src.count(old) != 1:
                results.append((name, "not-applied", f"置換対象が {src.count(old)} 箇所（1 箇所であるべき）"))
                continue
            open(full, "w", encoding="utf-8").write(src.replace(old, new, 1))
            try:
                if open(full, encoding="utf-8").read() == src:
                    results.append((name, "not-applied", "ファイルが変わっていない"))
                    continue
                rc, out, err = run("go build ./...", root)
                if rc != 0:
                    results.append((name, "build-error", (err or out).strip().splitlines()[0][:160]))
                    continue
                rc, out, err = run(f"go test -count=1 -run '^{testname}$' {pkg}", root)
                combined = out + err
                if f"--- FAIL: {testname}" in combined:
                    verdict, detail = "red", ""
                elif "no tests to run" in combined:
                    verdict, detail = "not-run", f"{testname} が実行されていない"
                elif rc == 0:
                    verdict, detail = "GREEN", "変異したのにテストが緑（テストが守っていない）"
                else:
                    verdict, detail = "red(別要因の可能性)", combined.strip().splitlines()[-1][:160]
                results.append((name, verdict, detail))
            finally:
                open(full, "w", encoding="utf-8").write(src)
        # 型で閉じている性質: 変異がコンパイルエラーになることを確かめる
        for name, path, old, new in COMPILE_GUARDED:
            full = os.path.join(root, path)
            src = open(full, encoding="utf-8").read()
            if src.count(old) != 1:
                results.append((name, "not-applied", f"置換対象が {src.count(old)} 箇所（1 箇所であるべき）"))
                continue
            open(full, "w", encoding="utf-8").write(src.replace(old, new, 1))
            try:
                rc, out, err = run("go build ./...", root)
                if rc != 0:
                    results.append((name, "red", "コンパイルエラー（型で閉じている）"))
                else:
                    results.append((name, "GREEN", "コンパイルが通ってしまう（型で閉じていない）"))
            finally:
                open(full, "w", encoding="utf-8").write(src)
    finally:
        shutil.rmtree(work, ignore_errors=True)

    bad = [r for r in results if r[1] != "red"]
    if args.json:
        print(json.dumps([{"変異": n, "判定": v, "備考": d} for n, v, d in results], ensure_ascii=False, indent=1))
    else:
        for n, v, d in results:
            mark = "OK  " if v == "red" else "NG  "
            print(f"{mark}{v:<24} {n}" + (f"  -- {d}" if d else ""))
    print(f"\n変異 {len(results)} 件 / red {len(results) - len(bad)} 件 / 要確認 {len(bad)} 件")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
