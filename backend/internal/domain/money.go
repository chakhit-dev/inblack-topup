package domain

import "errors"

type PaymentMethod string

const (
	PaymentPromptPay  PaymentMethod = "PROMPTPAY"
	PaymentTrueMoney  PaymentMethod = "TRUEMONEY"
	PaymentCreditCard PaymentMethod = "CREDIT_CARD"
)

type DiscountType string

const (
	DiscountPercentage DiscountType = "PERCENTAGE"
	DiscountFixed      DiscountType = "FIXED"
)

type VoucherRule struct {
	Type          DiscountType
	DiscountValue int64
	MaxDiscount   *int64
	MinSubtotal   int64
}

type OrderItemInput struct {
	UnitPrice int64
	Quantity  int64
}

type PriceBreakdown struct {
	Subtotal int64 `json:"subtotal"`
	Discount int64 `json:"discount"`
	Net      int64 `json:"net"`
	Fee      int64 `json:"fee"`
	Total    int64 `json:"total"`
	VAT      int64 `json:"vat"`
}

var (
	ErrInvalidPaymentMethod = errors.New("invalid payment method")
	ErrMinSubtotalNotMet    = errors.New("order subtotal does not meet voucher minimum requirement")
)

func CalculatePrice(items []OrderItemInput, voucher *VoucherRule, method PaymentMethod) (PriceBreakdown, error) {
	var subtotal int64
	for _, item := range items {
		subtotal += item.UnitPrice * item.Quantity
	}

	var discount int64
	if voucher != nil {
		if subtotal < voucher.MinSubtotal {
			return PriceBreakdown{}, ErrMinSubtotalNotMet
		}

		switch voucher.Type {
		case DiscountPercentage:
			discount = (subtotal * voucher.DiscountValue) / 100
			if voucher.MaxDiscount != nil && discount > *voucher.MaxDiscount {
				discount = *voucher.MaxDiscount
			}
		case DiscountFixed:
			discount = voucher.DiscountValue
		}

		if discount > subtotal {
			discount = subtotal
		}
	}

	net := subtotal - discount

	var fee int64
	switch method {
	case PaymentPromptPay:
		fee = 0
	case PaymentTrueMoney:
		fee = ceilDiv(net*15, 1000)
	case PaymentCreditCard:
		fee = ceilDiv(net*29, 1000) + 300
	default:
		return PriceBreakdown{}, ErrInvalidPaymentMethod
	}

	total := net + fee

	exVat := roundHalfUpDiv(total*100, 107)
	vat := total - exVat

	return PriceBreakdown{
		Subtotal: subtotal,
		Discount: discount,
		Net:      net,
		Fee:      fee,
		Total:    total,
		VAT:      vat,
	}, nil
}

func ceilDiv(a, b int64) int64 {
	if a == 0 {
		return 0
	}
	return (a + b - 1) / b
}

func roundHalfUpDiv(a, b int64) int64 {
	return (a + (b / 2)) / b
}
