// Package api embute a especificação OpenAPI servida pela aplicação.
package api

import _ "embed"

// OpenAPISpec é o conteúdo do contrato OpenAPI 3 (YAML) embutido no binário.
//
//go:embed openapi.yaml
var OpenAPISpec []byte
