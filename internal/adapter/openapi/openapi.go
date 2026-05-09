package openapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pb33f/libopenapi"

	"github.com/jayimbery/bt/internal/adapter"
	"github.com/jayimbery/bt/pkg/model"
)

// Adapter implements adapter.Adapter for OpenAPI 3.x specs.
type Adapter struct {
	httpClient *http.Client
}

// New returns a new OpenAPI adapter.
func New() adapter.Adapter {
	return &Adapter{httpClient: &http.Client{Timeout: 60 * time.Second}}
}

func (a *Adapter) Name() string { return "openapi" }

func (a *Adapter) Validate(ctx context.Context, target model.Target) error {
	data, err := a.loadSchema(ctx, target.SchemaPath)
	if err != nil {
		return fmt.Errorf("cannot read schema: %w", err)
	}
	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return fmt.Errorf("cannot parse schema: %w", err)
	}
	if _, err := doc.BuildV3Model(); err != nil {
		return fmt.Errorf("invalid OpenAPI spec: %w", err)
	}
	return nil
}

func (a *Adapter) Discover(ctx context.Context, target model.Target) ([]model.Operation, error) {
	data, err := a.loadSchema(ctx, target.SchemaPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read schema: %w", err)
	}
	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("cannot parse schema: %w", err)
	}
	v3model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("invalid OpenAPI spec: %w", err)
	}
	if v3model.Model.Paths == nil || v3model.Model.Paths.PathItems == nil {
		return nil, nil
	}
	var ops []model.Operation
	for path, item := range v3model.Model.Paths.PathItems.FromOldest() {
		for method, op := range operationsFromPathItem(item) {
			ops = append(ops, normaliseOperation(method, path, op))
		}
	}
	return ops, nil
}

func (a *Adapter) loadSchema(ctx context.Context, schemaPath string) ([]byte, error) {
	if schemaPath == "" {
		return nil, fmt.Errorf("schema path is empty")
	}
	if strings.HasPrefix(schemaPath, "http://") || strings.HasPrefix(schemaPath, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, schemaPath, nil)
		if err != nil {
			return nil, err
		}
		resp, err := a.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("fetch schema: HTTP %d", resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(schemaPath)
}
