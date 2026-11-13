package test

import (
	"testing"

	"github.com/inblack/topup-backend/internal/domain"
)

func ptrInt64(v int64) *int64 { return &v }

func TestCalculatePrice_SpecExamples(t *testing.T) {
	save10 := &domain.VoucherRule{
		Type:          domain.DiscountPercentage,
		DiscountValue: 10,
		MaxDiscount:   ptrInt64(5000),
		MinSubtotal:   20000,
	}
	flat20 := &domain.VoucherRule{
		Type:          domain.DiscountFixed,
		DiscountValue: 2000,
		MinSubtotal:   10000,
	}

	tests := []struct {
		name    string
		items   []domain.OrderItemInput
		voucher *domain.VoucherRule
		method  domain.PaymentMethod
		want    domain.PriceBreakdown
	}{
		{
			name: "Case 1: DIAMOND-1000 x2, SAVE10, Card",
			items: []domain.OrderItemInput{
				{UnitPrice: 34900, Quantity: 2},
			},
			voucher: save10,
			method:  domain.PaymentCreditCard,
			want: domain.PriceBreakdown{
				Subtotal: 69800,
				Discount: 5000,
				Net:      64800,
				Fee:      2180,
				Total:    66980,
				VAT:      4382,
			},
		},
		{
			name: "Case 2: MONTHLY-PASS x1 + FLASH-SKIN-01 x1, FLAT20, TrueMoney",
			items: []domain.OrderItemInput{
				{UnitPrice: 17900, Quantity: 1},
				{UnitPrice: 9900, Quantity: 1},
			},
			voucher: flat20,
			method:  domain.PaymentTrueMoney,
			want: domain.PriceBreakdown{
				Subtotal: 27800,
				Discount: 2000,
				Net:      25800,
				Fee:      387,
				Total:    26187,
				VAT:      1713,
			},
		},
		{
			name: "Case 3: DIAMOND-5000 x1, SAVE10, PromptPay",
			items: []domain.OrderItemInput{
				{UnitPrice: 159000, Quantity: 1},
			},
			voucher: save10,
			method:  domain.PaymentPromptPay,
			want: domain.PriceBreakdown{
				Subtotal: 159000,
				Discount: 5000,
				Net:      154000,
				Fee:      0,
				Total:    154000,
				VAT:      10075,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.CalculatePrice(tt.items, tt.voucher, tt.method)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
