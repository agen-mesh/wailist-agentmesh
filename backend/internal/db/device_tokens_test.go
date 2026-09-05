package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// The interesting behaviour in device_tokens is not the inserting -- it is what
// happens when the same physical phone changes hands. FCM hands an app the same
// registration token whoever is signed in, so getting this wrong delivers one
// person's run results to another person's phone.

func deviceUser(t *testing.T, name string) string {
	t.Helper()
	store := testStore(t)
	email := fmt.Sprintf("device-token-test-%s-%d@example.com", name, time.Now().UnixNano())
	user, err := store.CreateUser(context.Background(), email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func TestRegisterDeviceTokenIsIdempotent(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	userID := deviceUser(t, "idempotent")
	token := fmt.Sprintf("tok_idem_%d", time.Now().UnixNano())

	// The app re-registers on every sign-in, so this is the ordinary path and
	// not an edge case.
	first, err := store.RegisterDeviceToken(ctx, userID, token, "android")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.RegisterDeviceToken(ctx, userID, token, "android")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("re-registering created a second row: %s then %s", first.ID, second.ID)
	}

	got, err := store.DeviceTokensForUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("user has %d device rows, want 1", len(got))
	}
}

// The reason the upsert is keyed on the token alone.
func TestRegisterDeviceTokenMovesADeviceBetweenUsers(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	alice := deviceUser(t, "alice")
	bob := deviceUser(t, "bob")
	token := fmt.Sprintf("tok_shared_%d", time.Now().UnixNano())

	if _, err := store.RegisterDeviceToken(ctx, alice, token, "android"); err != nil {
		t.Fatal(err)
	}
	// Same phone, somebody else signs in. FCM returns the same token.
	if _, err := store.RegisterDeviceToken(ctx, bob, token, "android"); err != nil {
		t.Fatal(err)
	}

	aliceTokens, err := store.DeviceTokensForUser(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceTokens) != 0 {
		t.Fatalf("the previous owner still has %d devices; that phone would keep receiving their runs", len(aliceTokens))
	}

	bobTokens, err := store.DeviceTokensForUser(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	if len(bobTokens) != 1 || bobTokens[0].Token != token {
		t.Fatalf("new owner has %v, want the one token", bobTokens)
	}
}

func TestDeleteDeviceTokenIsScopedToItsOwner(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	owner := deviceUser(t, "owner")
	stranger := deviceUser(t, "stranger")
	token := fmt.Sprintf("tok_scoped_%d", time.Now().UnixNano())

	if _, err := store.RegisterDeviceToken(ctx, owner, token, "android"); err != nil {
		t.Fatal(err)
	}

	// Somebody who learned the token must not be able to silence it.
	if err := store.DeleteDeviceToken(ctx, stranger, token); err != nil {
		t.Fatal(err)
	}
	got, err := store.DeviceTokensForUser(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatal("another user deleted this device's registration")
	}

	// The owner can.
	if err := store.DeleteDeviceToken(ctx, owner, token); err != nil {
		t.Fatal(err)
	}
	got, err = store.DeviceTokensForUser(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("owner still has %d devices after deleting", len(got))
	}
}

func TestDeleteDeviceTokenToleratesAnAbsentRow(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	userID := deviceUser(t, "absent")

	// Sign-out calls this unconditionally, including on a device that never
	// managed to register. It must not be an error.
	if err := store.DeleteDeviceToken(ctx, userID, "tok_never_existed"); err != nil {
		t.Fatalf("deleting an absent token errored: %v", err)
	}
}

// The send path's cleanup, for when FCM says a token is dead. Not scoped to a
// user, because that verdict is about the token itself.
func TestDeleteDeviceTokenByValue(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	userID := deviceUser(t, "dead")
	token := fmt.Sprintf("tok_dead_%d", time.Now().UnixNano())

	if _, err := store.RegisterDeviceToken(ctx, userID, token, "android"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteDeviceTokenByValue(ctx, token); err != nil {
		t.Fatal(err)
	}
	got, err := store.DeviceTokensForUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("a token FCM rejected is still registered; every future run would retry it")
	}
}

func TestDeviceTokensForUserKeepsDevicesSeparate(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	userID := deviceUser(t, "multi")
	phone := fmt.Sprintf("tok_phone_%d", time.Now().UnixNano())
	tablet := fmt.Sprintf("tok_tablet_%d", time.Now().UnixNano())

	// One person, two devices. A column on users instead of a table would
	// silently make the newest one the only one.
	if _, err := store.RegisterDeviceToken(ctx, userID, phone, "android"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterDeviceToken(ctx, userID, tablet, "android"); err != nil {
		t.Fatal(err)
	}

	got, err := store.DeviceTokensForUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("user has %d devices, want 2", len(got))
	}
}

func TestRegisterDeviceTokenDefaultsPlatform(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	userID := deviceUser(t, "platform")
	token := fmt.Sprintf("tok_plat_%d", time.Now().UnixNano())

	// A client that omits platform must stay correct -- only Android exists.
	rec, err := store.RegisterDeviceToken(ctx, userID, token, "")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Platform != "android" {
		t.Fatalf("platform = %q, want android", rec.Platform)
	}
}
