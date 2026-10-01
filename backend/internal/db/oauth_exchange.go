package db

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"
)

// ConsumeOAuthExchangeCode succeeds once across all backend instances. Only a
// digest of the signed identifier is stored, independent of token encoding.
// Expired redemptions are pruned on subsequent exchanges.
func (s *Store) ConsumeOAuthExchangeCode(ctx context.Context, codeID string, expiresAt time.Time) (bool, error) {
	hash := sha256.Sum256([]byte(codeID))
	result, err := s.pool.Exec(ctx, `
		WITH expired AS (
			DELETE FROM oauth_exchange_redemptions WHERE expires_at <= CURRENT_TIMESTAMP
		)
		INSERT INTO oauth_exchange_redemptions (code_hash, expires_at)
		SELECT $1, $2::timestamptz WHERE $2::timestamptz > CURRENT_TIMESTAMP
		ON CONFLICT (code_hash) DO NOTHING
	`, hash[:], expiresAt)
	if err != nil {
		return false, fmt.Errorf("consume oauth exchange code: %w", err)
	}
	return result.RowsAffected() == 1, nil
}
