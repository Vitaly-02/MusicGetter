package domain

import "time"

type ExtensionSession struct {
	ID        ID        `json:"id"`
	OwnerID   ID        `json:"ownerId"`
	ExpiresAt time.Time `json:"expiresAt"`
}
