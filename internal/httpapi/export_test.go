package httpapi

import "net/http"

// Routes lists the patterns the server registers, for holding them against
// the OpenAPI description.
func Routes() []string {
	s := &Server{}
	s.register(http.NewServeMux(), nil)
	return s.routes
}
