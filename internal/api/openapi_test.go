package api

import (
	"bytes"
	"encoding/json"
	importer "musicgetter/internal/import"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestOpenAPIPathsAndLocalReferences(t *testing.T) {
	data, err := os.ReadFile("../../docs/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document["openapi"] != "3.1.1" {
		t.Fatal("unexpected OpenAPI version")
	}
	paths := document["paths"].(map[string]any)
	for path, method := range map[string]string{"/v1/pair/claim": "post", "/v1/me": "get", "/v1/destinations": "get", "/v1/imports": "post", "/v1/imports/{id}/tracks": "post", "/v1/imports/{id}/complete": "post", "/v1/imports/{id}/cancel": "post", "/v1/imports/{id}": "get"} {
		p, ok := paths[path].(map[string]any)
		if !ok || p[method] == nil {
			t.Fatalf("missing %s %s", method, path)
		}
	}
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				if !strings.HasPrefix(ref, "#/") {
					t.Fatal("unexpected external ref")
				}
				var resolved any = document
				for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
					m, ok := resolved.(map[string]any)
					if !ok {
						t.Fatal("broken ref", ref)
					}
					resolved = m[part]
					if resolved == nil {
						t.Fatal("missing ref", ref)
					}
				}
			}
			for _, child := range v {
				visit(child)
			}
		case []any:
			for _, child := range v {
				visit(child)
			}
		}
	}
	visit(document)
}

func TestOpenAPIExamplesMatchRequestValidation(t *testing.T) {
	data, err := os.ReadFile("../../docs/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]struct {
			Post struct {
				RequestBody struct {
					Content map[string]struct {
						Example json.RawMessage `json:"example"`
					} `json:"content"`
				} `json:"requestBody"`
			} `json:"post"`
		} `json:"paths"`
	}
	if err = json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	for path, target := range map[string]interface{ Validate() error }{"/v1/imports": &importer.CreateRequest{}, "/v1/imports/{id}/tracks": &importer.ChunkRequest{}, "/v1/imports/{id}/complete": &importer.CompleteRequest{}} {
		example := spec.Paths[path].Post.RequestBody.Content["application/json"].Example
		request := httptest.NewRequest("POST", path, bytes.NewReader(example))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		if !strictJSON(response, request, importer.MaxPayloadBytes, target) || target.Validate() != nil {
			t.Fatalf("example for %s rejected: %s", path, response.Body)
		}
	}
}
