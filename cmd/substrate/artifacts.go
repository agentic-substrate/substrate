package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"strings"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
)

func runArtifact(args []string, in io.Reader, out io.Writer) error {
	defaultDir, err := defaultStateDir()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dir := flags.String("state-dir", defaultDir, "private local state directory outside Git")
	path := flags.String("path", ".", "explicitly registered checkout")
	credential := flags.String("credential", "", "owner-only session credential file outside Git")
	var id, expected, operation, provenance *string
	var kind, commit, file, selector string
	var dependencies dependencyPaths
	switch args[0] {
	case "capture", "propose":
		if args[0] == "propose" {
			flags.StringVar(&kind, "kind", "skill", "skill or agent-definition")
			flags.StringVar(&commit, "commit", "", "full immutable Git commit")
			flags.StringVar(&file, "file", "", "regular repository-relative main document")
			flags.Var(&dependencies, "dependency", "explicit same-commit dependency path; repeat up to 32 times")
		}
		id = flags.String("artifact", "", "artifact to revise; empty creates an observation")
		expected = flags.String("expected", "", "expected base revision for an edit")
		operation = flags.String("operation", "", "stable contribution ID, reused for identical retries")
		provenance = flags.String("provenance", "", "evidence/source claim for this unverified observation")
	case "artifact":
		id = flags.String("id", "", "artifact to inspect within current context")
	case "retire":
		id = flags.String("id", "", "artifact to retire")
		expected = flags.String("expected", "", "expected current revision")
		operation = flags.String("operation", "", "stable retirement operation ID")
	case "pending":
	case "choices", "read":
		flags.StringVar(&selector, "selector", "", "qualified identity or explicit alias; empty lists permitted choices")
	default:
		return errors.New("unsupported artifact command")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("artifact commands do not accept positional arguments")
	}
	token, err := authority.ReadCredential(*credential)
	if err != nil {
		return err
	}
	auth := &authority.Store{Dir: *dir}
	if _, err := auth.Authenticate(token, *path); err != nil {
		return err
	}
	store, err := artifacts.Open(auth)
	if err != nil {
		return err
	}
	defer store.Close()
	session, err := store.Session(token, *path)
	if err != nil {
		return err
	}
	var result any
	switch args[0] {
	case "capture":
		content, readErr := io.ReadAll(io.LimitReader(in, 1024*1024+1))
		if readErr != nil {
			return errors.New("observation unavailable: could not read standard input")
		}
		result, err = session.Contribute(artifacts.Contribution{OperationID: *operation, ArtifactID: *id, ExpectedRevision: *expected, Kind: "memory", Content: string(content), Provenance: *provenance})
	case "propose":
		files := make([]artifacts.SourceFile, 0, len(dependencies))
		for _, path := range dependencies {
			files = append(files, artifacts.SourceFile{Path: path})
		}
		result, err = session.Contribute(artifacts.Contribution{OperationID: *operation, ArtifactID: *id, ExpectedRevision: *expected, Kind: kind, Provenance: *provenance, Source: &artifacts.Source{Commit: commit, Path: file, Files: files}})
	case "choices":
		result, err = session.Choices(selector)
	case "read":
		result, err = session.Resolve(selector)
	case "artifact":
		result, err = session.Inspect(*id)
	case "retire":
		result, err = session.Retire(*id, *expected, *operation)
	case "pending":
		result, err = session.Pending()
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}

type dependencyPaths []string

func (p *dependencyPaths) String() string { return strings.Join(*p, ",") }
func (p *dependencyPaths) Set(path string) error {
	if len(*p) >= 32 {
		return errors.New("source bundles permit at most 32 explicit dependencies")
	}
	*p = append(*p, path)
	return nil
}
