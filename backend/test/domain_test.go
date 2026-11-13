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
	if !domain.CanTransition(domain.StatusPendingPayment, domain.StatusPaid) {
		t.Error("PENDING_PAYMENT -> PAID should be allowed")
	}
	if !domain.CanTransition(domain.StatusExpired, domain.StatusPaid) {
		t.Error("EXPIRED -> PAID (late webhook) should be allowed")
	}
	if domain.CanTransition(domain.StatusCompleted, domain.StatusPendingPayment) {
		t.Error("COMPLETED -> PENDING_PAYMENT should be rejected")
	}
	if domain.CanTransition(domain.StatusPaid, domain.StatusFailed) {
		t.Error("PAID -> FAILED (skipping queue/progress) should be rejected")
	}
}
