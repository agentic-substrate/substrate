package authority

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/agentic-substrate/substrate/internal/strictjson"
)

type BackupScope struct {
	OwnerID  string
	SpaceIDs []string
	Bindings []Binding
	Contexts []Context
}

func (s *Store) WithBackupSnapshot(fn func([]byte, BackupScope) error) error {
	return s.locked(false, func(path string) error {
		f, err := openPrivate(path, syscall.O_RDONLY)
		if err != nil {
			return unavailable(err)
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return unavailable(err)
		}
		if info.Size() > 1024*1024 {
			return errors.New("authority exceeds the 1 MiB backup limit; revoke unused sessions and retry")
		}
		data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
		if err != nil {
			return unavailable(err)
		}
		scope, err := ValidateBackupSnapshot(data)
		if err != nil {
			return err
		}
		return fn(data, scope)
	})
}

func ValidateBackupSnapshot(data []byte) (BackupScope, error) {
	var st state
	if len(data) > 1024*1024 {
		return BackupScope{}, errors.New("authority exceeds the 1 MiB backup limit")
	}
	if err := strictjson.Decode(data, &st); err != nil {
		return BackupScope{}, unavailable(err)
	}
	if st.Version != 1 || !backupID(st.OwnerID) || !validName(st.OwnerName) || len(st.Spaces) == 0 || st.Sessions == nil {
		return BackupScope{}, ErrUnavailable
	}
	spaces, names := map[string]string{}, map[string]bool{}
	scope := BackupScope{OwnerID: st.OwnerID, Bindings: st.Bindings}
	for _, sp := range st.Spaces {
		if !backupID(sp.ID) || !validName(sp.Name) || spaces[sp.ID] != "" || names[sp.Name] {
			return BackupScope{}, ErrUnavailable
		}
		spaces[sp.ID], names[sp.Name] = sp.Name, true
		scope.SpaceIDs = append(scope.SpaceIDs, sp.ID)
	}
	if !names["Personal"] {
		return BackupScope{}, ErrUnavailable
	}
	paths, repos := map[string]bool{}, map[string]Binding{}
	for _, b := range st.Bindings {
		if spaces[b.SpaceID] == "" || !backupID(b.RepositoryID) || !filepath.IsAbs(b.Checkout) || !filepath.IsAbs(b.CommonDir) || filepath.Clean(b.Checkout) != b.Checkout || filepath.Clean(b.CommonDir) != b.CommonDir || strings.ContainsRune(b.Checkout, 0) || strings.ContainsRune(b.CommonDir, 0) || paths[b.Checkout] || b.Inode == 0 {
			return BackupScope{}, ErrUnavailable
		}
		if old, ok := repos[b.RepositoryID]; ok && (old.SpaceID != b.SpaceID || old.CommonDir != b.CommonDir || old.Device != b.Device || old.Inode != b.Inode) {
			return BackupScope{}, ErrUnavailable
		}
		for _, old := range repos {
			if old.CommonDir == b.CommonDir && old.Device == b.Device && old.Inode == b.Inode && old.RepositoryID != b.RepositoryID {
				return BackupScope{}, ErrUnavailable
			}
		}
		paths[b.Checkout], repos[b.RepositoryID] = true, b
		scope.Contexts = append(scope.Contexts, Context{OwnerID: st.OwnerID, SpaceID: b.SpaceID, SpaceName: spaces[b.SpaceID], RepositoryID: b.RepositoryID, Checkout: b.Checkout})
	}
	for hash, grant := range st.Sessions {
		ctx := grant.Context
		b, ok := repos[ctx.RepositoryID]
		if !backupID(hash) || !ok || ctx.OwnerID != st.OwnerID || ctx.SpaceID != b.SpaceID || ctx.SpaceName != spaces[ctx.SpaceID] || !paths[ctx.Checkout] || grant.ExpiresAt.IsZero() {
			return BackupScope{}, ErrUnavailable
		}
		found := false
		for _, binding := range st.Bindings {
			if binding.Checkout == ctx.Checkout && binding.RepositoryID == ctx.RepositoryID && binding.SpaceID == ctx.SpaceID {
				found = true
			}
		}
		if !found {
			return BackupScope{}, ErrUnavailable
		}
	}
	return scope, nil
}

func ResetBackupSessions(data []byte) ([]byte, error) {
	if _, err := ValidateBackupSnapshot(data); err != nil {
		return nil, err
	}
	var st state
	if err := strictjson.Decode(data, &st); err != nil {
		return nil, err
	}
	st.Sessions = map[string]session{}
	return json.Marshal(st)
}

func backupID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == id
}

func CheckPrivatePlacement(path string) error {
	if err := checkPrivate(path, true); err != nil {
		return err
	}
	return outsideCheckout(path)
}

func CheckOutsideCheckout(path string) error { return outsideCheckout(path) }
