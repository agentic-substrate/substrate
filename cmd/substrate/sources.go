package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
)

func runSource(args []string, out io.Writer) error {
	defaultDir, err := defaultStateDir()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dir := flags.String("state-dir", defaultDir, "private owner state directory outside Git")
	path := flags.String("path", ".", "explicitly registered checkout")
	id := flags.String("artifact", "", "Git-backed artifact identity")
	var registration artifacts.Registration
	var approval artifacts.Approval
	switch args[0] {
	case "source-register":
		flags.StringVar(&registration.Source, "source", "", "source namespace within this repository")
		flags.StringVar(&registration.Name, "name", "", "stable artifact name")
		flags.StringVar(&registration.Alias, "alias", "", "optional explicit delivery alias")
		flags.BoolVar(&registration.Overridable, "overridable", false, "permit an explicitly approved specialization")
	case "approve":
		flags.StringVar(&approval.OperationID, "operation", "", "stable approval operation ID")
		flags.StringVar(&approval.RevisionID, "revision", "", "exact candidate revision to approve")
		flags.StringVar(&approval.ExpectedRevision, "expected", "", "expected selected head; empty for first approval")
		flags.StringVar(&approval.Overrides, "overrides", "", "registered overridable default artifact")
		flags.StringVar(&approval.OverrideRevision, "override-revision", "", "exact selected default revision")
	default:
		return errors.New("unsupported owner source command")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("owner source commands do not accept positional arguments")
	}
	auth := &authority.Store{Dir: *dir}
	if _, err := auth.OwnerContext(*path); err != nil {
		return err
	}
	store, err := artifacts.Open(auth)
	if err != nil {
		return err
	}
	defer store.Close()
	owner, err := store.Owner(*path)
	if err != nil {
		return err
	}
	var result any
	if args[0] == "source-register" {
		registration.ArtifactID = *id
		result, err = owner.Register(registration)
	} else {
		approval.ArtifactID = *id
		result, err = owner.Approve(approval)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}
