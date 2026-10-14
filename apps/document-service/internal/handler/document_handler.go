package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/document-service/internal/cccd"
	"github.com/arda-labs/arda/apps/document-service/internal/extractor"
	"github.com/arda-labs/arda/apps/document-service/internal/metrics"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

const (
	CodeInvalidForm          = "document.extract.invalid_form"
	CodeUnsupportedDocType   = "document.extract.unsupported_doc_type"
	CodeMissingFront         = "document.extract.missing_front"
	CodeImageTooLarge        = "document.extract.image_too_large"
	CodeUnsupportedMediaType = "document.extract.unsupported_media_type"
	CodeNotADocument         = "document.extract.not_a_document"
	CodeModelBadOutput       = "document.extract.model_bad_output"
	CodeModelUnavailable     = "document.extract.model_unavailable"
	CodeModelTimeout         = "document.extract.model_timeout"
	CodeCanceled             = "document.extract.canceled"
)

type Extractor interface {
	Extract(ctx context.Context, front, back []byte) (cccd.Result, error)
}

type Options struct {
	Extractor     Extractor // nil when no model is configured
	ModelLocation string
	MaxImageBytes int64
	Timeout       time.Duration
	Metrics       *metrics.Metrics
	Logger        *slog.Logger
}

type DocumentHandler struct{ o Options }

func NewDocumentHandler(o Options) *DocumentHandler {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Metrics == nil {
		o.Metrics = metrics.New()
	}
	return &DocumentHandler{o: o}
}

type capabilitiesResponse struct {
	DocTypes      []string `json:"doc_types"`
	ModelLocation string   `json:"model_location"`
}

func (h *DocumentHandler) Capabilities(w http.ResponseWriter, r *http.Request) {
	types := []string{}
	if h.o.Extractor != nil {
		types = append(types, cccd.DocType)
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, capabilitiesResponse{DocTypes: types, ModelLocation: h.o.ModelLocation})
}

type problem struct {
	status        int
	code, message string
	outcome       string
}

func (h *DocumentHandler) Extract(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	outcome := "rejected"
	defer func() { h.o.Metrics.ObserveRequest(cccd.DocType, outcome, h.o.ModelLocation, time.Since(start)) }()

	if h.o.Extractor == nil {
		outcome = "disabled"
		h.fail(w, r, problem{http.StatusServiceUnavailable, CodeModelUnavailable, "Document extraction is not configured", ""})
		return
	}
	// Same cap for the body and the in-memory part budget, so image parts never spill to disk.
	maxBody := 2*h.o.MaxImageBytes + (1 << 20)
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			h.fail(w, r, problem{http.StatusRequestEntityTooLarge, CodeImageTooLarge, "Image is too large", ""})
			return
		}
		h.fail(w, r, problem{http.StatusBadRequest, CodeInvalidForm, "Request must be multipart/form-data", ""})
		return
	}
	defer r.MultipartForm.RemoveAll()

	if strings.TrimSpace(r.FormValue("doc_type")) != cccd.DocType {
		h.fail(w, r, problem{http.StatusBadRequest, CodeUnsupportedDocType, "Unsupported document type", ""})
		return
	}
	front, p := h.readImage(r, "front", true)
	if p != nil {
		h.fail(w, r, *p)
		return
	}
	back, p := h.readImage(r, "back", false)
	if p != nil {
		h.fail(w, r, *p)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.o.Timeout)
	defer cancel()
	res, err := h.o.Extractor.Extract(ctx, front, back)
	if err != nil {
		p := mapError(err)
		level := slog.LevelWarn
		if errors.Is(r.Context().Err(), context.Canceled) {
			p = problem{http.StatusRequestTimeout, CodeCanceled, "The request was canceled before extraction finished", "canceled"}
			level = slog.LevelInfo
		}
		outcome = p.outcome
		h.o.Logger.Log(r.Context(), level, "document extraction failed", "request_id", ardahttp.RequestID(r), "doc_type", cccd.DocType,
			"outcome", outcome, "duration_ms", time.Since(start).Milliseconds(), "err", err)
		h.fail(w, r, p)
		return
	}
	outcome = "ok"
	flags := 0
	res.Fields.Each(func(name string, f cccd.Field) {
		for _, fl := range f.Flags {
			flags++
			h.o.Metrics.ObserveFlag(name, fl)
		}
	})
	h.o.Logger.Info("document extracted", "request_id", ardahttp.RequestID(r), "doc_type", res.DocType,
		"card_version", res.CardVersion, "flags", flags, "warnings", len(res.Warnings), "attempts", res.Attempts,
		"duration_ms", time.Since(start).Milliseconds())
	ardahttp.WriteSuccess(w, r, http.StatusOK, res)
}

func (h *DocumentHandler) readImage(r *http.Request, name string, required bool) ([]byte, *problem) {
	file, header, err := r.FormFile(name)
	if errors.Is(err, http.ErrMissingFile) {
		if required {
			return nil, &problem{http.StatusBadRequest, CodeMissingFront, "Front image is required", ""}
		}
		return nil, nil
	}
	if err != nil {
		return nil, &problem{http.StatusBadRequest, CodeInvalidForm, "Invalid image part", ""}
	}
	defer file.Close()
	if header.Size > h.o.MaxImageBytes {
		return nil, &problem{http.StatusRequestEntityTooLarge, CodeImageTooLarge, "Image is too large", ""}
	}
	data, err := io.ReadAll(io.LimitReader(file, h.o.MaxImageBytes+1))
	if err != nil {
		return nil, &problem{http.StatusBadRequest, CodeInvalidForm, "Invalid image part", ""}
	}
	if int64(len(data)) > h.o.MaxImageBytes {
		return nil, &problem{http.StatusRequestEntityTooLarge, CodeImageTooLarge, "Image is too large", ""}
	}
	if extractor.SniffImageType(data) == "" {
		return nil, &problem{http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "Only JPEG and PNG images are accepted", ""}
	}
	return data, nil
}

func mapError(err error) problem {
	switch {
	case errors.Is(err, cccd.ErrNotCCCD):
		return problem{http.StatusUnprocessableEntity, CodeNotADocument, "The image is not a Vietnamese citizen ID card", "not_document"}
	case errors.Is(err, extractor.ErrUnsupportedImage):
		return problem{http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "Only JPEG and PNG images are accepted", "rejected"}
	case errors.Is(err, extractor.ErrImageTooLarge):
		return problem{http.StatusRequestEntityTooLarge, CodeImageTooLarge, "Image is too large", "rejected"}
	case errors.Is(err, cccd.ErrBadModelOutput):
		return problem{http.StatusBadGateway, CodeModelBadOutput, "The model returned an unusable answer", "bad_output"}
	case errors.Is(err, extractor.ErrModelTimeout), errors.Is(err, context.DeadlineExceeded):
		return problem{http.StatusGatewayTimeout, CodeModelTimeout, "The model did not answer in time", "timeout"}
	case errors.Is(err, extractor.ErrModelUnavailable):
		return problem{http.StatusServiceUnavailable, CodeModelUnavailable, "The document model is unavailable", "unavailable"}
	}
	return problem{http.StatusInternalServerError, ardaerrors.CodeInternal, "Internal error", "error"}
}

func (h *DocumentHandler) fail(w http.ResponseWriter, r *http.Request, p problem) {
	ardahttp.WriteProblem(w, r, p.status, ardaerrors.New(p.code, p.message))
}
