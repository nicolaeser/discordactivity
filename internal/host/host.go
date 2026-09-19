package host

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nicolaeser/DiscordActivity/internal/gateway"
	"github.com/nicolaeser/DiscordActivity/internal/store"
)

type Status struct {
	ID        int64  `json:"id"`
	Account   string `json:"account"`
	User      string `json:"user,omitempty"`
	Status    string `json:"status"`
	Activity  string `json:"activity,omitempty"`
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
	LastError string `json:"last_error,omitempty"`
}

type Host struct {
	parent context.Context
	store  *store.Store
	log    *slog.Logger
	mu     sync.Mutex
	sess   map[int64]*managed
}

type managed struct {
	acc    store.Account
	sess   *gateway.Session
	cancel context.CancelFunc
	done   chan struct{}
}

func New(parent context.Context, st *store.Store, log *slog.Logger) *Host {
	if log == nil {
		log = slog.Default()
	}
	return &Host{parent: parent, store: st, log: log, sess: map[int64]*managed{}}
}

func (h *Host) StartAll() error {
	accounts, err := h.store.List()
	if err != nil {
		return err
	}
	n := 0
	for _, acc := range accounts {
		h.Apply(acc)
		if acc.Enabled {
			if n > 0 {
				select {
				case <-h.parent.Done():
					return h.parent.Err()
				case <-time.After(5 * time.Second):
				}
			}
			n++
		}
	}
	return nil
}

func (h *Host) Apply(acc store.Account) {
	if !acc.Enabled {
		h.Stop(acc.ID)
		return
	}
	pres, err := acc.Presence()
	if err != nil {
		h.log.Error("invalid presence", "id", acc.ID, "error", err)
		h.Stop(acc.ID)
		return
	}
	h.mu.Lock()
	cur, ok := h.sess[acc.ID]
	var live *gateway.Session
	if ok && cur.acc.Token == acc.Token {
		live = cur.sess
		cur.acc = acc
	}
	h.mu.Unlock()
	if live != nil {
		live.SetPresence(pres)
		return
	}
	h.Stop(acc.ID)
	ctx, cancel := context.WithCancel(h.parent)
	sess := gateway.New(acc.ID, acc.Name, acc.Token, pres, h.log)
	done := make(chan struct{})
	h.mu.Lock()
	h.sess[acc.ID] = &managed{acc: acc, sess: sess, cancel: cancel, done: done}
	h.mu.Unlock()
	go func() {
		defer close(done)
		sess.Run(ctx)
	}()
}

func (h *Host) Stop(id int64) {
	h.mu.Lock()
	m, ok := h.sess[id]
	if ok {
		delete(h.sess, id)
	}
	h.mu.Unlock()
	if !ok {
		return
	}
	m.cancel()
	m.sess.Close()
	select {
	case <-m.done:
	case <-time.After(8 * time.Second):
	}
}

func (h *Host) Start(id int64) error {
	if err := h.store.SetEnabled(id, true); err != nil {
		return err
	}
	acc, err := h.store.Get(id)
	if err != nil {
		return err
	}
	h.Apply(acc)
	return nil
}

func (h *Host) StopAccount(id int64) error {
	if err := h.store.SetEnabled(id, false); err != nil {
		return err
	}
	h.Stop(id)
	return nil
}

func (h *Host) Restart(id int64) error {
	if err := h.store.SetEnabled(id, true); err != nil {
		return err
	}
	acc, err := h.store.Get(id)
	if err != nil {
		return err
	}
	h.Stop(id)
	h.Apply(acc)
	return nil
}

func (h *Host) Health() []Status {
	accounts, err := h.store.List()
	if err != nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []Status
	for _, acc := range accounts {
		item := Status{ID: acc.ID, Account: acc.Name, Status: acc.Status, Enabled: acc.Enabled}
		if m, ok := h.sess[acc.ID]; ok {
			snap := m.sess.Snapshot()
			item.User, item.Status, item.Activity, item.Connected, item.LastError = snap.User, snap.Status, snap.Activity, snap.Connected, snap.LastError
		}
		out = append(out, item)
	}
	return out
}

func (h *Host) Runtime(id int64) gateway.Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m, ok := h.sess[id]; ok {
		return m.sess.Snapshot()
	}
	return gateway.Snapshot{ID: id}
}
