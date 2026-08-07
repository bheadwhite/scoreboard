// Package migrations holds the SQL schema, embedded into the binary and applied
// on boot. Files are applied in lexical order and must be idempotent, since
// every boot re-runs all of them.
package migrations

import (
	"context"
	"embed"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

// Apply runs every embedded migration in lexical order.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := files.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		body, err := files.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}
