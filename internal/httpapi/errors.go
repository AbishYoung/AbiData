package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/fault"
	"abidata/internal/obs"
)

// ErrorBody is the error envelope's payload (brief <http_api>).
type ErrorBody struct {
	Code      fault.Code     `json:"code"`
	Title     string         `json:"title"`
	Message   string         `json:"message"`
	Recovery  string         `json:"recovery"`
	RequestID string         `json:"request_id"`
	Details   map[string]any `json:"details,omitempty"`
}

// Schema lists every catalog code as an enum so clients get a typed union.
func (ErrorBody) Schema(r huma.Registry) *huma.Schema {
	codes := make([]any, 0)
	for _, c := range fault.Codes() {
		codes = append(codes, string(c))
	}
	return &huma.Schema{
		Type:     huma.TypeObject,
		Required: []string{"code", "title", "message", "recovery", "request_id"},
		Properties: map[string]*huma.Schema{
			"code":       {Type: huma.TypeString, Enum: codes},
			"title":      {Type: huma.TypeString},
			"message":    {Type: huma.TypeString},
			"recovery":   {Type: huma.TypeString},
			"request_id": {Type: huma.TypeString},
			"details":    {Type: huma.TypeObject, AdditionalProperties: true},
		},
		AdditionalProperties: false,
	}
}

// APIError is the {"error": {...}} envelope. It implements huma.StatusError
// and huma.HeadersError.
type APIError struct {
	status  int
	headers http.Header
	Err     ErrorBody `json:"error"`
}

func (e *APIError) Error() string           { return string(e.Err.Code) + ": " + e.Err.Message }
func (e *APIError) GetStatus() int          { return e.status }
func (e *APIError) GetHeaders() http.Header { return e.headers }

// toAPIError converts any error into the envelope, logging unexpected ones.
func toAPIError(ctx context.Context, log *slog.Logger, err error) *APIError {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae
	}
	fe := fault.Classify(err)
	entry := fe.Entry()
	body := ErrorBody{
		Code: fe.Code, Title: entry.Title, Message: fe.DisplayMessage(), Recovery: entry.Recovery,
		RequestID: obs.RequestID(ctx), Details: fe.Details,
	}
	if fe.Code == fault.Internal {
		body.Message = entry.Message
		body.Details = nil
		if log != nil {
			log.ErrorContext(ctx, "internal error", "err", err)
		}
	}
	out := &APIError{status: entry.HTTPStatus, Err: body}
	if fe.RetryAfter > 0 {
		secs := int(fe.RetryAfter.Seconds() + 0.999)
		out.headers = http.Header{"Retry-After": []string{strconv.Itoa(max(secs, 1))}}
	}
	return out
}

// installHumaErrors routes huma's own errors (validation, parse) through the
// envelope. Request validation failures are BAD_REQUEST/400 (ADR 0003).
func installHumaErrors(log *slog.Logger) {
	huma.NewErrorWithContext = func(hctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
		ctx := context.Background()
		if hctx != nil {
			ctx = hctx.Context()
		}
		for _, e := range errs {
			var ae *APIError
			if errors.As(e, &ae) {
				return ae
			}
			var fe *fault.Error
			if errors.As(e, &fe) {
				return toAPIError(ctx, log, fe)
			}
		}
		code := fault.Internal
		switch {
		case status == http.StatusUnprocessableEntity || status == http.StatusBadRequest:
			code = fault.BadRequest
		case status == http.StatusNotFound:
			code = fault.NotFound
		case status == http.StatusUnauthorized:
			code = fault.Unauthenticated
		case status == http.StatusForbidden:
			code = fault.Forbidden
		case status == http.StatusRequestEntityTooLarge:
			code = fault.BadRequest
		case status == http.StatusPreconditionFailed || status == http.StatusConflict:
			code = fault.ConcurrentModification
		case status < 500:
			code = fault.BadRequest
		}
		fe := &fault.Error{Code: code}
		if code == fault.Internal && len(errs) > 0 {
			fe.Cause = errors.Join(errs...) // keep the cause for the server log
		}
		if code == fault.BadRequest {
			fe.Message = msg
			var details []map[string]any
			for _, e := range errs {
				var ed *huma.ErrorDetail
				if errors.As(e, &ed) {
					details = append(details, map[string]any{"location": ed.Location, "message": ed.Message})
				}
			}
			if len(details) > 0 {
				fe.Details = map[string]any{"errors": details}
			}
		}
		return toAPIError(ctx, log, fe)
	}
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		return huma.NewErrorWithContext(nil, status, msg, errs...)
	}
}

// writeError writes the envelope from a plain http handler.
func writeError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	ae := toAPIError(r.Context(), log, err)
	for k, vs := range ae.headers {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(ae.status)
	_ = json.NewEncoder(w).Encode(ae)
}

// writeHumaError writes the envelope from a huma middleware.
func writeHumaError(hctx huma.Context, log *slog.Logger, err error) {
	ae := toAPIError(hctx.Context(), log, err)
	for k, vs := range ae.headers {
		for _, v := range vs {
			hctx.AppendHeader(k, v)
		}
	}
	hctx.SetHeader("Content-Type", "application/json")
	hctx.SetHeader("Cache-Control", "no-store")
	hctx.SetStatus(ae.status)
	_ = json.NewEncoder(hctx.BodyWriter()).Encode(ae)
}
