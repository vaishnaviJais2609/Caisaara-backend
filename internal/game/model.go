package game

import "time"

type Game struct {
	ID                 string     `json:"id"`
	WhitePlayerID      int64      `json:"white_player_id"`
	BlackPlayerID      int64      `json:"black_player_id"`
	TimeControlMinutes int        `json:"time_control_minutes"`
	Status             string     `json:"status"`
	CreatedAt          time.Time  `json:"created_at"`
	StartedAt          time.Time  `json:"started_at"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
}
