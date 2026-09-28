package chromecookie

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"testing"
)

// encryptForTest は Chrome と同じ形（prefix + AES-128-CBC/IV=0x20*16/PKCS7）で暗号化する。
// hashPrefix が true なら meta.version>=24 と同じくホストハッシュ 32 バイトを前置する。
//
// hostKey が空でなければ、Chrome の cookie DB v24 以上と同じく先頭に SHA256(hostKey) を付ける。
func encryptForTest(t *testing.T, key []byte, prefix, plaintext, hostKey string) []byte {
	t.Helper()
	data := []byte(plaintext)
	if hostKey != "" {
		h := sha256.Sum256([]byte(hostKey))
		data = append(h[:], data...)
	}
	pad := aes.BlockSize - len(data)%aes.BlockSize
	data = append(data, bytes.Repeat([]byte{byte(pad)}, pad)...)

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, []byte("                ")).CryptBlocks(out, data)
	return append([]byte(prefix), out...)
}

func testKey(t *testing.T) []byte {
	t.Helper()
	// Chrome が Keychain を持たない環境で使う既定パスワード。既知ベクタとして使う。
	key, err := deriveKey([]byte("peanuts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 16 {
		t.Fatalf("AES-128 の鍵長は 16 バイト: got %d", len(key))
	}
	return key
}

// 鍵導出が仕様どおり（PBKDF2-HMAC-SHA1 / salt="saltysalt" / 1003 回 / 16 バイト）であること。
func TestDeriveKeyKnownVector(t *testing.T) {
	key := testKey(t)
	// 既知ベクタ。Go 実装とは独立に Python で算出して突き合わせた値:
	//   python3 -c "import hashlib;print(hashlib.pbkdf2_hmac('sha1',b'peanuts',b'saltysalt',1003,16).hex())"
	//   -> d9a09d499b4e1b7461f28e67972c6dbd
	// パラメータ（SHA1 / salt="saltysalt" / 1003 回 / 16 バイト）を 1 つでも変えるとここが落ちる。
	want := []byte{0xd9, 0xa0, 0x9d, 0x49, 0x9b, 0x4e, 0x1b, 0x74, 0x61, 0xf2, 0x8e, 0x67, 0x97, 0x2c, 0x6d, 0xbd}
	if !bytes.Equal(key, want) {
		t.Errorf("鍵導出が変わった: got % x, want % x", key, want)
	}
}

// v10 / v11 の両方を復号すること。
//
// 🚨 v11 を「プレフィックス無し = 平文」として素通しすると、復号されないバイト列が
// そのまま Cookie ヘッダへ載り、原因の分からない 401 になる。
func TestDecryptValueHandlesV10AndV11(t *testing.T) {
	key := testKey(t)
	const secret = "xoxd-SENTINEL-VALUE"
	for _, prefix := range []string{"v10", "v11"} {
		enc := encryptForTest(t, key, prefix, secret, "")
		got, err := decryptValue(enc, key, 0, "")
		if err != nil {
			t.Fatalf("%s: %v", prefix, err)
		}
		if got != secret {
			t.Errorf("%s: 復号結果が違う（生値は出さない）: len=%d want len=%d", prefix, len(got), len(secret))
		}
	}
}

// meta.version >= 24 では復号後の先頭 32 バイト（ホストハッシュ）を落とすこと。
//
// 🚨 落とし忘れると d cookie の頭が欠けて 401/404 になる。逆に、
// meta.version < 24 で落とすと先頭 32 文字が消える。両方向を固定する。
func TestDecryptValueStripsHashPrefixByMetaVersion(t *testing.T) {
	key := testKey(t)
	const secret = "xoxd-SENTINEL-VALUE-LONG-ENOUGH-TO-SURVIVE-32-BYTES"

	enc24 := encryptForTest(t, key, "v10", secret, ".slack.com")
	got, err := decryptValue(enc24, key, 24, ".slack.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != secret {
		t.Errorf("meta>=24: ハッシュ 32 バイトが落ちていない（got len=%d, want %d）", len(got), len(secret))
	}

	// meta<24 のデータに対して 32 バイトを落としてはいけない。
	enc0 := encryptForTest(t, key, "v10", secret, "")
	got, err = decryptValue(enc0, key, 0, ".slack.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != secret {
		t.Errorf("meta<24: 値が削られている（got len=%d, want %d）", len(got), len(secret))
	}

	// ハッシュより短い復号結果は、黙って通さずエラーにする。
	short := encryptForTest(t, key, "v10", "abc", "")
	if _, err := decryptValue(short, key, 24, ".slack.com"); err == nil {
		t.Error("32 バイト未満なのにエラーにならない")
	}
}

// PKCS7 のパディングは全バイトを検証すること（末尾 1 バイトだけ見ない）。
func TestPKCS7UnpadValidatesWholePadding(t *testing.T) {
	ok := append([]byte("0123456789ab"), 4, 4, 4, 4)
	got, err := pkcs7Unpad(ok, 16)
	if err != nil || string(got) != "0123456789ab" {
		t.Fatalf("正しいパディングを外せない: %q %v", got, err)
	}

	bad := map[string][]byte{
		"パディングが揃っていない": append([]byte("0123456789ab"), 1, 2, 3, 4),
		"パディング長が 0":    append([]byte("0123456789abcde"), 0),
		"ブロック長の倍数でない":  []byte("012345"),
		"空":            {},
	}
	for name, b := range bad {
		if _, err := pkcs7Unpad(b, 16); err == nil {
			t.Errorf("%s: エラーにすべき", name)
		}
	}
	// パディング長がデータ長を超える場合
	if _, err := pkcs7Unpad(bytes.Repeat([]byte{32}, 16), 16); err == nil {
		t.Error("パディング長がブロック長を超える場合はエラーにすべき")
	}
}

// 平文で保存された古い Cookie はそのまま返すこと。
func TestDecryptValuePassesThroughPlaintext(t *testing.T) {
	got, err := decryptValue([]byte("plain-value"), testKey(t), 0, "")
	if err != nil || got != "plain-value" {
		t.Errorf("平文が壊れた: %q %v", got, err)
	}
	if got, err := decryptValue(nil, testKey(t), 0, ""); err != nil || got != "" {
		t.Errorf("空の値: %q %v", got, err)
	}
}

// Cookie のドメイン一致はドット境界で判定すること。
//
// 🚨 SQL の LIKE '%slack.com' や単純な suffix 比較だと notslack.com が一致する。
func TestCookieHostMatches(t *testing.T) {
	cases := []struct {
		hostKey, reqHost string
		want             bool
	}{
		{".slack.com", "alpha.slack.com", true},
		{".slack.com", "slack.com", true},
		{"alpha.slack.com", "alpha.slack.com", true},
		{"alpha.slack.com", "beta.slack.com", false},
		{".notslack.com", "alpha.slack.com", false},
		// 🚨 素の suffix 比較だと通ってしまう組み合わせ（ドット境界が効いているかはここでしか分からない）。
		{"slack.com", "alpha.slack.com", false}, // host-only は完全一致のみ
		{"ack.com", "alpha.slack.com", false},   // 境界を跨いだ suffix
		{"lpha.slack.com", "alpha.slack.com", false},
		{"evilslack.com", "alpha.slack.com", false},
		{".slack.com.evil.jp", "alpha.slack.com", false},
		{"", "alpha.slack.com", false},
	}
	for _, c := range cases {
		if got := HostMatches(c.hostKey, c.reqHost); got != c.want {
			t.Errorf("host_key=%q req=%q: got %v, want %v", c.hostKey, c.reqHost, got, c.want)
		}
	}
}
