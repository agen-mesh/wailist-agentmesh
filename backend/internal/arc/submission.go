package arc

import (
	"errors"
	"strings"
)

var ErrNotSubmitted = errors.New("Arc action was not submitted")
var ErrTransactionReverted = errors.New("Arc transaction reverted")

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

// Only a decoded rejection response can prove this send did not enter the pool.
func definitiveRejection(err error) bool {
	var response *rpcError
	if !errors.As(err, &response) {
		return false
	}
	if response.Code == -32600 || response.Code == -32602 {
		return true
	}
	if response.Code != -32000 && response.Code != -32003 {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(response.Message))
	for _, reason := range []string{
		"insufficient funds for gas * price + value",
		"insufficient funds for transfer",
		"intrinsic gas too low",
		"exceeds block gas limit",
		"max fee per gas less than block base fee",
		"invalid sender",
	} {
		if message == reason || strings.HasPrefix(message, reason+":") {
			return true
		}
	}
	return false
}
