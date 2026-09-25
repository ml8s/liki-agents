package domain

import "errors"

var (
	ErrInvalidInput = errors.New("invalid input")
)

type Error struct {
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.Code
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func NewError(code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}
