package domain

type OrderStatus string

const (
	StatusPendingPayment OrderStatus = "PENDING_PAYMENT"
	StatusPaid           OrderStatus = "PAID"
	StatusInQueue        OrderStatus = "IN_QUEUE"
	StatusInProgress     OrderStatus = "IN_PROGRESS"
	StatusCompleted      OrderStatus = "COMPLETED"
	StatusExpired        OrderStatus = "EXPIRED"
	StatusPaymentFailed  OrderStatus = "PAYMENT_FAILED"
	StatusFailed         OrderStatus = "FAILED"
	StatusRefunded       OrderStatus = "REFUNDED"
	StatusNeedsReview    OrderStatus = "NEEDS_REVIEW"
)

func CanTransition(from, to OrderStatus) bool {
	if from == to {
		return false
	}

	if to == StatusNeedsReview {
		return true
	}

	switch from {
	case StatusPendingPayment:
		return to == StatusPaid || to == StatusExpired || to == StatusPaymentFailed
	case StatusExpired:
		return to == StatusPaid
	case StatusPaid:
		return to == StatusInQueue
	case StatusInQueue:
		return to == StatusInProgress
	case StatusInProgress:
		return to == StatusCompleted || to == StatusFailed || to == StatusInQueue
	case StatusFailed:
		return to == StatusRefunded || to == StatusInQueue
	case StatusCompleted:
		return to == StatusRefunded
	default:
		return false
	}
}
