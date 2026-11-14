package domain

import "net/http"

type ProblemDetail struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
	Code     string `json:"code"`
}

const (
	CodeOutOfStock            = "OUT_OF_STOCK"
	CodeVoucherExhausted      = "VOUCHER_EXHAUSTED"
	CodePerCustomerLimit      = "PER_CUSTOMER_LIMIT"
	CodeIdempotencyReused     = "IDEMPOTENCY_KEY_REUSED"
	CodeInvalidTransition     = "INVALID_TRANSITION"
	CodeInvalidOrderID        = "INVALID_ORDER_ID"
	CodeValidationFailed      = "VALIDATION_FAILED"
	CodeNotFound              = "NOT_FOUND"
	CodeUnauthorized          = "UNAUTHORIZED"
	CodeForbidden             = "FORBIDDEN"
	CodeRateLimitExceeded     = "RATE_LIMIT_EXCEEDED"
	CodeInternalServerError   = "INTERNAL_SERVER_ERROR"
	CodePerOrderLimitExceeded = "PER_ORDER_LIMIT_EXCEEDED"
	CodeVoucherInvalid        = "VOUCHER_INVALID"
	CodeVoucherMinSubtotal    = "VOUCHER_MIN_SUBTOTAL"
	CodeIdempotencyKeyReused  = "IDEMPOTENCY_KEY_REUSED"
)

func NewProblem(status int, code, title, detail string) ProblemDetail {
	return ProblemDetail{
		Type:   "https://inblackm.com/errors/" + code,
		Title:  title,
		Status: status,
		Detail: detail,
		Code:   code,
	}
}

func ErrProblemOutOfStock(detail string) ProblemDetail {
	return NewProblem(http.StatusConflict, CodeOutOfStock, "Product Out of Stock", detail)
}

func ErrProblemVoucherExhausted(detail string) ProblemDetail {
	return NewProblem(http.StatusUnprocessableEntity, CodeVoucherExhausted, "Voucher Exhausted", detail)
}

func ErrProblemPerCustomerLimit(detail string) ProblemDetail {
	return NewProblem(http.StatusUnprocessableEntity, CodePerCustomerLimit, "Per Customer Limit Exceeded", detail)
}

func ErrProblemIdempotencyReused() ProblemDetail {
	return NewProblem(http.StatusUnprocessableEntity, CodeIdempotencyReused, "Idempotency Key Reused with Different Payload", "The provided Idempotency-Key was already used with a different request payload.")
}

func ErrProblemInvalidTransition(detail string) ProblemDetail {
	return NewProblem(http.StatusConflict, CodeInvalidTransition, "Invalid State Transition", detail)
}
