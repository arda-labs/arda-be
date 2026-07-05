package ardaerrors

import (
	"fmt"
	"net/http"
)

const (
	CodeUnknown          = "common.error.unknown"
	CodeInternal         = "common.error.internal"
	CodeBadGateway       = "common.error.bad_gateway"
	CodeUnauthorized     = "auth.error.unauthorized"
	CodeForbidden        = "auth.error.forbidden"
	CodeNotFound         = "common.error.not_found"
	CodeConflict         = "common.error.conflict"
	CodeInvalidJSON      = "validation.invalid_json"
	CodeInvalidInput     = "validation.invalid_input"
	CodeRequired         = "validation.required"
	CodeMethodNotAllowed = "common.error.method_not_allowed"

	CodeUserNotFound                  = "iam.user.not_found"
	CodeRoleNotFound                  = "iam.role.not_found"
	CodePermissionNotFound            = "iam.permission.not_found"
	CodeSuperAdminLastActive          = "iam.superadmin.last_active"
	CodeSuperAdminSystemUserProtected = "iam.superadmin.system_user_protected"
	CodeSuperAdminRoleProtected       = "iam.superadmin.role_protected"
	CodeSuperAdminPermissionProtected = "iam.superadmin.permission_protected"
	CodeSessionLimitReached           = "iam.session.limit_reached"
)

type Error struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	Cause     error             `json:"-"`
}

type Response struct {
	Error Error `json:"error"`
}

func New(code, message string) *Error {
	return &Error{Code: normalizeCode(code), Message: normalizeMessage(code, message)}
}

func Wrap(code, message string, cause error) *Error {
	return &Error{Code: normalizeCode(code), Message: normalizeMessage(code, message), Cause: cause}
}

func FromStatus(status int, message string) *Error {
	return New(CodeForStatus(status), message)
}

func CodeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return CodeInvalidInput
	case http.StatusUnauthorized:
		return CodeUnauthorized
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusConflict:
		return CodeConflict
	case http.StatusMethodNotAllowed:
		return CodeMethodNotAllowed
	case http.StatusBadGateway:
		return CodeBadGateway
	default:
		if status >= 500 {
			return CodeInternal
		}
		return CodeUnknown
	}
}

func MessageForCode(code string) string {
	switch code {
	case CodeInternal:
		return "Internal server error"
	case CodeBadGateway:
		return "Upstream service error"
	case CodeUnauthorized:
		return "Authentication is required"
	case CodeForbidden:
		return "You do not have permission to perform this action"
	case CodeNotFound:
		return "Resource not found"
	case CodeConflict:
		return "Resource conflict"
	case CodeInvalidJSON:
		return "Request body is not valid JSON"
	case CodeInvalidInput:
		return "Request is invalid"
	case CodeRequired:
		return "Required field is missing"
	case CodeMethodNotAllowed:
		return "Method not allowed"
	case CodeUserNotFound:
		return "User not found"
	case CodeRoleNotFound:
		return "Role not found"
	case CodePermissionNotFound:
		return "Permission not found"
	case CodeSuperAdminLastActive:
		return "Cannot remove the last active superadmin"
	case CodeSuperAdminSystemUserProtected:
		return "System superadmin user is protected"
	case CodeSuperAdminRoleProtected:
		return "System superadmin role is protected"
	case CodeSuperAdminPermissionProtected:
		return "System superadmin permission is protected"
	case CodeSessionLimitReached:
		return "Maximum concurrent sessions reached"
	default:
		return "Something went wrong"
	}
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

func (e *Error) WithRequestID(requestID string) *Error {
	e.RequestID = requestID
	return e
}

func normalizeCode(code string) string {
	if code == "" {
		return CodeUnknown
	}
	return code
}

func normalizeMessage(code, message string) string {
	if message != "" {
		return message
	}
	return MessageForCode(code)
}
