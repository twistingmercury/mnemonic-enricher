package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/handlers"
	"github.com/twistingmercury/mnemonic/internal/service"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestEncodeCursor_DecodeCursor_Roundtrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		offset int
	}{
		{name: "zero offset", offset: 0},
		{name: "positive offset", offset: 20},
		{name: "large offset", offset: 10000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cursor := handlers.EncodeCursor(tt.offset)
			assert.NotEmpty(t, cursor)
			got := handlers.DecodeCursor(cursor)
			assert.Equal(t, tt.offset, got)
		})
	}
}

func TestDecodeCursor_InvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		cursor string
	}{
		{name: "empty string", cursor: ""},
		{name: "invalid base64", cursor: "not-valid-base64!!!"},
		{name: "valid base64 but invalid json", cursor: "aGVsbG8="},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := handlers.DecodeCursor(tt.cursor)
			assert.Equal(t, 0, got)
		})
	}
}

func TestRespondError_ServiceErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantType   string
	}{
		{
			name:       "not found",
			err:        service.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantType:   "not-found",
		},
		{
			name:       "wrapped not found",
			err:        errors.Join(service.ErrNotFound, errors.New("agent not found")),
			wantStatus: http.StatusNotFound,
			wantType:   "not-found",
		},
		{
			name:       "conflict",
			err:        service.ErrConflict,
			wantStatus: http.StatusConflict,
			wantType:   "conflict",
		},
		{
			name:       "invalid input",
			err:        service.ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
			wantType:   "validation-error",
		},
		{
			name:       "service unavailable",
			err:        service.ErrServiceUnavailable,
			wantStatus: http.StatusServiceUnavailable,
			wantType:   "service-unavailable",
		},
		{
			name:       "unknown error",
			err:        errors.New("unexpected"),
			wantStatus: http.StatusInternalServerError,
			wantType:   "internal-error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

			handlers.RespondError(c, tt.err)

			assert.Equal(t, tt.wantStatus, w.Code)
			assert.Contains(t, w.Body.String(), tt.wantType)
		})
	}
}

func TestParseIntQuery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		query      string
		defaultVal int
		minVal     int
		maxVal     int
		want       int
	}{
		{name: "missing param uses default", query: "", defaultVal: 20, minVal: 1, maxVal: 100, want: 20},
		{name: "valid param", query: "50", defaultVal: 20, minVal: 1, maxVal: 100, want: 50},
		{name: "below min", query: "0", defaultVal: 20, minVal: 1, maxVal: 100, want: 1},
		{name: "above max", query: "500", defaultVal: 20, minVal: 1, maxVal: 100, want: 100},
		{name: "non-numeric uses default", query: "abc", defaultVal: 20, minVal: 1, maxVal: 100, want: 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/test?limit="+tt.query, nil)

			got := handlers.ParseIntQuery(c, "limit", tt.defaultVal, tt.minVal, tt.maxVal)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRespondValidationError(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)

	handlers.RespondValidationError(c, "bad fields", []handlers.FieldError{
		{Field: "name", Code: "REQUIRED", Message: "name is required"},
	})

	require.Equal(t, http.StatusBadRequest, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "validation-error")
	assert.Contains(t, body, "REQUIRED")
	assert.Contains(t, body, "name is required")
}
