package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testAuth(t *testing.T) *Auth {
	t.Helper()
	a, err := New(Config{
		ClientID:      "client",
		ClientSecret:  "secret",
		SessionSecret: "test-session-secret",
		AppURL:        "http://localhost:5173",
	}, func(context.Context, string, string, string, string, string) (Identity, error) {
		return Identity{ID: "user-1"}, nil
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestNewRequiresASessionSecret(t *testing.T) {
	_, err := New(Config{ClientID: "c", ClientSecret: "s", AppURL: "http://x"}, nil)
	if err == nil {
		t.Fatal("expected an error when SESSION_SECRET is missing")
	}
	if !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Errorf("error should name the missing variable, got: %v", err)
	}
}

func TestNewRefusesDevLoginInProduction(t *testing.T) {
	_, err := New(Config{
		ClientID: "c", ClientSecret: "s", SessionSecret: "x",
		AppURL: "http://x", Production: true, DevLogin: true,
	}, nil)
	if err == nil {
		t.Fatal("dev login must not be combinable with production")
	}
}

func TestSignedTokenRoundTrips(t *testing.T) {
	a := testAuth(t)

	token, err := a.sign(claims{UserID: "user-1", Expires: time.Now().Add(time.Hour).UnixMilli()})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	got := a.verify(token)
	if got == nil {
		t.Fatal("a freshly signed token should verify")
	}
	if got.UserID != "user-1" {
		t.Errorf("expected user-1, got %q", got.UserID)
	}
}

func TestTamperedPayloadIsRejected(t *testing.T) {
	a := testAuth(t)

	token, _ := a.sign(claims{UserID: "user-1", Expires: time.Now().Add(time.Hour).UnixMilli()})
	_, sig, _ := strings.Cut(token, ".")

	// Forge a payload claiming to be somebody else, keeping the old signature.
	forged, _ := a.sign(claims{UserID: "admin", Expires: time.Now().Add(time.Hour).UnixMilli()})
	forgedBody, _, _ := strings.Cut(forged, ".")

	if a.verify(forgedBody+"."+sig) != nil {
		t.Fatal("a payload swapped under an old signature must not verify")
	}
}

func TestTokenSignedWithAnotherSecretIsRejected(t *testing.T) {
	a := testAuth(t)

	other, err := New(Config{
		ClientID: "c", ClientSecret: "s",
		SessionSecret: "a-completely-different-secret",
		AppURL:        "http://localhost:5173",
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	token, _ := other.sign(claims{UserID: "user-1", Expires: time.Now().Add(time.Hour).UnixMilli()})
	if a.verify(token) != nil {
		t.Fatal("a token signed with a different secret must not verify")
	}
}

func TestExpiredTokenIsRejected(t *testing.T) {
	a := testAuth(t)

	token, _ := a.sign(claims{UserID: "user-1", Expires: time.Now().Add(-time.Minute).UnixMilli()})
	if a.verify(token) != nil {
		t.Fatal("an expired token must not verify")
	}
}

func TestMalformedTokensAreRejected(t *testing.T) {
	a := testAuth(t)

	for _, token := range []string{"", ".", "junk", "a.b", "no-dot-at-all", "....", "eyJ.", ".sig"} {
		if a.verify(token) != nil {
			t.Errorf("malformed token %q must not verify", token)
		}
	}
}

func TestInterceptorReadsTheSessionCookie(t *testing.T) {
	a := testAuth(t)
	token, _ := a.sign(claims{UserID: "user-42", Expires: time.Now().Add(time.Hour).UnixMilli()})

	if got := cookieValue("other=1; "+sessionCookie+"="+token+"; x=2", sessionCookie); got != token {
		t.Errorf("failed to pull the session cookie out of a multi-cookie header")
	}
	if got := cookieValue("other=1", sessionCookie); got != "" {
		t.Errorf("expected empty when the cookie is absent, got %q", got)
	}
}

func TestStartRedirectsToGoogleWithState(t *testing.T) {
	a := testAuth(t)

	req := httptest.NewRequest("GET", "/auth/google/start", nil)
	rec := httptest.NewRecorder()
	a.start(rec, req)

	if rec.Code != 302 {
		t.Fatalf("expected a redirect, got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, googleAuthURL) {
		t.Errorf("should redirect to Google, got %q", location)
	}
	for _, want := range []string{"client_id=client", "state=", "redirect_uri="} {
		if !strings.Contains(location, want) {
			t.Errorf("redirect is missing %q: %s", want, location)
		}
	}

	// The state must also be set as a cookie, or the callback has nothing to
	// compare against.
	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthCookie {
			found = true
			if !c.HttpOnly {
				t.Error("the state cookie must be HttpOnly")
			}
		}
	}
	if !found {
		t.Error("start did not set the oauth state cookie")
	}
}

func TestCallbackRejectsAMismatchedState(t *testing.T) {
	a := testAuth(t)

	// A validly signed state, but for a different nonce than the query carries.
	state, _ := a.sign(claims{Nonce: "the-real-nonce", Provider: "google", Expires: time.Now().Add(time.Minute).UnixMilli()})

	req := httptest.NewRequest("GET", "/auth/google/callback?code=abc&state=an-attackers-nonce", nil)
	req.AddCookie(&http.Cookie{Name: oauthCookie, Value: state})
	rec := httptest.NewRecorder()
	a.callback(rec, req)

	if got := rec.Header().Get("Location"); !strings.Contains(got, "auth_error=bad_state") {
		t.Errorf("expected a bad_state redirect, got %q", got)
	}
}

func TestCallbackRejectsAMissingState(t *testing.T) {
	a := testAuth(t)

	req := httptest.NewRequest("GET", "/auth/google/callback?code=abc&state=whatever", nil)
	rec := httptest.NewRecorder()
	a.callback(rec, req)

	if got := rec.Header().Get("Location"); !strings.Contains(got, "auth_error=missing_state") {
		t.Errorf("expected a missing_state redirect, got %q", got)
	}
}

func TestRedirectURIMatchesTheRegisteredShape(t *testing.T) {
	a := testAuth(t)
	if got := a.redirectURI(); got != "http://localhost:5173/auth/google/callback" {
		t.Errorf("unexpected redirect URI: %s", got)
	}
}

func TestUnregisteredAuthPathsAre404(t *testing.T) {
	// With DevLogin off, /auth/dev/login must not quietly fall through to the
	// SPA and answer 200 — that reads like an exposed backdoor.
	a := testAuth(t)
	mux := http.NewServeMux()
	a.Routes(mux)

	for _, path := range []string{"/auth/dev/login", "/auth/nonsense", "/auth/"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 404 {
			t.Errorf("%s: expected 404, got %d", path, rec.Code)
		}
	}
}

func TestDevLoginRouteExistsWhenEnabled(t *testing.T) {
	a, err := New(Config{
		ClientID: "c", ClientSecret: "s", SessionSecret: "x",
		AppURL: "http://localhost:5173", DevLogin: true,
	}, func(context.Context, string, string, string, string, string) (Identity, error) {
		return Identity{ID: "user-1"}, nil
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	mux := http.NewServeMux()
	a.Routes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/auth/dev/login?name=Bill", nil))

	if rec.Code != 302 {
		t.Fatalf("expected a redirect after dev login, got %d", rec.Code)
	}
	var session bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			session = true
		}
	}
	if !session {
		t.Error("dev login should set a session cookie")
	}
}

func TestSafeNextRejectsOpenRedirects(t *testing.T) {
	// This value lands in a Location header after a genuine Google consent
	// screen, so anything that escapes the origin is a phishing primitive.
	hostile := []string{
		"//evil.com",
		"//evil.com/path",
		"https://evil.com",
		"http://evil.com",
		"/\\evil.com",
		"/\\/evil.com",
		"javascript:alert(1)",
		"evil.com",
		"/path\r\nLocation: https://evil.com",
		"/path\nSet-Cookie: x=y",
		"//evil.com/redirect?to=https://evil.com",
	}
	for _, next := range hostile {
		if got := safeNext(next); got != "/" {
			t.Errorf("safeNext(%q) = %q — must fall back to /", next, got)
		}
	}
}

func TestSafeNextKeepsSameOriginPaths(t *testing.T) {
	for _, next := range []string{"/", "/join/ABC123", "/match/1?x=2", "/a/b/c"} {
		if got := safeNext(next); got != next {
			t.Errorf("safeNext(%q) = %q — a same-origin path should survive", next, got)
		}
	}
}

func TestSafeNextRejectsOverlyLongValues(t *testing.T) {
	if got := safeNext("/" + strings.Repeat("a", 600)); got != "/" {
		t.Errorf("expected fallback for an over-long path, got %q", got)
	}
}

func TestStartCarriesNextInsideTheSignedState(t *testing.T) {
	a := testAuth(t)

	req := httptest.NewRequest("GET", "/auth/google/start?next=/join/ABC123", nil)
	rec := httptest.NewRecorder()
	a.start(rec, req)

	var state string
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthCookie {
			state = c.Value
		}
	}
	claims := a.verify(state)
	if claims == nil {
		t.Fatal("state cookie did not verify")
	}
	if claims.Next != "/join/ABC123" {
		t.Errorf("expected the destination in the signed state, got %q", claims.Next)
	}
	// It must NOT ride in the query string to Google, where it could be edited.
	if strings.Contains(rec.Header().Get("Location"), "join/ABC123") {
		t.Error("the destination must not travel in the Google redirect URL")
	}
}

func TestStartSanitizesAHostileNext(t *testing.T) {
	a := testAuth(t)

	req := httptest.NewRequest("GET", "/auth/google/start?next=//evil.com", nil)
	rec := httptest.NewRecorder()
	a.start(rec, req)

	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthCookie {
			if got := a.verify(c.Value); got == nil || got.Next != "/" {
				t.Errorf("hostile next was not sanitized: %+v", got)
			}
		}
	}
}
