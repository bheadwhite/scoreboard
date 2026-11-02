// Command server runs the Scoreboard API and serves the built SPA.
//
// In production the Go binary serves both, so everything is same-origin and
// CORS never enters the picture. In development the Vite dev server proxies to
// this process instead.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/bheadwhite/scoreboard/backend/gen/scoreboard/v1/scoreboardv1connect"
	"github.com/bheadwhite/scoreboard/backend/internal/auth"
	"github.com/bheadwhite/scoreboard/backend/internal/config"
	"github.com/bheadwhite/scoreboard/backend/internal/events"
	"github.com/bheadwhite/scoreboard/backend/internal/service"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
	"github.com/bheadwhite/scoreboard/backend/migrations"
)

func main() {
	config.LoadDotEnv()

	ctx := context.Background()

	databaseURL := config.Env("DATABASE_URL", "")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	st, err := store.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer st.Close()

	if err := migrations.Apply(ctx, st.Pool()); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	log.Println("migrations applied")

	production := config.Env("NODE_ENV", "") == "production" || config.Env("K_SERVICE", "") != ""

	// What the one family is called in the database. Nothing shows it — the app
	// stopped naming families when it stopped having more than one — so it only
	// matters to whoever reads the tables or runs cmd/consolidate.
	familyName := config.Env("FAMILY_NAME", "Gauntlet")

	authn, err := auth.New(auth.Config{
		ClientID:      config.Env("GOOGLE_CLIENT_ID", config.Env("GCP_OAUTH_CLIENT", "")),
		ClientSecret:  config.Env("GOOGLE_CLIENT_SECRET", config.Env("GCP_OAUTH_SECRET", "")),
		SessionSecret: config.Env("SESSION_SECRET", ""),
		AppURL:        config.Env("APP_URL", "http://localhost:3000"),
		Production:    production,
		DevLogin:      config.Env("AUTH_DEV_LOGIN", "") == "true",
	}, func(ctx context.Context, provider, providerID, email, name, avatarURL string) (auth.Identity, error) {
		u, err := st.UpsertOAuthUser(ctx, provider, providerID, email, name, avatarURL)
		if err != nil {
			return auth.Identity{}, err
		}
		// Signing in is joining. There is one family, the UI never asks which,
		// and this is the only place that decides — so a player is never
		// sitting in the app with no scoreboard to look at. On an empty
		// database the first person through mints the family and owns it, which
		// is the only time FAMILY_NAME is read.
		if _, err := st.EnsureDefaultMembership(ctx, u.ID, familyName); err != nil {
			return auth.Identity{}, err
		}
		return auth.Identity{ID: u.ID, Email: email, DisplayName: u.DisplayName, AvatarURL: u.AvatarURL}, nil
	})
	if err != nil {
		log.Fatalf("auth: %v", err)
	}

	// Where mutations announce themselves and open browsers listen. In process
	// on purpose — see internal/events.
	hub := events.NewHub()

	// Who may erase a match from the permanent record. Empty by default, and
	// empty means nobody — granting it takes a deploy, which is the point. The
	// count is logged and the addresses are not; that a deployment has two
	// administrators is operational, who they are is not.
	admins := auth.NewAdmins(config.Env("ADMIN_EMAILS", ""))
	if len(admins) == 0 {
		log.Println("no administrators configured — no match can be voided (set ADMIN_EMAILS)")
	} else {
		log.Printf("administrators: %d", len(admins))
	}

	opts := []connect.HandlerOption{connect.WithInterceptors(authn.Interceptor())}

	api := http.NewServeMux()
	api.Handle(scoreboardv1connect.NewUserServiceHandler(service.NewUserService(st, hub, admins), opts...))
	api.Handle(scoreboardv1connect.NewFamilyServiceHandler(service.NewFamilyService(st, hub), opts...))
	api.Handle(scoreboardv1connect.NewGameServiceHandler(service.NewGameService(st, hub), opts...))
	api.Handle(scoreboardv1connect.NewMatchServiceHandler(service.NewMatchService(st, hub, admins), opts...))
	api.Handle(scoreboardv1connect.NewStatsServiceHandler(service.NewStatsService(st, hub, admins), opts...))

	if !authn.Configured() {
		log.Println("warning: Google sign-in is not configured — set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET")
	}

	root := http.NewServeMux()
	root.Handle("/api/", http.StripPrefix("/api", api))
	// /health is the one to call. Google Front End reserves /healthz on
	// *.run.app and answers it with its own 404 before the request reaches the
	// container, so that path is unusable in production even though it works
	// locally. Both are registered so local tooling pointed at either still
	// works; only /health is reachable once deployed.
	root.HandleFunc("/health", health(st))
	root.HandleFunc("/healthz", health(st))
	root.HandleFunc("/tasks/sweep", sweep(st, config.Env("SWEEP_TOKEN", "")))
	// Not a Connect route: the browser consumes this with EventSource, which
	// needs a plain streaming HTTP response.
	root.HandleFunc("/events", service.Events(st, hub, authn))
	root.HandleFunc("/config.json", frontendConfig(
		authn.Configured(),
		config.Env("AUTH_DEV_LOGIN", "") == "true",
	))
	authn.Routes(root)
	root.Handle("/", spaHandler(config.Env("STATIC_DIR", "../frontend/dist")))

	port := config.Env("PORT", "8080")
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           h2c.NewHandler(root, &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server: %v", err)
	}
}

// health reports whether the process can actually reach the database, so a
// green check means something. It must be a registered route: an unregistered
// path falls through to the SPA handler and returns 200 with index.html, which
// would look like a passing health check while testing nothing at all.
func health(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		if err := st.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}
}

// sweep keeps the database awake. Cloud Scheduler calls it every few minutes so
// a free-tier Supabase project is not paused for inactivity — a real risk for
// an app that goes quiet between family gatherings.
//
// It used to have a second job, and the second job was the reason it existed:
// finalizing claims whose week had run out. Nothing ripens on a clock any more,
// so there is nothing to settle and no match anywhere whose outcome depends on
// this endpoint being called. What is left is the request itself.
//
// Kept as a POST behind the same token rather than folded into /health, because
// the schedule, the alerting and the Cloud Scheduler job are all pointed here
// and moving them buys nothing. If the free tier stops needing a heartbeat,
// this can go entirely.
func sweep(st *store.Store, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if token == "" {
			http.Error(w, "sweeping is not configured", http.StatusNotFound)
			return
		}

		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		if err := st.Ping(ctx); err != nil {
			log.Printf("sweep: %v", err)
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}
}

// frontendConfig tells the browser which sign-in options actually work.
//
// An endpoint rather than a value templated into index.html: under the Vite dev
// server the Go binary never touches index.html, so a templated value is absent
// exactly when a developer is looking at it, and the frontend has to guess.
// Guessing produced a sign-in screen offering a Google button next to an error
// saying Google was not configured. Asking the server is right everywhere.
//
// These are capability flags, not credentials, so no authentication is needed.
func frontendConfig(googleEnabled, devLogin bool) http.HandlerFunc {
	body, _ := json.Marshal(map[string]bool{
		"googleEnabled": googleEnabled,
		"devLogin":      devLogin,
	})
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	}
}

// spaHandler serves the built frontend, falling back to index.html so client
// routes deep-link correctly. Hashed assets are cached hard; index.html never
// is, or a deploy would not take effect until caches expired.
func spaHandler(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")

	// Read once at startup: index.html is small and never changes while the
	// process is alive, so a file read per page load would be wasted work.
	page, err := os.ReadFile(index)
	if err != nil {
		log.Printf("warning: could not read %s: %v", index, err)
		page = nil
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Clean(r.URL.Path)
		path := filepath.Join(dir, clean)

		if info, err := os.Stat(path); err == nil && !info.IsDir() && clean != "/index.html" {
			if strings.HasPrefix(clean, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fs.ServeHTTP(w, r)
			return
		}

		if page == nil {
			http.Error(w, "frontend not built", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(page)
	})
}
