package email

import (
	"fmt"
	"net/smtp"
	"os"
)

func SendVerificationEmail(to string, verificationLink string) error {

	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	username := os.Getenv("SMTP_USERNAME")
	password := os.Getenv("SMTP_PASSWORD")

	auth := smtp.PlainAuth(
		"",
		username,
		password,
		host,
	)

	subject := "Verify your email"

	body := fmt.Sprintf(
		"Hello,\n\nPlease verify your email by clicking the link below:\n\n%s\n\nThis link will expire soon.\n",
		verificationLink,
	)

	message := []byte(
		"Subject: " + subject + "\r\n" +
			"From: " + username + "\r\n" +
			"To: " + to + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
			"\r\n" +
			body,
	)

	return smtp.SendMail(
		host+":"+port,
		auth,
		username,
		[]string{to},
		message,
	)
}
