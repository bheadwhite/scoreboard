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
	m.claimed_by_team_id, m.concede_deadline, m.dispute_reason, m.finalized_at`

func scanMatch(row pgx.Row) (model.Match, error) {
	var m model.Match
	err := row.Scan(&m.ID, &m.FamilyID, &m.GameID, &m.Status,
		&m.Contract.Kind, &m.Contract.Target, &m.Contract.Detail,
		&m.CreatedBy, &m.CreatedAt, &m.StartedAt, &m.ClaimedAt,
		&m.ClaimedByTeamID, &m.ConcedeDeadline, &m.DisputeReason, &m.FinalizedAt)
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
		       p.status, p.invited_at, p.responded_at, p.ready_at, p.dispute_reason,
		       u.id, u.display_name, u.avatar_url, u.created_at
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
			&p.Status, &p.InvitedAt, &p.RespondedAt, &p.ReadyAt, &p.DisputeReason,
			&p.User.ID, &p.User.DisplayName, &p.User.AvatarURL, &p.User.CreatedAt); err != nil {
			return err
		}
		m.Participants = append(m.Participants, p)
	}
	return partRows.Err()
}

// invitePlayer records an invitation. No team is created — an invitation is a
// question, and the roster only gains a member when it is answered.
func invitePlayer(ctx context.Context, tx pgx.Tx, matchID, userID string) error {
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
		// Not invited, but a family member walking up to a lobby can join.
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

func (s *Store) CreateMatch(ctx context.Context, familyID, gameID, createdBy string, invite []string) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(ctx,
			`INSERT INTO matches (family_id, game_id, created_by) VALUES ($1, $2, $3) RETURNING id`,
			familyID, gameID, createdBy).Scan(&id); err != nil {
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

func (s *Store) JoinMatch(ctx context.Context, matchID, userID string) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if m.Status != model.StatusLobby {
			return fmt.Errorf("%w: this match has already started", ErrConflict)
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
// the same person is not re-invited by a later bulk invite, and so the lobby can
// show who has actually answered.
func (s *Store) DeclineMatch(ctx context.Context, matchID, userID string) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if m.Status != model.StatusLobby {
			return fmt.Errorf("%w: this match has already started", ErrConflict)
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

// InvitePlayers asks more people once the lobby exists.
func (s *Store) InvitePlayers(ctx context.Context, matchID string, userIDs []string) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if m.Status != model.StatusLobby {
			return fmt.Errorf("%w: this match has already started", ErrConflict)
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
		if m.Status != model.StatusLobby {
			return fmt.Errorf("%w: you cannot leave a match that has started", ErrConflict)
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

// SetTeams replaces a lobby's whole team layout. Every participant must appear
// exactly once, so the match can never end up with a player on no team or two.
func (s *Store) SetTeams(ctx context.Context, matchID string, teams []TeamAssignment) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if m.Status != model.StatusLobby {
			return fmt.Errorf("%w: teams are locked once the match starts", ErrConflict)
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

		// Who you are playing with is part of what you agreed to.
		if err := clearReady(ctx, tx, matchID); err != nil {
			return err
		}

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// SetReady records, or withdraws, a player's affirmation of the current
// contract.
func (s *Store) SetReady(ctx context.Context, matchID, userID string, ready bool) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if m.Status != model.StatusLobby {
			return fmt.Errorf("%w: this match has already started", ErrConflict)
		}
		if m.Contract.Kind == "" || m.Contract.Kind == model.ContractUnspecified {
			return fmt.Errorf("%w: agree on the winning contract first", ErrConflict)
		}
		if _, ok := participant(m, userID); !ok {
			return fmt.Errorf("%w: join the match before saying you are ready", ErrConflict)
		}

		var value any
		if ready {
			value = time.Now()
		}
		if _, err := tx.Exec(ctx,
			`UPDATE match_participants SET ready_at = $3 WHERE match_id = $1 AND user_id = $2`,
			matchID, userID, value); err != nil {
			return err
		}

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// clearReady withdraws every affirmation, used whenever the terms change.
func clearReady(ctx context.Context, tx pgx.Tx, matchID string) error {
	_, err := tx.Exec(ctx, `UPDATE match_participants SET ready_at = NULL WHERE match_id = $1`, matchID)
	return err
}

func (s *Store) SetContract(ctx context.Context, matchID string, c model.Contract) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if m.Status != model.StatusLobby {
			return fmt.Errorf("%w: the contract is locked once the match starts", ErrConflict)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE matches SET contract_kind = $2, contract_target = $3, contract_detail = $4 WHERE id = $1`,
			matchID, c.Kind, c.Target, c.Detail); err != nil {
			return err
		}
		// Everyone affirmed the previous terms, not these. Making them say so
		// again is the whole point of the ready check.
		if err := clearReady(ctx, tx, matchID); err != nil {
			return err
		}
		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// StartMatch locks the contract and begins play.
func (s *Store) StartMatch(ctx context.Context, matchID string) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if m.Status != model.StatusLobby {
			return fmt.Errorf("%w: this match has already started", ErrConflict)
		}
		// Only players who accepted count. Outstanding invitations do not hold
		// the game up — whoever showed up plays.
		joined := m.JoinedParticipants()
		if len(joined) < 2 {
			return fmt.Errorf("%w: at least two players need to join before you can start", ErrConflict)
		}
		if len(m.Teams) < 2 {
			return fmt.Errorf("%w: a match needs at least two sides", ErrConflict)
		}

		// An unanswered invitation at kickoff is a no.
		if _, err := tx.Exec(ctx, `
			UPDATE match_participants
			SET status = 'declined', responded_at = now(), team_id = NULL
			WHERE match_id = $1 AND status = 'invited'`, matchID); err != nil {
			return err
		}
		if m.Contract.Kind == "" || m.Contract.Kind == model.ContractUnspecified {
			return fmt.Errorf("%w: set the winning contract before starting", ErrConflict)
		}
		if waiting := m.AwaitingReady(); len(waiting) > 0 {
			names := make([]string, 0, len(waiting))
			for _, p := range waiting {
				names = append(names, p.User.DisplayName)
			}
			return fmt.Errorf("%w: still waiting on %s to say they are ready",
				ErrConflict, strings.Join(names, ", "))
		}

		if _, err := tx.Exec(ctx,
			`UPDATE matches SET status = 'in_progress', started_at = now() WHERE id = $1`, matchID); err != nil {
			return err
		}
		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// ClaimVictory declares that teamID won and starts the concession clock.
// Re-claiming the same team is a no-op, so a double-tap on a bad connection is
// harmless. Claiming a different team resets everyone's response.
func (s *Store) ClaimVictory(ctx context.Context, matchID, userID, teamID string, window time.Duration) (model.Match, error) {
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

		switch m.Status {
		case model.StatusInProgress:
			// normal path
		case model.StatusClaimed:
			if m.ClaimedByTeamID != nil && *m.ClaimedByTeamID == teamID {
				out = m // idempotent re-claim
				return nil
			}
			// A different team is now claiming; everyone responds again.
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
			    concede_deadline = now() + $3::interval, dispute_reason = ''
			WHERE id = $1`,
			matchID, teamID, intervalArg(window)); err != nil {
			return err
		}

		// A match whose claiming team is everybody has nobody left to concede.
		if err := finalizeIfRatified(ctx, tx, matchID); err != nil {
			return err
		}

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// answerable checks that userID is entitled to answer the claim standing on
// this match, and that there is still a claim to answer.
//
// CLAIMED and DISPUTED are both open for answers. They are not two stages — they
// are what the same set of answers looks like with and without an objection in
// it — so anything a player may do to one, they may do to the other. That is
// what lets somebody move from objecting back to conceding without an organizer
// unpicking it for them.
func answerable(m model.Match, userID string) error {
	if m.Status != model.StatusClaimed && m.Status != model.StatusDisputed {
		return fmt.Errorf("%w: there is no claim to answer", ErrConflict)
	}
	p, ok := participant(m, userID)
	if !ok {
		return fmt.Errorf("%w: you are not in this match", ErrConflict)
	}
	if m.ClaimedByTeamID != nil && p.TeamID != nil && *p.TeamID == *m.ClaimedByTeamID {
		return fmt.Errorf("%w: you are on the claiming team", ErrConflict)
	}
	return nil
}

// Concede records that userID accepts the standing claim, withdrawing any
// objection they had made. Idempotent. When the last outstanding opponent
// concedes, the match finalizes in the same transaction.
func (s *Store) Concede(ctx context.Context, matchID, userID string, window time.Duration) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if m.Status == model.StatusFinal {
			out = m // already settled; nothing to do
			return nil
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

// Dispute rejects the standing claim, withdrawing any concession userID had
// already given. This clears the concede deadline, which is what distinguishes
// disagreement from silence: silence eventually times out into a concession, an
// objection stops the clock for as long as it stands.
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

// WithdrawClaim takes back a claim the caller's own team made. The match goes
// back to being in play with nothing claimed and every answer wiped.
//
// This is "never mind", not "you won". A claimant who has decided the other
// side actually won cannot hand them the win from here — only they can claim
// it, which keeps the rule that no result reaches the record without the
// winner asserting it and the loser accepting it.
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
		if m.Status != model.StatusClaimed && m.Status != model.StatusDisputed {
			return fmt.Errorf("%w: there is no claim to withdraw", ErrConflict)
		}
		if _, ok := participant(m, userID); !ok {
			return fmt.Errorf("%w: you are not in this match", ErrConflict)
		}
		if !m.OnClaimingTeam(userID) {
			return fmt.Errorf("%w: only the team that claimed the win can take it back", ErrConflict)
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
			    concede_deadline = NULL, dispute_reason = ''
			WHERE id = $1`, matchID); err != nil {
			return err
		}

		out, err = loadMatch(ctx, tx, matchID)
		return err
	})

	return out, err
}

// settleAnswers brings the match's status and clock back in line with the
// answers now on it, and finalizes if they are unanimous.
//
// Every answer is reversible, so status has to be derived from the answers each
// time rather than latched the first time somebody objects. Otherwise a match
// stays DISPUTED after the only objection is withdrawn, with its clock stopped
// forever and nothing but an organizer able to end it.
//
// Withdrawing the last objection restarts the clock with a full window rather
// than resuming the old one. The players who had said nothing were watching an
// argument, not sitting on an unanswered claim, and it is not fair to finalize
// against them for silence they kept while the claim was not actually standing.
func settleAnswers(ctx context.Context, tx pgx.Tx, matchID string, window time.Duration) error {
	m, err := loadMatch(ctx, tx, matchID)
	if err != nil {
		return err
	}
	if m.Status != model.StatusClaimed && m.Status != model.StatusDisputed {
		return nil
	}

	if len(m.Objectors()) > 0 {
		_, err := tx.Exec(ctx, `
			UPDATE matches SET status = 'disputed', concede_deadline = NULL
			WHERE id = $1`, matchID)
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE matches
		SET status = 'claimed',
		    concede_deadline = CASE WHEN status = 'disputed'
		                            THEN now() + $2::interval
		                            ELSE concede_deadline END,
		    dispute_reason = ''
		WHERE id = $1`, matchID, intervalArg(window)); err != nil {
		return err
	}
	return finalizeIfRatified(ctx, tx, matchID)
}

// ResolveDispute settles a disputed match: either the named team wins, or the
// match is voided. Owner-only; the service enforces that.
func (s *Store) ResolveDispute(ctx context.Context, matchID, winningTeamID string, void bool) (model.Match, error) {
	var out model.Match

	err := s.tx(ctx, func(tx pgx.Tx) error {
		m, err := loadMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if m.Status != model.StatusDisputed {
			return fmt.Errorf("%w: this match is not disputed", ErrConflict)
		}

		if void {
			if _, err := tx.Exec(ctx,
				`UPDATE matches SET status = 'canceled', concede_deadline = NULL WHERE id = $1`,
				matchID); err != nil {
				return err
			}
		} else {
			if !hasTeam(m, winningTeamID) {
				return fmt.Errorf("%w: no such team in this match", ErrConflict)
			}
			if err := finalize(ctx, tx, matchID, winningTeamID); err != nil {
				return err
			}
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

// finalizeIfRatified finalizes the match if every opponent of the claiming team
// has conceded.
func finalizeIfRatified(ctx context.Context, tx pgx.Tx, matchID string) error {
	m, err := loadMatch(ctx, tx, matchID)
	if err != nil {
		return err
	}
	if m.Status != model.StatusClaimed || m.ClaimedByTeamID == nil {
		return nil
	}
	if len(m.AwaitingConcession()) > 0 {
		return nil
	}
	// A dispute must not be steamrolled by the others conceding.
	for _, p := range m.OpposingParticipants() {
		if p.DisputedAt != nil {
			return nil
		}
	}
	return finalize(ctx, tx, matchID, *m.ClaimedByTeamID)
}

// finalizeIfExpired finalizes a claimed match whose deadline has passed. A
// disputed match has no deadline, so it is never swept.
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
