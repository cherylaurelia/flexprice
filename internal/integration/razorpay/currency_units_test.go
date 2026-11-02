package razorpay

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Razorpay takes amounts in the currency's own smallest unit: JPY has no subunit, KWD has
// three decimals and the last digit must be 0.
// https://razorpay.com/docs/api/orders/create/ (amount) and
// https://razorpay.com/blog/razorpay-supports-zero-exponent-currency
var currencyUnitCases = []struct {
	name     string
	currency string
	major    string
	want     int64
}{
	{name: "INR is unchanged", currency: "inr", major: "299", want: 29900},
	{name: "USD is unchanged", currency: "usd", major: "12.34", want: 1234},
	{name: "JPY has no subunit", currency: "jpy", major: "12000", want: 12000},
	{name: "KRW has no subunit", currency: "KRW", major: "50000", want: 50000},
	{name: "KWD has three decimals", currency: "kwd", major: "12.5", want: 12500},
	{name: "KWD last digit is sent as 0", currency: "kwd", major: "295.991", want: 295990},
	{name: "BHD has three decimals", currency: "bhd", major: "10", want: 10000},
}

type capturingClient struct {
	RazorpayClient
	paymentLink map[string]interface{}
	order       map[string]interface{}
	recurring   map[string]interface{}
	refundUnits int64
}

func (c *capturingClient) CreatePaymentLink(_ context.Context, data map[string]interface{}) (map[string]interface{}, error) {
	c.paymentLink = data
	return map[string]interface{}{"id": "plink_1", "short_url": "https://rzp.io/i/1", "status": "created"}, nil
}

func (c *capturingClient) CreateOrder(_ context.Context, data map[string]interface{}) (map[string]interface{}, error) {
	c.order = data
	return map[string]interface{}{"id": "order_1"}, nil
}

func (c *capturingClient) CreateRecurringPayment(_ context.Context, data map[string]interface{}) (map[string]interface{}, error) {
	c.recurring = data
	return map[string]interface{}{"razorpay_payment_id": "pay_1"}, nil
}

func (c *capturingClient) RefundPayment(_ context.Context, _ string, units int64, _ string) (map[string]interface{}, error) {
	c.refundUnits = units
	return map[string]interface{}{"id": "rfnd_1", "status": "processed"}, nil
}

type syncedCustomer struct{ RazorpayCustomerService }

func (syncedCustomer) EnsureCustomerSyncedToRazorpay(context.Context, string, interfaces.CustomerService) (*customer.Customer, error) {
	return &customer.Customer{Name: "Acme", Metadata: map[string]string{"razorpay_customer_id": "cust_1"}}, nil
}

type openInvoice struct {
	interfaces.InvoiceService
	remaining decimal.Decimal
	currency  string
}

func (i openInvoice) GetInvoice(context.Context, string) (*dto.InvoiceResponse, error) {
	return &dto.InvoiceResponse{
		ID:              "inv_1",
		Currency:        i.currency,
		PaymentStatus:   types.PaymentStatusPending,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		AmountRemaining: i.remaining,
	}, nil
}

func TestCreatePaymentLink_SendsCurrencySmallestUnit(t *testing.T) {
	for _, tt := range currencyUnitCases {
		t.Run(tt.name, func(t *testing.T) {
			client := &capturingClient{}
			svc := NewPaymentService(client, syncedCustomer{}, nil, grantingLocker{}, logger.NewNoopLogger())
			major := decimal.RequireFromString(tt.major)

			_, err := svc.CreatePaymentLink(context.Background(), &CreatePaymentLinkRequest{
				InvoiceID:  "inv_1",
				CustomerID: "cus_1",
				Amount:     major,
				Currency:   tt.currency,
			}, nil, openInvoice{remaining: major, currency: tt.currency})
			require.NoError(t, err)

			assert.Equal(t, tt.want, client.paymentLink["amount"])
		})
	}
}

func TestBuildLineItems_SendsCurrencySmallestUnit(t *testing.T) {
	for _, tt := range currencyUnitCases {
		t.Run(tt.name, func(t *testing.T) {
			svc := &InvoiceSyncService{logger: logger.NewNoopLogger()}

			items, err := svc.buildLineItems(&invoice.Invoice{
				ID:       "inv_1",
				Currency: tt.currency,
				LineItems: []*invoice.InvoiceLineItem{
					{Amount: decimal.RequireFromString(tt.major), Currency: tt.currency},
				},
			})
			require.NoError(t, err)

			item, ok := items["0"].(RazorpayLineItem)
			require.True(t, ok)
			assert.Equal(t, tt.want, item.Amount)
		})
	}
}

func TestRefundAdapter_SendsCurrencySmallestUnit(t *testing.T) {
	for _, tt := range currencyUnitCases {
		t.Run(tt.name, func(t *testing.T) {
			client := &capturingClient{}
			adapter := &RefundAdapter{Client: client}

			_, err := adapter.RefundPayment(context.Background(), interfaces.RefundProviderRequest{
				GatewayPaymentID: "pay_1",
				Amount:           decimal.RequireFromString(tt.major),
				Currency:         tt.currency,
				IdempotencyKey:   "k1",
			})
			require.NoError(t, err)

			assert.Equal(t, tt.want, client.refundUnits)
		})
	}
}

func TestAutoCharge_SendsCurrencySmallestUnit(t *testing.T) {
	for _, tt := range currencyUnitCases {
		t.Run(tt.name, func(t *testing.T) {
			client := &capturingClient{}
			svc := NewPaymentService(client, syncedCustomer{}, nil, grantingLocker{}, logger.NewNoopLogger())

			_, err := svc.AutoCharge(context.Background(), AutoChargeRequest{
				InvoiceID:          "inv_1",
				RazorpayCustomerID: "cust_1",
				TokenID:            "token_1",
				Amount:             decimal.RequireFromString(tt.major),
				Currency:           tt.currency,
			})
			require.NoError(t, err)

			assert.Equal(t, tt.want, client.order["amount"])
			assert.Equal(t, tt.want, client.recurring["amount"])
		})
	}
}

func TestExtractAmountFromPayment_ReadsCurrencySmallestUnit(t *testing.T) {
	tests := []struct {
		currency string
		units    float64
		want     string
	}{
		{currency: "INR", units: 29900, want: "299"},
		{currency: "JPY", units: 12000, want: "12000"},
		{currency: "KWD", units: 12500, want: "12.5"},
	}
	for _, tt := range tests {
		t.Run(tt.currency, func(t *testing.T) {
			got := extractAmountFromPayment(map[string]interface{}{"amount": tt.units, "currency": tt.currency})
			assert.True(t, decimal.RequireFromString(tt.want).Equal(got), "got %s, want %s", got, tt.want)
		})
	}
}

func TestBuildSyncResponse_ReadsCurrencySmallestUnit(t *testing.T) {
	svc := &InvoiceSyncService{logger: logger.NewNoopLogger()}

	got := svc.buildSyncResponse(map[string]interface{}{"currency": "JPY", "amount": float64(12000), "amount_due": float64(12000)})

	assert.True(t, decimal.NewFromInt(12000).Equal(got.Amount), "amount %s", got.Amount)
	assert.True(t, decimal.NewFromInt(12000).Equal(got.AmountDue), "amount due %s", got.AmountDue)
}

// Razorpay needs a last digit of 0, so the charge can sit just under the invoice.
func TestToSmallestUnit_ThreeDecimalRoundsDown(t *testing.T) {
	assert.Equal(t, int64(12340), toSmallestUnit(decimal.RequireFromString("12.345"), "KWD"))
	assert.True(t, decimal.RequireFromString("12.34").Equal(FromSmallestUnit(12340, "KWD")))
}

type reconcilePayments struct{ interfaces.PaymentService }

func (reconcilePayments) GetPayment(context.Context, string) (*dto.PaymentResponse, error) {
	return &dto.PaymentResponse{ID: "pay_1", DestinationID: "inv_1"}, nil
}

type reconcileInvoices struct {
	interfaces.InvoiceService
	due    decimal.Decimal
	status types.PaymentStatus
}

func (i *reconcileInvoices) GetInvoice(context.Context, string) (*dto.InvoiceResponse, error) {
	return &dto.InvoiceResponse{ID: "inv_1", AmountDue: i.due, AmountPaid: decimal.Zero}, nil
}

func (i *reconcileInvoices) ReconcilePaymentStatus(_ context.Context, _ string, status types.PaymentStatus, _ *decimal.Decimal) error {
	i.status = status
	return nil
}

// Pins the known limit: an invoice whose last digit isn't 0 stays pending after payment.
func TestReconcile_ThreeDecimalRemainderStaysPending(t *testing.T) {
	tests := []struct {
		name string
		due  string
		want types.PaymentStatus
	}{
		{name: "last digit 0 settles", due: "12.340", want: types.PaymentStatusSucceeded},
		{name: "non-zero last digit stays pending", due: "12.345", want: types.PaymentStatusPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewPaymentService(&capturingClient{}, syncedCustomer{}, nil, grantingLocker{}, logger.NewNoopLogger())
			invoices := &reconcileInvoices{due: decimal.RequireFromString(tt.due)}
			paid := FromSmallestUnit(toSmallestUnit(invoices.due, "KWD"), "KWD")

			require.NoError(t, svc.ReconcilePaymentWithInvoice(context.Background(), "pay_1", paid, reconcilePayments{}, invoices))

			assert.Equal(t, tt.want, invoices.status)
		})
	}
}

// Pins the known limit: rounding each line separately can make lines sum below the total.
func TestBuildLineItems_ThreeDecimalLinesCanSumBelowTotal(t *testing.T) {
	svc := &InvoiceSyncService{logger: logger.NewNoopLogger()}
	line := decimal.RequireFromString("1.005")

	items, err := svc.buildLineItems(&invoice.Invoice{
		ID:        "inv_1",
		Currency:  "kwd",
		LineItems: []*invoice.InvoiceLineItem{{Amount: line, Currency: "kwd"}, {Amount: line, Currency: "kwd"}},
	})
	require.NoError(t, err)

	first, ok := items["0"].(RazorpayLineItem)
	require.True(t, ok)
	second, ok := items["1"].(RazorpayLineItem)
	require.True(t, ok)
	assert.Equal(t, int64(2000), first.Amount+second.Amount, "invoice total is 2.010")
}

type authorizationCapture struct {
	capturingClient
	data map[string]interface{}
}

func (c *authorizationCapture) CreateAuthorizationLink(_ context.Context, data map[string]interface{}) (map[string]interface{}, error) {
	c.data = data
	return map[string]interface{}{"id": "inv_rzp_1", "short_url": "https://rzp.io/i/1"}, nil
}

func TestCreateAuthorizationLink_Currency(t *testing.T) {
	maxAmount := decimal.NewFromInt(15000)

	t.Run("two decimals send the expected units", func(t *testing.T) {
		for _, currency := range []string{"INR", "usd"} {
			client := &authorizationCapture{}
			adapter := &CheckoutAdapter{Svc: NewPaymentService(client, syncedCustomer{}, nil, grantingLocker{}, logger.NewNoopLogger())}

			_, err := adapter.CreateAuthorizationLink(context.Background(), interfaces.AuthorizationLinkRequest{
				Amount:    decimal.NewFromInt(299),
				Currency:  currency,
				MaxAmount: &maxAmount,
			})
			require.NoError(t, err)

			assert.Equal(t, int64(29900), client.data["amount"])
			registration, ok := client.data["subscription_registration"].(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, int64(1500000), registration["max_amount"])
		}
	})

	t.Run("other precisions are refused before calling Razorpay", func(t *testing.T) {
		for _, currency := range []string{"JPY", "krw", "KWD", "bhd"} {
			client := &authorizationCapture{}
			adapter := &CheckoutAdapter{Svc: NewPaymentService(client, syncedCustomer{}, nil, grantingLocker{}, logger.NewNoopLogger())}

			_, err := adapter.CreateAuthorizationLink(context.Background(), interfaces.AuthorizationLinkRequest{
				Amount:   decimal.NewFromInt(12000),
				Currency: currency,
			})

			require.True(t, ierr.IsValidation(err), "currency %s: %v", currency, err)
			assert.Nil(t, client.data)
		}
	})
}
