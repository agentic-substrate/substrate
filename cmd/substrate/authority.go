package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/agentic-substrate/substrate/internal/authority"
)

func defaultStateDir() (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "substrate"), err
}
func runAuthority(args []string, out io.Writer) error {
	defaultDir, err := defaultStateDir()
	if err != nil {
		return err
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	stateDir := flags.String("state-dir", defaultDir, "owner-only authority directory outside Git")
	var name, path, space, credential, destination *string
	switch command {
	case "init":
		name = flags.String("owner", "", "local owner display name")
	case "space":
		name = flags.String("name", "", "explicit space name")
	case "register", "session":
		path = flags.String("path", ".", "Git checkout to bind")
		space = flags.String("space", "", "explicit space name or ID")
		if command == "session" {
			destination = flags.String("out", "", "new private credential file outside Git")
		}
	case "context", "revoke":
		credential = flags.String("credential", "", "owner-only session credential file")
		if command == "context" {
			path = flags.String("path", ".", "checkout to resolve within the session")
		}
	case "discover":
		path = flags.String("path", ".", "Git checkout containing discovery metadata")
	case "bindings":
	default:
		return errors.New("unknown authority command")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments; use explicit flags")
	}
	store := &authority.Store{Dir: *stateDir}
	var result any
	switch command {
	case "init":
		if err := store.Initialize(*name); err != nil {
			return err
		}
		result = map[string]string{"status": "local owner initialized"}
	case "space":
		if err := store.CreateSpace(*name); err != nil {
			return err
		}
		result = map[string]string{"status": "space created"}
	case "bindings":
		result, err = store.Inventory()
	case "register":
		result, err = store.Register(*path, *space)
	case "discover":
		hint, discoverErr := authority.Discover(*path)
		err = discoverErr
		result = struct {
			Trust string             `json:"trust"`
			Hint  authority.Manifest `json:"hint"`
		}{"untrusted discovery hint", hint}
	case "session":
		if *destination == "" {
			return errors.New("session requires -out for a new private credential file outside Git")
		}
		token, createErr := store.CreateSession(*path, *space)
		if createErr != nil {
			return createErr
		}
		if err := authority.WriteCredential(*destination, token); err != nil {
			_ = store.RevokeSession(token)
			return err
		}
		result = map[string]string{"status": "session created; expires in 24 hours", "credential_file": *destination}
	case "context", "revoke":
		token, readErr := authority.ReadCredential(*credential)
		if readErr != nil {
			return readErr
		}
		if command == "context" {
			result, err = store.Authenticate(token, *path)
		} else {
			err = store.RevokeSession(token)
			result = map[string]string{"status": "session revoked"}
		}
	}
	if err != nil {
		return err
	}
	if err := json.NewEncoder(out).Encode(result); err != nil {
		return fmt.Errorf("writing command result: %w", err)
	}
	return nil
}
