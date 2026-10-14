package cccd

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/document-service/internal/extractor"
)

var (
	ErrNotCCCD        = errors.New("image is not a CCCD")
	ErrBadModelOutput = errors.New("model output unusable")
)

type Completer interface {
	Complete(ctx context.Context, images [][]byte, prompt string) (string, error)
}

// vietnam has no DST; a fixed zone avoids shipping tzdata in the image.
var vietnam = time.FixedZone("ICT", 7*3600)

type Service struct {
	llm      Completer
	model    string
	location string
	maxSide  int
	now      func() time.Time
}

func NewService(llm Completer, model, location string, maxSide int, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{llm: llm, model: model, location: location, maxSide: maxSide, now: now}
}

// Extract reads a front image and an optional back image. Unusable model
// output is retried once within the same ctx deadline; model transport
// errors are not retried.
func (s *Service) Extract(ctx context.Context, front, back []byte) (Result, error) {
	images := make([][]byte, 0, 2)
	for _, raw := range [][]byte{front, back} {
		if raw == nil {
			continue
		}
		img, err := extractor.PrepareImage(raw, s.maxSide)
		if err != nil {
			return Result{}, err
		}
		images = append(images, img)
	}

	var out map[string]any
	for attempt := 0; attempt < 2 && out == nil; attempt++ {
		text, err := s.llm.Complete(ctx, images, Prompt)
		if err != nil {
			return Result{}, err
		}
		if obj, err := extractor.ExtractJSONObject(text); err == nil {
			if _, ok := obj["card_version"]; ok {
				out = obj
			}
		}
	}
	if out == nil {
		return Result{}, ErrBadModelOutput
	}

	version := normalizeCardVersion(str(out, "card_version"))
	if version == cardNotCCCD {
		return Result{}, ErrNotCCCD
	}
	res := s.build(out, version, back != nil)
	res.Model, res.ModelLocation = s.model, s.location
	return res, nil
}

func (s *Service) build(out map[string]any, version string, hasBack bool) Result {
	dob, dobT := dateField(str(out, "date_of_birth"))
	issue, issueT := dateField(str(out, "issue_date"))
	expiry, expiryT, noExpiry := expiryField(str(out, "expiry_date"))
	f := Fields{
		IDNumber:      idField(str(out, "id_number")),
		FullName:      textField(str(out, "full_name")),
		DateOfBirth:   dob,
		Sex:           sexField(str(out, "sex")),
		Nationality:   textField(str(out, "nationality")),
		PlaceOfOrigin: textField(str(out, "place_of_origin")),
		Residence:     textField(str(out, "residence")),
		IssueDate:     issue,
		IssuePlace:    textField(str(out, "issue_place")),
		ExpiryDate:    expiry,
	}
	validate(&f, dates{dob: dobT, issue: issueT, expiry: expiryT, noExpiry: noExpiry}, s.today())

	warnings := []string{}
	if !hasBack {
		warnings = append(warnings, WarnBackSideMissing)
	}
	if version == CardUnknown {
		warnings = append(warnings, WarnCardVersionUnknown)
	}
	if strings.EqualFold(strings.TrimSpace(str(out, "image_quality")), "low") {
		warnings = append(warnings, WarnImageQualityLow)
	}
	return Result{DocType: DocType, CardVersion: version, Fields: f, Warnings: warnings}
}

// today is the Vietnam calendar date at UTC midnight, comparable with parsed dates.
func (s *Service) today() time.Time {
	n := s.now().In(vietnam)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// str tolerates models that emit numbers or null instead of strings.
func str(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}
