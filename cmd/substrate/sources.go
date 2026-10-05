package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/agentic-substrate/substrate/internal/node"
)

func runSource(args []string, out io.Writer) error {
	defaultDir, err := defaultStateDir()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dir := flags.String("state-dir", defaultDir, "private owner state directory outside Git")
	path := flags.String("path", ".", "explicitly registered checkout")
	id := flags.String("artifact", "", "artifact identity within the registered owner context")
	var registration artifacts.Registration
	var approval artifacts.Approval
	var action, decision, proposal, revision, destination, outputPath, reviewCredential string
	var expected, operation string
	switch args[0] {
	case "publication-policy":
		flags.StringVar(&action, "action", "", "export or publish")
		flags.StringVar(&decision, "decision", "", "allow or deny under the local owner's policy")
	case "review-grant":
		flags.StringVar(&proposal, "proposal", "", "exact source-scoped publication proposal")
		flags.StringVar(&revision, "revision", "", "exact proposal revision")
		flags.StringVar(&destination, "destination-path", "", "registered Personal destination checkout")
		flags.StringVar(&outputPath, "out", "", "new private review credential file outside Git")
	case "review-revoke":
		flags.StringVar(&reviewCredential, "review-credential", "", "private dedicated review credential file")
	case "source-register":
		flags.StringVar(&registration.Source, "source", "", "source namespace within this repository")
		flags.StringVar(&registration.Name, "name", "", "stable artifact name")
		flags.StringVar(&registration.Alias, "alias", "", "optional explicit delivery alias")
		flags.BoolVar(&registration.Overridable, "overridable", false, "permit an explicitly approved specialization")
	case "approve":
		flags.BoolVar(&approval.ResolveConflict, "resolve-conflict", false, "explicitly review this candidate against the current head and approve a fresh resolution")
		flags.StringVar(&approval.OperationID, "operation", "", "stable approval operation ID")
		flags.StringVar(&approval.RevisionID, "revision", "", "exact candidate revision to approve")
		flags.StringVar(&approval.ExpectedRevision, "expected", "", "expected selected head; empty for first approval")
		flags.StringVar(&approval.Overrides, "overrides", "", "registered overridable default artifact")
		flags.StringVar(&approval.OverrideRevision, "override-revision", "", "exact selected default revision")
	case "restore-artifact":
		flags.StringVar(&expected, "expected", "", "expected retired head revision")
		flags.StringVar(&operation, "operation", "", "stable restoration operation ID")
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
	lock, err := node.Lock(auth)
	if err != nil {
		return err
	}
	defer lock.Close()
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
	switch args[0] {
	case "publication-policy":
		if decision != "allow" && decision != "deny" {
			return errors.New("publication policy requires explicit -decision allow or deny")
		}
		err = owner.SetPublicationPolicy(action, *id, decision == "allow")
		result = map[string]string{"status": "publication policy updated"}
	case "review-grant":
		if outputPath == "" {
			return errors.New("review grant requires -out for a new private credential file")
		}
		var token string
		token, err = owner.IssuePublicationReview(proposal, revision, destination)
		if err == nil {
			if writeErr := authority.WriteCredential(outputPath, token); writeErr != nil {
				_ = owner.RevokePublicationReview(token)
				return writeErr
			}
		}
		result = map[string]string{"status": "review credential issued; expires in fifteen minutes", "credential_file": outputPath}
	case "review-revoke":
		var token string
		token, err = authority.ReadCredential(reviewCredential)
		if err == nil {
			err = owner.RevokePublicationReview(token)
		}
		result = map[string]string{"status": "review credential revoked"}
	case "source-register":
		registration.ArtifactID = *id
		result, err = owner.Register(registration)
	case "restore-artifact":
		result, err = owner.Restore(*id, expected, operation)
	default:
		approval.ArtifactID = *id
		result, err = owner.Approve(approval)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}
