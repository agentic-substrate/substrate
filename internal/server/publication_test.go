package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/agentic-substrate/substrate/internal/node"
)

func TestBrowserArtifactScopeOriginAndStrictPublicationBody(t *testing.T) {
	home := t.TempDir()
	auth := &authority.Store{Dir: filepath.Join(home, "state")}
	if err := auth.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	if err := auth.CreateSpace("Work"); err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	contexts := map[string]authority.Context{}
	for _, space := range []string{"Work", "Personal"} {
		root := filepath.Join(home, space)
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git %v %s", err, out)
		}
		if _, err := auth.Register(root, space); err != nil {
			t.Fatal(err)
		}
		token, err := auth.CreateSession(root, space)
		if err != nil {
			t.Fatal(err)
		}
		tokens[space] = token
		contexts[space], err = auth.Authenticate(token, root)
		if err != nil {
			t.Fatal(err)
		}
	}
	runtime, err := node.Start(auth, new(true))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	response := node.Call(context.Background(), auth.Dir, node.Request{Action: "capture", Token: tokens["Work"], Contribution: artifacts.Contribution{OperationID: "save", Kind: "memory", Content: "Private customer source", Provenance: "private"}})
	if response.Error != "" {
		t.Fatal(response.Error)
	}
	var source artifacts.Receipt
	if json.Unmarshal(response.Result, &source) != nil {
		t.Fatal("receipt")
	}
	handler := New(fstest.MapFS{}, Options{Authority: auth, Host: "127.0.0.1:9842"})
	request := func(method, path, token, body string, headers http.Header) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://127.0.0.1:9842"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if method == "POST" {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "http://127.0.0.1:9842")
		}
		for name, values := range headers {
			req.Header[name] = values
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	w := request("GET", "/api/artifacts", tokens["Work"], "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), source.ArtifactID) {
		t.Fatalf("inventory unavailable: %d %s", w.Code, w.Body.String())
	}
	w = request("GET", "/api/artifacts", tokens["Personal"], "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), source.ArtifactID) || strings.Contains(w.Body.String(), "Private") {
		t.Fatalf("Personal inventory leaked: %d %s", w.Code, w.Body.String())
	}
	known := request("GET", "/api/artifacts/"+source.ArtifactID, tokens["Personal"], "", nil)
	missing := request("GET", "/api/artifacts/"+strings.Repeat("a", 64), tokens["Personal"], "", nil)
	if known.Code != 403 || missing.Code != 403 || known.Body.String() != missing.Body.String() {
		t.Fatalf("missing/hidden differ: %d %q %d %q", known.Code, known.Body.String(), missing.Code, missing.Body.String())
	}
	for _, headers := range []http.Header{{"Origin": {"https://evil.invalid"}}, {"Origin": {"http://127.0.0.1:9842", "https://evil.invalid"}}, {"Sec-Fetch-Site": {"cross-site"}}, {"Sec-Fetch-Site": {"same-origin", "cross-site"}}, {"Authorization": {"Bearer " + tokens["Work"], "Bearer forged"}}} {
		if w := request("GET", "/api/artifacts", tokens["Work"], "", headers); w.Code != 403 {
			t.Fatalf("hostile headers accepted: %+v %d", headers, w.Code)
		}
	}
	for _, body := range []string{`{"approved":true}`, `{"content":"\ud800"}`, "{\"content\":\"\xff\"}", `{} {}`, strings.Repeat("x", 2*1024*1024+1)} {
		if w := request("POST", "/api/publications", tokens["Work"], body, nil); w.Code != 400 {
			t.Fatalf("malformed publication accepted: %d %s", w.Code, w.Body.String())
		}
	}
	in := artifacts.PublicationInput{OperationID: "draft", Content: "General lesson", Sources: []artifacts.PublicationSource{{ArtifactID: source.ArtifactID, RevisionID: source.RevisionID}}, Destination: artifacts.PublicationDestination{SpaceID: contexts["Personal"].SpaceID, RepositoryID: contexts["Personal"].RepositoryID}}
	data, _ := json.Marshal(in)
	if w := request("POST", "/api/publications", tokens["Work"], string(data), nil); w.Code != 403 {
		t.Fatalf("mandatory export bypassed: %d %s", w.Code, w.Body.String())
	}
	if w := request("POST", "/api/publication-review", tokens["Work"], `{"operation_id":"approval","revision_id":"pretend","snapshot":"pretend"}`, nil); w.Code != 403 {
		t.Fatalf("agent approved publication: %d %s", w.Code, w.Body.String())
	}
	if w := request("POST", "/api/publications", tokens["Work"], string(data), http.Header{"Origin": nil}); w.Code != 403 {
		t.Fatalf("missing mutation Origin accepted: %d", w.Code)
	}
}
