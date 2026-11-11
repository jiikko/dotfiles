// Package source は slack-cli を起動して、会話の材料 (発言・スレッド・ユーザー・チャンネル) を JSON で読む。
//
// Slack へは slack-cli を通してだけ触る。slack-cli は読み取り専用のメソッドしか呼ばない約束をテストと変異検証で
// 固めているので、ここで Slack の API を直に呼ぶ口を作らない (読み取り専用の保証が 2 実装になる)。
package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/jiikko/dotfiles/src/termsafe"
	"subproc"
)

// IsChannelID は id が Slack のチャンネル ID の形 (英大文字と数字) か。チャンネル ID はファイル名に使うので、
// パスの区切りを含む値で外へ書かないよう、書く側はこれで確かめる。
func IsChannelID(id string) bool { return channelID.MatchString(id) }

var channelID = regexp.MustCompile(`^[A-Z0-9]+$`)

// Runner は slack-cli を引数 args で 1 回起動し、stdout と stderr を返す。
//
// stderr は捨てない: slack-cli は取得が途中で切れても (ページの上限・続きの cursor が無い) 取れた分を出して rc=0 で終わり、
// 切れたことは stderr にしか書かない。読む画面では端末の stderr が見えないので、呼び出し側が板の注記に載せる。
type Runner interface {
	Run(ctx context.Context, args ...string) (stdout, stderr []byte, err error)
}

// ExecRunner は実物の slack-cli (Bin) を起動する。Tee が nil でなければ stderr をそこへも流す (429 の待ちのように、
// 走っている間に出る知らせを端末で見せる。読む画面の中では画面が崩れるので nil にする)。
type ExecRunner struct {
	Bin string
	Tee io.Writer
}

// Run は Bin を起動して stdout と stderr を返す。失敗は終了コードと stderr を添えて返す。
func (r ExecRunner) Run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	cmd := subproc.CommandContext(ctx, r.Bin, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if r.Tee != nil {
		cmd.Stderr = io.MultiWriter(&errb, termsafe.BlockWriter(r.Tee))
	}
	if err := cmd.Run(); err != nil {
		return nil, errb.Bytes(), fmt.Errorf("%s %s: %w: %s", r.Bin, args[0], err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), errb.Bytes(), nil
}

// Quiet は端末へ stderr を流さない runner を返す (読む画面の中で流すと画面が崩れる。警告は板の注記に載る)。
// Throttle は中の runner を静かにし、列はそのまま共有する (列を分けると、同じプロセスの中で 2 本ずつ Slack へ届く)。
// 実物の ExecRunner 以外 (テストの偽物など) はそのまま返す。
func Quiet(r Runner) Runner {
	switch er := r.(type) {
	case *Throttle:
		return &Throttle{r: Quiet(er.r), g: er.g}
	case ExecRunner:
		er.Tee = nil
		return er
	case *ExecRunner:
		c := *er
		c.Tee = nil
		return c
	}
	return r
}

// Message は slack-cli の history / thread の -json の 1 件 (使う欄だけ)。
type Message struct {
	Ts          string `json:"ts"`
	ThreadTs    string `json:"thread_ts"`
	User        string `json:"user"`
	Username    string `json:"username"`
	BotID       string `json:"bot_id"`
	Text        string `json:"text"`
	ReplyCount  int    `json:"reply_count"`
	LatestReply string `json:"latest_reply"`
}

// IsThreadParent はスレッドの親か (history は親に thread_ts == ts を付ける)。
func (m Message) IsThreadParent() bool { return m.ThreadTs != "" && m.ThreadTs == m.Ts }

// IsBroadcastReply は「チャンネルにも投稿する」を付けた返信か。history と thread の両方に同じ ts で出るので、
// history 側では捨ててスレッドの側に寄せる。
func (m Message) IsBroadcastReply() bool { return m.ThreadTs != "" && m.ThreadTs != m.Ts }

// Channel は slack-cli の channels の -json の 1 件 (使う欄だけ)。
type Channel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsPrivate  bool   `json:"is_private"`
	IsArchived bool   `json:"is_archived"`
	IsMember   bool   `json:"is_member"`
	NumMembers int    `json:"num_members"`
}

// Me は slack-cli の whoami の -json (使う欄だけ)。
type Me struct {
	UserID string `json:"user_id"`
	URL    string `json:"url"` // https://<workspace>.slack.com/
}

type user struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"real_name"`
	IsBot    bool   `json:"is_bot"`
	Profile  struct {
		RealName    string `json:"real_name"`
		DisplayName string `json:"display_name"`
	} `json:"profile"`
}

// HistoryLimit は 1 チャンネルの history で読む上限。届いたら打ち切りとして利用者に知らせる (遡る期間を縮める案内)。
const HistoryLimit = 20000

// ThreadLimit は 1 スレッドで読む返信の上限。slack-cli は上限に届くと黙って切るので、届いたら打ち切りとして扱う。
const ThreadLimit = 1000

// OldSlackCLINote は、slack-cli がスレッドの親の reply_count / latest_reply を出さない版の疑いがあるときの注記。
// その版では返信だけが新しいスレッドが見えなくなるので、黙らずに板へ出す。止めはしない: 返信が全部消されたスレッドの親は
// 新しい版でも動きを持たず、それしか無いチャンネルを止めると、正しい版でも読めなくなる。
const OldSlackCLINote = "スレッドの親に返信の数・最後の返信が付いていない。slack-cli が古い (3cf3c5e より前) と、期間より前に始まったスレッドの新しい返信が出ない"

// Client は slack-cli のサブコマンドを型付きで呼ぶ。
type Client struct {
	R Runner
}

// decode は slack-cli を呼んで stdout の JSON を v へ読み、stderr (警告) を返す。
func (c Client) decode(ctx context.Context, v any, args ...string) (string, error) {
	out, errb, err := c.R.Run(ctx, args...)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return "", fmt.Errorf("slack %s の JSON を読めない: %w", args[0], err)
	}
	return warnings(errb), nil
}

// slackCLIWarnPrefix は slack-cli が警告の行に付ける印 (~/src/slack-cli の cmd/slack/main.go splitListErrTo・internal/config/config.go)。
// slack-cli は同じ stderr に案内 (internal/slack/resolve.go の「→ workspace:」・チャンネル一覧のキャッシュ・ローカル設定) も毎回出すので、
// stderr を丸ごと警告にすると板の注記が呼び出しの数だけ案内で埋まる。slack-cli が警告の印を変えたら、ここも合わせる。
const slackCLIWarnPrefix = "警告:"

// warnings は slack-cli の stderr から警告だけを取り出し、1 つの警告を 1 行にして「 / 」でつなぐ (無ければ空)。
// 警告は字下げした続きの行を持つ (打ち切りの「結果は不完全です。」と回避策。slack-cli の internal/slack/api.go TruncatedError.Error)。
// 続きを落とすと回避策が板から消えるので、警告の直後の字下げした行は同じ警告に足す。
func warnings(errb []byte) string {
	var ws []string
	in := false
	for _, raw := range strings.Split(string(errb), "\n") {
		l := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(l, slackCLIWarnPrefix):
			ws, in = append(ws, l), true
		case in && l != "" && (strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t")):
			ws[len(ws)-1] += " " + l
		default:
			in = false
		}
	}
	return strings.Join(ws, " / ")
}

// Whoami は接続中のユーザーとワークスペースを返す。
func (c Client) Whoami(ctx context.Context) (Me, error) {
	var me Me
	if _, err := c.decode(ctx, &me, "whoami", "-json"); err != nil {
		return Me{}, err
	}
	if me.UserID == "" {
		return Me{}, errors.New("slack whoami が user_id を返さない")
	}
	return me, nil
}

// Users はワークスペースのユーザー。Names は user ID から表示名への表で、ボットと無効化済みのユーザーも含める
// (含めないと退職者・ボットの発言が ID のまま出る)。Bots はボットの user ID の集合。
type Users struct {
	Names map[string]string
	Bots  map[string]bool
}

// slackbotID は Slackbot の user ID。users.list は Slackbot の is_bot を偽で返すので、ボットの集合へ名指しで足す。
const slackbotID = "USLACKBOT"

// Users はワークスペースのユーザーを読む。
func (c Client) Users(ctx context.Context) (Users, error) {
	var us []user
	if _, err := c.decode(ctx, &us, "users", "-json", "-bots", "-deleted"); err != nil {
		return Users{}, err
	}
	r := Users{Names: make(map[string]string, len(us)), Bots: map[string]bool{slackbotID: true}}
	for _, u := range us {
		if u.IsBot {
			r.Bots[u.ID] = true
		}
		for _, n := range []string{u.Profile.DisplayName, u.RealName, u.Profile.RealName, u.Name} {
			if n != "" {
				r.Names[u.ID] = n
				break
			}
		}
	}
	return r, nil
}

// IsBot は search の 1 件 f がボットの投稿かを返す。user の無い投稿 (Reacji Channeler・incoming webhook などの
// 古い形のボット) もボットに数える。search.messages は bot_id を返さない (実測 2026-11-11: 6 チャンネル 1,200 件で 0 件)。
func (us Users) IsBot(f Found) bool {
	return f.User == "" || us.Bots[f.User]
}

// Channels はワークスペースのチャンネルの一覧。
func (c Client) Channels(ctx context.Context) ([]Channel, error) {
	var chs []Channel
	_, err := c.decode(ctx, &chs, "channels", "-json")
	return chs, err
}

// Fetched は history / thread の結果。Truncated は件数の上限に届いたこと、Warning は slack-cli の stderr のうち警告の行 (途中で切れた等。空なら無し)。
// MaybeOld は history の親が 1 つもスレッドの動きを持たなかったこと (OldSlackCLINote)。
type Fetched struct {
	Msgs      []Message
	Truncated bool
	Warning   string
	MaybeOld  bool
}

// History は channel の oldest より新しい発言 (親とスレッド外の発言)。
// 🚨 フラグは引数より前に置く (slack-cli は引数の後ろのフラグを使い方の誤りとして rc=2 で止める)。
//
// 親が 1 つもスレッドの動き (reply_count / latest_reply) を持たないなら MaybeOld を立てる (OldSlackCLINote)。
func (c Client) History(ctx context.Context, channel, oldest string) (Fetched, error) {
	var f Fetched
	warn, err := c.decode(ctx, &f.Msgs, "history", "-json", "-oldest", oldest, "-n", strconv.Itoa(HistoryLimit), channel)
	if err != nil {
		return Fetched{}, err
	}
	parents, active := 0, 0
	for _, m := range f.Msgs {
		if m.IsThreadParent() {
			parents++
			if m.ReplyCount > 0 || m.LatestReply != "" {
				active++
			}
		}
	}
	f.Truncated, f.Warning, f.MaybeOld = len(f.Msgs) >= HistoryLimit, warn, parents > 0 && active == 0
	return f, nil
}

// Thread はスレッドの親と返信 (古い順)。
func (c Client) Thread(ctx context.Context, channel, parentTs string) (Fetched, error) {
	var f Fetched
	warn, err := c.decode(ctx, &f.Msgs, "thread", "-json", "-n", strconv.Itoa(ThreadLimit), channel, parentTs)
	if err != nil {
		return Fetched{}, err
	}
	f.Truncated, f.Warning = len(f.Msgs) >= ThreadLimit, warn
	return f, nil
}

// Found は slack-cli の search の -json の 1 件 (使う欄だけ)。search.messages は channel をオブジェクトで返す。
type Found struct {
	Ts        string `json:"ts"`
	User      string `json:"user"`
	Username  string `json:"username"`
	Text      string `json:"text"`
	Permalink string `json:"permalink"`
	Channel   struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"channel"`
}

// SearchCount は search の 1 ページの件数 (search.messages の count の上限)。
const SearchCount = 100

// Search は query に一致する発言の page ページ目 (新しい順)。
//
// 🚨 返る件数が count より少ないことを終わりと読まない: 続きがあるページでも少なく返る (実測 2026-11-11: 1 ページ目が 65 件で
// 2 ページ目が 100 件)。終わりは空のページか、呼び出し側が発言の時刻で決める (範囲の外のページは空で返る。実測 2026-11-11)。
func (c Client) Search(ctx context.Context, query string, page int) ([]Found, error) {
	var fs []Found
	_, err := c.decode(ctx, &fs, "search", "-json", "-n", strconv.Itoa(SearchCount), "-page", strconv.Itoa(page), query)
	return fs, err
}
