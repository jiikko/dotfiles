// Package chromecookie は macOS の Google Chrome のプロファイルから Cookie を復号して取り出す。
//
// slack-cli / esa-cli / newrelic-nrql-cli が共有する（以前は 3 repo にコピーがあり、
// 修正が片方にしか当たらずに分岐していた）。どの Cookie を使うか・案内に出すフラグ名は
// 呼び出し側が持つ。
//
// 🚨 ここで扱う値はいずれも「なりすましログインが可能な資格情報」。
// ファイル・ログ・標準出力へ生値を出さないこと。
// 一時コピーの後始末は workspace.go の 3 段構えに必ず載せる（Workspace.NewTempDir を通す）。
//
// エラーの分類（呼び出し側が複数のプロファイルを順に試すときの扱い。errors.go）:
//   - *EnvError: 即停止。Keychain の鍵を取れない / 作業領域の異常 / macOS 以外。どのプロファイルでも同じ
//   - IsMissing: 黙って skip。Cookie DB が無い（ENOENT）
//   - *ReadError: 記録して skip し、全滅したら IssueNote で理由を添える
//     （アクセス拒否・壊れた DB・全件復号失敗・-wal/-shm の読み取り失敗）
//   - それ以外: 未知。止める側に倒す（分類は許可リスト）
package chromecookie

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// 🚨 Google Chrome 専用。
//
// 3 ツール共通の方針（esa-cli の issue 003）。対応表の値（Keychain のサービス名・Application
// Support 配下のディレクトリ名）は実機で確認しないと正しいか分からないため、
// 手元で確認できる Chrome だけに絞る。未確認の値を並べると「動くように見えて
// 別ブラウザの領域を読みに行く」形の事故になる。
const (
	chromeKeychainAccount = "Chrome"              // security -a
	chromeKeychainService = "Chrome Safe Storage" // security -s
	chromeSupportSubdir   = "Google/Chrome"       // ~/Library/Application Support 配下
	ChromeName            = "Google Chrome"       // エラーメッセージ用の表示名
)

// Cookie は復号済みの 1 Cookie。
type Cookie struct {
	Host  string // host_key（先頭ドットを含む場合がある）
	Name  string
	Value string
}

// deriveKey は PBKDF2-SHA1（macOS: 1003 回, 16 バイト）で AES-128 鍵を導出する。
func deriveKey(password []byte) ([]byte, error) {
	return pbkdf2.Key(sha1.New, string(password), []byte("saltysalt"), 1003, 16)
}

// decryptValue は encrypted_value を復号する。
// v10 / v11 プレフィックスなら AES-128-CBC（IV=0x20*16, PKCS7）で復号し、
// metaVersion>=24 なら復号後の先頭 32 バイトが SHA256(hostKey) であることを照合してから落とす。
//
// 🚨 v11 を「プレフィックス無し = 平文」として素通しさせないこと。素通しすると
// 復号されないバイト列がそのまま Cookie ヘッダへ載り、原因の分からない 401 になる。
//
// 🚨 v24 以上の「先頭 32 バイト = SHA256(host_key)」の照合を省かないこと。鍵が違っても
// PKCS7 の末尾は約 1/256 の確率で偶然通り、v24 の値は長いので 32 バイト落としても何か残る。
// 照合しないと、鍵違いのゴミが「復号できた cookie」として通り、全件失敗の検出
// （decryptStats.allFailed）が実際の件数では働かない（nrql の同じコードでレビューが再現）。
// 根拠: Chromium の sqlite_persistent_cookie_store（DB バージョン 24 の domain hash prefix。
// 暗号化前の値の先頭に host_key の SHA256 を付け、復号時に一致しなければ失敗として扱う）。
// 🚨 照合の入力（host_key の表記）を取り違えると**正常な cookie が全部**復号失敗になる。
// host_key は DB の列の値をそのまま使う（先頭ドットを落とさない・小文字化しない）。
// 2026-09-28 に meta v24 の実 Chrome で、正常な Cookie が照合を通り認証できることを確認した。
func decryptValue(enc, key []byte, metaVersion int, hostKey string) (string, error) {
	if len(enc) == 0 {
		return "", nil
	}
	if len(enc) < 3 {
		return string(enc), nil
	}
	switch string(enc[:3]) {
	case "v10", "v11":
		// 復号へ進む
	default:
		// 古い Chrome の平文データ
		return string(enc), nil
	}
	ciphertext := enc[3:]
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", errors.New("暗号文の長さが不正です")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	iv := []byte("                ") // 0x20 * 16
	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(ciphertext))
	mode.CryptBlocks(plain, ciphertext)

	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return "", err
	}
	if metaVersion >= 24 {
		if len(plain) < sha256.Size {
			return "", errors.New("復号結果がハッシュプレフィックスより短いです")
		}
		want := sha256.Sum256([]byte(hostKey))
		if subtle.ConstantTimeCompare(plain[:sha256.Size], want[:]) != 1 {
			return "", errors.New("復号結果の先頭がホスト名のハッシュと一致しません（鍵が違う可能性）")
		}
		plain = plain[sha256.Size:]
	}
	return string(plain), nil
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("PKCS7: データ長が不正です")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > blockSize || pad > len(data) {
		return nil, errors.New("PKCS7: パディングが不正です")
	}
	// 🚨 最終バイトだけでなく、パディング全体が同じ値であることを確かめる。
	// 最終バイトしか見ない実装は 1,2,3,4 のような不正なパディングを通し、
	// 復号結果の末尾にゴミが残った Cookie 値をそのまま送ることになる。
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, errors.New("PKCS7: パディングバイトが揃っていません")
		}
	}
	return data[:len(data)-pad], nil
}

// SupportDir は ~/Library/Application Support/Google/Chrome を返す（cwd 非依存: HOME 起点）。
func SupportDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", chromeSupportSubdir), nil
}

// ProfileDir は ~/Library/Application Support/Google/Chrome/<profile> を返す。
func ProfileDir(profile string) (string, error) {
	base, err := SupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, profile), nil
}

// CookieDBPath は Cookie DB の絶対パスを返す。
//
// 無ければ IsMissing が真になるエラー（黙って skip してよい）、stat が ENOENT 以外で
// 失敗したら *ReadError（記録して skip）。
func CookieDBPath(profile string) (string, error) {
	base, err := ProfileDir(profile)
	if err != nil {
		return "", err
	}
	// 新しい Chrome は Cookies を Network/ サブディレクトリに置く。両方を候補にする。
	candidates := []string{
		filepath.Join(base, "Network", "Cookies"),
		filepath.Join(base, "Cookies"),
	}
	for _, c := range candidates {
		_, err := os.Stat(c)
		if err == nil {
			return c, nil
		}
		// 🚨 「無い」(ENOENT) だけを次の候補へ進める。それ以外（権限・I/O）は「見つからない」に
		// 化けさせない（案内が「プロファイル名を確認」になる）。かつ EnvError にもしない:
		// chmod 000 や root 所有（sudo で起動した Chrome が作ったもの）はプロファイル固有で、
		// 止めると後ろの正常なプロファイルが使えなくなる。
		if !errors.Is(err, fs.ErrNotExist) {
			return "", readFailure("Cookie DB ", c, err)
		}
	}
	return "", &MissingError{Profile: profile, Candidates: candidates}
}

// copyCookieDB は Cookie DB を一時ディレクトリへコピーする。
// WAL に未反映のセッション Cookie を取りこぼさないよう、-wal / -shm も同名でコピーする。
// 返り値: 一時 DB パスと後始末関数（① の defer で必ず呼ぶ）と、読めずに飛ばしたファイルの記録。
func (w *Workspace) copyCookieDB(src string) (string, func(), skippedReads, error) {
	tmpdir, cleanup, err := w.NewTempDir()
	if err != nil {
		return "", nil, nil, err
	}

	var skipped skippedReads
	for _, suffix := range []string{"", "-wal", "-shm"} {
		s := src + suffix
		data, err := os.ReadFile(s)
		if err != nil {
			if suffix == "" {
				cleanup()
				return "", nil, nil, readFailure("Cookie DB ", src, err)
			}
			// -wal / -shm は存在しないこともある（ENOENT は記録しない）。
			// 🚨 それ以外の失敗は記録する。WAL にだけあるセッション cookie を取りこぼすと
			// 「ログインしていない」に化けるので、見つからなかったときの案内に添える。
			skipped.Add(err)
			continue
		}
		dst := filepath.Join(tmpdir, "Cookies"+suffix)
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			cleanup()
			return "", nil, nil, w.tempRootError(err)
		}
	}
	return filepath.Join(tmpdir, "Cookies"), cleanup, skipped, nil
}

// rawCookie は cookies テーブルの 1 行（復号前）。
type rawCookie struct {
	host, name, plain string
	enc               []byte
}

// decryptStats は復号の試行結果（全件失敗 = 鍵が合っていない、を判定するため）。
type decryptStats struct {
	tried, failed int
	firstErr      error
}

// allFailed は「1 件以上試して、すべて失敗した」かを返す。
func (d decryptStats) allFailed() bool { return d.tried > 0 && d.failed == d.tried }

// decryptCookies は復号前の行を復号する。1 件の失敗では止めない（取れたものだけ返す）。
func decryptCookies(rows []rawCookie, key []byte, metaVersion int) ([]Cookie, decryptStats) {
	var out []Cookie
	var st decryptStats
	for _, r := range rows {
		value := r.plain
		if value == "" && len(r.enc) > 0 {
			st.tried++
			v, derr := decryptValue(r.enc, key, metaVersion, r.host)
			if derr != nil {
				st.failed++
				if st.firstErr == nil {
					st.firstErr = derr
				}
				continue // 1 件の復号失敗で全体を止めない（全件失敗は Result.Diagnose が判定する）
			}
			value = v
		}
		out = append(out, Cookie{Host: r.host, Name: r.name, Value: value})
	}
	return out, st
}

// Result は ReadCookies の結果。
type Result struct {
	Cookies []Cookie

	decrypt decryptStats
	skipped skippedReads
}

// Diagnose は「目的の Cookie が見つからなかった」ときに呼び、原因の手がかりがあれば *ReadError を返す。
// 全件の復号失敗（鍵違い）→ DecryptFailed、-wal / -shm を読めなかった → ReadIncomplete。
// 手がかりが無ければ nil（= 本当に無い。ログインしていない等。黙って skip してよい）。
//
// 🚨 「ログインしているか確認して」は最後の選択肢。復号が全件失敗した / 読めなかったファイルが
// ある、のに「ログインして」と案内すると、原因にたどり着けない。
func (r Result) Diagnose(what string) error {
	if r.decrypt.allFailed() {
		return &ReadError{Kind: DecryptFailed, Msg: fmt.Sprintf(
			"暗号化された Cookie %d 件の復号にすべて失敗しました", r.decrypt.tried), Err: r.decrypt.firstErr}
	}
	return r.skipped.AsError(what)
}

// ReadCookies は指定プロファイルの Cookie DB を作業領域へコピーし、password（KeychainPassword の値）で
// 復号して返す。names を渡すとその名前の Cookie だけを読む（露出面を最小にする）。空なら全件。
//
// password を引数で受けるのは、呼び出し側が Keychain をテストで差し替えられるようにするため
// （実物の security は許可ダイアログを出す）。
//
// 戻り値のエラーの分類はパッケージの doc を参照。DB を開けない・cookies テーブルを読めない
// （壊れた DB・sqlite 以外のファイル・古いスキーマ）は、鍵を取れた後のこのプロファイル固有の
// 問題なので *ReadError（ReadBroken）にする。
func (w *Workspace) ReadCookies(profile string, password []byte, names ...string) (Result, error) {
	key, err := deriveKey(password)
	if err != nil {
		return Result{}, err
	}
	src, err := CookieDBPath(profile)
	if err != nil {
		return Result{}, err
	}
	dbPath, cleanup, skipped, err := w.copyCookieDB(src)
	if err != nil {
		return Result{}, err
	}
	defer cleanup() // ①: 正常終了・エラー・panic を覆う

	broken := func(err error) (Result, error) {
		return Result{}, &ReadError{Kind: ReadBroken, Msg: "Cookie DB を読めません", Err: err}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return broken(err)
	}
	defer func() { _ = db.Close() }()

	var metaVersion int
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'version'`).Scan(&metaVersion); err != nil {
		// meta が読めない場合は 0 扱い（トリム無し）で続行
		metaVersion = 0
	}

	query := `SELECT host_key, name, value, encrypted_value FROM cookies`
	var args []any
	if len(names) > 0 {
		query += ` WHERE name IN (?` + strings.Repeat(`, ?`, len(names)-1) + `)`
		for _, n := range names {
			args = append(args, n)
		}
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return broken(fmt.Errorf("cookies テーブルの読み取りに失敗: %w", err))
	}
	defer func() { _ = rows.Close() }()

	var raw []rawCookie
	for rows.Next() {
		var r rawCookie
		if err := rows.Scan(&r.host, &r.name, &r.plain, &r.enc); err != nil {
			return broken(err)
		}
		raw = append(raw, r)
	}
	if err := rows.Err(); err != nil {
		return broken(err)
	}
	out, st := decryptCookies(raw, key, metaVersion)
	return Result{Cookies: out, decrypt: st, skipped: skipped}, nil
}

// HostMatches は Cookie の host_key が対象ホストに送信されるべきか判定する。
// 標準の Cookie ドメインマッチ（ドット境界）を用い、suffix 文字列比較の誤爆を避ける。
//
// 🚨 SQL の LIKE '%slack.com' で絞らないこと。"notslack.com" / "evilslack.com" が
// 一致してしまい、無関係なサイトの Cookie を送る経路になる。
func HostMatches(hostKey, reqHost string) bool {
	if hostKey == "" {
		return false
	}
	if strings.HasPrefix(hostKey, ".") {
		d := hostKey[1:] // domain cookie
		return reqHost == d || strings.HasSuffix(reqHost, "."+d)
	}
	return hostKey == reqHost // host-only cookie は完全一致のみ
}
