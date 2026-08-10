package auth

import "time"

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Nickname     string    `json:"nickname"`
	PasswordHash string    `json:"-"`
	Status       int       `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}
