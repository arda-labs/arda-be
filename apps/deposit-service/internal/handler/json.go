package handler

import (
	"encoding/json"
	"net/http"
)

func jsonDecodeDeposit(r *http.Request, target any) error {
	return json.NewDecoder(r.Body).Decode(target)
}
