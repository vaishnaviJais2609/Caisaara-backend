package service

import "errors"

var (
	ErrInternal        = errors.New("internal server error")
	ErrPostNotFound    = errors.New("post not found")
	ErrCommentNotFound = errors.New("comment not found")
	ErrNotAllowed      = errors.New("you are not allowed to do this")
	ErrInvalidBody     = errors.New("body must be between 1 and 2000 characters")
	ErrInvalidComment  = errors.New("comment must be between 1 and 500 characters")
	ErrGameNotFound    = errors.New("game not found")
	ErrGameNotFinished = errors.New("only finished games can be shared")
	ErrInvalidFEN      = errors.New("invalid fen")
	ErrSelfFollow      = errors.New("you cannot follow yourself")
	ErrUserNotFound    = errors.New("user not found")
	ErrUserBlocked     = errors.New("you cannot follow this user")
)
