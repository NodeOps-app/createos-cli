package api

// User represents a CreateOS user
type User struct {
	ID               string  `json:"id"`
	DisplayName      *string `json:"displayName"`
	Username         *string `json:"username"`
	Email            string  `json:"email"`
	ProfileImagePath *string `json:"profileImagePath"`
	SuspendedAt      *string `json:"suspendedAt"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

type Response[T any] struct {
	Status string `json:"status"`
	Data   T      `json:"data"`
}
