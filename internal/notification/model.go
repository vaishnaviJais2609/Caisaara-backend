package notification

import "time"

const (
	TypeFriendRequest  = "friend_request"
	TypeFriendAccepted = "friend_accepted"
	TypeGameChallenge  = "game_challenge"

	TypeChallengeAccepted   = "challenge_accepted"
	TypeChallengeDeclined   = "challenge_declined"
	TypeChallengeCancelled  = "challenge_cancelled"
	TypeTournamentRound     = "tournament_round_started"
	TypeTournamentFinished  = "tournament_finished"
	TypeTournamentCancelled = "tournament_cancelled"
	TypeDirectMessage       = "direct_message"
	TypePostComment         = "post_comment"
	TypePostReaction        = "post_reaction"
	TypeClubPromoted        = "club_promoted"
	TypeClubDemoted         = "club_demoted"
	TypeClubNewOwner        = "club_new_owner"
	TypeClubKicked          = "club_kicked"
	TypeClubBanned          = "club_banned"
	TypeFollow              = "user_follow"
)

type Notification struct {
	ID        int64          `json:"id"`
	UserID    int64          `json:"-"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	Data      map[string]any `json:"data"`
	IsRead    bool           `json:"is_read"`
	CreatedAt time.Time      `json:"created_at"`
}

var AllTypes = []string{
	TypeFriendRequest,
	TypeFriendAccepted,
	TypeGameChallenge,
	TypeChallengeAccepted,
	TypeChallengeDeclined,
	TypeChallengeCancelled,
	TypeTournamentRound,
	TypeTournamentFinished,
	TypeTournamentCancelled,
	TypeDirectMessage,
	TypePostComment,
	TypePostReaction,
	TypeClubPromoted,
	TypeClubDemoted,
	TypeClubNewOwner,
	TypeClubKicked,
	TypeClubBanned,
	TypeFollow,
}

type Preference struct {
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
}
