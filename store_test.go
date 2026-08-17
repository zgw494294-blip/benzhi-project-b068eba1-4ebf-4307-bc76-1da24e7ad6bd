package seedpool

import (
	"context"
	"errors"
	"testing"
)

func TestStoreAllocatesInRequestOrderAndHonorsCaps(t *testing.T) {
	store := NewStore(func() (string, error) { return "round-1", nil })
	input := []InventoryItem{{Variety: "bean", Packets: 4}, {Variety: "lettuce", Packets: 2}}
	round, err := store.CreateRound(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	input[0].Packets = 99
	if round.Inventory[0].Packets != 4 {
		t.Fatalf("create result changed with input mutation: %#v", round.Inventory)
	}
	maxPackets := 3
	if _, err := store.AddRequest(context.Background(), round.ID, PlotRequest{
		PlotID: "plot-a", Items: []RequestItem{{Variety: "bean", Packets: 4}}, MaxPackets: &maxPackets,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddRequest(context.Background(), round.ID, PlotRequest{
		PlotID: "plot-b", Items: []RequestItem{{Variety: "bean", Packets: 3}, {Variety: "lettuce", Packets: 2}},
	}); err != nil {
		t.Fatal(err)
	}
	finalized, err := store.Finalize(context.Background(), round.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Status != StatusFinalized || finalized.Allocation == nil {
		t.Fatalf("unexpected finalized round: %#v", finalized)
	}
	got := finalized.Allocation.Requests
	if got[0].Status != AllocationPartial || got[0].AllocatedPackets != 3 {
		t.Fatalf("first allocation = %#v", got[0])
	}
	if got[1].Status != AllocationPartial || got[1].AllocatedPackets != 3 {
		t.Fatalf("second allocation = %#v", got[1])
	}
	if finalized.Allocation.Remaining[0].Packets != 0 || finalized.Allocation.Remaining[1].Packets != 0 {
		t.Fatalf("remaining inventory = %#v", finalized.Allocation.Remaining)
	}
	if _, err := store.Finalize(context.Background(), round.ID); !errors.Is(err, ErrAlreadyFinalized) {
		t.Fatalf("second finalize error = %v", err)
	}
}

func TestStoreRejectsInvalidAndDuplicateRequests(t *testing.T) {
	store := NewStore(func() (string, error) { return "round-1", nil })
	round, err := store.CreateRound(context.Background(), []InventoryItem{{Variety: "bean", Packets: 1}})
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	invalid := PlotRequest{PlotID: "plot-a", Items: []RequestItem{{Variety: "bean", Packets: 1}}, MaxPackets: &zero}
	if _, err := store.AddRequest(context.Background(), round.ID, invalid); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid request error = %v", err)
	}
	valid := PlotRequest{PlotID: "plot-a", Items: []RequestItem{{Variety: "bean", Packets: 1}}}
	if _, err := store.AddRequest(context.Background(), round.ID, valid); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddRequest(context.Background(), round.ID, valid); !errors.Is(err, ErrDuplicatePlot) {
		t.Fatalf("duplicate request error = %v", err)
	}
}

func TestStoreIdentifierFailureDoesNotInsertRound(t *testing.T) {
	store := NewStore(func() (string, error) { return "", errors.New("source unavailable") })
	if _, err := store.CreateRound(context.Background(), []InventoryItem{{Variety: "bean", Packets: 1}}); !errors.Is(err, ErrIdentifierSource) {
		t.Fatalf("identifier error = %v", err)
	}
	if _, err := store.GetRound(context.Background(), "round-1"); !errors.Is(err, ErrRoundNotFound) {
		t.Fatalf("round after source failure = %v", err)
	}
}

func TestStoreReadSnapshotsAreIndependent(t *testing.T) {
	store := NewStore(func() (string, error) { return "round-1", nil })
	created, err := store.CreateRound(context.Background(), []InventoryItem{{Variety: "bean", Packets: 2}})
	if err != nil {
		t.Fatal(err)
	}
	created.Inventory[0].Packets = 0
	created.Inventory = append(created.Inventory, InventoryItem{Variety: "new", Packets: 1})
	read, err := store.GetRound(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Inventory) != 1 || read.Inventory[0].Packets != 2 {
		t.Fatalf("stored inventory was exposed: %#v", read.Inventory)
	}
}

func TestStoreCancellationPreventsCommit(t *testing.T) {
	store := NewStore(func() (string, error) { return "round-1", nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.CreateRound(ctx, []InventoryItem{{Variety: "bean", Packets: 1}}); !errors.Is(err, ErrCanceled) {
		t.Fatalf("create cancellation error = %v", err)
	}
	if _, err := store.GetRound(context.Background(), "round-1"); !errors.Is(err, ErrRoundNotFound) {
		t.Fatalf("canceled create inserted round: %v", err)
	}
	round, err := store.CreateRound(context.Background(), []InventoryItem{{Variety: "bean", Packets: 1}})
	if err != nil {
		t.Fatal(err)
	}
	canceledAdd, cancelAdd := context.WithCancel(context.Background())
	cancelAdd()
	if _, err := store.AddRequest(canceledAdd, round.ID, PlotRequest{PlotID: "plot-a", Items: []RequestItem{{Variety: "bean", Packets: 1}}}); !errors.Is(err, ErrCanceled) {
		t.Fatalf("canceled add error = %v", err)
	}
	open, err := store.GetRound(context.Background(), round.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(open.Requests) != 0 || open.Status != StatusOpen {
		t.Fatalf("canceled add changed round: %#v", open)
	}
	if _, err := store.AddRequest(context.Background(), round.ID, PlotRequest{PlotID: "plot-a", Items: []RequestItem{{Variety: "bean", Packets: 1}}}); err != nil {
		t.Fatal(err)
	}
	canceledFinalize, cancelFinalize := context.WithCancel(context.Background())
	cancelFinalize()
	if _, err := store.Finalize(canceledFinalize, round.ID); !errors.Is(err, ErrCanceled) {
		t.Fatalf("canceled finalize error = %v", err)
	}
	open, err = store.GetRound(context.Background(), round.ID)
	if err != nil {
		t.Fatal(err)
	}
	if open.Status != StatusOpen || open.Allocation != nil {
		t.Fatalf("canceled finalize changed round: %#v", open)
	}
}
