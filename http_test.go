package seedpool

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPWorkflowAndStrictJSON(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStore(func() (string, error) { return "round-1", nil })))
	defer server.Close()

	status, body := httpJSON(t, server.Client(), http.MethodPost, server.URL+"/rounds", `{"inventory":[{"variety":"bean","packets":2},{"variety":"lettuce","packets":1}]}`)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", status, body)
	}
	var created Round
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	requestURL := server.URL + "/rounds/" + created.ID + "/requests"
	status, body = httpJSON(t, server.Client(), http.MethodPost, requestURL, `{"plot_id":"plot-a","items":[{"variety":"bean","packets":2}]}`)
	if status != http.StatusCreated {
		t.Fatalf("request status = %d body = %s", status, body)
	}
	status, body = httpJSON(t, server.Client(), http.MethodPost, server.URL+"/rounds/"+created.ID+"/finalize", "")
	if status != http.StatusOK {
		t.Fatalf("finalize status = %d body = %s", status, body)
	}
	var finalized Round
	if err := json.Unmarshal([]byte(body), &finalized); err != nil {
		t.Fatal(err)
	}
	if finalized.Status != StatusFinalized || finalized.Allocation.Requests[0].Status != AllocationFulfilled {
		t.Fatalf("finalized response = %#v", finalized)
	}
	status, _ = httpJSON(t, server.Client(), http.MethodPost, requestURL, `{"plot_id":"plot-b","items":[{"variety":"bean","packets":1}]}`)
	if status != http.StatusConflict {
		t.Fatalf("request after finalize status = %d", status)
	}
	status, _ = httpJSON(t, server.Client(), http.MethodPost, server.URL+"/rounds", `{"inventory":[],"extra":true}`)
	if status != http.StatusBadRequest {
		t.Fatalf("strict JSON status = %d", status)
	}
	status, _ = httpJSON(t, server.Client(), http.MethodPost, server.URL+"/rounds", `{"inventory":[{"variety":"bean","packets":1}]} {}`)
	if status != http.StatusBadRequest {
		t.Fatalf("trailing JSON status = %d", status)
	}
}

func TestHTTPMaxPacketsPresenceAndMethods(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStore(func() (string, error) { return "round-1", nil })))
	defer server.Close()
	status, body := httpJSON(t, server.Client(), http.MethodPost, server.URL+"/rounds", `{"inventory":[{"variety":"bean","packets":1}]}`)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", status, body)
	}
	var round Round
	if err := json.Unmarshal([]byte(body), &round); err != nil {
		t.Fatal(err)
	}
	endpoint := server.URL + "/rounds/" + round.ID + "/requests"
	for _, payload := range []string{
		`{"plot_id":"p1","items":[{"variety":"bean","packets":1}],"max_packets":0}`,
		`{"plot_id":"p2","items":[{"variety":"bean","packets":1}],"max_packets":null}`,
	} {
		status, _ = httpJSON(t, server.Client(), http.MethodPost, endpoint, payload)
		if status != http.StatusBadRequest {
			t.Fatalf("invalid max_packets status = %d", status)
		}
	}
	status, _ = httpJSON(t, server.Client(), http.MethodPost, server.URL+"/rounds/"+round.ID+"/finalize", `{}`)
	if status != http.StatusBadRequest {
		t.Fatalf("finalize body status = %d", status)
	}
	status, _ = httpJSON(t, server.Client(), http.MethodPut, server.URL+"/rounds", `{}`)
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("method status = %d", status)
	}
}

func TestHTTPIdentifierFailureIsServerError(t *testing.T) {
	server := httptest.NewServer(NewServer(NewStore(func() (string, error) { return "", errors.New("unavailable") })))
	defer server.Close()
	status, body := httpJSON(t, server.Client(), http.MethodPost, server.URL+"/rounds", `{"inventory":[{"variety":"bean","packets":1}]}`)
	if status != http.StatusInternalServerError || !strings.Contains(body, "identifier") {
		t.Fatalf("identifier response = %d %s", status, body)
	}
}

func TestHTTPContextCancellation(t *testing.T) {
	server := NewServer(NewStore(nil))
	request := httptest.NewRequest(http.MethodPost, "/rounds", strings.NewReader(`{"inventory":[{"variety":"bean","packets":1}]}`))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("canceled request status = %d", response.Code)
	}
}

func httpJSON(t *testing.T, client *http.Client, method, endpoint, payload string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(method, endpoint, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}
