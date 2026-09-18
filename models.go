package typesafe

import (
	"context"
	"encoding/json"
	"net/http"
)

// ModelsService provides access to the Models API resource (`GET /v1/models`),
// reached through `client.Models`.
type ModelsService struct {
	client *Client
}

// List retrieves the models and model aliases available to the account via `GET /v1/models`.
// Returns *ListModelsResponse containing Models ([]ModelCard), RequestID, and RawHTTPResponse.
func (m *ModelsService) List(ctx context.Context, opts ...RequestOption) (*ListModelsResponse, error) {
	rawBytes, httpResp, err := m.client.doRequest(ctx, http.MethodGet, "/v1/models", nil, opts...)
	if err != nil {
		return nil, err
	}

	var parsedAny any
	if err := json.Unmarshal(rawBytes, &parsedAny); err != nil {
		return nil, NewTypeSafeError("Unexpected response shape from GET /v1/models; expected { models: [...] }.", err)
	}
	rootMap, ok := parsedAny.(map[string]any)
	if !ok {
		return nil, NewTypeSafeError("Unexpected response shape from GET /v1/models; expected { models: [...] }.")
	}
	modelsVal, exists := rootMap["models"]
	if !exists || modelsVal == nil {
		return nil, NewTypeSafeError("Unexpected response shape from GET /v1/models; expected { models: [...] }.")
	}
	if _, ok := modelsVal.([]any); !ok {
		return nil, NewTypeSafeError("Unexpected response shape from GET /v1/models; expected { models: [...] }.")
	}

	var wire struct {
		Models []ModelCard `json:"models"`
	}
	if err := json.Unmarshal(rawBytes, &wire); err != nil {
		return nil, NewTypeSafeError("Unexpected response shape from GET /v1/models; expected { models: [...] }.", err)
	}

	return &ListModelsResponse{
		Models:          wire.Models,
		RequestID:       requestIDFromHeader(httpResp.Header),
		RawHTTPResponse: httpResp,
	}, nil
}

// ListWithResponse retrieves the available models wrapped in WithResponse[[]ModelCard],
// mirroring the JS/TS SDK's `client.models.list().withResponse()`.
func (m *ModelsService) ListWithResponse(ctx context.Context, opts ...RequestOption) (*WithResponse[[]ModelCard], error) {
	res, err := m.List(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &WithResponse[[]ModelCard]{
		Data:      res.Models,
		Response:  res.RawHTTPResponse,
		RequestID: res.RequestID,
	}, nil
}
