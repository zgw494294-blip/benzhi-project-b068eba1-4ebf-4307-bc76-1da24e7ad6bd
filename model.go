package seedpool

import (
	"errors"
	"fmt"
	"strings"
)

const (
	StatusOpen      = "open"
	StatusFinalized = "finalized"

	AllocationFulfilled = "fulfilled"
	AllocationPartial   = "partial"
)

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrRoundNotFound      = errors.New("round not found")
	ErrRoundClosed        = errors.New("round is finalized")
	ErrAlreadyFinalized   = errors.New("round already finalized")
	ErrDuplicatePlot      = errors.New("plot already has a request")
	ErrIdentifierSource   = errors.New("identifier source failed")
	ErrIdentifierConflict = errors.New("identifier already exists")
	ErrCanceled           = errors.New("operation canceled")
)

// InventoryItem describes packets available for one seed variety.
type InventoryItem struct {
	Variety string `json:"variety"`
	Packets int    `json:"packets"`
}

// RequestItem describes the number of packets requested for one variety.
type RequestItem struct {
	Variety string `json:"variety"`
	Packets int    `json:"packets"`
}

// PlotRequest is one plot's request in the order it was submitted.
type PlotRequest struct {
	PlotID     string        `json:"plot_id"`
	Items      []RequestItem `json:"items"`
	MaxPackets *int          `json:"max_packets,omitempty"`
}

// AllocationItem records how many packets of a requested variety were assigned.
type AllocationItem struct {
	Variety string `json:"variety"`
	Packets int    `json:"packets"`
}

// AllocationRequest records the result for one plot request.
type AllocationRequest struct {
	PlotID           string           `json:"plot_id"`
	Items            []AllocationItem `json:"items"`
	RequestedPackets int              `json:"requested_packets"`
	AllocatedPackets int              `json:"allocated_packets"`
	Status           string           `json:"status"`
}

// Allocation is the immutable result created when a round is finalized.
type Allocation struct {
	Requests  []AllocationRequest `json:"requests"`
	Remaining []InventoryItem     `json:"remaining"`
}

// Round is a defensive snapshot of a distribution round.
type Round struct {
	ID         string          `json:"id"`
	Status     string          `json:"status"`
	Inventory  []InventoryItem `json:"inventory"`
	Requests   []PlotRequest   `json:"requests"`
	Allocation *Allocation     `json:"allocation,omitempty"`
}

func validateInventory(inventory []InventoryItem) error {
	if len(inventory) == 0 {
		return fmt.Errorf("%w: inventory must not be empty", ErrInvalidInput)
	}
	seen := make(map[string]struct{}, len(inventory))
	for _, item := range inventory {
		if strings.TrimSpace(item.Variety) == "" {
			return fmt.Errorf("%w: variety must not be empty", ErrInvalidInput)
		}
		if item.Packets <= 0 {
			return fmt.Errorf("%w: packets for %q must be positive", ErrInvalidInput, item.Variety)
		}
		if _, ok := seen[item.Variety]; ok {
			return fmt.Errorf("%w: duplicate variety %q", ErrInvalidInput, item.Variety)
		}
		seen[item.Variety] = struct{}{}
	}
	return nil
}

func validateRequest(request PlotRequest) error {
	if strings.TrimSpace(request.PlotID) == "" {
		return fmt.Errorf("%w: plot_id must not be empty", ErrInvalidInput)
	}
	if len(request.Items) == 0 {
		return fmt.Errorf("%w: items must not be empty", ErrInvalidInput)
	}
	if request.MaxPackets != nil && *request.MaxPackets <= 0 {
		return fmt.Errorf("%w: max_packets must be positive", ErrInvalidInput)
	}
	seen := make(map[string]struct{}, len(request.Items))
	for _, item := range request.Items {
		if strings.TrimSpace(item.Variety) == "" {
			return fmt.Errorf("%w: variety must not be empty", ErrInvalidInput)
		}
		if item.Packets <= 0 {
			return fmt.Errorf("%w: packets for %q must be positive", ErrInvalidInput, item.Variety)
		}
		if _, ok := seen[item.Variety]; ok {
			return fmt.Errorf("%w: duplicate variety %q in request", ErrInvalidInput, item.Variety)
		}
		seen[item.Variety] = struct{}{}
	}
	if _, err := requestPacketTotal(request.Items); err != nil {
		return err
	}
	return nil
}

func requestPacketTotal(items []RequestItem) (int, error) {
	maxInt := int(^uint(0) >> 1)
	total := 0
	for _, item := range items {
		if item.Packets > maxInt-total {
			return 0, fmt.Errorf("%w: requested packet total is too large", ErrInvalidInput)
		}
		total += item.Packets
	}
	return total, nil
}

func cloneInventory(items []InventoryItem) []InventoryItem {
	return append([]InventoryItem(nil), items...)
}

func cloneRequest(request PlotRequest) PlotRequest {
	request.Items = append([]RequestItem(nil), request.Items...)
	if request.MaxPackets != nil {
		maxPackets := *request.MaxPackets
		request.MaxPackets = &maxPackets
	}
	return request
}

func cloneRequests(requests []PlotRequest) []PlotRequest {
	cloned := make([]PlotRequest, len(requests))
	for i, request := range requests {
		cloned[i] = cloneRequest(request)
	}
	return cloned
}

func cloneAllocation(allocation *Allocation) *Allocation {
	if allocation == nil {
		return nil
	}
	cloned := &Allocation{
		Requests:  make([]AllocationRequest, len(allocation.Requests)),
		Remaining: cloneInventory(allocation.Remaining),
	}
	for i, request := range allocation.Requests {
		cloned.Requests[i] = request
		cloned.Requests[i].Items = append([]AllocationItem(nil), request.Items...)
	}
	return cloned
}

func cloneRound(round Round) Round {
	round.Inventory = cloneInventory(round.Inventory)
	round.Requests = cloneRequests(round.Requests)
	round.Allocation = cloneAllocation(round.Allocation)
	return round
}
