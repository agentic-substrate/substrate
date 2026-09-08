package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/agentic-substrate/substrate/internal/importer"
	"github.com/spf13/cobra"
)

// reviewBodyPreview is the listing cap in runes. A reviewer who cannot see
// that a body was cut cannot decide from the listing (SYNC-5).
const reviewBodyPreview = 240

func newReviewCmd(d Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "List and decide queued review items",
	}
	cmd.AddCommand(newReviewListCmd(d), newReviewDecideCmd(d, "approve"), newReviewDecideCmd(d, "reject"))
	return cmd
}

func newReviewListCmd(d Deps) *cobra.Command {
	var f struct {
		server string
		token  string
		team   string
		status string
	}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show queued review items",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			api, err := newAPIClient(d, "review list", f.server, f.token)
			if err != nil {
				return err
			}
			q := url.Values{}
			if f.status != "" {
				q.Set("status", f.status)
			}
			if f.team != "" {
				q.Set("team", f.team)
			}
			path := "/v1/review"
			if enc := q.Encode(); enc != "" {
				path += "?" + enc
			}
			raw, err := api.get(cmd.Context(), path)
			if err != nil {
				return err
			}
			var parsed struct {
				Items []reviewItem `json:"items"`
			}
			if err := json.Unmarshal(raw, &parsed); err != nil {
				return &UserError{"review list: unreadable response", err.Error(), "check that --server points at a Substrate control plane"}
			}
			_, err = fmt.Fprint(d.stdout(), formatReviewList(parsed.Items, f.status))
			return err
		},
	}
	cmd.Flags().StringVar(&f.server, "server", "", "control plane base URL (default $SUBSTRATE_URL)")
	cmd.Flags().StringVar(&f.token, "token", "", "bearer token (default $SUBSTRATE_TOKEN)")
	cmd.Flags().StringVar(&f.team, "team", "", "team UUID filter")
	cmd.Flags().StringVar(&f.status, "status", "open", "review status filter")
	return cmd
}

// newReviewDecideCmd builds `approve` or `reject`. The decision is the verb,
// so there is no --decision to get wrong; --reason is required on reject
// because a rejection with no recorded reason is unanswerable later (GOV-2).
func newReviewDecideCmd(d Deps, verb string) *cobra.Command {
	var f struct {
		server   string
		token    string
		reason   string
		hostname string
		asKind   string
		yes      bool
	}
	decision := "approved"
	if verb == "reject" {
		decision = "rejected"
	}
	op := "review " + verb
	cmd := &cobra.Command{
		Use:   verb + " <review-id>",
		Short: "Record a " + decision + " decision on one review item (this commits)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if verb == "reject" && strings.TrimSpace(f.reason) == "" {
				return &UserError{
					What: op + ": no --reason",
					Why:  "a rejection with no recorded reason cannot be explained later",
					Next: `re-run with --reason "duplicate of the laptop copy"`,
				}
			}
			api, err := newAPIClient(d, op, f.server, f.token)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(d.stdout(), "%s %s\n", decision, id); err != nil {
				return err
			}
			if err := confirm(d, op, f.yes); err != nil {
				return err
			}
			body, err := json.Marshal(map[string]any{
				"decision": decision,
				"reason":   f.reason,
				"hostname": f.hostname,
				"as_kind":  f.asKind,
				// The wire protocol keeps both fields (see internal/rest).
				"dry_run": false,
				"commit":  true,
			})
			if err != nil {
				return fmt.Errorf("%s: %w", op, err)
			}
			raw, err := api.post(cmd.Context(), "/v1/review/"+url.PathEscape(id)+"/decide", body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(d.stdout(), formatDecideResponse(raw))
			return err
		},
	}
	cmd.Flags().StringVar(&f.server, "server", "", "control plane base URL (default $SUBSTRATE_URL)")
	cmd.Flags().StringVar(&f.token, "token", "", "bearer token (default $SUBSTRATE_TOKEN)")
	cmd.Flags().StringVar(&f.reason, "reason", "", "why this side (or the rejection) wins; required on reject")
	cmd.Flags().StringVar(&f.hostname, "hostname", "", "winning machine for an import_conflict")
	cmd.Flags().StringVar(&f.asKind, "as-kind", "", "instruction or preference (R15 kind flip)")
	cmd.Flags().BoolVar(&f.yes, "yes", false, "skip the confirmation prompt; required when stdin is not a terminal")
	return cmd
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

func formatDecideResponse(raw []byte) string {
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return string(raw)
	}
	var b strings.Builder
	for _, key := range []string{"id", "status", "decision", "hostname", "as_kind", "reason"} {
		writeDecideField(&b, parsed, key)
	}
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
