// Package auth is hand-rolled Google OAuth plus stateless signed-cookie
// sessions — no passport-equivalent, no external identity service. Ported from
// the pattern in sesh/server/auth.js.
//
// The shape: /auth/google/start sets a short-lived signed state cookie and
// redirects to Google. /auth/google/callback verifies that state, exchanges the
// code, reads the profile, upserts the player, and sets a session cookie. A
// Connect interceptor turns that cookie back into an identity on every RPC.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
)

const (
	sessionCookie = "scoreboard_session"
	oauthCookie   = "scoreboard_oauth"

	sessionMaxAge = 30 * 24 * time.Hour
	oauthMaxAge   = 10 * time.Minute
)

// Google's endpoints. Fixed URLs rather than a discovery document: they have
// not moved in a decade and one less startup fetch is one less failure mode.
const (
	googleAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL    = "https://oauth2.googleapis.com/token"
	googleUserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"
	// profile is included because this app shows names and faces on a
	// scoreboard; email alone would leave everyone anonymous.
	googleScope = "openid email profile"
)

// Identity is an authenticated player.
type Identity struct {
	ID          string
	Email       string
	DisplayName string
	AvatarURL   string
}

type contextKey struct{}

// FromContext returns the authenticated caller, if there is one.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(contextKey{}).(Identity)
	return id, ok
}

// Require returns the caller, or a Connect unauthenticated error. Every handler
// that touches player data starts with this.
func Require(ctx context.Context) (Identity, error) {
	id, ok := FromContext(ctx)
	if !ok || id.ID == "" {
		return Identity{}, connect.NewError(connect.CodeUnauthenticated, errors.New("sign in to continue"))
	}
	return id, nil
}

// Config configures the OAuth flow.
type Config struct {
	ClientID     string
	ClientSecret string
	// Signs session and state cookies. Changing it logs everybody out.
	SessionSecret string
	// The app's public origin, e.g. https://scoreboard-xyz.a.run.app. The
	// OAuth redirect URI is derived from it, so it must match exactly what is
	// registered in the Google console.
	AppURL string
	// Secure cookies and no dev login. Set this in production.
	Production bool
	// DevLogin enables GET /auth/dev/login, which mints a session without
	// Google. Refused when Production is set.
	DevLogin bool
}

// Lookup resolves a player from a provider identity, creating them on first
// sign-in. Implemented by the store.
type Lookup func(ctx context.Context, provider, providerID, email, name, avatarURL string) (Identity, error)

// Auth holds the configured OAuth flow.
type Auth struct {
	cfg    Config
	secret []byte
	lookup Lookup
	client *http.Client
}

// New validates the configuration and returns the flow.
func New(cfg Config, lookup Lookup) (*Auth, error) {
	if cfg.SessionSecret == "" {
		return nil, errors.New("auth: SESSION_SECRET is required — without it, sessions cannot be signed")
	}
	if cfg.AppURL == "" {
		return nil, errors.New("auth: APP_URL is required — the OAuth redirect URI is derived from it")
	}
	if cfg.Production && cfg.DevLogin {
		return nil, errors.New("auth: dev login cannot be enabled in production")
	}
	if !cfg.configured() && !cfg.DevLogin {
		return nil, errors.New("auth: set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET (or AUTH_DEV_LOGIN=true for local development)")
	}
	if cfg.DevLogin {
		log.Println("auth: DEV LOGIN enabled at /auth/dev/login — never run this in production.")
	}

	return &Auth{
		cfg:    cfg,
		secret: []byte(cfg.SessionSecret),
		lookup: lookup,
		client: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (c Config) configured() bool { return c.ClientID != "" && c.ClientSecret != "" }

// Configured reports whether Google sign-in can actually be offered.
func (a *Auth) Configured() bool { return a.cfg.configured() }

func (a *Auth) redirectURI() string {
	return strings.TrimSuffix(a.cfg.AppURL, "/") + "/auth/google/callback"
}

// ── Signed tokens ────────────────────────────────────────────────────────────

type claims struct {
	UserID   string `json:"uid,omitempty"`
	Nonce    string `json:"n,omitempty"`
	Provider string `json:"p,omitempty"`
	// Where to land after a successful sign-in. Travels inside the signed
	// state cookie rather than the query string, so it cannot be swapped
	// between leaving for Google and coming back.
	Next    string `json:"next,omitempty"`
	Expires int64  `json:"exp"`
}

// sign produces "<base64url payload>.<base64url hmac>".
func (a *Auth) sign(c claims) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(body))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return body + "." + sig, nil
}

// verify checks the signature and expiry, returning nil for anything invalid.
func (a *Auth) verify(token string) *claims {
	body, sig, ok := strings.Cut(token, ".")
	if !ok || body == "" || sig == "" {
		return nil
	}

	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(body))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	// Constant time, so a forged signature cannot be discovered byte by byte.
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil
	}
	var c claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil
	}
	if c.Expires > 0 && time.Now().UnixMilli() > c.Expires {
		return nil
	}
	return &c
}

// safeNext sanitizes a post-sign-in destination down to a same-origin path.
//
// This value ends up in a Location header, so anything less strict is an open
// redirect: an attacker sends "sign in and you will be sent to evil.com", the
// victim sees a genuine Google consent screen, and lands somewhere hostile
// already trusting the flow. Only a plain absolute path is allowed —
// protocol-relative ("//evil.com"), scheme-bearing, and backslash forms are all
// rejected rather than repaired.
func safeNext(next string) string {
	const fallback = "/"
	if next == "" || len(next) > 512 {
		return fallback
	}
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return fallback
	}
	if strings.ContainsAny(next, "\\\r\n") || strings.Contains(next, "://") {
		return fallback
	}
	return next
}

func nonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ── Cookies ──────────────────────────────────────────────────────────────────

func (a *Auth) setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		// Lax rather than Strict: the OAuth callback is a cross-site
		// navigation back from Google, and Strict would withhold the state
		// cookie exactly when it is needed.
		SameSite: http.SameSiteLaxMode,
		Secure:   a.cfg.Production,
		MaxAge:   int(maxAge.Seconds()),
	})
}

func (a *Auth) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.cfg.Production,
		MaxAge:   -1,
	})
}

func (a *Auth) setSession(w http.ResponseWriter, userID string) error {
	token, err := a.sign(claims{UserID: userID, Expires: time.Now().Add(sessionMaxAge).UnixMilli()})
	if err != nil {
		return err
	}
	a.setCookie(w, sessionCookie, token, sessionMaxAge)
	return nil
}

// sessionToken accepts the cookie or an Authorization: Bearer header. Both
// carry the same signed token, so a future native client needs no server
// changes.
func sessionToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// ── Connect interceptor ──────────────────────────────────────────────────────

// Interceptor resolves the session on every RPC. Requests without one pass
// through unauthenticated and are rejected by Require inside the handler, which
// leaves room for public RPCs later.
func (a *Auth) Interceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			token := ""
			if h := req.Header().Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			} else if raw := req.Header().Get("Cookie"); raw != "" {
				token = cookieValue(raw, sessionCookie)
			}
			if token == "" {
				return next(ctx, req)
			}
			if c := a.verify(token); c != nil && c.UserID != "" {
				ctx = context.WithValue(ctx, contextKey{}, Identity{ID: c.UserID})
			}
			return next(ctx, req)
		}
	}
}

// cookieValue pulls one cookie out of a raw Cookie header. Connect exposes
// headers rather than an *http.Request, so the standard parser is unavailable.
func cookieValue(header, name string) string {
	for _, part := range strings.Split(header, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && k == name {
			if decoded, err := url.QueryUnescape(v); err == nil {
				return decoded
			}
			return v
		}
	}
	return ""
}

// ── OAuth flow ───────────────────────────────────────────────────────────────

// Routes registers the auth endpoints on mux.
func (a *Auth) Routes(mux *http.ServeMux) {
	// Anything under /auth that is not handled below gets an honest 404.
	// Without this it would fall through to the SPA handler and answer 200 with
	// index.html — harmless, but a disabled /auth/dev/login answering 200 reads
	// like an exposed backdoor to anyone auditing this.
	mux.HandleFunc("/auth/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	mux.HandleFunc("/auth/google/start", a.start)
	mux.HandleFunc("/auth/google/callback", a.callback)
	mux.HandleFunc("/auth/logout", a.logout)
	if a.cfg.DevLogin {
		mux.HandleFunc("/auth/dev/login", a.devLogin)
	}
}

func (a *Auth) fail(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, a.cfg.AppURL+"/?auth_error="+url.QueryEscape(reason), http.StatusFound)
}

func (a *Auth) start(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.configured() {
		a.fail(w, r, "google_not_configured")
		return
	}

	n, err := nonce()
	if err != nil {
		a.fail(w, r, "server_error")
		return
	}
	state, err := a.sign(claims{
		Nonce:    n,
		Provider: "google",
		Next:     safeNext(r.URL.Query().Get("next")),
		Expires:  time.Now().Add(oauthMaxAge).UnixMilli(),
	})
	if err != nil {
		a.fail(w, r, "server_error")
		return
	}
	a.setCookie(w, oauthCookie, state, oauthMaxAge)

	params := url.Values{
		"client_id":     {a.cfg.ClientID},
		"redirect_uri":  {a.redirectURI()},
		"response_type": {"code"},
		"scope":         {googleScope},
		"state":         {n},
		"access_type":   {"online"},
		"prompt":        {"select_account"},
	}
	http.Redirect(w, r, googleAuthURL+"?"+params.Encode(), http.StatusFound)
}

func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		a.fail(w, r, e)
		return
	}

	// The state cookie is single use: clear it before doing anything else, so a
	// replayed callback cannot reuse it.
	raw, err := r.Cookie(oauthCookie)
	a.clearCookie(w, oauthCookie)
	if err != nil {
		a.fail(w, r, "missing_state")
		return
	}
	state := a.verify(raw.Value)
	if state == nil || state.Provider != "google" || state.Nonce == "" || state.Nonce != q.Get("state") {
		a.fail(w, r, "bad_state")
		return
	}

	code := q.Get("code")
	if code == "" {
		a.fail(w, r, "missing_code")
		return
	}

	accessToken, err := a.exchange(r.Context(), code)
	if err != nil {
		log.Printf("auth: token exchange failed: %v", err)
		a.fail(w, r, "token_exchange_failed")
		return
	}

	profile, err := a.profile(r.Context(), accessToken)
	if err != nil {
		log.Printf("auth: profile fetch failed: %v", err)
		a.fail(w, r, "no_profile")
		return
	}
	if profile.Sub == "" {
		a.fail(w, r, "no_profile")
		return
	}

	id, err := a.lookup(r.Context(), "google", profile.Sub, profile.Email, profile.displayName(), profile.Picture)
	if err != nil {
		log.Printf("auth: user lookup failed: %v", err)
		a.fail(w, r, "login_failed")
		return
	}
	if err := a.setSession(w, id.ID); err != nil {
		a.fail(w, r, "server_error")
		return
	}

	http.Redirect(w, r, a.cfg.AppURL+safeNext(state.Next), http.StatusFound)
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	a.clearCookie(w, sessionCookie)
	if r.Method == http.MethodGet {
		http.Redirect(w, r, a.cfg.AppURL+"/", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("ok"))
}

// devLogin mints a session without Google, for local development. Registered
// only when DevLogin is set, which New refuses to combine with Production.
func (a *Auth) devLogin(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = "Dev Player"
	}
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))

	id, err := a.lookup(r.Context(), "dev", "dev-"+slug, slug+"@dev.local", name, "")
	if err != nil {
		http.Error(w, "dev login failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.setSession(w, id.ID); err != nil {
		http.Error(w, "could not set session", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, a.cfg.AppURL+safeNext(r.URL.Query().Get("next")), http.StatusFound)
}

// ── Google calls ─────────────────────────────────────────────────────────────

func (a *Auth) exchange(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {a.cfg.ClientID},
		"client_secret": {a.cfg.ClientSecret},
		"redirect_uri":  {a.redirectURI()},
		"grant_type":    {"authorization_code"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	res, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google returned %d: %s", res.StatusCode, string(body))
	}

	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", errors.New("no access token in response")
	}
	return token.AccessToken, nil
}

type googleProfile struct {
	Sub     string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Given   string `json:"given_name"`
	Picture string `json:"picture"`
}

// displayName prefers the full name, then the given name, then the local part
// of the email — so nobody ends up on the scoreboard as a blank.
func (p googleProfile) displayName() string {
	if n := strings.TrimSpace(p.Name); n != "" {
		return n
	}
	if n := strings.TrimSpace(p.Given); n != "" {
		return n
	}
	if local, _, ok := strings.Cut(p.Email, "@"); ok {
		return local
	}
	return ""
}

func (a *Auth) profile(ctx context.Context, accessToken string) (googleProfile, error) {
	var p googleProfile

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleUserInfoURL, nil)
	if err != nil {
		return p, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	res, err := a.client.Do(req)
	if err != nil {
		return p, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return p, err
	}
	if res.StatusCode != http.StatusOK {
		return p, fmt.Errorf("google returned %d: %s", res.StatusCode, string(body))
	}
	return p, json.Unmarshal(body, &p)
}
