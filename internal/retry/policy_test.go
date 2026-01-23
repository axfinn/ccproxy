package retry

import (
	"net/http"
	"testing"
)

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{
			name:       "400 Bad Request should NOT retry",
			statusCode: http.StatusBadRequest,
			want:       false,
		},
		{
			name:       "401 Unauthorized should NOT retry",
			statusCode: http.StatusUnauthorized,
			want:       false,
		},
		{
			name:       "403 Forbidden should NOT retry",
			statusCode: http.StatusForbidden,
			want:       false,
		},
		{
			name:       "404 Not Found should NOT retry",
			statusCode: http.StatusNotFound,
			want:       false,
		},
		{
			name:       "429 Too Many Requests SHOULD retry",
			statusCode: http.StatusTooManyRequests,
			want:       true,
		},
		{
			name:       "500 Internal Server Error SHOULD retry",
			statusCode: http.StatusInternalServerError,
			want:       true,
		},
		{
			name:       "502 Bad Gateway SHOULD retry",
			statusCode: http.StatusBadGateway,
			want:       true,
		},
		{
			name:       "503 Service Unavailable SHOULD retry",
			statusCode: http.StatusServiceUnavailable,
			want:       true,
		},
		{
			name:       "504 Gateway Timeout SHOULD retry",
			statusCode: http.StatusGatewayTimeout,
			want:       true,
		},
	}

	policy := NewPolicy(DefaultRetryConfig())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
			}
			got := policy.ShouldRetry(nil, resp, 0)
			if got != tt.want {
				t.Errorf("ShouldRetry() = %v, want %v for status %d", got, tt.want, tt.statusCode)
			}
		})
	}
}

func TestShouldSwitchAccount(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{
			name:       "400 Bad Request should NOT switch",
			statusCode: http.StatusBadRequest,
			want:       false,
		},
		{
			name:       "401 Unauthorized SHOULD switch",
			statusCode: http.StatusUnauthorized,
			want:       true,
		},
		{
			name:       "403 Forbidden SHOULD switch",
			statusCode: http.StatusForbidden,
			want:       true,
		},
		{
			name:       "429 Too Many Requests SHOULD switch",
			statusCode: http.StatusTooManyRequests,
			want:       true,
		},
		{
			name:       "500 Internal Server Error should NOT switch",
			statusCode: http.StatusInternalServerError,
			want:       false,
		},
		{
			name:       "503 Service Unavailable SHOULD switch",
			statusCode: http.StatusServiceUnavailable,
			want:       true,
		},
	}

	policy := NewPolicy(DefaultRetryConfig())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
			}
			got := policy.ShouldSwitchAccount(nil, resp)
			if got != tt.want {
				t.Errorf("ShouldSwitchAccount() = %v, want %v for status %d", got, tt.want, tt.statusCode)
			}
		})
	}
}
