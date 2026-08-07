// Command server runs the Scoreboard API and serves the built SPA.
//
// In production the Go binary serves both, so everything is same-origin and
// CORS never enters the picture. In development the Vite dev server proxies to
// this process instead.
package main

import (
	"context"
	"crypto/subtle"
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

	authn, err := auth.New(auth.Config{
		ClientID:      config.Env("GOOGLE_CLIENT_ID", config.Env("GCP_OAUTH_CLIENT", "")),
		ClientSecret:  config.Env("GOOGLE_CLIENT_SECRET", config.Env("GCP_OAUTH_SECRET", "")),
		SessionSecret: config.Env("SESSION_SECRET", ""),
		AppURL:        config.Env("APP_URL", "http://localhost:5173"),
		Production:    production,
		DevLogin:      config.Env("AUTH_DEV_LOGIN", "") == "true",
	}, func(ctx context.Context, provider, providerID, email, name, avatarURL string) (auth.Identity, error) {
		u, err := st.UpsertOAuthUser(ctx, provider, providerID, email, name, avatarURL)
		if err != nil {
			return auth.Identity{}, err
		}
		return auth.Identity{ID: u.ID, Email: email, DisplayName: u.DisplayName, AvatarURL: u.AvatarURL}, nil
	})
	if err != nil {
		log.Fatalf("auth: %v", err)
	}

	concedeWindow := config.Duration("CONCEDE_WINDOW", 24*time.Hour)
	log.Printf("concession window: %s", concedeWindow)

	opts := []connect.HandlerOption{connect.WithInterceptors(authn.Interceptor())}

	api := http.NewServeMux()
	api.Handle(scoreboardv1connect.NewUserServiceHandler(service.NewUserService(st), opts...))
	api.Handle(scoreboardv1connect.NewFamilyServiceHandler(service.NewFamilyService(st), opts...))
	api.Handle(scoreboardv1connect.NewGameServiceHandler(service.NewGameService(st), opts...))
	api.Handle(scoreboardv1connect.NewMatchServiceHandler(service.NewMatchService(st, concedeWindow), opts...))
	api.Handle(scoreboardv1connect.NewStatsServiceHandler(service.NewStatsService(st), opts...))

	// Runtime configuration for the browser, substituted into index.html. The
	// frontend holds no credentials of its own now — sign-in is a redirect to
	// this server — so all it needs to know is which options to offer.
	frontendConfig := map[string]string{
		"__GOOGLE_ENABLED__": boolString(authn.Configured()),
		"__DEV_LOGIN__":      boolString(config.Env("AUTH_DEV_LOGIN", "") == "true"),
	}
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
	authn.Routes(root)
	root.Handle("/", spaHandler(config.Env("STATIC_DIR", "../frontend/dist"), frontendConfig))

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

// sweep finalizes claimed matches whose concession window has closed.
//
// This endpoint does double duty. Cloud Scheduler calls it every few minutes,
// which settles expired claims *and* keeps the database from being paused for
// inactivity on a free tier — a real risk for an app that goes quiet between
// family gatherings.
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

		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()

		n, err := st.SweepExpiredClaims(ctx)
		if err != nil {
			log.Printf("sweep: %v", err)
			http.Error(w, "sweep failed", http.StatusInternalServerError)
			return
		}
		if n > 0 {
			log.Printf("sweep: finalized %d match(es) on timeout", n)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}
}

// spaHandler serves the built frontend, falling back to index.html so client
// routes deep-link correctly. Hashed assets are cached hard; index.html never
// is, or a deploy would not take effect until caches expired.
//
// subs are placeholder replacements applied to index.html, which is how runtime
// configuration reaches the browser without being compiled into the bundle.
func spaHandler(dir string, subs map[string]string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")

	// Read and substitute once at startup: index.html is small, it never
	// changes while the process is alive, and doing it per request would put
	// a file read in front of every page load.
	var page []byte
	if raw, err := os.ReadFile(index); err != nil {
		log.Printf("warning: could not read %s: %v", index, err)
	} else {
		out := string(raw)
		for placeholder, value := range subs {
			out = strings.ReplaceAll(out, placeholder, jsStringEscape(value))
		}
		page = []byte(out)
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

// jsStringEscape makes a value safe to drop inside a double-quoted JavaScript
// string literal. These values are operator-supplied rather than user-supplied,
// but injecting unescaped text into a <script> block is not a habit worth
// having.
func jsStringEscape(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		// Escaped as unicode so a stray "</script>" cannot close the block.
		"<", "\\u003C",
		">", "\\u003E",
		"&", "\\u0026",
	)
	return r.Replace(s)
}

// boolString renders a Go bool as a JavaScript literal for the injected config.
func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
