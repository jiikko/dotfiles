package store

// 添付 (issue 453)。PG が `pro-con card attach` で付けたファイル (作った画面の見た目・コマンドの出力) を、dispatcher が
// 状態の置き場の attachments/<カード>/ (0700 / 0600) へ移して記録に載せる。
// 書き手は dispatcher だけ (426 の決定 1): 付ける側は受付の箱の files/ にファイルを置いてから依頼を置く (SubmitAttachment)。
//
// 失敗モード:
//   - files/ に置いた後、依頼を置く前に落ちる → 依頼の無いファイルが残る。dispatcher が stageTTL を過ぎたものを消す (SweepAttachments)
//   - 移した後、記録を書く前に落ちる → 次の Apply が同じ依頼をもう一度当てる。移し先の名前は依頼の ID で決まるので、移し先に在れば移し済み
//   - 除けた依頼 → files/ のファイルも消す (Apply)
//   - カードが記録から外れた (削除・書庫へ移した) → その attachments/<カード>/ を消す。置き場に残るのは記録にあるカードの添付だけ

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"pro-con/card"
)

const (
	// KindAttachment は添付の依頼の種類 (KindEvent と同じく定数で持つ。"attach" は attach の間の指示の記録で使っている)
	KindAttachment = "attachment"
	AttachDir      = "attachments"
	StageDir       = "files" // inbox の下。依頼より先に置く添付のファイル
	// MaxAttachBytes は添付 1 件の上限 (Retina の全画面の png が 10 MiB 前後)
	MaxAttachBytes = 20 << 20
	// MaxAttachPerCard はカード 1 枚に付けられる数 (置き場を PG の出し過ぎで埋めない)
	MaxAttachPerCard = 50
	// stageTTL は依頼の来ない files/ のファイルを消すまでの長さ (置いてから依頼を置くまでは同じプロセスの数 ms)
	stageTTL = time.Hour
)

var (
	extPattern = regexp.MustCompile(`^\.[a-z0-9]{1,10}$`)
	// idPattern は Submit が振る依頼の ID の形 (箱に手で置かれた名前を glob のパターンとして使わない)
	idPattern = regexp.MustCompile(`^[0-9]{20}-[0-9a-f]{8}$`)
)

// attachExt は置き場のファイル名に使う拡張子 (小文字。英数字だけ。合わなければ付けない)。
func attachExt(name string) string {
	if e := strings.ToLower(filepath.Ext(name)); extPattern.MatchString(e) {
		return e
	}
	return ""
}

// AttachKindOf は元のファイル名から、人間がどう見るかを決める。
func AttachKindOf(name string) card.AttachKind {
	switch attachExt(name) {
	// 🚨 .svg は入れない: 既定のアプリがブラウザだと中の JS が動く。画像は Preview に固定して開く (ui/attachments.go)
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".heic", ".pdf":
		return card.AttachImage
	case ".txt", ".ans", ".log", ".out":
		return card.AttachText
	}
	return card.AttachFile
}

// SubmitAttachment は src を受付の箱の files/ に写してから、添付の依頼を置く (どのプロセスから呼んでもよい)。依頼の ID を返す。
func SubmitAttachment(dir, cardID, src, note string) (string, error) {
	fi, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	switch {
	case !fi.Mode().IsRegular():
		return "", fmt.Errorf("%s は普通のファイルではない", src)
	case fi.Size() == 0:
		return "", fmt.Errorf("%s は空", src)
	case fi.Size() > MaxAttachBytes:
		return "", fmt.Errorf("%s は %d MiB を超える (%d バイト)", src, MaxAttachBytes>>20, fi.Size())
	}
	r := Request{Kind: KindAttachment, CardID: cardID, Name: filepath.Base(src), Note: note}
	return submitWith(dir, r, func(box, id string) error {
		stage := filepath.Join(box, StageDir)
		if err := os.MkdirAll(stage, 0o700); err != nil {
			return err
		}
		return copyFile(src, filepath.Join(stage, id+attachExt(src)))
	})
}

// copyFile は src を dst へ 0600 で写す (一時ファイルに書いてから rename。書きかけを dispatcher に見せない)。
// 写す途中で上限を超えて伸びたら止める (stat と読むあいだにファイルが育つ)。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-*") // CreateTemp は 0600
	if err != nil {
		return err
	}
	n, err := io.Copy(tmp, io.LimitReader(in, MaxAttachBytes+1))
	if err == nil && n > MaxAttachBytes {
		err = fmt.Errorf("%s は写す途中で %d MiB を超えた", src, MaxAttachBytes>>20)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), dst)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// stageAttachment は添付の依頼の、箱に置かれたファイルと移し先を決め、r に移し先の絶対パスと大きさを入れる (Apply が apply の前に呼ぶ。
// 🚨 依頼に書かれたパスは信じない: 置き場の名前は依頼の ID と元の名前の拡張子だけで決める)。移し先に既に在れば移し済み (前の Apply が
// 記録を書く前に落ちた)。
func stageAttachment(dir string, r *Request) (staged string, err error) {
	if r.CardID == "" || filepath.Base(r.CardID) != r.CardID || strings.HasPrefix(r.CardID, ".") {
		return "", fmt.Errorf("attachment: カードの ID が不正: %q", r.CardID)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	name := r.ID + attachExt(r.Name)
	staged = filepath.Join(dir, InboxDir, StageDir, name)
	dst := filepath.Join(abs, AttachDir, r.CardID, name)
	fi, err := os.Lstat(staged)
	if errors.Is(err, fs.ErrNotExist) {
		staged = ""
		fi, err = os.Lstat(dst)
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", errors.New("attachment: 添付のファイルが受付の箱に無い")
	case err != nil:
		return "", fmt.Errorf("attachment: %w", err)
	case !fi.Mode().IsRegular():
		return "", errors.New("attachment: 添付が普通のファイルではない")
	case fi.Size() > MaxAttachBytes:
		return "", fmt.Errorf("attachment: %d MiB を超える (%d バイト)", MaxAttachBytes>>20, fi.Size())
	}
	r.File, r.Size = dst, fi.Size()
	return staged, nil
}

// adoptAttachment は箱のファイルを移し先 (0700 のカードの置き場) へ移す。staged が空なら移し済み。
func adoptAttachment(staged, dst string) error {
	d := filepath.Dir(dst)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(d, 0o700); err != nil {
		return err
	}
	if staged == "" {
		return nil
	}
	return os.Rename(staged, dst) // 箱のファイルは copyFile が 0600 で作っている (rename は権限を保つ)
}

// SweepAttachments は記録に無いカードの添付 (削除した・書庫へ移した) と、依頼の来ない箱のファイルを消す (dispatcher だけが呼ぶ)。
// 消したカードの ID を返す。
func SweepAttachments(dir string, now time.Time) ([]string, error) {
	st, err := Load(dir)
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for _, c := range st.Cards {
		live[c.ID] = true
	}
	var gone []string
	var errs []error
	ents, err := os.ReadDir(filepath.Join(dir, AttachDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	for _, e := range ents {
		if live[e.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, AttachDir, e.Name())); err != nil {
			errs = append(errs, err)
			continue
		}
		gone = append(gone, e.Name())
	}
	box := filepath.Join(dir, InboxDir)
	staged, err := os.ReadDir(filepath.Join(box, StageDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	for _, e := range staged {
		id := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if strings.HasPrefix(e.Name(), ".") { // 書きかけの一時ファイル (.<名前>.tmp-*)
			id = ""
		}
		if id != "" {
			if _, err := os.Stat(filepath.Join(box, id+".json")); err == nil {
				continue // 依頼がまだ箱にある (次の Apply が移す)
			}
		}
		fi, err := e.Info()
		if err != nil || now.Sub(fi.ModTime()) < stageTTL {
			continue
		}
		if err := os.Remove(filepath.Join(box, StageDir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return gone, errors.Join(errs...)
}
