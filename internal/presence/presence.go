package presence

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	TypePlaying   = 0
	TypeStreaming = 1
	TypeListening = 2
	TypeWatching  = 3
	TypeCustom    = 4
	TypeCompeting = 5
)

type Update struct {
	Since      int64      `json:"since"`
	Activities []Activity `json:"activities"`
	Status     string     `json:"status"`
	AFK        bool       `json:"afk"`
}

type Activity struct {
	Name          string      `json:"name"`
	Type          int         `json:"type"`
	URL           string      `json:"url,omitempty"`
	Details       string      `json:"details,omitempty"`
	State         string      `json:"state,omitempty"`
	ApplicationID string      `json:"application_id,omitempty"`
	Timestamps    *Timestamps `json:"timestamps,omitempty"`
	Assets        *Assets     `json:"assets,omitempty"`
	Emoji         *Emoji      `json:"emoji,omitempty"`
	Buttons       []string    `json:"buttons,omitempty"`
	Metadata      *Metadata   `json:"metadata,omitempty"`
}

type Timestamps struct {
	Start *int64 `json:"start,omitempty"`
}

type Assets struct {
	LargeImage string `json:"large_image,omitempty"`
	LargeText  string `json:"large_text,omitempty"`
	SmallImage string `json:"small_image,omitempty"`
	SmallText  string `json:"small_text,omitempty"`
}

type Emoji struct {
	Name string `json:"name"`
}

type Metadata struct {
	ButtonURLs []string `json:"button_urls,omitempty"`
}

type Options struct {
	Status, ActivityType, Name, Details, State, URL string
	ApplicationID, Start                            string
	LargeImage, LargeText, SmallImage, SmallText    string
	Emoji, Button1Label, Button1URL                 string
	Button2Label, Button2URL                        string
	Now                                             time.Time
}

func Build(opts Options) (Update, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	status, stream, err := ParseStatus(opts.Status)
	if err != nil {
		return Update{}, err
	}
	var since int64
	if status == "idle" {
		since = now.UnixMilli()
	}
	act, ok, err := buildActivity(opts, stream, now)
	if err != nil {
		return Update{}, err
	}
	activities := []Activity{}
	if ok {
		activities = []Activity{act}
	}
	return Update{Since: since, Activities: activities, Status: status, AFK: status == "idle"}, nil
}

func (u Update) Label() string {
	if len(u.Activities) == 0 {
		return ""
	}
	a := u.Activities[0]
	switch a.Type {
	case TypeStreaming:
		return "Streaming " + a.Name
	case TypeListening:
		return "Listening to " + a.Name
	case TypeWatching:
		return "Watching " + a.Name
	case TypeCustom:
		if a.State != "" {
			return a.State
		}
		return a.Name
	case TypeCompeting:
		return "Competing in " + a.Name
	default:
		if a.Name == "" {
			return ""
		}
		return "Playing " + a.Name
	}
}

func ParseStatus(raw string) (string, bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "online", "on":
		return "online", false, nil
	case "idle", "away", "afk":
		return "idle", false, nil
	case "dnd", "d&d", "do_not_disturb":
		return "dnd", false, nil
	case "invisible", "invis", "offline":
		return "invisible", false, nil
	case "streaming", "stream":
		return "online", true, nil
	default:
		return "", false, fmt.Errorf("unknown status %q", raw)
	}
}

func parseType(raw string) (int, bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "none", "off":
		return 0, false, nil
	case "0", "playing", "play", "game":
		return TypePlaying, true, nil
	case "1", "streaming", "stream":
		return TypeStreaming, true, nil
	case "2", "listening", "listen":
		return TypeListening, true, nil
	case "3", "watching", "watch":
		return TypeWatching, true, nil
	case "4", "custom", "custom_status":
		return TypeCustom, true, nil
	case "5", "competing", "compete":
		return TypeCompeting, true, nil
	default:
		return 0, false, fmt.Errorf("unknown activity type %q", raw)
	}
}

func buildActivity(opts Options, stream bool, now time.Time) (Activity, bool, error) {
	kind, on, err := parseType(opts.ActivityType)
	if err != nil {
		return Activity{}, false, err
	}
	if !on && stream {
		kind, on = TypeStreaming, true
	}
	if !on {
		return Activity{}, false, nil
	}
	name, state, details, url := strings.TrimSpace(opts.Name), strings.TrimSpace(opts.State), strings.TrimSpace(opts.Details), strings.TrimSpace(opts.URL)
	if kind == TypeCustom {
		if state == "" {
			state = name
		}
		name = "Custom Status"
		if state == "" && strings.TrimSpace(opts.Emoji) == "" {
			return Activity{}, false, nil
		}
	}
	if kind == TypeStreaming {
		if name == "" {
			name = "Streaming"
		}
		if url == "" {
			url = "https://twitch.tv/discord"
		}
	}
	if name == "" && kind != TypeCustom {
		return Activity{}, false, fmt.Errorf("activity name required")
	}
	act := Activity{
		Name: name, Type: kind, URL: url, Details: details, State: state,
		ApplicationID: strings.TrimSpace(opts.ApplicationID),
	}
	if start, err := parseTime(opts.Start, now); err != nil {
		return Activity{}, false, err
	} else if start != nil {
		act.Timestamps = &Timestamps{Start: start}
	}
	if li, lt, si, st := strings.TrimSpace(opts.LargeImage), strings.TrimSpace(opts.LargeText), strings.TrimSpace(opts.SmallImage), strings.TrimSpace(opts.SmallText); li != "" || lt != "" || si != "" || st != "" {
		act.Assets = &Assets{LargeImage: li, LargeText: lt, SmallImage: si, SmallText: st}
	}
	if e := strings.TrimSpace(opts.Emoji); e != "" {
		act.Emoji = &Emoji{Name: e}
	}
	var labels, urls []string
	add := func(l, u string) {
		l, u = strings.TrimSpace(l), strings.TrimSpace(u)
		if l != "" && u != "" {
			labels, urls = append(labels, l), append(urls, u)
		}
	}
	add(opts.Button1Label, opts.Button1URL)
	add(opts.Button2Label, opts.Button2URL)
	if len(labels) > 0 {
		act.Buttons = labels
		act.Metadata = &Metadata{ButtonURLs: urls}
	}
	return act, true, nil
}

func parseTime(raw string, now time.Time) (*int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.EqualFold(raw, "now") {
		ms := now.UnixMilli()
		return &ms, nil
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if n > 0 && n < 1_000_000_000_000 {
			n *= 1000
		}
		return &n, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("start: want now, unix, or RFC3339")
	}
	ms := t.UnixMilli()
	return &ms, nil
}
