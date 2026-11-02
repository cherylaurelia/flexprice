package razorpay

import (
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/types"
)

// Razorpay requires the last digit of a three-decimal amount to be 0: 295.991 is sent as 295990.
const threeDecimalStep = 10

// toSmallestUnit converts a major-unit amount to the integer Razorpay expects for that currency:
// paise for INR, whole yen for JPY, fils for KWD.
func toSmallestUnit(amount decimal.Decimal, currency string) int64 {
	units := types.ToSmallestUnit(amount, currency)
	if types.GetCurrencyPrecision(currency) == 3 {
		return units - units%threeDecimalStep
	}
	return units
}

// FromSmallestUnit converts an amount reported by Razorpay back to major units for that currency.
func FromSmallestUnit(units int64, currency string) decimal.Decimal {
	return types.FromSmallestUnit(units, currency)
}
