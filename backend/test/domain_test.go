package test

import (
	"testing"

	"github.com/inblack/topup-backend/internal/domain"
)

func TestOrderID_SpecExamplesAndGenerate(t *testing.T) {
	if err := domain.ValidateOrderID("ORD-2587965432159-229"); err != nil {
		t.Errorf("expected valid order ID, got error: %v", err)
	}

	if err := domain.ValidateOrderID("ORD-2587965432159-458"); err == nil {
		t.Errorf("expected error for invalid checksum, got nil")
	}

	for i := 0; i < 100; i++ {
		id, err := domain.GenerateOrderID()
		if err != nil {
			t.Fatalf("failed to generate order ID: %v", err)
		}
		if err := domain.ValidateOrderID(id); err != nil {
			t.Errorf("generated invalid order ID %s: %v", id, err)
		}
	}
}

func TestStateMachine_Transitions(t *testing.T) {
	allStatuses := []domain.OrderStatus{
		domain.StatusPendingPayment,
		domain.StatusPaid,
		domain.StatusInQueue,
		domain.StatusInProgress,
		domain.StatusCompleted,
		domain.StatusExpired,
		domain.StatusPaymentFailed,
		domain.StatusFailed,
		domain.StatusRefunded,
		domain.StatusNeedsReview,
	}

	allowed := map[domain.OrderStatus]map[domain.OrderStatus]bool{
		domain.StatusPendingPayment: {
			domain.StatusPaid:          true,
			domain.StatusExpired:       true,
			domain.StatusPaymentFailed: true,
		},
		domain.StatusExpired: {
			domain.StatusPaid: true,
		},
		domain.StatusPaid: {
			domain.StatusInQueue: true,
		},
		domain.StatusInQueue: {
			domain.StatusInProgress: true,
		},
		domain.StatusInProgress: {
			domain.StatusCompleted: true,
			domain.StatusFailed:    true,
			domain.StatusInQueue:   true,
		},
		domain.StatusFailed: {
			domain.StatusRefunded: true,
			domain.StatusInQueue:  true,
		},
		domain.StatusCompleted: {
			domain.StatusRefunded: true,
		},
	}

	for _, from := range allStatuses {
		for _, to := range allStatuses {
			want := false
			if from != to {
				if to == domain.StatusNeedsReview {
					want = true
				} else if targets, ok := allowed[from]; ok && targets[to] {
					want = true
				}
			}

			got := domain.CanTransition(from, to)
			if got != want {
				t.Errorf("CanTransition(%s -> %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}
