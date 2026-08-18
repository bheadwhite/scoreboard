package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bheadwhite/scoreboard/backend/internal/model"
)

// querier is the shared surface of *pgxpool.Pool and pgx.Tx, so the loaders
// below work either inside a transaction or on their own.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

const matchColumns = `
	m.id, m.family_id, m.game_id, m.status,
	m.contract_kind, m.contract_target, m.contract_detail,
	m.created_by, m.created_at, m.started_at, m.claimed_at,
	m.claimed_by_team_id, m.claimed_by_user_id, m.concede_deadline,
	m.dispute_reason, m.finalized_at`

func scanMatch(row pgx.Row) (model.Match, error) {
	var m model.Match
	err := row.Scan(&m.ID, &m.FamilyID, &m.GameID, &m.Status,
		&m.Contract.Kind, &m.Contract.Target, &m.Contract.Detail,
		&m.CreatedBy, &m.CreatedAt, &m.StartedAt, &m.ClaimedAt,
		&m.ClaimedByTeamID, &m.ClaimedByUserID, &m.ConcedeDeadline,
		&m.DisputeReason, &m.FinalizedAt)
	return m, err
}

// loadMatch reads a match with its game, teams and participants.
func loadMatch(ctx context.Context, q querier, id string) (model.Match, error) {
	m, err := scanMatch(q.QueryRow(ctx, `SELECT `+matchColumns+` FROM matches m WHERE m.id = $1`, id))
	if err != nil {
		return m, noRows(err)
	}
	return m, hydrate(ctx, q, &m)
}

// hydrate fills in the game, teams and participants of an already-scanned match.
func hydrate(ctx context.Context, q querier, m *model.Match) error {
	var g model.Game
	err := q.QueryRow(ctx, `
		SELECT id, family_id, name, emoji, team_based, min_players, max_players, created_at
		FROM games WHERE id = $1`, m.GameID).
		Scan(&g.ID, &g.FamilyID, &g.Name, &g.Emoji, &g.TeamBased,
			&g.MinPlayers, &g.MaxPlayers, &g.CreatedAt)
	if err != nil {
		return noRows(err)
	}
	m.Game = &g

	teamRows, err := q.Query(ctx,
		`SELECT id, match_id, name, ordinal FROM match_teams WHERE match_id = $1 ORDER BY ordinal, id`, m.ID)
	if err != nil {
		return err
	}
	m.Teams = nil
	for teamRows.Next() {
		var t model.MatchTeam
		if err := teamRows.Scan(&t.ID, &t.MatchID, &t.Name, &t.Ordinal); err != nil {
			teamRows.Close()
			return err
		}
		m.Teams = append(m.Teams, t)
	}
	teamRows.Close()
	if err := teamRows.Err(); err != nil {
		return err
	}

	partRows, err := q.Query(ctx, `
		SELECT p.match_id, p.team_id, p.result, p.conceded_at, p.disputed_at,
		       p.status, p.invited_at, p.responded_at, p.dispute_reason,
		       u.id, u.display_name, u.avatar_url, u.created_at, `+isPlaceholder+`
		FROM match_participants p
		JOIN users u ON u.id = p.user_id
		WHERE p.match_id = $1
		ORDER BY u.display_name`, m.ID)
	if err != nil {
		return err
	}
	defer partRows.Close()

	m.Participants = nil
	for partRows.Next() {
		var p model.MatchParticipant
		if err := partRows.Scan(&p.MatchID, &p.TeamID, &p.Result, &p.ConcededAt, &p.DisputedAt,
			&p.Status, &p.InvitedAt, &p.RespondedAt, &p.DisputeReason,
			&p.User.ID, &p.User.DisplayName, &p.User.AvatarURL, &p.User.CreatedAt,
			&p.User.Placeholder); err != nil {
			return err
		}
		m.Participants = append(m.Participants, p)
	}
	return partRows.Err()
}

// invitePlayer records an invitation. No team is created — an invitation is a
// question, and the roster only gains a member when it is answered.
//
// Except for a player with no account, who is joined outright. There is no
// device to put the question on, so an invitation to one would be a question
// nobody can ever answer: it would sit unanswered on the match forever and the
// person who is plainly sitting at the table would never appear in the game
// they are playing.
//
// So adding them is a statement rather than a question — somebody is asserting
// that they played. That is the one place in this app where a player is put
// into a match without agreeing to it, and it is worth being clear about what
// pays for it: everything they are entered into is disputable by them the
// moment they sign up and an administrator links the two, because objections
// outlive finalization. Their consent is deferred, not skipped.
//
// The branch lives here rather than at the call sites so CreateMatch and
// InvitePlayers cannot disagree about it.
func invitePlayer(ctx context.Context, tx pgx.Tx, matchID, userID string) error {
	var placeholder bool
	if err := tx.QueryRow(ctx,
		`SELECT u.provider = $2 FROM users u WHERE u.id = $1`,
		userID, placeholderProvider).Scan(&placeholder); err != nil {
		return noRows(err)
	}
	if placeholder {
		return joinParticipant(ctx, tx, matchID, userID)
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO match_participants (match_id, user_id, status)
		VALUES ($1, $2, 'invited')
		ON CONFLICT (match_id, user_id) DO NOTHING`, matchID, userID)
	return err
}

// joinParticipant marks a player as in and gives them a team of their own.
// Every joined participant always has a team, including in free-for-all games;
// SetTeams regroups them later if the game calls for it.
func joinParticipant(ctx context.Context, tx pgx.Tx, matchID, userID string) error {
	var status string
	var teamID *string
	err := tx.QueryRow(ctx,
		`SELECT status, team_id FROM match_participants WHERE match_id = $1 AND user_id = $2`,
		matchID, userID).Scan(&status, &teamID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Not invited, but a family member walking up to the table can join.
	case err != nil:
		return err
	case status == model.InviteJoined && teamID != nil:
		return nil // already in; joining twice is a no-op
	}

	var name string
	if err := tx.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, userID).Scan(&name); err != nil {
		return noRows(err)
	}
	if strings.TrimSpace(name) == "" {
		name = "Player"
	}

	var ordinal int32
	if err := tx.QueryRow(ctx,
		`SELECT coalesce(max(ordinal) + 1, 0) FROM match_teams WHERE match_id = $1`, matchID).
		Scan(&ordinal); err != nil {
		return err
	}

	var newTeam string
	if err := tx.QueryRow(ctx,
		`INSERT INTO match_teams (match_id, name, ordinal) VALUES ($1, $2, $3) RETURNING id`,
		matchID, name, ordinal).Scan(&newTeam); err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO match_participants (match_id, user_id, team_id, status, responded_at)
		VALUES ($1, $2, $3, 'joined', now())
		ON CONFLICT (match_id, user_id) DO UPDATE SET
			team_id      = EXCLUDED.team_id,
			status       = 'joined',
			responded_at = now()`,
		matchID, userID, newTeam)
	return err
}

// CreateMatch posts a match. It is in play immediately: there is no lobby to
// wait in, because the only thing a lobby was ever waiting for was people
// agreeing to play, and joining says that by itself now.
//
// The contract is set here and never again. That is what makes joining a
// complete commitment — you can read the terms before you tap, and nobody can
// edit them afterward — and it is why the ready check that used to sit between
// joining and playing has nothing left to affirm.
func (s *Store) CreateMatch(
	ctx context.Context, familyID, gameID, createdBy string, c model.Contract, invite []string,
) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(ctx, `
			INSERT INTO matches (family_id, game_id, created_by, status, started_at,
			                     contract_kind, contract_target, contract_detail)
			VALUES ($1, $2, $3, 'in_progress', now(), $4, $5, $6)
			RETURNING id`,
			familyID, gameID, createdBy, c.Kind, c.Target, c.Detail).Scan(&id); err != nil {
			return err
		}

		// The creator is in by definition; everybody else is asked.
		if err := joinParticipant(ctx, tx, id, createdBy); err != nil {
			return err
		}
		for _, uid := range invite {
			if uid == createdBy {
				continue
			}
			// Only family members can be pulled into a match.
			var member bool
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM family_members WHERE family_id = $1 AND user_id = $2)`,
				familyID, uid).Scan(&member); err != nil {
				return err
			}
			if !member {
				return fmt.Errorf("%w: user %s is not in this family", ErrConflict, uid)
			}
			if err := invitePlayer(ctx, tx, id, uid); err != nil {
				return err
			}
		}

		var err error
		out, err = loadMatch(ctx, tx, id)
		return err
	})

	return out, err
}

// rosterOpen reports whether the shape of the match can still change: who is
// playing, and which side they are on.
//
// It closes when a winner is named rather than when play begins, because play
// begins the moment the match is posted and people wander into a game after
// that all the time. What it protects is the claim: teams decide who owes an
// answer, so letting the roster move under a standing claim would change who
// was being asked halfway through asking them.
func rosterOpen(m model.Match) error {
	switch m.Status {
	case model.StatusInProgress:
		return nil
	case model.StatusClaimed:
		return fmt.Errorf("%w: somebody has already called this one", ErrConflict)
	default:
		return fmt.Errorf("%w: this match is over", ErrConflict)
	}
}

// JoinMatch confirms that userID is playing. This is the whole commitment —
// there is nothing to affirm afterward and nothing to wait for, because the
// match is already under way.
//
// Open until a winner is named, so somebody who joins the table halfway through
// the evening is on the scoreboard for it.
func (s *Store) JoinMatch(ctx context.Context, matchID, userID string) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if err := rosterOpen(m); err != nil {
			return err
		}
		if err := joinParticipant(ctx, tx, matchID, userID); err != nil {
			return err
		}
		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// DeclineMatch turns down an invitation. The row is kept rather than deleted so
// the same person is not re-invited by a later bulk invite, and so the match can
// show who has actually answered.
func (s *Store) DeclineMatch(ctx context.Context, matchID, userID string) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if err := rosterOpen(m); err != nil {
			return err
		}

		var teamID *string
		err = tx.QueryRow(ctx, `
			UPDATE match_participants
			SET status = 'declined', responded_at = now(), team_id = NULL
			WHERE match_id = $1 AND user_id = $2
			RETURNING team_id`, matchID, userID).Scan(&teamID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: you were not invited to this match", ErrConflict)
		}
		if err != nil {
			return err
		}

		// Drop any team the player held, if declining emptied it.
		if _, err := tx.Exec(ctx, `
			DELETE FROM match_teams t
			WHERE t.match_id = $1
			  AND NOT EXISTS (SELECT 1 FROM match_participants p WHERE p.team_id = t.id)`,
			matchID); err != nil {
			return err
		}

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// InvitePlayers asks more people once the match exists.
func (s *Store) InvitePlayers(ctx context.Context, matchID string, userIDs []string) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if err := rosterOpen(m); err != nil {
			return err
		}

		for _, uid := range userIDs {
			var member bool
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM family_members WHERE family_id = $1 AND user_id = $2)`,
				m.FamilyID, uid).Scan(&member); err != nil {
				return err
			}
			if !member {
				return fmt.Errorf("%w: that player is not in this family", ErrConflict)
			}
			if err := invitePlayer(ctx, tx, matchID, uid); err != nil {
				return err
			}
		}

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

func (s *Store) LeaveMatch(ctx context.Context, matchID, userID string) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if err := rosterOpen(m); err != nil {
			return err
		}

		var teamID *string
		if err := tx.QueryRow(ctx,
			`DELETE FROM match_participants WHERE match_id = $1 AND user_id = $2 RETURNING team_id`,
			matchID, userID).Scan(&teamID); err != nil {
			return noRows(err)
		}
		// Drop the team too, if leaving emptied it.
		if teamID != nil {
			if _, err := tx.Exec(ctx, `
				DELETE FROM match_teams t WHERE t.id = $1
				  AND NOT EXISTS (SELECT 1 FROM match_participants p WHERE p.team_id = t.id)`,
				*teamID); err != nil {
				return err
			}
		}

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// TeamAssignment is one side of a proposed team layout.
type TeamAssignment struct {
	Name    string
	UserIDs []string
}

// SetTeams replaces the whole team layout. Every joined player must appear
// exactly once, so the match can never end up with a player on no team or two.
func (s *Store) SetTeams(ctx context.Context, matchID string, teams []TeamAssignment) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if err := rosterOpen(m); err != nil {
			return err
		}

		assigned := map[string]bool{}
		for _, t := range teams {
			for _, uid := range t.UserIDs {
				if assigned[uid] {
					return fmt.Errorf("%w: %s is on more than one team", ErrConflict, uid)
				}
				assigned[uid] = true
			}
		}
		joined := m.JoinedParticipants()
		for _, p := range joined {
			if !assigned[p.User.ID] {
				return fmt.Errorf("%w: %s is not on a team", ErrConflict, p.User.DisplayName)
			}
		}
		if len(assigned) != len(joined) {
			return fmt.Errorf("%w: team roster includes someone who is not in this match", ErrConflict)
		}
		if len(teams) < 2 {
			return fmt.Errorf("%w: a match needs at least two sides", ErrConflict)
		}

		// Teams are FK'd with ON DELETE SET NULL, so this clears the old
		// assignments before the new ones are written.
		if _, err := tx.Exec(ctx, `DELETE FROM match_teams WHERE match_id = $1`, matchID); err != nil {
			return err
		}

		for i, t := range teams {
			name := strings.TrimSpace(t.Name)
			if name == "" {
				name = fmt.Sprintf("Team %d", i+1)
			}
			var teamID string
			if err := tx.QueryRow(ctx,
				`INSERT INTO match_teams (match_id, name, ordinal) VALUES ($1, $2, $3) RETURNING id`,
				matchID, name, i).Scan(&teamID); err != nil {
				return err
			}
			for _, uid := range t.UserIDs {
				if _, err := tx.Exec(ctx,
					`UPDATE match_participants SET team_id = $3 WHERE match_id = $1 AND user_id = $2`,
					matchID, uid, teamID); err != nil {
					return err
				}
			}
		}

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// ClaimVictory names the winning side and starts the clock.
//
// Two people can do this: anybody on the side they are naming, and whoever
// posted the match, who may name any side at all. That second one is the short
// circuit — the game is plainly over, everyone at the table knows who won, and
// making the winner find their phone to assert it is the kind of ceremony this
// app kept accumulating. It is also the reason a claim now records who made it
// as well as who it names.
//
// The claimant's own answer comes with the claim. If they named somebody else's
// side they are, by that fact, an opponent of the team they just crowned — and
// they have plainly already accepted the result, so writing their concession
// here keeps every downstream rule counting the same set of outstanding
// answers without a special case for the claimant.
//
// Re-claiming the same team is a no-op, so a double-tap on a bad connection is
// harmless. Naming a different team resets everyone's answer, because they were
// answering a different question.
func (s *Store) ClaimVictory(
	ctx context.Context, matchID, userID, teamID string, window time.Duration,
) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}

		claimant, ok := participant(m, userID)
		if !ok {
			return fmt.Errorf("%w: you are not in this match", ErrConflict)
		}
		if teamID == "" {
			if claimant.TeamID == nil {
				return fmt.Errorf("%w: you are not on a team", ErrConflict)
			}
			teamID = *claimant.TeamID
		}
		if !hasTeam(m, teamID) {
			return fmt.Errorf("%w: no such team in this match", ErrConflict)
		}
		own := claimant.TeamID != nil && *claimant.TeamID == teamID
		if !own && m.CreatedBy != userID {
			return fmt.Errorf(
				"%w: only whoever set this game up can call it for somebody else", ErrConflict)
		}

		// Two sides with somebody on them, or there is nobody to ask. This is
		// the one guard that used to live in StartMatch and still has work to
		// do: a match one person joined has a single team, and naming it would
		// take a self-declared win straight into the record with nobody left to
		// answer it. That is the thing this whole app refuses to do.
		if sidesInPlay(m) < 2 {
			return fmt.Errorf(
				"%w: nobody has joined yet — a result needs somebody to answer for it", ErrConflict)
		}

		switch m.Status {
		case model.StatusInProgress:
			// normal path
		case model.StatusClaimed:
			if m.ClaimedByTeamID != nil && *m.ClaimedByTeamID == teamID {
				out = m // idempotent re-claim
				return nil
			}
			// A different team is now named; everyone answers again.
		default:
			return fmt.Errorf("%w: this match is not in play", ErrConflict)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE match_participants
			SET conceded_at = NULL, disputed_at = NULL, dispute_reason = ''
			WHERE match_id = $1`, matchID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE matches
			SET status = 'claimed', claimed_at = now(), claimed_by_team_id = $2,
			    claimed_by_user_id = $3, concede_deadline = now() + $4::interval,
			    dispute_reason = ''
			WHERE id = $1`,
			matchID, teamID, userID, intervalArg(window)); err != nil {
			return err
		}
		// Saying it is agreeing with it. Only meaningful when the claimant is
		// not on the side they named; harmless when they are, since a player on
		// the winning team is never asked.
		if !own {
			if _, err := tx.Exec(ctx,
				`UPDATE match_participants SET conceded_at = now() WHERE match_id = $1 AND user_id = $2`,
				matchID, userID); err != nil {
				return err
			}
		}

		// A match whose named side is everybody has nobody left to ask.
		if err := finalizeIfRatified(ctx, tx, matchID); err != nil {
			return err
		}

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// sidesInPlay counts the teams that actually have a joined player on them.
// Empty teams linger only in transit; this is the number that decides whether
// there is a contest at all.
func sidesInPlay(m model.Match) int {
	seen := map[string]bool{}
	for _, p := range m.JoinedParticipants() {
		if p.TeamID != nil {
			seen[*p.TeamID] = true
		}
	}
	return len(seen)
}

// answerable checks that userID is entitled to answer the claim on this match,
// and that there is a claim to answer.
//
// Reachable on a settled match on purpose, which is the change that follows
// from an objection being a mark rather than a block. The moment somebody is
// most likely to notice a result they disagree with is when they see it on the
// scoreboard, and by then it has finalized — under the old rules that was too
// late and the only remedy was an administrator erasing the match. Now it costs
// nothing to let them say so: the result stays, the record stays, and it stops
// counting toward the clean numbers until they take it back.
//
// The winner's own side is the interesting case, and it turns on whether the
// match has settled.
//
// While the claim is live they are shut out, because they have a better door:
// WithdrawClaim takes the whole thing off the table instead of flagging it, and
// somebody who thinks they did not win should be able to say so without leaving
// an asterisk on a result that then stands anyway.
//
// Once it has settled that door is closed — finalizing is the one irreversible
// step — and shutting them out too would leave the person credited with a win
// as the only player in the match unable to say it did not happen. Which is a
// real position to be in: whoever posted the match can name anybody the winner,
// and in a two-player game their claim is also the only answer it needs, so the
// result can be on the board before the supposed winner has looked at their
// phone. "I did not win this", from them, is the most damning objection anybody
// could file, and it has to be sayable.
func answerable(m model.Match, userID string) error {
	p, ok := participant(m, userID)
	if !ok {
		return fmt.Errorf("%w: you are not in this match", ErrConflict)
	}
	named := m.ClaimedByTeamID != nil && p.TeamID != nil && *p.TeamID == *m.ClaimedByTeamID

	switch m.Status {
	case model.StatusFinal:
		return nil
	case model.StatusClaimed:
		if named {
			return fmt.Errorf(
				"%w: you are on the side that was named — take the claim back instead", ErrConflict)
		}
		return nil
	default:
		return fmt.Errorf("%w: there is no result to answer", ErrConflict)
	}
}

// Concede records that userID accepts the claim, withdrawing any objection they
// had made. Idempotent. When the last outstanding opponent answers, the match
// finalizes in the same transaction.
//
// This is also the only way a contested result becomes clean again, including
// long after it settled: an objection lasts exactly as long as its author
// leaves it there, and this is them taking it down.
func (s *Store) Concede(ctx context.Context, matchID, userID string, window time.Duration) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if err := answerable(m, userID); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE match_participants
			SET conceded_at = coalesce(conceded_at, now()),
			    disputed_at = NULL,
			    dispute_reason = ''
			WHERE match_id = $1 AND user_id = $2`, matchID, userID); err != nil {
			return err
		}

		if err := settleAnswers(ctx, tx, matchID, window); err != nil {
			return err
		}

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// Dispute objects to the claim, withdrawing any concession userID had already
// given.
//
// It does not stop the clock and it does not stop the match settling. An
// objection used to do both, which handed one person a veto over a result and
// left the family organizer to break the tie — a referee ruling on an argument
// they were not in, which is precisely the shortcut this app exists to refuse.
// What it does now is mark: the match is contested for as long as the objection
// stands, and a contested match is kept out of every number on the scoreboard
// while staying fully visible as a match. The claim still settles on schedule.
//
// Which means the concede deadline is left exactly where it was. Nothing here
// needs to distinguish disagreement from silence any more, because neither one
// changes where the match ends up.
func (s *Store) Dispute(ctx context.Context, matchID, userID, reason string, window time.Duration) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if err := answerable(m, userID); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE match_participants
			SET disputed_at = coalesce(disputed_at, now()),
			    conceded_at = NULL,
			    dispute_reason = $3
			WHERE match_id = $1 AND user_id = $2`,
			matchID, userID, strings.TrimSpace(reason)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE matches SET dispute_reason = $2 WHERE id = $1`,
			matchID, strings.TrimSpace(reason)); err != nil {
			return err
		}

		if err := settleAnswers(ctx, tx, matchID, window); err != nil {
			return err
		}

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// WithdrawClaim takes the claim back off the table. The match goes back to
// being in play with nothing named and every answer wiped.
//
// Two people may do it: whoever made the claim, and anybody on the side it
// named. The second is not generosity, it is the counterweight to the creator
// being able to name a winner who never asked to be one — if somebody hands you
// a win you did not earn, refusing it outright should not require you to file
// an objection against your own name and live with the asterisk.
//
// This is "never mind", not "you won". A claimant who has decided the other
// side actually won cannot hand them the win from here — only somebody
// entitled to name that side can, which keeps the rule that no result reaches
// the record without being asserted and then answered.
func (s *Store) WithdrawClaim(ctx context.Context, matchID, userID string) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if m.Status == model.StatusInProgress {
			out = m // nothing claimed; already where this would leave it
			return nil
		}
		if m.Status != model.StatusClaimed {
			return fmt.Errorf("%w: there is no claim to withdraw", ErrConflict)
		}
		if _, ok := participant(m, userID); !ok {
			return fmt.Errorf("%w: you are not in this match", ErrConflict)
		}
		author := m.ClaimedByUserID != nil && *m.ClaimedByUserID == userID
		if !author && !m.OnClaimingTeam(userID) {
			return fmt.Errorf(
				"%w: only whoever called it, or the side they called it for, can take it back", ErrConflict)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE match_participants
			SET conceded_at = NULL, disputed_at = NULL, dispute_reason = ''
			WHERE match_id = $1`, matchID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE matches
			SET status = 'in_progress', claimed_at = NULL, claimed_by_team_id = NULL,
			    claimed_by_user_id = NULL, concede_deadline = NULL, dispute_reason = ''
			WHERE id = $1`, matchID); err != nil {
			return err
		}

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// settleAnswers finalizes the match if every opponent has now answered.
//
// This used to be a much bigger job: it derived the status from the answers,
// stopped and restarted the clock, and carried a long argument about why
// DISPUTED could not be latched. All of that existed to manage a state that no
// longer exists. An objection changes nothing about where a match is going, so
// there is no status to recompute and no clock to move — the only question left
// is whether everyone has spoken.
//
// It is still called after every answer rather than only after a concession,
// because an objection can be the last answer outstanding, and a claim that
// everyone has now objected to should settle exactly as promptly as one
// everyone accepted. It settles contested, which is the point.
func settleAnswers(ctx context.Context, tx pgx.Tx, matchID string, window time.Duration) error {
	return finalizeIfRatified(ctx, tx, matchID)
}

func (s *Store) CancelMatch(ctx context.Context, matchID string) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if m.Status == model.StatusFinal {
			return fmt.Errorf("%w: a finalized match cannot be canceled", ErrConflict)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE matches SET status = 'canceled', concede_deadline = NULL WHERE id = $1`,
			matchID); err != nil {
			return err
		}
		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// DeleteMatch erases a match and everything hanging off it, returning the match
// as it last stood so the caller can announce and log what went.
//
// The only removal in the store, and the only exit from the permanent record.
// Everything derived from a match is computed on read — LoadParticipations joins
// matches, and nothing anywhere is materialized — so deleting the row is all it
// takes for a finalized result to leave the standings, every head-to-head record
// and both players' profiles at once. There is no counter to decrement and no
// cache to rebuild.
//
// Children are deleted explicitly, in order, rather than left to the foreign
// keys. Both would work: match_participants and match_teams cascade from
// matches. But match_participants.team_id is ON DELETE SET NULL against
// match_teams while the participant row is itself being cascade-deleted, and
// spelling the order out means nobody has to work out what Postgres does with
// that.
func (s *Store) DeleteMatch(ctx context.Context, matchID string) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `DELETE FROM match_participants WHERE match_id = $1`, matchID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM match_teams WHERE match_id = $1`, matchID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM matches WHERE id = $1`, matchID); err != nil {
			return err
		}

		out = m
		return nil
	})

	return out, err
}

// GetMatch loads a match, finalizing it first if its concession clock has run
// out. This lazy check keeps the record correct even if the sweeper is down.
func (s *Store) GetMatch(ctx context.Context, matchID string) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		if err := finalizeIfExpired(ctx, tx, matchID); err != nil {
			return err
		}
		var err error
		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// ListMatchesFilter narrows ListMatches.
type ListMatchesFilter struct {
	FamilyID string
	Statuses []string
	// Restrict to matches this user played in.
	UserID string
	Limit  int32
	Offset int32
}

func (s *Store) ListMatches(ctx context.Context, f ListMatchesFilter) ([]model.Match, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	q := `SELECT ` + matchColumns + ` FROM matches m WHERE m.family_id = $1`
	args := []any{f.FamilyID}

	if len(f.Statuses) > 0 {
		args = append(args, f.Statuses)
		q += fmt.Sprintf(` AND m.status = ANY($%d)`, len(args))
	}
	if f.UserID != "" {
		args = append(args, f.UserID)
		q += fmt.Sprintf(
			` AND EXISTS (SELECT 1 FROM match_participants p WHERE p.match_id = m.id AND p.user_id = $%d)`,
			len(args))
	}
	args = append(args, limit, f.Offset)
	q += fmt.Sprintf(` ORDER BY coalesce(m.finalized_at, m.claimed_at, m.started_at, m.created_at) DESC
	                   LIMIT $%d OFFSET $%d`, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}

	var out []model.Match
	for rows.Next() {
		m, err := scanMatch(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		if err := hydrate(ctx, s.pool, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SweepExpiredClaims finalizes every claimed match whose concession window has
// closed, and returns how many it settled. Called by the scheduled sweeper.
//
// Contested matches are swept along with the rest. An objection used to clear
// the deadline and hide the match from this query; now it leaves the clock
// alone, so the sweep reaches every abandoned claim and each one lands with
// whatever objections it collected still on it.
func (s *Store) SweepExpiredClaims(ctx context.Context) (int, error) {
	var ids []string

	rows, err := s.pool.Query(ctx, `
		SELECT id FROM matches
		WHERE status = 'claimed' AND concede_deadline IS NOT NULL AND concede_deadline <= now()`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	settled := 0
	for _, id := range ids {
		err := s.tx(ctx, func(tx pgx.Tx) error { return finalizeIfExpired(ctx, tx, id) })
		if err != nil {
			return settled, err
		}
		settled++
	}
	return settled, nil
}

// finalizeIfRatified finalizes the match once every opponent of the named team
// has answered — either way.
//
// Answered, not agreed. The old bar was unanimous concession, and an objection
// held the match open until an organizer settled it. With objections reduced to
// a mark, holding the match open would mean holding it open forever: nobody is
// coming to break the tie, and the objector has no reason to move. So a claim
// everybody objected to settles just as promptly as one everybody accepted. It
// settles contested, which is the objection doing exactly the work it now does.
func finalizeIfRatified(ctx context.Context, tx pgx.Tx, matchID string) error {
	m, err := loadMatch(ctx, tx, matchID)
	if err != nil {
		return err
	}
	if m.Status != model.StatusClaimed || m.ClaimedByTeamID == nil {
		return nil
	}
	if !m.Answered() {
		return nil
	}
	return finalize(ctx, tx, matchID, *m.ClaimedByTeamID)
}

// finalizeIfExpired finalizes a claimed match whose deadline has passed. Every
// claimed match has a deadline — an objection no longer clears it — so this
// reaches all of them, including the contested ones.
func finalizeIfExpired(ctx context.Context, tx pgx.Tx, matchID string) error {
	m, err := loadMatch(ctx, tx, matchID)
	if err != nil {
		return err
	}
	if m.Status != model.StatusClaimed || m.ClaimedByTeamID == nil || m.ConcedeDeadline == nil {
		return nil
	}
	if m.ConcedeDeadline.After(time.Now()) {
		return nil
	}
	return finalize(ctx, tx, matchID, *m.ClaimedByTeamID)
}

// finalize writes the result onto every participant and closes the match. This
// is the only place a match becomes part of the permanent record.
func finalize(ctx context.Context, tx pgx.Tx, matchID, winningTeamID string) error {
	// Only the joined roster reaches the record; a declined invitation is not a
	// loss.
	if _, err := tx.Exec(ctx, `
		UPDATE match_participants
		SET result = CASE WHEN team_id = $2 THEN 'win' ELSE 'loss' END
		WHERE match_id = $1 AND status = 'joined'`, matchID, winningTeamID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE matches
		SET status = 'final', finalized_at = now(), claimed_by_team_id = $2, concede_deadline = NULL
		WHERE id = $1`, matchID, winningTeamID)
	return err
}

// participant finds a player who actually joined. Someone holding an
// unanswered invitation is not in the match and cannot claim or concede.
func participant(m model.Match, userID string) (model.MatchParticipant, bool) {
	for _, p := range m.JoinedParticipants() {
		if p.User.ID == userID {
			return p, true
		}
	}
	return model.MatchParticipant{}, false
}

func hasTeam(m model.Match, teamID string) bool {
	for _, t := range m.Teams {
		if t.ID == teamID {
			return true
		}
	}
	return false
}

// intervalArg renders a duration for Postgres. The simple query protocol means
// durations travel as text, so this has to be a valid interval literal.
func intervalArg(d time.Duration) string {
	if d <= 0 {
		d = 24 * time.Hour
	}
	return fmt.Sprintf("%d seconds", int64(d.Seconds()))
}
