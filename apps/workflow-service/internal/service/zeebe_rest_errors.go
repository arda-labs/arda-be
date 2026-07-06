package service

import "errors"

var ErrZeebeRestUnavailable = errors.New("zeebe REST client is not configured")
