package model

import "time"

type User struct {
	ID       int64
	Username string
	Email    string
	Password string

	EmailVerified              bool
	EmailVerificationToken     string
	EmailVerificationExpiresAt time.Time
}
