// Package handlers provides shared utilities for REST handler packages.
// Sub-packages under handlers implement the actual endpoint logic for agents,
// patterns, skills, skill files, and search.
//
// Documentation:
//   - API: docs/api/openapi/mnemonic-v1.yaml
//   - Design: docs/design/service-layer.md (Error Mapping, Cursor-Based Pagination)
package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/twistingmercury/mnemonic/internal/service"
)

// ProblemBaseURI is the base URI for problem type URIs.
const ProblemBaseURI = "https://mnemonic.example.com/problems/"

// ProblemDetail represents an RFC 7807 Problem Details response.
// @Description RFC 7807 Problem Details error response
type ProblemDetail struct {
	Type     string       `json:"type"             example:"https://mnemonic.example.com/problems/not-found"`
	Title    string       `json:"title"            example:"Not Found"`
	Status   int          `json:"status"           example:"404"`
	Detail   string       `json:"detail,omitempty" example:"agent not found"`
	Instance string       `json:"instance,omitempty" example:"/v1/api/agents/my-agent"`
	TraceID  string       `json:"traceId,omitempty"  example:"trace-abc123"`
	Errors   []FieldError `json:"errors,omitempty"`
}

// FieldError represents a single field-level validation error.
// @Description Field-level validation error returned inside a ProblemDetail
type FieldError struct {
	Field   string `json:"field"   example:"name"`
	Code    string `json:"code"    example:"REQUIRED" enums:"REQUIRED,MAX_LENGTH,MAX_ITEMS,TOO_LARGE,INVALID_FORMAT,INVALID_VALUE"`
	Message string `json:"message" example:"name is required"`
}

// Pagination represents cursor-based pagination metadata.
// @Description Cursor-based pagination metadata returned with list responses
type Pagination struct {
	Limit      int     `json:"limit"       example:"100"`
	Cursor     *string `json:"cursor"      example:"dGVzdA=="`
	NextCursor *string `json:"next_cursor" example:"dGVzdA=="`
	HasMore    bool    `json:"has_more"    example:"false"`
}

// CursorPayload encodes pagination state as an opaque string.
type CursorPayload struct {
	Offset int `json:"o"`
}

// EncodeCursor creates an opaque cursor from an offset.
func EncodeCursor(offset int) string {
	data, _ := json.Marshal(CursorPayload{Offset: offset})
	return base64.URLEncoding.EncodeToString(data)
}

// DecodeCursor extracts the offset from an opaque cursor.
// Returns 0 for empty or invalid cursors (first page).
func DecodeCursor(cursor string) int {
	if cursor == "" {
		return 0
	}
	data, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return 0
	}
	var payload CursorPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0
	}
	return payload.Offset
}

// RespondError maps a service error to an RFC 7807 Problem Details response.
func RespondError(c *gin.Context, err error) {
	traceID := c.GetHeader("X-Request-ID")
	instance := c.Request.URL.Path

	switch {
	case errors.Is(err, service.ErrNotFound):
		c.JSON(http.StatusNotFound, ProblemDetail{
			Type:     ProblemBaseURI + "not-found",
			Title:    "Not Found",
			Status:   http.StatusNotFound,
			Detail:   err.Error(),
			Instance: instance,
			TraceID:  traceID,
		})
	case errors.Is(err, service.ErrConflict):
		c.JSON(http.StatusConflict, ProblemDetail{
			Type:     ProblemBaseURI + "conflict",
			Title:    "Conflict",
			Status:   http.StatusConflict,
			Detail:   err.Error(),
			Instance: instance,
			TraceID:  traceID,
		})
	case errors.Is(err, service.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, ProblemDetail{
			Type:     ProblemBaseURI + "validation-error",
			Title:    "Validation Error",
			Status:   http.StatusBadRequest,
			Detail:   err.Error(),
			Instance: instance,
			TraceID:  traceID,
		})
	case errors.Is(err, service.ErrServiceUnavailable):
		c.JSON(http.StatusServiceUnavailable, ProblemDetail{
			Type:     ProblemBaseURI + "service-unavailable",
			Title:    "Service Unavailable",
			Status:   http.StatusServiceUnavailable,
			Detail:   err.Error(),
			Instance: instance,
			TraceID:  traceID,
		})
	default:
		c.JSON(http.StatusInternalServerError, ProblemDetail{
			Type:     ProblemBaseURI + "internal-error",
			Title:    "Internal Error",
			Status:   http.StatusInternalServerError,
			Detail:   "an unexpected error occurred",
			Instance: instance,
			TraceID:  traceID,
		})
	}
}

// RespondValidationError returns a 400 response with field-level validation errors.
func RespondValidationError(c *gin.Context, detail string, fieldErrors []FieldError) {
	traceID := c.GetHeader("X-Request-ID")
	instance := c.Request.URL.Path

	c.JSON(http.StatusBadRequest, ProblemDetail{
		Type:     ProblemBaseURI + "validation-error",
		Title:    "Validation Error",
		Status:   http.StatusBadRequest,
		Detail:   detail,
		Instance: instance,
		TraceID:  traceID,
		Errors:   fieldErrors,
	})
}

// ParseIntQuery parses an integer query parameter with a default value.
// Clamps the result to [min, max].
func ParseIntQuery(c *gin.Context, key string, defaultVal, minVal, maxVal int) int {
	raw := c.Query(key)
	if raw == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return defaultVal
	}
	if val < minVal {
		return minVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
}

// ParseIntQueryStrict returns (value, true) or (defaultVal, false) when the raw value is out of [minVal, maxVal].
// An empty string returns (defaultVal, true).
func ParseIntQueryStrict(c *gin.Context, key string, defaultVal, minVal, maxVal int) (int, bool) {
	raw := c.Query(key)
	if raw == "" {
		return defaultVal, true
	}
	val, err := strconv.Atoi(raw)
	if err != nil || val < minVal || val > maxVal {
		return defaultVal, false
	}
	return val, true
}

// DecodeCursorStrict returns (offset, true). An empty cursor returns (0, true).
// A non-empty, non-decodable cursor returns (0, false).
func DecodeCursorStrict(cursor string) (int, bool) {
	if cursor == "" {
		return 0, true
	}
	data, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, false
	}
	var payload CursorPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, false
	}
	return payload.Offset, true
}

// ParseFloatQuery parses a float64 query parameter with a default value.
// Clamps the result to [min, max].
func ParseFloatQuery(c *gin.Context, key string, defaultVal, minVal, maxVal float64) float64 {
	raw := c.Query(key)
	if raw == "" {
		return defaultVal
	}
	val, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return defaultVal
	}
	if val < minVal {
		return minVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
}
