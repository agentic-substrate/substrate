package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/agentic-substrate/substrate/internal/importer"
)

// reviewBodyPreview is the listing cap in runes. A reviewer who cannot see
// that a body was cut cannot decide from the listing (SYNC-5).
const reviewBodyPreview = 240

func reviewCmd(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	if len(args) == 0 {
		return fmt.Errorf("want list or decide subcommand")
	}
	switch args[0] {
	case "list":
		return reviewList(args[1:], stdout)
	case "decide":
		return reviewDecide(args[1:], stdout)
	default:
		return fmt.Errorf("unknown review command %q", args[0])
	}
}

func reviewList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("review list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	server := fs.String("server", os.Getenv("SUBSTRATE_URL"), "control plane base URL")
	token := fs.String("token", os.Getenv("SUBSTRATE_TOKEN"), "bearer token")
	team := fs.String("team", "", "team UUID filter")
	status := fs.String("status", "open", "review status filter")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *server == "" {
		return fmt.Errorf("review list: -server or SUBSTRATE_URL is required")
	}
	if *token == "" {
		return fmt.Errorf("review list: -token or SUBSTRATE_TOKEN is required")
	}
	q := url.Values{}
	if *status != "" {
		q.Set("status", *status)
	}
	if *team != "" {
		q.Set("team", *team)
	}
	path := "/v1/review"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	raw, err := reviewGET(*server, *token, path)
	if err != nil {
		return err
	}
	var parsed struct {
		Items []reviewItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("review list: parse: %w", err)
	}
	_, err = fmt.Fprint(stdout, formatReviewList(parsed.Items, *status))
	return err
}

type reviewItem struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Status  string          `json:"status"`
	TeamID  string          `json:"team_id,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

type conflictPayload struct {
	Slot     string                  `json:"slot"`
	Hostname string                  `json:"hostname"`
	Title    string                  `json:"title"`
	Diff     string                  `json:"diff"`
	Pair     []importer.ConflictSide `json:"pair"`
}

func formatReviewList(items []reviewItem, statusFilter string) string {
	if len(items) == 0 {
		label := statusFilter
		if label == "" {
			label = "review"
		}
		return fmt.Sprintf("0 %s review items\n", label)
	}
	var b strings.Builder
	for i, it := range items {
		if i > 0 {
			b.WriteByte('\n')
		}
		formatReviewItem(&b, it)
	}
	return b.String()
}

func formatReviewItem(b *strings.Builder, it reviewItem) {
	fmt.Fprintf(b, "id: %s\n", it.ID)
	fmt.Fprintf(b, "kind: %s\n", it.Kind)
	fmt.Fprintf(b, "status: %s\n", it.Status)
	var p conflictPayload
	if len(it.Payload) > 0 {
		_ = json.Unmarshal(it.Payload, &p)
	}
	if p.Slot != "" {
		fmt.Fprintf(b, "slot: %s\n", p.Slot)
	}
	if p.Title != "" {
		fmt.Fprintf(b, "title: %s\n", p.Title)
	}
	if p.Diff != "" {
		writePreview(b, "diff", "", p.Diff)
	}
	for _, side := range p.Pair {
		hosts := strings.Join(side.Hostnames, ",")
		if hosts == "" {
			hosts = "?"
		}
		kind := importer.Classify(importer.Block{
			Heading: headingOfReviewSlot(p.Slot),
			Body:    side.Body,
			Rel:     relOfReviewSlot(p.Slot),
		}).Kind
		writePreview(b, hosts, kind, side.Body)
	}
}

func writePreview(b *strings.Builder, label, kind, body string) {
	display := strings.ReplaceAll(body, "\n", " ")
	shown, truncated := truncateRunes(display, reviewBodyPreview)
	if kind != "" {
		fmt.Fprintf(b, "  %s (%s): %s", label, kind, shown)
	} else {
		fmt.Fprintf(b, "  %s: %s", label, shown)
	}
	if truncated {
		fmt.Fprintf(b, "…\n    (truncated, showed %d of %d bytes)\n", len(shown), len(display))
	} else {
		b.WriteByte('\n')
	}
}

func truncateRunes(s string, n int) (string, bool) {
	if utf8.RuneCountInString(s) <= n {
		return s, false
	}
	runes := []rune(s)
	return string(runes[:n]), true
}

func headingOfReviewSlot(slot string) string {
	_, h, ok := strings.Cut(slot, "#")
	if !ok {
		return slot
	}
	return h
}

func relOfReviewSlot(slot string) string {
	rel, _, ok := strings.Cut(slot, "#")
	if !ok {
		return slot
	}
	return rel
}

func reviewDecide(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("review decide", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	server := fs.String("server", os.Getenv("SUBSTRATE_URL"), "control plane base URL")
	token := fs.String("token", os.Getenv("SUBSTRATE_TOKEN"), "bearer token")
	decision := fs.String("decision", "", "approved or rejected")
	reason := fs.String("reason", "", "why this side (or rejection) wins")
	hostname := fs.String("hostname", "", "winning machine for an import_conflict")
	asKind := fs.String("as-kind", "", "instruction or preference (R15 kind flip)")
	dryRun := fs.Bool("dry-run", false, "plan the apply; write nothing (default unless -commit)")
	commit := fs.Bool("commit", false, "perform the decide write")
	id := ""
	flagArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id = args[0]
		flagArgs = args[1:]
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	switch {
	case id != "" && fs.NArg() == 0:
	case id == "" && fs.NArg() == 1:
		id = fs.Arg(0)
	default:
		return fmt.Errorf("review decide: want one review id")
	}
	if *server == "" {
		return fmt.Errorf("review decide: -server or SUBSTRATE_URL is required")
	}
	if *token == "" {
		return fmt.Errorf("review decide: -token or SUBSTRATE_TOKEN is required")
	}
	if *decision == "" {
		return fmt.Errorf("review decide: -decision is required")
	}
	if strings.TrimSpace(*reason) == "" {
		return fmt.Errorf("review decide: -reason is required")
	}
	if *dryRun && *commit {
		return fmt.Errorf("review decide: -dry-run and -commit cannot both be set")
	}
	doCommit := *commit && !*dryRun
	body, err := json.Marshal(map[string]any{
		"decision": *decision,
		"reason":   *reason,
		"hostname": *hostname,
		"as_kind":  *asKind,
		"dry_run":  !doCommit,
		"commit":   doCommit,
	})
	if err != nil {
		return err
	}
	path := "/v1/review/" + id + "/decide"
	raw, err := reviewPOST(*server, *token, path, body)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(stdout, formatDecideResponse(raw, !doCommit))
	return err
}

func formatDecideResponse(raw []byte, dryRun bool) string {
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return string(raw)
	}
	var b strings.Builder
	if dryRun {
		b.WriteString("dry-run\n")
	}
	writeDecideField(&b, parsed, "id")
	writeDecideField(&b, parsed, "status")
	writeDecideField(&b, parsed, "decision")
	writeDecideField(&b, parsed, "hostname")
	writeDecideField(&b, parsed, "as_kind")
	writeDecideField(&b, parsed, "reason")
	writeDecideRows(&b, parsed, "activate")
	writeDecideRows(&b, parsed, "retire")
	if b.Len() == 0 {
		return string(raw)
	}
	return b.String()
}

func writeDecideField(b *strings.Builder, parsed map[string]any, key string) {
	v, ok := parsed[key]
	if !ok || v == nil {
		return
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" || s == "<nil>" {
		return
	}
	fmt.Fprintf(b, "%s: %s\n", key, s)
}

func writeDecideRows(b *strings.Builder, parsed map[string]any, key string) {
	raw, ok := parsed[key]
	if !ok {
		return
	}
	rows, ok := raw.([]any)
	if !ok {
		return
	}
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := m["kind"].(string)
		body, _ := m["body"].(string)
		fmt.Fprintf(b, "%s: %s %s\n", key, kind, strings.ReplaceAll(body, "\n", " "))
	}
}

func reviewGET(server, token, path string) ([]byte, error) {
	return reviewRequest(http.MethodGet, server, token, path, nil, "review list")
}

func reviewPOST(server, token, path string, body []byte) ([]byte, error) {
	return reviewRequest(http.MethodPost, server, token, path, body, "review decide")
}

func reviewRequest(method, server, token, path string, body []byte, op string) ([]byte, error) {
	u := strings.TrimRight(server, "/") + path
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, u, rdr) //nolint:gosec // G704: -server is the operator's control plane
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req) //nolint:gosec // G704: -server is the operator's control plane
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer func() { _ = res.Body.Close() }()
	resp, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read: %w", op, err)
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("%s: %s: %s", op, res.Status, resp)
	}
	return resp, nil
}
