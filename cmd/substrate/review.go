package main

import (
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

func reviewGET(server, token, path string) ([]byte, error) {
	url := strings.TrimRight(server, "/") + path
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil) //nolint:gosec // G704: -server is the operator's control plane
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req) //nolint:gosec // G704: -server is the operator's control plane
	if err != nil {
		return nil, fmt.Errorf("review list: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("review list: read: %w", err)
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("review list: %s: %s", res.Status, body)
	}
	return body, nil
}
