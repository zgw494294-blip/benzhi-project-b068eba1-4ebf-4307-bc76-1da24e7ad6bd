package seedpool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxJSONBody = 1 << 20

// Server exposes the round store through a small JSON API.
type Server struct {
	Store *Store
}

func NewServer(store *Store) *Server {
	if store == nil {
		store = NewStore(nil)
	}
	return &Server{Store: store}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	}
	segments, err := routeSegments(r.URL.EscapedPath())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(segments) == 1 && segments[0] == "rounds" {
		if r.Method != http.MethodPost {
			writeMethodError(w, http.MethodPost)
			return
		}
		s.createRound(w, r)
		return
	}
	if len(segments) >= 2 && segments[0] == "rounds" {
		id, err := url.PathUnescape(segments[1])
		if err != nil || id == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("%w: invalid round id", ErrInvalidInput))
			return
		}
		switch {
		case len(segments) == 2 && r.Method == http.MethodGet:
			s.getRound(w, r, id)
		case len(segments) == 3 && segments[2] == "requests" && r.Method == http.MethodPost:
			s.addRequest(w, r, id)
		case len(segments) == 3 && segments[2] == "finalize" && r.Method == http.MethodPost:
			s.finalize(w, r, id)
		default:
			writeError(w, http.StatusNotFound, ErrRoundNotFound)
		}
		return
	}
	writeError(w, http.StatusNotFound, ErrRoundNotFound)
}

func routeSegments(path string) ([]string, error) {
	if path == "" || path[0] != '/' || strings.HasSuffix(path, "/") {
		return nil, fmt.Errorf("%w: invalid path", ErrInvalidInput)
	}
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return nil, fmt.Errorf("%w: invalid path", ErrInvalidInput)
	}
	return strings.Split(trimmed, "/"), nil
}

type createRoundPayload struct {
	Inventory *[]InventoryItem `json:"inventory"`
}

type requestPayload struct {
	PlotID     string          `json:"plot_id"`
	Items      []RequestItem   `json:"items"`
	MaxPackets json.RawMessage `json:"max_packets"`
}

func (s *Server) createRound(w http.ResponseWriter, r *http.Request) {
	var payload createRoundPayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if payload.Inventory == nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: inventory is required", ErrInvalidInput))
		return
	}
	round, err := s.Store.CreateRound(r.Context(), *payload.Inventory)
	if err != nil {
		writeError(w, statusForError(err), err)
		return
	}
	w.Header().Set("Location", "/rounds/"+url.PathEscape(round.ID))
	writeJSON(w, http.StatusCreated, round)
}

func (s *Server) addRequest(w http.ResponseWriter, r *http.Request, roundID string) {
	var payload requestPayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	request := PlotRequest{PlotID: payload.PlotID, Items: payload.Items}
	if len(payload.MaxPackets) != 0 {
		if bytes.Equal(bytes.TrimSpace(payload.MaxPackets), []byte("null")) {
			writeError(w, http.StatusBadRequest, fmt.Errorf("%w: max_packets must be a positive integer", ErrInvalidInput))
			return
		}
		var maxPackets int
		if err := json.Unmarshal(payload.MaxPackets, &maxPackets); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("%w: max_packets must be a positive integer", ErrInvalidInput))
			return
		}
		request.MaxPackets = &maxPackets
	}
	round, err := s.Store.AddRequest(r.Context(), roundID, request)
	if err != nil {
		writeError(w, statusForError(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, round)
}

func (s *Server) finalize(w http.ResponseWriter, r *http.Request, roundID string) {
	if r.Body != nil {
		if err := rejectBody(r); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	round, err := s.Store.Finalize(r.Context(), roundID)
	if err != nil {
		writeError(w, statusForError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, round)
}

func (s *Server) getRound(w http.ResponseWriter, r *http.Request, roundID string) {
	round, err := s.Store.GetRound(r.Context(), roundID)
	if err != nil {
		writeError(w, statusForError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, round)
}

func decodeJSON(r *http.Request, target any) error {
	if r.Body == nil {
		return fmt.Errorf("%w: request body is required", ErrInvalidInput)
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: malformed JSON: %v", ErrInvalidInput, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%w: exactly one JSON value is required", ErrInvalidInput)
		}
		return fmt.Errorf("%w: malformed trailing JSON: %v", ErrInvalidInput, err)
	}
	return nil
}

func rejectBody(r *http.Request) error {
	var extra any
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%w: finalize does not accept a request body", ErrInvalidInput)
	}
	return nil
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, ErrInvalidInput):
		return http.StatusBadRequest
	case errors.Is(err, ErrRoundNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrRoundClosed), errors.Is(err, ErrAlreadyFinalized), errors.Is(err, ErrDuplicatePlot), errors.Is(err, ErrIdentifierConflict):
		return http.StatusConflict
	case errors.Is(err, ErrCanceled):
		return http.StatusRequestTimeout
	default:
		return http.StatusInternalServerError
	}
}

func writeMethodError(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("%w: method must be %s", ErrInvalidInput, method))
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
