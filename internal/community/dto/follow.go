package dto

type FollowCountsResponse struct {
	Followers int `json:"followers"`
	Following int `json:"following"`
}

type FollowStatusResponse struct {
	IsFollowing bool `json:"is_following"`
}

type UserListResponse struct {
	Users        []AuthorResponse `json:"users"`
	NextBeforeID *int64          `json:"next_before_id,omitempty"`
}
