// Package config holds the small amount of environment handling shared by the
// server and the maintenance commands. There is no config library here on
// purpose — everything is an environment variable, and dotenv files exist only
// so local development does not need a wrapper script.
package config

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LoadDotEnv reads .env and .env.local from the working directory and its
// parent, so a command run from backend/ still finds the files that live at the
// repo root. Values already present in the environment always win, so a real
// deployment is never overridden by a stray file.
func LoadDotEnv() {
	for _, dir := range []string{".", ".."} {
		loadFile(filepath.Join(dir, ".env"))
		loadFile(filepath.Join(dir, ".env.local"))
	}
}

func loadFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}

// Env returns an environment variable, or def when it is unset or empty.
//
// Surrounding quotes are stripped. They are always an artifact rather than part
// of the value: `make` exports an included `KEY="value"` with the quotes still
// attached, and because a value already in the environment wins over the dotenv
// file, a quoted APP_URL silently produced redirect targets like
// `"http://localhost:3000"/` — which reads as an auth bug and is not one.
func Env(key, def string) string {
	if v := unquote(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// unquote removes one matching pair of surrounding quotes.
func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// Duration parses an environment variable as a Go duration, falling back to def
// with a warning rather than failing to boot over a typo.
func Duration(key string, def time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		log.Printf("%s=%q is not a duration, using %s", key, raw, def)
		return def
	}
	return d
}

// Redact hides the password in a Postgres URL so connection strings can be
// logged safely.
func Redact(databaseURL string) string {
	at := strings.LastIndex(databaseURL, "@")
	if at < 0 {
		return databaseURL
	}
	scheme := strings.Index(databaseURL, "://")
	if scheme < 0 {
		return databaseURL
	}
	userinfo := databaseURL[scheme+3 : at]
	name, _, ok := strings.Cut(userinfo, ":")
	if !ok {
		return databaseURL
	}
	return databaseURL[:scheme+3] + name + ":***" + databaseURL[at:]
}
