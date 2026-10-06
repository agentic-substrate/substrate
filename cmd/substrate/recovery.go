package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"

	"github.com/agentic-substrate/substrate/internal/recovery"
)

func runRecovery(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("recovery requires backup or restore")
	}
	command := args[0]
	defaultDir, err := defaultStateDir()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	stateDir := flags.String("state-dir", defaultDir, "private local state directory outside Git")
	var backup, destination *string
	switch command {
	case "backup":
		destination = flags.String("out", "", "unused backup directory outside Git")
	case "restore":
		backup = flags.String("backup", "", "private complete backup directory")
	default:
		return errors.New("unknown recovery command")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("recovery requires explicit flags and accepts no positional arguments")
	}
	switch command {
	case "backup":
		if *destination == "" {
			return errors.New("backup requires -out for an unused backup directory outside Git")
		}
		err = recovery.Backup(*stateDir, *destination)
	case "restore":
		explicitDestination := false
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "state-dir" {
				explicitDestination = true
			}
		})
		if *backup == "" || !explicitDestination {
			return errors.New("restore requires -backup and an unused explicit -state-dir destination")
		}
		err = recovery.Restore(*backup, *stateDir)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(map[string]string{"status": command + " complete"})
}
