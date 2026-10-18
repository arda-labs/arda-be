package service

import "errors"

var ErrZeebeRestUnavailable = errors.New("zeebe REST client is not configured")

// ErrZeebeVariablesUnavailable means process variables could not be read from
// the Zeebe Elasticsearch exporter (not configured, unreachable, or it
// answered with something that is not a search result). Callers surface it as
// 503: the engine is fine, its read model is not.
var ErrZeebeVariablesUnavailable = errors.New("zeebe elasticsearch variable read model is unavailable")
