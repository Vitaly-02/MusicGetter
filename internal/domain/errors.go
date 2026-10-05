package domain

import "errors"

var (
	ErrInvalid   = errors.New("invalid domain data")
	ErrNotFound  = errors.New("resource not found")
	ErrConflict  = errors.New("resource conflict")
	ErrLeaseLost = errors.New("job lease lost")
	ErrStorage   = errors.New("storage operation failed")
)
