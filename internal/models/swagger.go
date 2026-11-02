package models

// Swagger response wrappers

type ErrorResponseSwagger struct {
	Success bool   `json:"success" example:"false"`
	Message string `json:"message" example:"Ошибка"`
	Error   string `json:"error" example:"bad_request"`
}

type GenericSuccessResponse struct {
	Success bool   `json:"success" example:"true"`
	Message string `json:"message"`
}

// --- Request structs for inline handlers ---

// GenerateOAuthTokenRequest запрос на генерацию OAuth deep link
type GenerateOAuthTokenRequest struct {
	UserID string `json:"user_id" binding:"required"`
}

// SendNotificationRequest запрос на отправку уведомления
type SendNotificationRequest struct {
	UserID  string `json:"user_id" binding:"required"`
	Title   string `json:"title" binding:"required"`
	Message string `json:"message" binding:"required"`
}

// SendBroadcastRequest запрос на рассылку
type SendBroadcastRequest struct {
	Title   string `json:"title" binding:"required"`
	Message string `json:"message" binding:"required"`
}
