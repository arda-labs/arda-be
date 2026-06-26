package ardaerrors

import "fmt"

type Error struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	Cause   error             `json:"-"`
}

func New(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func Wrap(code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *Error) WithField(field, code string) *Error {
	if e.Fields == nil {
		e.Fields = make(map[string]string)
	}
	e.Fields[field] = code
	return e
}
