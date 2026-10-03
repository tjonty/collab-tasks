package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/changes"
	"github.com/tjonty/collab-tasks/api/internal/domain"
)

// respondError writes err as the contract's {code, message, details?} body
// with the matching status. Unexpected errors are logged, not returned.
func respondError(w http.ResponseWriter, r *http.Request, err error) {
	status, body := classify(err)
	if status >= http.StatusInternalServerError {
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "status", status, "err", err)
	}
	writeError(w, status, body)
}

func classify(err error) (int, apigen.Error) {
	var invalid *domain.ValidationError
	var rejected *changes.Error
	switch {
	case errors.As(err, &invalid):
		return http.StatusUnprocessableEntity, apigen.Error{
			Code:    apigen.ErrorCodeValidationFailed,
			Message: invalid.Error(),
			Details: &apigen.ErrorDetails{Field: &invalid.Field},
		}
	case errors.As(err, &rejected):
		return statusFor(rejected.Code), apigen.Error{Code: rejected.Code, Message: rejected.Message}
	case unavailable(err):
		return http.StatusServiceUnavailable, apigen.Error{
			Code:    apigen.ErrorCodeUnavailable,
			Message: "the database is unreachable; retrying with the same ids is safe",
		}
	default:
		return http.StatusInternalServerError, apigen.Error{Code: apigen.ErrorCodeInternal, Message: "internal error"}
	}
}

func statusFor(code apigen.ErrorCode) int {
	switch code {
	case apigen.ErrorCodeUnknownUser:
		return http.StatusUnauthorized
	case apigen.ErrorCodeProjectNotFound:
		return http.StatusNotFound
	case apigen.ErrorCodeValidationFailed:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusConflict
	}
}

// unavailable reports whether err means Postgres can't be reached right now,
// as opposed to a bug.
func unavailable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 08: connection exception; 57P0x: the server is shutting down;
		// 53300: too many connections.
		return strings.HasPrefix(pgErr.Code, "08") || strings.HasPrefix(pgErr.Code, "57P0") || pgErr.Code == "53300"
	}
	var connectErr *pgconn.ConnectError
	var netErr net.Error
	return errors.As(err, &connectErr) || errors.As(err, &netErr) || pgconn.SafeToRetry(err)
}

// badBody answers a request body that isn't valid JSON for the operation.
func badBody(w http.ResponseWriter, _ *http.Request, err error) {
	writeError(w, http.StatusUnprocessableEntity, apigen.Error{
		Code:    apigen.ErrorCodeValidationFailed,
		Message: err.Error(),
	})
}

// badParam answers a path parameter that doesn't parse, e.g. a projectId
// that isn't a UUID.
func badParam(w http.ResponseWriter, _ *http.Request, err error) {
	body := apigen.Error{Code: apigen.ErrorCodeValidationFailed, Message: err.Error()}
	var paramErr *apigen.InvalidParamFormatError
	if errors.As(err, &paramErr) {
		body.Details = &apigen.ErrorDetails{Field: &paramErr.ParamName}
	}
	writeError(w, http.StatusUnprocessableEntity, body)
}

func writeError(w http.ResponseWriter, status int, body apigen.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write error response", "err", err)
	}
}
