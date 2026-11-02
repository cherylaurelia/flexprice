package moyasar

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturingPaymentClient struct {
	MoyasarClient
	req *CreatePaymentRequest
}

func (c *capturingPaymentClient) CreatePayment(_ context.Context, req *CreatePaymentRequest) (*CreatePaymentResponse, error) {
	c.req = req
	return &CreatePaymentResponse{MoyasarPayment: MoyasarPayment{ID: "pay_1", Status: "initiated"}}, nil
}

func TestCreatePaymentLink_SendsCurrencySmallestUnit(t *testing.T) {
	tests := []struct {
		currency string
		major    string
		want     int
	}{
		{currency: "SAR", major: "100.5", want: 10050},
		{currency: "JPY", major: "12000", want: 12000},
		{currency: "KWD", major: "12.5", want: 12500},
	}
	for _, tt := range tests {
		t.Run(tt.currency, func(t *testing.T) {
			client := &capturingPaymentClient{}
			svc := NewPaymentService(client, nil, nil, logger.NewNoopLogger())

			_, err := svc.CreatePaymentLink(context.Background(), CreatePaymentLinkRequest{
				InvoiceID: "inv_1",
				Amount:    decimal.RequireFromString(tt.major),
				Currency:  tt.currency,
			}, nil, nil)
			require.NoError(t, err)

			assert.Equal(t, tt.want, client.req.Amount)
		})
	}
}
