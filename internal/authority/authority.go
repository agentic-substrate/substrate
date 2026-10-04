package authority

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrDenied = errors.New("context denied: use a trusted credential for an explicitly registered checkout and space")
var ErrUnavailable = errors.New("local authority unavailable: initialize or repair the private state directory")

type Store struct {
	Dir string
	now func() time.Time
}
type Context struct {
	OwnerID      string `json:"owner_id"`
	SpaceID      string `json:"space_id"`
	SpaceName    string `json:"space_name"`
	RepositoryID string `json:"repo_id"`
	Checkout     string `json:"checkout"`
}
type Binding struct {
	SpaceID      string `json:"space_id"`
	RepositoryID string `json:"repo_id"`
	Checkout     string `json:"checkout"`
	CommonDir    string `json:"common_dir"`
	Device       uint64 `json:"device"`
	Inode        uint64 `json:"inode"`
}
type space struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type session struct {
	Context   Context   `json:"context"`
	ExpiresAt time.Time `json:"expires_at"`
}
type state struct {
	Version   int                `json:"version"`
	OwnerID   string             `json:"owner_id"`
	OwnerName string             `json:"owner_name"`
	Spaces    []space            `json:"spaces"`
	Bindings  []Binding          `json:"bindings"`
	Sessions  map[string]session `json:"sessions"`
}

func randomID() string {
	value := make([]byte, 32)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
}
func digest(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
func (s *Store) time() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}
func validName(name string) bool {
	return strings.TrimSpace(name) == name && len(name) > 0 && len(name) <= 80 && !strings.ContainsAny(name, "\r\n\t")
}

func (s *Store) Initialize(name string) error {
	if !validName(name) {
		return errors.New("owner name must contain 1–80 characters without surrounding whitespace")
	}
	return s.update(true, func(st *state) error {
		if st.OwnerID != "" {
			return errors.New("local owner already initialized; use its existing private state directory")
		}
		*st = state{Version: 1, OwnerID: randomID(), OwnerName: name, Spaces: []space{{randomID(), "Personal"}}, Sessions: map[string]session{}}
		return nil
	})
}
func (s *Store) CreateSpace(name string) error {
	if !validName(name) {
		return errors.New("space name must contain 1–80 characters without surrounding whitespace")
	}
	return s.update(false, func(st *state) error {
		for _, sp := range st.Spaces {
			if sp.Name == name || sp.ID == name {
				return errors.New("space already exists")
			}
		}
		st.Spaces = append(st.Spaces, space{randomID(), name})
		return nil
	})
}
func findSpace(st *state, value string) (space, error) {
	for _, sp := range st.Spaces {
		if sp.Name == value || sp.ID == value {
			return sp, nil
		}
	}
	return space{}, ErrDenied
}
func (s *Store) Register(path, selectedSpace string) (Binding, error) {
	var binding Binding
	err := s.update(false, func(st *state) error {
		sp, err := findSpace(st, selectedSpace)
		if err != nil {
			return err
		}
		current, err := checkout(path)
		if err != nil {
			return ErrDenied
		}
		repo := randomID()
		for _, b := range st.Bindings {
			if b.CommonDir == current.CommonDir && b.Device == current.Device && b.Inode == current.Inode {
				if b.SpaceID != sp.ID {
					return ErrDenied
				}
				repo = b.RepositoryID
			}
			if b.Checkout == current.Checkout {
				return errors.New("checkout already registered; changed bindings require a new registration location")
			}
		}
		current.SpaceID, current.RepositoryID = sp.ID, repo
		st.Bindings = append(st.Bindings, current)
		binding = current
		return nil
	})
	if err != nil {
		return Binding{}, err
	}
	return binding, nil
}
func bindingFor(st *state, path string) (Binding, error) {
	current, err := checkout(path)
	if err != nil {
		return Binding{}, ErrDenied
	}
	for _, b := range st.Bindings {
		if b.Checkout == current.Checkout && b.CommonDir == current.CommonDir && b.Device == current.Device && b.Inode == current.Inode {
			return b, nil
		}
	}
	return Binding{}, ErrDenied
}
func (s *Store) CreateSession(path, selectedSpace string) (string, error) {
	token := randomID()
	err := s.update(false, func(st *state) error {
		sp, err := findSpace(st, selectedSpace)
		if err != nil {
			return err
		}
		binding, err := bindingFor(st, path)
		if err != nil || binding.SpaceID != sp.ID {
			return ErrDenied
		}
		ctx := Context{OwnerID: st.OwnerID, SpaceID: sp.ID, SpaceName: sp.Name, RepositoryID: binding.RepositoryID, Checkout: binding.Checkout}
		for key, grant := range st.Sessions {
			if !s.time().Before(grant.ExpiresAt) {
				delete(st.Sessions, key)
			}
		}
		st.Sessions[digest(token)] = session{Context: ctx, ExpiresAt: s.time().Add(24 * time.Hour)}
		return nil
	})
	if err != nil {
		return "", err
	}
	return token, nil
}
func (s *Store) Authenticate(token, path string) (Context, error) {
	if len(token) != 64 {
		return Context{}, ErrDenied
	}
	var ctx Context
	err := s.read(func(st *state) error {
		grant, ok := st.Sessions[digest(token)]
		if !ok || !s.time().Before(grant.ExpiresAt) || grant.Context.OwnerID != st.OwnerID {
			return ErrDenied
		}
		if path == "" {
			path = grant.Context.Checkout
		}
		b, err := bindingFor(st, path)
		if err != nil || b.Checkout != grant.Context.Checkout || b.SpaceID != grant.Context.SpaceID || b.RepositoryID != grant.Context.RepositoryID {
			return ErrDenied
		}
		ctx = grant.Context
		return nil
	})
	return ctx, err
}
func (s *Store) RevokeSession(token string) error {
	return s.update(false, func(st *state) error { delete(st.Sessions, digest(token)); return nil })
}

// Authorize must run before object metadata or content is inspected.
func (ctx Context) Authorize(spaceID, repositoryID string) error {
	if ctx.OwnerID == "" || ctx.SpaceID == "" || ctx.RepositoryID == "" || ctx.SpaceID != spaceID || ctx.RepositoryID != repositoryID {
		return ErrDenied
	}
	return nil
}
func (s *Store) Inventory() (any, error) {
	var result any
	err := s.read(func(st *state) error {
		result = struct {
			OwnerID  string    `json:"owner_id"`
			Spaces   []space   `json:"spaces"`
			Bindings []Binding `json:"bindings"`
		}{st.OwnerID, st.Spaces, st.Bindings}
		return nil
	})
	return result, err
}
func unavailable(err error) error { return fmt.Errorf("%w: %v", ErrUnavailable, err) }
