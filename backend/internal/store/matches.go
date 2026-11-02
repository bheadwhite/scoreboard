package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// concede_deadline is deliberately absent. The column still exists — dropping
// one in the same release that stops reading it takes the rollback with it, the
// same rule that left ready_at behind — but nothing writes it and nothing reads
// it, because no result waits on a clock any more.
const matchColumns = `
	m.id, m.family_id, m.game_id, m.status,
	m.contract_kind, m.contract_target, m.contract_detail,
	m.created_by, m.created_at, m.started_at, m.claimed_at,
	m.claimed_by_team_id, m.claimed_by_user_id,
	m.dispute_reason, m.finalized_at`

func scanMatch(row pgx.Row) (model.Match, error) {
	var m model.Match
	err := row.Scan(&m.ID, &m.FamilyID, &m.GameID, &m.Status,
		&m.Contract.Kind, &m.Contract.Target, &m.Contract.Detail,
		&m.CreatedBy, &m.CreatedAt, &m.StartedAt, &m.ClaimedAt,
		&m.ClaimedByTeamID, &m.ClaimedByUserID,
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
// Except when whoever posted the match is the one adding them. byCreator is that
// case, and it joins them outright: the host is at the table and can see who is
// sitting at it, so adding somebody is a statement rather than a question, and
// making them tap a second time to confirm it bought nothing. It stops at a no —
// somebody who declined stays declined — and everybody put in this way can object
// to whatever gets recorded, for as long as the record exists.
//
// And except for a player with no account, who is joined outright by anyone.
// There is no device to put the question on, so an invitation to one would be a
// question nobody can ever answer: it would sit unanswered on the match forever
// and the person who is plainly sitting at the table would never appear in the
// game they are playing.
//
// So adding them is a statement too — somebody is asserting that they played. It
// is worth being clear about what pays for it: everything they are entered into
// is disputable by them the moment they sign up and an administrator links the
// two, because objections outlive finalization. Their consent is deferred, not
// skipped.
//
// The branch lives here rather than at the call sites so CreateMatch and
// InvitePlayers cannot disagree about it.
func invitePlayer(ctx context.Context, tx pgx.Tx, matchID, userID string, byCreator bool) error {
	var placeholder bool
	if err := tx.QueryRow(ctx,
		`SELECT u.provider = $2 FROM users u WHERE u.id = $1`,
		userID, placeholderProvider).Scan(&placeholder); err != nil {
		return noRows(err)
	}
	if placeholder {
		return joinParticipant(ctx, tx, matchID, userID)
	}
	if byCreator {
		var status string
		err := tx.QueryRow(ctx,
			`SELECT status FROM match_participants WHERE match_id = $1 AND user_id = $2`,
			matchID, userID).Scan(&status)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if status == model.InviteDeclined {
			return fmt.Errorf("%w: they said no — that is theirs to change", ErrConflict)
		}
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

		// The creator is in by definition, and so is everybody they add.
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
			if err := invitePlayer(ctx, tx, id, uid, true); err != nil {
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
// that all the time. What it protects is the result: teams are what a win and a
// loss get written against, so letting the roster move under a recorded result
// would rewrite who won and lost after the fact.
//
// It reopens if the side a result credits refuses it, which puts the match back
// in play with nobody named — the roster is editable again exactly while there
// is no result to corrupt.
func rosterOpen(m model.Match) error {
	switch m.Status {
	case model.StatusInProgress:
		return nil
	case model.StatusFinal:
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
//
// byCreator is set when the host is doing this on somebody else's behalf, which
// now only comes up for an invitation another player sent: anybody the host adds
// themselves is already in. It is narrower than joining yourself in one specific
// way, and the narrowing is the point: it only works on an invitation nobody has
// answered. Somebody who declined stays declined — the host can start a game
// past people who are not paying attention, never past a no.
//
// What it deliberately does not buy is an unchallengeable result. Anybody put in
// this way can object to whatever gets recorded, exactly like a player who joined
// themselves, and one objection holds it out of every tally.
func (s *Store) JoinMatch(ctx context.Context, matchID, userID string, byCreator bool) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if err := rosterOpen(m); err != nil {
			return err
		}
		if byCreator {
			var status string
			err := tx.QueryRow(ctx,
				`SELECT status FROM match_participants WHERE match_id = $1 AND user_id = $2`,
				matchID, userID).Scan(&status)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: invite them first", ErrConflict)
			}
			if err != nil {
				return err
			}
			switch status {
			case model.InviteJoined:
				out = m // already playing
				return nil
			case model.InviteDeclined:
				return fmt.Errorf("%w: they said no — that is theirs to change", ErrConflict)
			}
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

// InvitePlayers adds more people once the match exists: into the game if the host
// is the one adding them, otherwise as an invitation. See invitePlayer.
func (s *Store) InvitePlayers(ctx context.Context, matchID string, userIDs []string, byCreator bool) (model.Match, error) {
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
			if err := invitePlayer(ctx, tx, matchID, uid, byCreator); err != nil {
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
// ClaimVictory names the winner, which records the result. There is nothing
// between this call and the permanent record.
//
// It used to open a pending stage: the claim sat collecting answers and settled
// either when the last opponent spoke or when a week ran out. Both endings
// wrote the same row. An unanswered claim settled as claimed, a unanimously
// rejected one settled as claimed and contested, and the only thing the wait
// produced was a delay between somebody saying a thing and the app admitting
// they had said it. What actually protects the record is downstream of here and
// always was — anybody in the match can object, at any point, forever.
//
// The guards that survive are the ones that were never about waiting: you may
// name your own side, only the creator may name somebody else's, and there have
// to be two sides in play so the result is recorded against somebody.
func (s *Store) ClaimVictory(
	ctx context.Context, matchID, userID, teamID string,
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

		// Two sides with somebody on them, or there is nobody the result is
		// recorded against. A match one person joined has a single team, and
		// naming it would put a win on the board that nobody in the world is in
		// a position to deny — not merely unanswered, unanswerable. That is the
		// thing this whole app refuses to do, and it is the one guard here that
		// nothing downstream can make up for.
		if sidesInPlay(m) < 2 {
			return fmt.Errorf(
				"%w: nobody else has joined yet — a result has to be recorded against somebody",
				ErrConflict)
		}

		switch m.Status {
		case model.StatusInProgress:
			// normal path
		case model.StatusFinal:
			if m.ClaimedByTeamID != nil && *m.ClaimedByTeamID == teamID {
				out = m // idempotent re-claim
				return nil
			}
			// Naming a different side is rewriting a settled result, which is
			// not something one player does to another's record. The side that
			// holds the win takes it back first; then anybody may claim again.
			return fmt.Errorf(
				"%w: this game already has a result — whoever it credits has to take it back first",
				ErrConflict)
		default:
			return fmt.Errorf("%w: this match is not in play", ErrConflict)
		}

		// Wipes the answers a previous claim collected. Only reachable after a
		// withdrawal, which already cleared them; belt and braces, so no
		// objection can ever outlive the result it was aimed at.
		if _, err := tx.Exec(ctx, `
			UPDATE match_participants
			SET conceded_at = NULL, disputed_at = NULL, dispute_reason = ''
			WHERE match_id = $1`, matchID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE matches
			SET claimed_at = now(), claimed_by_team_id = $2, claimed_by_user_id = $3,
			    dispute_reason = ''
			WHERE id = $1`,
			matchID, teamID, userID); err != nil {
			return err
		}
		// Saying it is agreeing with it. Only meaningful when the claimant is
		// not on the side they named — the creator settling a game on
		// everybody's behalf — and it puts their position on the answer board
		// where everyone can see whose word this result rests on.
		if !own {
			if _, err := tx.Exec(ctx,
				`UPDATE match_participants SET conceded_at = now() WHERE match_id = $1 AND user_id = $2`,
				matchID, userID); err != nil {
				return err
			}
		}

		if err := finalize(ctx, tx, matchID, teamID); err != nil {
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

// answerable checks that userID may take a position on this match's result, and
// that there is a result to take a position on.
//
// It got much shorter when the pending stage went. The old version turned on
// whether the claim was still live, because that decided whether the named
// winner had a better door than objecting — WithdrawClaim, which took the whole
// thing off the table instead of leaving an asterisk on a result that stood
// anyway. Nothing is ever live now, so there is one case: a settled result, and
// everybody in the match may answer it.
//
// Everybody includes the side credited with the win, and that is not an
// oversight. Whoever posts a match can name any side, so a result can be on the
// board before the person it credits has looked at their phone. Shutting them
// out would leave them the only player in the match unable to say it did not
// happen, and "I did not win this" from them is the most damning thing anybody
// could say about a result.
func answerable(m model.Match, userID string) error {
	if _, ok := participant(m, userID); !ok {
		return fmt.Errorf("%w: you are not in this match", ErrConflict)
	}
	if m.Status != model.StatusFinal {
		return fmt.Errorf("%w: there is no result to answer", ErrConflict)
	}
	return nil
}

// Concede records that userID says the result is right, withdrawing any
// objection they had made. Idempotent.
//
// It moves no tally — the result already counts — so this is a position on the
// answer board rather than a step in a protocol. Two things make it worth
// having. It is the only way a contested result becomes clean again, at any
// point including years later, because an objection lasts exactly as long as
// its author leaves it there and this is them taking it down. And it is the
// difference between a player who has looked at a result and agrees with it and
// one who has not looked, which is the only thing the board can honestly say
// about somebody who has said nothing.
func (s *Store) Concede(ctx context.Context, matchID, userID string) (model.Match, error) {
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

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// Dispute objects to the result, withdrawing any concession userID had already
// given.
//
// This is the whole of what protects the permanent record, and it is worth
// being plain that it is doing that job alone now. Nothing stands between a
// claim and the board any more, so nothing filters a result on the way in; what
// there is instead is that every result can be marked wrong on the way out, by
// anybody who was in the match, at any point in its life, without a majority
// and without anybody's permission. A contested match is kept out of every
// number on the scoreboard while staying completely visible as a match, with
// the objector's name and reasons on it.
//
// One objection is enough and it is never adjudicated. It is also never final:
// Concede takes it back, and either of them can be done again tomorrow. The
// mark is a position somebody is holding, not a verdict anybody reached.
func (s *Store) Dispute(ctx context.Context, matchID, userID, reason string) (model.Match, error) {
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

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// WithdrawClaim takes a result back off the board. The match returns to being
// in play with nobody credited, every answer cleared and every participation
// back to pending, exactly as if nobody had ever called it.
//
// Two things about this are load-bearing, and they pull against each other.
//
// It reaches a finalized match, which is new. Finalizing used to be the one
// irreversible step in the app and it could afford to be, because a claim spent
// a week in front of everybody first — there was somewhere to catch a mistake
// before it hardened. Nothing is pending now, so a misplaced tap is on the
// permanent record instantly, and an app whose only remedy for that is an
// administrator with database access is an app that will accumulate wrong
// results.
//
// And it belongs to the side the result names, and to nobody else — not to
// whoever called it, which is where the old rule has to be narrowed rather than
// carried over. Whoever posts a match may name any side, so in a game you set
// up you could credit your opponent, take the loss, and then retract your own
// claim and take the loss back off the board. That is a record you can erase a
// defeat from, which is not a record. So:
//
//	a win can always be refused; a loss can never be erased.
//
// The common misfire is covered by that rule rather than in spite of it —
// tapping the wrong team credits a side you are on, and a side you are on can
// hand it straight back. What it deliberately leaves uncovered is regretting a
// win you handed to somebody else, and the remedy there is the same one
// everybody else has: object to it, under your own name, and let it sit there
// contested. Failing that, an administrator can void the match.
//
// This is "never mind", not "you won". Somebody who has decided the other side
// actually won cannot hand it to them from here — only a player entitled to
// name that side can, which keeps every result on the board something a person
// asserted rather than something that fell out of somebody else's retraction.
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
		if m.Status != model.StatusFinal {
			return fmt.Errorf("%w: there is no result to take back", ErrConflict)
		}
		if _, ok := participant(m, userID); !ok {
			return fmt.Errorf("%w: you are not in this match", ErrConflict)
		}
		if !m.OnClaimingTeam(userID) {
			return fmt.Errorf(
				"%w: only the side this result credits can take it back — "+
					"if you think it is wrong, object to it", ErrConflict)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE match_participants
			SET result = 'pending', conceded_at = NULL, disputed_at = NULL, dispute_reason = ''
			WHERE match_id = $1`, matchID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE matches
			SET status = 'in_progress', claimed_at = NULL, claimed_by_team_id = NULL,
			    claimed_by_user_id = NULL, finalized_at = NULL, dispute_reason = ''
			WHERE id = $1`, matchID); err != nil {
			return err
		}

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
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
			`UPDATE matches SET status = 'canceled' WHERE id = $1`,
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

// GetMatch loads a match. A plain read: it used to open a transaction and
// finalize the match first if its concession clock had run out, because a
// result could arrive by the passage of time rather than by anybody doing
// anything. Nothing arrives on its own any more — a result exists because
// somebody named it — so reading is only reading.
func (s *Store) GetMatch(ctx context.Context, matchID string) (model.Match, error) {
	return loadMatch(ctx, s.pool, matchID)
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

// finalize writes the result onto every participant and closes the match. This
// is the only place a match becomes part of the permanent record, and it now
// has exactly one caller: ClaimVictory, in the same transaction as the claim.
//
// There used to be two others, finalizeIfRatified and finalizeIfExpired, for
// the two ways a pending claim could mature — the last opponent answering, and
// a week going by. Both wrote what this writes, from the same claim, which is
// the observation the pending stage did not survive.
//
// Note what finalizing is not. It is not the moment a result becomes
// uncontestable, because objections outlive it; and it is no longer irrevocable
// either, because the side a result credits can hand it back. What it is is the
// moment a result starts counting.
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
		SET status = 'final', finalized_at = now(), claimed_by_team_id = $2
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
