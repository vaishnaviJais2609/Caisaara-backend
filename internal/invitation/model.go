package invitation

type Invite struct {
	Code               string `json:"code"`
	CreatorID          int64  `json:"creator_id"`
	TimeControlMinutes int    `json:"time_control_minutes"`
	Color              string `json:"color"`
}
