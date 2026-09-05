package db

import (
	"context"

	"github.com/agentmesh/backend/internal/models"
)

// Device tokens: the addresses push notifications are sent to.
//
// Split into its own file rather than added to store.go, which is already
// long, the same way the geofence work was kept separate.

const deviceTokenColumns = `id, user_id, token, platform, created_at, last_seen_at`

// RegisterDeviceToken records that this device belongs to this user, or moves
// it if it belonged to somebody else.
//
// The upsert is on token alone, not on (user_id, token), and that is what
// makes signing out and back in as a different person on the same phone
// correct: FCM hands the app the same registration token whoever is signed in.
// Without the reassignment the previous account would keep receiving that
// device's notifications -- not merely an untidy duplicate row, but somebody's
// run results delivered to somebody else's phone.
//
// last_seen_at is touched on every call because the app re-registers on each
// sign-in, which is the only regular evidence that a token is still alive.
func (s *Store) RegisterDeviceToken(ctx context.Context, userID, token, platform string) (models.DeviceToken, error) {
	if platform == "" {
		platform = "android"
	}
	var d models.DeviceToken
	err := s.pool.QueryRow(ctx, `
		INSERT INTO device_tokens (user_id, token, platform)
		VALUES ($1, $2, $3)
		ON CONFLICT (token) DO UPDATE
		   SET user_id = EXCLUDED.user_id,
		       platform = EXCLUDED.platform,
		       last_seen_at = NOW()
		RETURNING `+deviceTokenColumns,
		userID, token, platform,
	).Scan(&d.ID, &d.UserID, &d.Token, &d.Platform, &d.CreatedAt, &d.LastSeenAt)
	if err != nil {
		return models.DeviceToken{}, err
	}
	return d, nil
}

// DeleteDeviceToken removes one device's registration, scoped to its owner.
//
// The user_id predicate is not redundant. Without it any signed-in user who
// learned another device's token could unregister it -- a small but free way
// to silence somebody else's notifications.
//
// Deleting a token that is not there is not an error: sign-out calls this
// unconditionally, and a device that never managed to register must still be
// able to sign out cleanly.
func (s *Store) DeleteDeviceToken(ctx context.Context, userID, token string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM device_tokens WHERE user_id = $1 AND token = $2`,
		userID, token,
	)
	return err
}

// DeleteDeviceTokenByValue removes a token regardless of who owns it.
//
// Used only by the send path, when FCM reports a token as unregistered. That
// verdict is about the token itself -- the app was uninstalled, or FCM rotated
// it -- so it is deliberately not scoped to a user: the row is dead whoever it
// is attached to, and keeping it means retrying a guaranteed failure on every
// future run forever.
func (s *Store) DeleteDeviceTokenByValue(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM device_tokens WHERE token = $1`, token)
	return err
}

// DeviceTokensForUser returns every device this user has registered.
//
// Ordered by last_seen_at descending so that if a send is ever capped, the
// devices most recently proven alive are the ones that receive it.
func (s *Store) DeviceTokensForUser(ctx context.Context, userID string) ([]models.DeviceToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+deviceTokenColumns+`
		FROM device_tokens WHERE user_id = $1 ORDER BY last_seen_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.DeviceToken
	for rows.Next() {
		var d models.DeviceToken
		if err := rows.Scan(&d.ID, &d.UserID, &d.Token, &d.Platform, &d.CreatedAt, &d.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
