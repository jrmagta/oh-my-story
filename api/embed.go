// Package api holds the OpenAPI description of the HTTP API, served at
// /api/openapi.yaml. It is written by hand; a test in internal/httpapi
// keeps its paths and the server's routes the same.
package api

import _ "embed"

//go:embed openapi.yaml
var OpenAPI []byte
