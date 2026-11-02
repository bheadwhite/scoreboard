// Command consolidate folds every family into one.
//
// The app used to be multi-family and is not any more: the UI never names a
// family, never offers to create or join one, and signing in puts you in the
// one that exists. This is the other half of that change — the databases that
// already have relatives scattered across a scoreboard each, whose matches
// belong in the same record.
//
//	go run ./cmd/consolidate                          # show the plan, change nothing
//	go run ./cmd/consolidate -into "The Whites"       # plan, naming the destination
//	go run ./cmd/consolidate -into "The Whites" -yes  # do it
//
// It is a dry run unless you pass -yes, and it prints exactly what it would
// move first. Matches, teams, results and family-specific games all move; the
// emptied families are then deleted. Nothing leaves the permanent record.
//
// Safe to run against production, and meant to be: the destination keeps its
// id, name and invite code, so nobody is signed out and no link breaks.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/bheadwhite/scoreboard/backend/internal/config"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

func main() {
	log.SetFlags(0)

	var (
		into = flag.String("into", "",
			"the family everyone ends up in, by name or id (default: the one the app already uses)")
		yes = flag.Bool("yes", false, "actually merge; without this it is a dry run")
	)
	flag.Parse()

	config.LoadDotEnv()

	databaseURL := config.Env("DATABASE_URL", "")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	st, err := store.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer st.Close()

	log.Printf("database: %s\n", config.Redact(databaseURL))

	if err := run(ctx, st, *into, *yes); err != nil {
		log.Fatalf("consolidate: %v", err)
	}
}

func run(ctx context.Context, st *store.Store, into string, commit bool) error {
	dest, err := st.ResolveFamily(ctx, into)
	if errors.Is(err, store.ErrNotFound) {
		if into == "" {
			return errors.New("there are no families to consolidate")
		}
		return fmt.Errorf("no family called %q", into)
	}
	if err != nil {
		return err
	}

	plan, err := st.PlanConsolidation(ctx, dest.ID)
	if err != nil {
		return err
	}

	fmt.Printf("\nEverything ends up in %q (%s)\n", dest.Name, dest.ID)
	fmt.Printf("  invite code %s — unchanged, so shared links keep working\n", dest.InviteCode)
	fmt.Printf("  %d member(s), %d match(es), %d game(s) of its own today\n\n",
		plan.Destination.Members, plan.Destination.Matches, plan.Destination.Games)

	if plan.Nothing() {
		fmt.Println("There is nothing else to fold in — this is already the only family.")
		if !isDefaultAlready(ctx, st, dest.ID) {
			return designate(ctx, st, dest.ID, commit)
		}
		return nil
	}

	fmt.Println("Absorbing:")
	for _, f := range plan.Sources {
		fmt.Printf("  %-24s %s\n", f.Name, f.ID)
		fmt.Printf("  %-24s %d member(s), %d match(es), %d game(s)\n", "", f.Members, f.Matches, f.Games)
	}

	fmt.Printf("\nWhich means:\n")
	fmt.Printf("  %d player(s) join %s\n", plan.JoiningMembers, dest.Name)
	fmt.Printf("  %d match(es) move, keeping their results, teams and history\n", plan.MovingMatches)
	fmt.Printf("  %d family-specific game(s) move\n", plan.MovingGames)
	if plan.DuplicateGames > 0 {
		fmt.Printf("  %d duplicate game(s) collapse into one, their matches repointed\n", plan.DuplicateGames)
	}
	fmt.Printf("  %d emptied family(ies) are deleted\n", len(plan.Sources))
	fmt.Println("\nNo match is deleted and no result is changed.")

	if !commit {
		fmt.Println("\nDry run. Re-run with -yes to actually merge.")
		return nil
	}

	if err := st.Consolidate(ctx, dest.ID); err != nil {
		return err
	}

	after, err := st.DefaultFamily(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("\nDone. %q is now the only family, with %d member(s).\n", after.Name, after.MemberCount)
	fmt.Println("Anyone signing in from now on joins it automatically.")
	return nil
}

// isDefaultAlready reports whether the destination is what sign-in will use.
func isDefaultAlready(ctx context.Context, st *store.Store, id string) bool {
	f, err := st.DefaultFamily(ctx)
	return err == nil && f.ID == id
}

// designate points sign-in at the destination without merging anything, which
// is the only work left when it is already the only family.
func designate(ctx context.Context, st *store.Store, id string, commit bool) error {
	fmt.Println("It is not the family sign-in uses, though, which this would fix.")
	if !commit {
		fmt.Println("\nDry run. Re-run with -yes to point sign-in at it.")
		return nil
	}
	if err := st.SetDefaultFamily(ctx, id); err != nil {
		return err
	}
	fmt.Println("\nDone. Anyone signing in from now on joins it automatically.")
	return nil
}
