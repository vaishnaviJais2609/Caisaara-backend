package dto

type ForgotPasswordData struct {
	Email string `json:"email" binding:"required,email"`
}
