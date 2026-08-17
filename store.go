package seedpool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// IdentifierSource provides round identifiers. It is called before a new
// round is inserted, so a source failure leaves the store unchanged.
type IdentifierSource func() (string, error)

type storedRound struct {
	ID         string
	Status     string
	Inventory  []InventoryItem
	Requests   []PlotRequest
	Allocation *Allocation
}

// Store keeps all rounds in process-local memory.
type Store struct {
	mu     sync.Mutex
	rounds map[string]*storedRound
	ids    IdentifierSource
}

var generatedID uint64

func defaultIdentifier() (string, error) {
	sequence := atomic.AddUint64(&generatedID, 1)
	return fmt.Sprintf("round-%d-%d", time.Now().UnixNano(), sequence), nil
}

// NewStore creates an empty store. A nil source uses a process-local source.
func NewStore(ids IdentifierSource) *Store {
	if ids == nil {
		ids = defaultIdentifier
	}
	return &Store{rounds: make(map[string]*storedRound), ids: ids}
}

func operationContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func checkContext(ctx context.Context) error {
	if err := operationContext(ctx).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrCanceled, err)
	}
	return nil
}

// CreateRound validates and stores a new open round.
func (s *Store) CreateRound(ctx context.Context, inventory []InventoryItem) (Round, error) {
	if err := validateInventory(inventory); err != nil {
		return Round{}, err
	}
	storedInventory := cloneInventory(inventory)
	if err := checkContext(ctx); err != nil {
		return Round{}, err
	}
	id, err := s.ids()
	if err != nil {
		return Round{}, fmt.Errorf("%w: %v", ErrIdentifierSource, err)
	}
	if id == "" {
		return Round{}, fmt.Errorf("%w: empty identifier", ErrIdentifierSource)
	}
	if err := checkContext(ctx); err != nil {
		return Round{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rounds[id]; exists {
		return Round{}, fmt.Errorf("%w: %s", ErrIdentifierConflict, id)
	}
	if err := checkContext(ctx); err != nil {
		return Round{}, err
	}
	s.rounds[id] = &storedRound{
		ID:        id,
		Status:    StatusOpen,
		Inventory: storedInventory,
	}
	return s.snapshotLocked(id)
}

// AddRequest appends one request to an open round.
func (s *Store) AddRequest(ctx context.Context, roundID string, request PlotRequest) (Round, error) {
	if err := validateRequest(request); err != nil {
		return Round{}, err
	}
	if err := checkContext(ctx); err != nil {
		return Round{}, err
	}
	storedRequest := cloneRequest(request)

	s.mu.Lock()
	defer s.mu.Unlock()
	round, ok := s.rounds[roundID]
	if !ok {
		return Round{}, ErrRoundNotFound
	}
	if round.Status != StatusOpen {
		return Round{}, ErrRoundClosed
	}
	for _, existing := range round.Requests {
		if existing.PlotID == request.PlotID {
			return Round{}, ErrDuplicatePlot
		}
	}
	if err := checkContext(ctx); err != nil {
		return Round{}, err
	}
	round.Requests = append(round.Requests, storedRequest)
	return s.snapshotLocked(roundID)
}

// Finalize computes and atomically commits the immutable allocation result.
func (s *Store) Finalize(ctx context.Context, roundID string) (Round, error) {
	if err := checkContext(ctx); err != nil {
		return Round{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	round, ok := s.rounds[roundID]
	if !ok {
		return Round{}, ErrRoundNotFound
	}
	if round.Status != StatusOpen {
		return Round{}, ErrAlreadyFinalized
	}
	allocation, err := calculateAllocation(ctx, round)
	if err != nil {
		return Round{}, err
	}
	next := &storedRound{
		ID:         round.ID,
		Status:     StatusFinalized,
		Inventory:  cloneInventory(round.Inventory),
		Requests:   cloneRequests(round.Requests),
		Allocation: cloneAllocation(&allocation),
	}
	if err := checkContext(ctx); err != nil {
		return Round{}, err
	}
	s.rounds[roundID] = next
	return s.snapshotLocked(roundID)
}

// GetRound returns a defensive snapshot of a round.
func (s *Store) GetRound(ctx context.Context, roundID string) (Round, error) {
	if err := checkContext(ctx); err != nil {
		return Round{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked(roundID)
}

func (s *Store) snapshotLocked(roundID string) (Round, error) {
	round, ok := s.rounds[roundID]
	if !ok {
		return Round{}, ErrRoundNotFound
	}
	return cloneRound(Round{
		ID:         round.ID,
		Status:     round.Status,
		Inventory:  round.Inventory,
		Requests:   round.Requests,
		Allocation: round.Allocation,
	}), nil
}

func calculateAllocation(ctx context.Context, round *storedRound) (Allocation, error) {
	stock := make(map[string]int, len(round.Inventory))
	for _, item := range round.Inventory {
		stock[item.Variety] = item.Packets
	}
	result := Allocation{Requests: make([]AllocationRequest, 0, len(round.Requests))}
	for _, request := range round.Requests {
		if err := checkContext(ctx); err != nil {
			return Allocation{}, err
		}
		requestedPackets, err := requestPacketTotal(request.Items)
		if err != nil {
			return Allocation{}, err
		}
		capPackets := requestedPackets
		if request.MaxPackets != nil && *request.MaxPackets < capPackets {
			capPackets = *request.MaxPackets
		}
		remainingCap := capPackets
		allocated := AllocationRequest{
			PlotID:           request.PlotID,
			Items:            make([]AllocationItem, 0, len(request.Items)),
			RequestedPackets: requestedPackets,
		}
		for _, item := range request.Items {
			if err := checkContext(ctx); err != nil {
				return Allocation{}, err
			}
			take := stock[item.Variety]
			if take > item.Packets {
				take = item.Packets
			}
			if take > remainingCap {
				take = remainingCap
			}
			if take < 0 {
				take = 0
			}
			allocated.Items = append(allocated.Items, AllocationItem{Variety: item.Variety, Packets: take})
			allocated.AllocatedPackets += take
			stock[item.Variety] -= take
			remainingCap -= take
		}
		if allocated.AllocatedPackets == requestedPackets {
			allocated.Status = AllocationFulfilled
		} else {
			allocated.Status = AllocationPartial
		}
		result.Requests = append(result.Requests, allocated)
	}
	result.Remaining = make([]InventoryItem, 0, len(round.Inventory))
	for _, item := range round.Inventory {
		result.Remaining = append(result.Remaining, InventoryItem{Variety: item.Variety, Packets: stock[item.Variety]})
	}
	return result, nil
}
