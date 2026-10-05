package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"strings"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/agentic-substrate/substrate/internal/node"
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
	var identifiers, aliases, topics, related dependencyPaths
	var query, relatedTo string
	var limit int
	var revision string
	switch args[0] {
	case "capture", "propose":
		flags.Var(&identifiers, "identifier", "explicit exact search identifier; repeat up to 32 times")
		flags.Var(&aliases, "search-alias", "explicit search alias; does not approve a source")
		flags.Var(&topics, "topic", "explicit topic label")
		flags.Var(&related, "related", "related artifact ID within current scope")
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
	case "resolve-conflict":
		id = flags.String("id", "", "memory artifact to resolve")
		flags.StringVar(&revision, "revision", "", "exact existing revision to keep or choose")
		expected = flags.String("expected", "", "expected current revision")
		operation = flags.String("operation", "", "stable resolution operation ID")
	case "pending":
	case "search":
		flags.StringVar(&relatedTo, "related-to", "", "one-hop related results from this current permitted artifact")
		flags.StringVar(&query, "query", "", "lexical query, exact identifier, or explicit alias/topic")
		flags.IntVar(&limit, "limit", 20, "maximum results; 1 through 100")
	case "read":
		id = flags.String("id", "", "current artifact ID")
		flags.StringVar(&revision, "revision", "", "current revision ID")
		flags.StringVar(&selector, "selector", "", "exact qualified identity or explicit alias")
	case "choices":
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
	request := node.Request{Token: token, Checkout: *path, Action: args[0]}
	switch args[0] {
	case "capture":
		content, readErr := io.ReadAll(io.LimitReader(in, 1024*1024+1))
		if readErr != nil {
			return errors.New("observation unavailable: could not read standard input")
		}
		request.Action = "capture"
		request.Contribution = artifacts.Contribution{OperationID: *operation, ArtifactID: *id, ExpectedRevision: *expected, Kind: "memory", Content: string(content), Provenance: *provenance, Associations: artifacts.Associations{Identifiers: identifiers, Aliases: aliases, Topics: topics, Related: related}}
	case "propose":
		files := make([]artifacts.SourceFile, 0, len(dependencies))
		for _, path := range dependencies {
			files = append(files, artifacts.SourceFile{Path: path})
		}
		request.Action = "capture"
		request.Contribution = artifacts.Contribution{OperationID: *operation, ArtifactID: *id, ExpectedRevision: *expected, Kind: kind, Provenance: *provenance, Source: &artifacts.Source{Commit: commit, Path: file, Files: files}, Associations: artifacts.Associations{Identifiers: identifiers, Aliases: aliases, Topics: topics, Related: related}}
	case "choices":
		request.Read.Selector = selector
	case "read":
		request.Read = artifacts.ReadRequest{ArtifactID: *id, RevisionID: revision, Selector: selector}
	case "artifact":
		request.ID = *id
	case "retire":
		request.ID, request.Expected, request.Operation = *id, *expected, *operation
	case "resolve-conflict":
		request.Resolution = artifacts.Resolution{OperationID: *operation, ArtifactID: *id, RevisionID: revision, ExpectedRevision: *expected}
	case "pending":
	case "search":
		request.Search = artifacts.SearchRequest{Query: query, Limit: limit, RelatedTo: relatedTo}
	}
	response := node.Call(context.Background(), *dir, request)
	if response.Error != "" {
		return errors.New(response.Error)
	}
	_, err = out.Write(append(response.Result, '\n'))
	return err
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
