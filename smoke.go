package seedpool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"time"
)

// RunSmoke exercises the complete HTTP workflow with a bounded deadline.
func RunSmoke(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(operationContext(ctx), 2*time.Second)
	defer cancel()
	server := httptest.NewServer(NewServer(NewStore(nil)))
	defer server.Close()

	createBody := struct {
		Inventory []InventoryItem `json:"inventory"`
	}{Inventory: []InventoryItem{{Variety: "bean", Packets: 3}, {Variety: "lettuce", Packets: 2}}}
	var created Round
	if err := smokeJSON(ctx, http.MethodPost, server.URL+"/rounds", createBody, http.StatusCreated, &created); err != nil {
		return fmt.Errorf("create round: %w", err)
	}
	if created.ID == "" {
		return fmt.Errorf("create round returned no id")
	}

	request := PlotRequest{PlotID: "plot-a", Items: []RequestItem{{Variety: "bean", Packets: 2}}}
	if err := smokeJSON(ctx, http.MethodPost, server.URL+"/rounds/"+created.ID+"/requests", request, http.StatusCreated, nil); err != nil {
		return fmt.Errorf("append request: %w", err)
	}
	var finalized Round
	if err := smokeJSON(ctx, http.MethodPost, server.URL+"/rounds/"+created.ID+"/finalize", nil, http.StatusOK, &finalized); err != nil {
		return fmt.Errorf("finalize round: %w", err)
	}
	if finalized.Status != StatusFinalized || finalized.Allocation == nil {
		return fmt.Errorf("finalize round returned incomplete result")
	}
	var retrieved Round
	if err := smokeJSON(ctx, http.MethodGet, server.URL+"/rounds/"+created.ID, nil, http.StatusOK, &retrieved); err != nil {
		return fmt.Errorf("retrieve round: %w", err)
	}
	if retrieved.Status != StatusFinalized || retrieved.Allocation == nil {
		return fmt.Errorf("retrieve round returned incomplete result")
	}
	return nil
}

func smokeJSON(ctx context.Context, method, endpoint string, body any, wantStatus int, result any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		payload, _ := io.ReadAll(response.Body)
		return fmt.Errorf("got HTTP %d: %s", response.StatusCode, string(payload))
	}
	if result == nil {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(result)
}
