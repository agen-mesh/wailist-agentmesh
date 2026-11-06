package arc

import (
	"errors"
	"math/big"
	"strings"
)

var (
	// usdcScale is 10^6, the ERC-20 base unit used on every chain in this package.
	usdcScale = big.NewInt(1_000_000)
	// nativePerMicro is 10^12. Arc native wei / 10^12 = ERC-20 base units.
	// 1 USDC = 10^18 native wei = 10^6 ERC-20 units.
	nativePerMicro = new(big.Int).Exp(big.NewInt(10), big.NewInt(12), nil)

	errAmount = errors.New("amount must be a positive USDC decimal with at most 6 fractional digits")
)

// ParseUSDCMicros converts a decimal USDC string to ERC-20 base units.
// "1.5" is 1500000. More than 6 fractional digits is an error, not a rounding.
func ParseUSDCMicros(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") || strings.ContainsAny(s, "eE, ") {
		return nil, errAmount
	}
	whole, frac, found := strings.Cut(s, ".")
	if !found {
		frac = ""
	}
	if whole == "" {
		whole = "0"
	}
	if len(frac) > 6 || !digits(whole) || !digits(frac) {
		return nil, errAmount
	}
	frac += strings.Repeat("0", 6-len(frac))
	n, ok := new(big.Int).SetString(whole+frac, 10)
	if !ok {
		return nil, errAmount
	}
	return n, nil
}

// FormatUSDC renders base units as a decimal without trailing zeros.
// A nil amount renders as "0".
func FormatUSDC(micros *big.Int) string {
	if micros == nil || micros.Sign() == 0 {
		return "0"
	}
	neg := micros.Sign() < 0
	v := new(big.Int).Abs(micros)
	q, r := new(big.Int).QuoRem(v, usdcScale, new(big.Int))
	frac := r.Text(10)
	frac = strings.Repeat("0", 6-len(frac)) + frac
	frac = strings.TrimRight(frac, "0")
	out := q.Text(10)
	if frac != "" {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

// NativeWeiToUSDCMicros converts an Arc native balance (18 decimals) to the
// ERC-20 view (6 decimals), truncating anything below one micro-USDC.
// It is a display helper. Transfers use balanceOf, not this value.
func NativeWeiToUSDCMicros(wei *big.Int) *big.Int {
	if wei == nil || wei.Sign() <= 0 {
		return big.NewInt(0)
	}
	return new(big.Int).Quo(wei, nativePerMicro)
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
