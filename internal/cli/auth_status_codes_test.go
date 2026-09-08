package cli

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/cutover"
)

func statusServer(t *testing.T, code int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func loginError(t *testing.T, code int) *UserError {
	t.Helper()
	srv := statusServer(t, code)
	err := validateToken(t.Context(), Deps{HTTP: srv.Client()}, srv.URL, fakeToken)
	var ue *UserError
	if err == nil {
		t.Fatalf("validateToken accepted a %d response", code)
	}
	if !errors.As(err, &ue) {
		t.Fatalf("validateToken(%d) returned %v, want a UserError", code, err)
	}
	return ue
}

// Turn red by folding 403 back in with 401: rest.go maps every policy denial
// and every RLS refusal to 403, so a valid brand-new token whose scope denies
// would be called invalid and the operator sent to mint another forever.
func TestValidateTokenSeparates401From403(t *testing.T) {
	unauth := loginError(t, http.StatusUnauthorized)
	denied := loginError(t, http.StatusForbidden)
	if !strings.Contains(unauth.Next, "admin create-user") {
		t.Fatalf("401 advice does not tell the operator to mint a token: %q", unauth.Next)
	}
	if strings.Contains(denied.Next, "admin create-user") {
		t.Fatalf("403 advice tells the operator to mint a token, but the token authenticated: %q", denied.Next)
	}
	if !strings.Contains(denied.What, "denied") || strings.Contains(denied.What, "rejected that token") {
		t.Fatalf("403 does not read as a permissions problem: %q", denied.What)
	}
	if unauth.Why == denied.Why {
		t.Fatalf("401 and 403 share the same explanation: %q", unauth.Why)
	}
}

// Turn red by letting 503 fall through to the generic >=400 branch: a login
// during a database outage then reads as the server rejecting the credential,
// which it never even looked at.
func TestValidateTokenTreats503AsUnavailable(t *testing.T) {
	ue := loginError(t, http.StatusServiceUnavailable)
	if strings.Contains(ue.What, "rejected") {
		t.Fatalf("503 reads as a rejection: %q", ue.What)
	}
	if !strings.Contains(ue.Why, "not checked") {
		t.Fatalf("503 does not say the token went unvalidated: %q", ue.Why)
	}
}

// Turn red by dropping the doctor server check's UserError unwrapping: the
// 403 permissions advice would be replaced by "run auth login again".
func TestDoctorCarriesThe403Advice(t *testing.T) {
	srv := statusServer(t, http.StatusForbidden)
	c := doctorServerCheck(NewRoot(Deps{ConfigDir: t.TempDir()}), Deps{HTTP: srv.Client()},
		Config{Server: srv.URL, Token: fakeToken}, nil)
	if c.Ok {
		t.Fatal("doctor called a 403 healthy")
	}
	if strings.Contains(c.Fix, "admin create-user") {
		t.Fatalf("doctor tells a denied principal to mint a new token: %q", c.Fix)
	}
}

// Turn red by removing the nil fallback from Deps.installer: a command that
// reaches for the installer would nil-panic instead of using the OS one, and
// each sibling command would invent its own default.
func TestDepsInstallerFallsBackToTheOSInstaller(t *testing.T) {
	if got := (Deps{}).installer(); got == nil {
		t.Fatal("Deps{}.installer() is nil, want the OS installer")
	}
	fake := &cutover.FakeInstaller{}
	if got := (Deps{Installer: fake}).installer(); got != fake {
		t.Fatalf("Deps.installer() = %v, want the injected installer", got)
	}
}
