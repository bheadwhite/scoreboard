// Command reset clears app data from the database.
//
// This exists because it is easy to end up with test families sitting in a
// database you also intend to use for real. It is deliberately awkward to fire
// by accident: it prints what it would delete and exits unless you pass -yes.
//
//	go run ./cmd/reset                        # show what is in the database
//	go run ./cmd/reset -family "The Whites"   # target one family
//	go run ./cmd/reset -family "The Whites" -yes
//	go run ./cmd/reset -all -yes              # clear everything
//
// The built-in game catalog is never touched — it is schema, not data.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bheadwhite/scoreboard/backend/internal/config"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

func main() {
	log.SetFlags(0)

	var (
		family = flag.String("family", "", "family to clear, by name or id")
		all    = flag.Bool("all", false, "clear every family, match and player")
		users  = flag.Bool("users", false, "with -family, also delete players left in no family")
		yes    = flag.Bool("yes", false, "actually delete; without this it is a dry run")
		prod   = flag.Bool("i-know-this-is-production", false,
			"required to delete anything from a non-local database")
	)
	flag.Parse()

	config.LoadDotEnv()

	databaseURL := config.Env("DATABASE_URL", "")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st, err := store.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer st.Close()

	log.Printf("database: %s\n", config.Redact(databaseURL))

	// A local Postgres is a scratch database. Anything else is somebody's real
	// record of who beat whom, and "clear my test fixtures" must never quietly
	// mean that. This tool once wiped a live family because one .env pointed at
	// production; the extra flag exists so that can only happen on purpose.
	if !isLocal(databaseURL) && (*all || *family != "") && *yes && !*prod {
		log.Fatalf(`refusing to delete from a remote database.

  %s

This does not look like a local database. If you really mean to delete from it,
re-run with -i-know-this-is-production. Otherwise point DATABASE_URL at a local
Postgres first.`, config.Redact(databaseURL))
	}

	switch {
	case *all:
		if err := clearAll(ctx, st, *yes); err != nil {
			log.Fatalf("clear: %v", err)
		}
	case *family != "":
		if err := clearFamily(ctx, st, *family, *users, *yes); err != nil {
			log.Fatalf("clear: %v", err)
		}
	default:
		if err := list(ctx, st); err != nil {
			log.Fatalf("list: %v", err)
		}
		fmt.Println("\nNothing was changed. Pass -family <name> or -all to target something.")
	}
}

// isLocal reports whether the URL points at a developer's own machine. Only
// loopback counts — a hostname that merely looks harmless is still remote.
func isLocal(databaseURL string) bool {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == ""
}

// list prints what is currently in the database.
func list(ctx context.Context, st *store.Store) error {
	rows, err := st.Pool().Query(ctx, `
		SELECT f.name, f.id,
		       (SELECT count(*) FROM family_members m WHERE m.family_id = f.id),
		       (SELECT count(*) FROM matches x WHERE x.family_id = f.id),
		       (SELECT count(*) FROM matches x WHERE x.family_id = f.id AND x.status = 'final')
		FROM families f
		ORDER BY f.created_at`)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Println("\nFamilies:")
	found := false
	for rows.Next() {
		var name, id string
		var members, matches, final int
		if err := rows.Scan(&name, &id, &members, &matches, &final); err != nil {
			return err
		}
		found = true
		fmt.Printf("  %-24s %s\n", name, id)
		fmt.Printf("  %-24s %d member(s), %d match(es), %d on the record\n", "", members, matches, final)
	}
	if !found {
		fmt.Println("  (none)")
	}
	if err := rows.Err(); err != nil {
		return err
	}

	var players, customGames int
	if err := st.Pool().QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&players); err != nil {
		return err
	}
	if err := st.Pool().QueryRow(ctx,
		`SELECT count(*) FROM games WHERE family_id IS NOT NULL`).Scan(&customGames); err != nil {
		return err
	}
	fmt.Printf("\nPlayers: %d\nFamily-specific games: %d\n", players, customGames)
	return nil
}

// clearFamily deletes one family. Everything below it — members, its own games,
// its matches, their teams and participants — goes with it by cascade.
func clearFamily(ctx context.Context, st *store.Store, target string, pruneUsers, commit bool) error {
	var id, name string
	err := st.Pool().QueryRow(ctx, `
		SELECT id, name FROM families
		WHERE id::text = $1 OR lower(name) = lower($1)`, target).Scan(&id, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no family called %q", target)
	}
	if err != nil {
		return err
	}

	var members, matches int
	if err := st.Pool().QueryRow(ctx,
		`SELECT count(*) FROM family_members WHERE family_id = $1`, id).Scan(&members); err != nil {
		return err
	}
	if err := st.Pool().QueryRow(ctx,
		`SELECT count(*) FROM matches WHERE family_id = $1`, id).Scan(&matches); err != nil {
		return err
	}

	fmt.Printf("\nWould delete family %q (%s)\n", name, id)
	fmt.Printf("  %d membership(s)\n  %d match(es), with their teams and results\n", members, matches)
	if pruneUsers {
		fmt.Println("  plus any player left in no family at all")
	}

	if !commit {
		fmt.Println("\nDry run. Re-run with -yes to actually delete.")
		return nil
	}

	tag, err := st.Pool().Exec(ctx, `DELETE FROM families WHERE id = $1`, id)
	if err != nil {
		return err
	}
	fmt.Printf("\nDeleted %d family.\n", tag.RowsAffected())

	if pruneUsers {
		// Safe because a user is only referenced by a family they own or a
		// match they created, and both are gone by now.
		orphans, err := st.Pool().Exec(ctx, `
			DELETE FROM users u
			WHERE NOT EXISTS (SELECT 1 FROM family_members m WHERE m.user_id = u.id)
			  AND NOT EXISTS (SELECT 1 FROM match_participants p WHERE p.user_id = u.id)`)
		if err != nil {
			return err
		}
		fmt.Printf("Deleted %d player(s) who were left in no family.\n", orphans.RowsAffected())
	}
	return nil
}

// clearAll empties the app tables, leaving the schema and the built-in game
// catalog in place.
func clearAll(ctx context.Context, st *store.Store, commit bool) error {
	counts := map[string]int{}
	for _, table := range []string{"families", "matches", "users"} {
		var n int
		if err := st.Pool().QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
			return err
		}
		counts[table] = n
	}

	fmt.Printf("\nWould delete EVERYTHING:\n  %d family(ies)\n  %d match(es)\n  %d player(s)\n",
		counts["families"], counts["matches"], counts["users"])
	fmt.Println("  (the built-in game catalog is kept)")

	if !commit {
		fmt.Println("\nDry run. Re-run with -yes to actually delete.")
		return nil
	}
	if counts["families"] == 0 && counts["users"] == 0 {
		fmt.Println("\nAlready empty.")
		return nil
	}

	// Order matters: families.owner_id and matches.created_by both reference
	// users without a cascade, so families have to go first. Deleting them
	// cascades to memberships, family games, matches, teams and participants.
	err := st.Tx(ctx, func(tx pgx.Tx) error {
		for _, stmt := range []string{
			`DELETE FROM families`,
			`DELETE FROM games WHERE family_id IS NOT NULL`,
			`DELETE FROM users`,
		} {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("%s: %w", strings.ToLower(stmt), err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Println("\nCleared. The game catalog is untouched.")
	return nil
}
