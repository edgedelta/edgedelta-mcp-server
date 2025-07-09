package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/go-openapi/spec"
)

func main() {
	// Read and parse swagger.json
	data, err := os.ReadFile("swagger.json")
	if err != nil {
		log.Fatalf("Failed to read swagger.json: %v", err)
	}

	swaggerSpec := &spec.Swagger{}
	if err := json.Unmarshal(data, swaggerSpec); err != nil {
		log.Fatalf("Failed to parse swagger.json: %v", err)
	}

	// Expand all $ref fields
	err = spec.ExpandSpec(swaggerSpec, &spec.ExpandOptions{
		RelativeBase: "",
	})
	if err != nil {
		log.Fatalf("Failed to expand spec: %v", err)
	}

	// Access the specific path and operation
	pathItem, exists := swaggerSpec.Paths.Paths["/v1/orgs/{org_id}/confs"]
	if !exists {
		log.Fatal("Path not found")
	}

	if pathItem.Post == nil {
		log.Fatal("POST operation not found")
	}

	// Find the body parameter
	for _, param := range pathItem.Post.Parameters {
		if param.In == "body" && param.Name == "request" {
			schemaJSON, _ := json.MarshalIndent(param.Schema, "", "  ")

			fmt.Printf("Parameter schema:\n%s\n", string(schemaJSON))
		}
	}
}
