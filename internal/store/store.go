package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nicolaeser/DiscordActivity/internal/presence"
	_ "modernc.org/sqlite"
)

type Account struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Token         string `json:"token,omitempty"`
	TokenHint     string `json:"token_hint,omitempty"`
	Enabled       bool   `json:"enabled"`
	Status        string `json:"status"`
	ActivityType  string `json:"activity_type"`
	ActivityName  string `json:"activity_name"`
	Details       string `json:"details"`
	State         string `json:"state"`
	URL           string `json:"url"`
	ApplicationID string `json:"application_id"`
	Start         string `json:"start"`
	LargeImage    string `json:"large_image"`
	LargeText     string `json:"large_text"`
	SmallImage    string `json:"small_image"`
	SmallText     string `json:"small_text"`
	Emoji         string `json:"emoji"`
	Button1Label  string `json:"button_1_label"`
	Button1URL    string `json:"button_1_url"`
	Button2Label  string `json:"button_2_label"`
	Button2URL    string `json:"button_2_url"`
}

func (a Account) Presence() (presence.Update, error) {
	return presence.Build(presence.Options{
		Status: a.Status, ActivityType: a.ActivityType, Name: a.ActivityName,
		Details: a.Details, State: a.State, URL: a.URL, ApplicationID: a.ApplicationID,
		Start: a.Start, LargeImage: a.LargeImage, LargeText: a.LargeText,
		SmallImage: a.SmallImage, SmallText: a.SmallText, Emoji: a.Emoji,
		Button1Label: a.Button1Label, Button1URL: a.Button1URL,
		Button2Label: a.Button2Label, Button2URL: a.Button2URL,
	})
}

func (a Account) Public() Account {
	t := strings.TrimSpace(a.Token)
	a.Token = ""
	switch {
	case t == "":
		a.TokenHint = ""
	case len(t) <= 4:
		a.TokenHint = "••••"
	default:
		a.TokenHint = "••••" + t[len(t)-4:]
	}
	return a
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "data.sqlite"
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) List() ([]Account, error) {
	rows, err := s.db.Query(`SELECT ` + cols + ` FROM accounts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Get(id int64) (Account, error) {
	a, err := scan(s.db.QueryRow(`SELECT `+cols+` FROM accounts WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return Account{}, fmt.Errorf("account %d not found", id)
	}
	return a, err
}

func (s *Store) Insert(a Account) (Account, error) {
	normalize(&a)
	res, err := s.db.Exec(`INSERT INTO accounts (`+writeCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, args(a)...)
	if err != nil {
		return Account{}, err
	}
	a.ID, err = res.LastInsertId()
	return a, err
}

func (s *Store) Update(a Account) (Account, error) {
	cur, err := s.Get(a.ID)
	if err != nil {
		return Account{}, err
	}
	if strings.TrimSpace(a.Token) == "" {
		a.Token = cur.Token
	}
	normalize(&a)
	_, err = s.db.Exec(`UPDATE accounts SET name=?,token=?,enabled=?,status=?,activity_type=?,activity_name=?,details=?,state=?,url=?,application_id=?,activity_start=?,large_image=?,large_text=?,small_image=?,small_text=?,emoji=?,button_1_label=?,button_1_url=?,button_2_label=?,button_2_url=? WHERE id=?`,
		append(args(a), a.ID)...)
	return a, err
}

func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM accounts WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("account %d not found", id)
	}
	return nil
}

func (s *Store) SetEnabled(id int64, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	res, err := s.db.Exec(`UPDATE accounts SET enabled=? WHERE id=?`, en, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("account %d not found", id)
	}
	return nil
}

const cols = `id, name, token, enabled, status, activity_type, activity_name, details, state, url, application_id, activity_start, large_image, large_text, small_image, small_text, emoji, button_1_label, button_1_url, button_2_label, button_2_url`
const writeCols = `name, token, enabled, status, activity_type, activity_name, details, state, url, application_id, activity_start, large_image, large_text, small_image, small_text, emoji, button_1_label, button_1_url, button_2_label, button_2_url`

const schema = `CREATE TABLE IF NOT EXISTS accounts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL DEFAULT '',
	token TEXT NOT NULL UNIQUE,
	enabled INTEGER NOT NULL DEFAULT 1,
	status TEXT NOT NULL DEFAULT 'online',
	activity_type TEXT NOT NULL DEFAULT '',
	activity_name TEXT NOT NULL DEFAULT '',
	details TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL DEFAULT '',
	url TEXT NOT NULL DEFAULT '',
	application_id TEXT NOT NULL DEFAULT '',
	activity_start TEXT NOT NULL DEFAULT '',
	large_image TEXT NOT NULL DEFAULT '',
	large_text TEXT NOT NULL DEFAULT '',
	small_image TEXT NOT NULL DEFAULT '',
	small_text TEXT NOT NULL DEFAULT '',
	emoji TEXT NOT NULL DEFAULT '',
	button_1_label TEXT NOT NULL DEFAULT '',
	button_1_url TEXT NOT NULL DEFAULT '',
	button_2_label TEXT NOT NULL DEFAULT '',
	button_2_url TEXT NOT NULL DEFAULT ''
);`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (Account, error) {
	var a Account
	var enabled int
	err := row.Scan(&a.ID, &a.Name, &a.Token, &enabled, &a.Status, &a.ActivityType, &a.ActivityName, &a.Details, &a.State, &a.URL, &a.ApplicationID, &a.Start, &a.LargeImage, &a.LargeText, &a.SmallImage, &a.SmallText, &a.Emoji, &a.Button1Label, &a.Button1URL, &a.Button2Label, &a.Button2URL)
	a.Enabled = enabled != 0
	return a, err
}

func normalize(a *Account) {
	if strings.TrimSpace(a.Name) == "" {
		a.Name = "account"
	}
	if strings.TrimSpace(a.Status) == "" {
		a.Status = "online"
	}
}

func args(a Account) []any {
	en := 0
	if a.Enabled {
		en = 1
	}
	return []any{a.Name, a.Token, en, a.Status, a.ActivityType, a.ActivityName, a.Details, a.State, a.URL, a.ApplicationID, a.Start, a.LargeImage, a.LargeText, a.SmallImage, a.SmallText, a.Emoji, a.Button1Label, a.Button1URL, a.Button2Label, a.Button2URL}
}
