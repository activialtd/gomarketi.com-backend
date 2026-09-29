package handler

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// TestRegisterRoutes guards against a class of failure that only ever showed
// up at deploy time: Gin panics when the same method and path are registered
// twice, or when two wildcards conflict at the same position. Nothing catches
// that at compile time — the handlers exist either way — so the service built
// fine, shipped, and then crashed on start.
//
// Registration touches no dependencies, so a handler over a nil service is
// enough to exercise it.
func TestRegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("route registration panicked — usually a duplicate path or a wildcard conflict: %v", r)
		}
	}()

	Register(gin.New(), New(nil, zerolog.Nop(), nil), zerolog.Nop(), []string{"http://localhost:3000"}, nil)
}

// TestNoDuplicateRoutes reports every duplicate at once with its path, rather
// than failing on whichever one Gin happens to hit first.
func TestNoDuplicateRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	Register(r, New(nil, zerolog.Nop(), nil), zerolog.Nop(), nil, nil)

	seen := map[string]int{}
	for _, route := range r.Routes() {
		seen[route.Method+" "+route.Path]++
	}
	for route, count := range seen {
		if count > 1 {
			t.Errorf("route registered %d times: %s", count, route)
		}
	}
}
