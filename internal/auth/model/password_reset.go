package model

import "time"

type PasswordResetToken struct {
	ID             int64
	UserID         int64
	CodeHash       string
	ResetTokenHash *string
	ExpiresAt      time.Time
	VerifiedAt     *time.Time
	UsedAt         *time.Time
	Attempts       int
	CreatedAt      time.Time
}
