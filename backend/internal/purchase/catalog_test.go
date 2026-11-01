package purchase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agentmesh/backend/internal/arkiv"
)

func migrationCatalog(t *testing.T) (ArkivCatalog, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	client, err := arkiv.NewClientWithHTTP(server.URL, func(context.Context, string) error { return nil }, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Query(t.Context(), arkiv.QueryOptions{Limit: 1}); err == nil || calls.Load() == 0 {
		t.Fatal("catalog RPC observation did not detect the positive control")
	}
	calls.Store(0)
	return ArkivCatalog{Client: client}, &calls
}

func historicalSource(chain uint64) json.RawMessage {
	fields := `"entity_key":"0x` + strings.Repeat("b", 64) + `","snapshot_hash":"0x` + strings.Repeat("c", 64) + `","version":"1"`
	if chain != 0 {
		fields = `"arkiv_chain_id":` + strconv.FormatUint(chain, 10) + `,` + fields
	}
	return json.RawMessage(`{` + fields + `}`)
}

func immutablePurchaseTerms(t *testing.T, o Order) []byte {
	t.Helper()
	raw, err := json.Marshal(struct {
		Intent     Intent
		IntentHash string
		Offer      Offer
		Quote      Quote
		Request    json.RawMessage
	}{o.Intent, o.IntentHash, o.Offer, o.Quote, o.Request})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCatalogRejectsLegacyNetworkBeforeRPC(t *testing.T) {
	for _, chain := range []uint64{0, 7738577, 1} {
		t.Run(strconv.FormatUint(chain, 10), func(t *testing.T) {
			catalog, calls := migrationCatalog(t)
			o := protocolOrder(t).Offer
			o.Source = historicalSource(chain)
			before := append([]byte(nil), o.Source...)
			if err := catalog.Revalidate(t.Context(), o); !errors.Is(err, ErrRetiredOffer) || !errors.Is(err, arkiv.ErrSnapshotNetwork) {
				t.Fatalf("wrong migration rejection: %v", err)
			}
			if calls.Load() != 0 || !bytes.Equal(before, o.Source) {
				t.Fatal("legacy source was queried or rewritten")
			}
		})
	}
}

func TestLegacyUnsignedOrderCannotApproveOrReleaseOnNewNetwork(t *testing.T) {
	for _, action := range []string{"approve", "release"} {
		for _, chain := range []uint64{0, 7738577} {
			t.Run(action+"/"+strconv.FormatUint(chain, 10), func(t *testing.T) {
				s, journal, rail, merchant := serviceFixture(t)
				catalog, calls := migrationCatalog(t)
				s.Catalog = catalog
				journal.o.Offer.Source = historicalSource(chain)
				if action == "approve" {
					journal.o.State = AwaitingApproval
					journal.o.ApprovedAt = nil
				}
				before := immutablePurchaseTerms(t, journal.o)
				state := journal.o.State
				var err error
				if action == "approve" {
					_, err = s.Decide(t.Context(), journal.o.AccountID, journal.o.ID, journal.o.IntentHash, "approve")
					if !errors.Is(err, ErrConflict) {
						t.Fatal("approval lost the conflict classification")
					}
				} else {
					_, err = s.ReleaseAuthorization(t.Context(), journal.o.AccountID, journal.o.ID)
				}
				if !errors.Is(err, ErrRetiredOffer) {
					t.Fatalf("legacy action lacked migration rejection: %v", err)
				}
				if journal.o.State != state || journal.o.SignatureReleasedAt != nil || journal.reserved != 0 || journal.consumed != 0 || rail.sent.Load() != 0 || merchant.calls.Load() != 0 || calls.Load() != 0 {
					t.Fatal("legacy approval produced a side effect")
				}
				if !bytes.Equal(before, immutablePurchaseTerms(t, journal.o)) {
					t.Fatal("legacy immutable terms were rewritten")
				}
			})
		}
	}
}

func TestReleasedLegacyOrderReconcilesWithoutNewCatalog(t *testing.T) {
	for _, chain := range []uint64{0, 7738577} {
		for _, sealed := range []bool{false, true} {
			for _, outcome := range []string{"unknown", "paid", "expired"} {
				t.Run(strconv.FormatUint(chain, 10)+"/sealed="+strconv.FormatBool(sealed)+"/"+outcome, func(t *testing.T) {
					s, journal, rail, merchant := serviceFixture(t)
					journal.o.Offer.Source = historicalSource(chain)
					before := immutablePurchaseTerms(t, journal.o)
					o, err := s.ReleaseAuthorization(t.Context(), journal.o.AccountID, journal.o.ID)
					if err != nil {
						t.Fatal(err)
					}
					signature := signOrder(t, o)
					if sealed {
						rail.lost, rail.recoveryErr = true, ErrUncertain
						if _, err = s.Submit(t.Context(), o.AccountID, o.ID, signature); !errors.Is(err, ErrUncertain) {
							t.Fatalf("original uncertain submission: %v", err)
						}
					}
					txHash, envelope, sent := journal.o.TxHash, journal.o.SignedEnvelope, rail.sent.Load()
					catalog, catalogCalls := migrationCatalog(t)
					restarted := *s
					restarted.Catalog = catalog
					if outcome == "paid" {
						rail.event = &PaymentEvent{ChainID: ChainID, TxHash: "original-payment", LogIndex: "4", BlockHash: "confirmed-block", BlockNumber: "260"}
					} else if outcome == "expired" {
						rail.expired = true
					}
					for range 2 {
						_, err = restarted.Reconcile(t.Context(), o.AccountID, o.ID)
						if outcome == "unknown" {
							if !errors.Is(err, ErrUncertain) {
								t.Fatalf("unknown payment was not held: %v", err)
							}
						} else if err != nil {
							t.Fatal(err)
						}
					}
					if catalogCalls.Load() != 0 || rail.sent.Load() != sent || journal.o.TxHash != txHash || journal.o.SignedEnvelope != envelope || !bytes.Equal(before, immutablePurchaseTerms(t, journal.o)) {
						t.Fatal("migration changed the original payment or consulted the new catalog")
					}
					if _, err := Authorization(journal.o, signature); err != nil {
						t.Fatal("original authorization no longer matches its immutable intent")
					}
					switch outcome {
					case "paid":
						if journal.o.State != Completed || journal.reserved != 0 || journal.consumed != 1 || merchant.calls.Load() != 1 {
							t.Fatal("original payment did not settle exactly one merchant claim")
						}
					case "expired":
						if journal.o.State != Expired || journal.reserved != 0 || journal.consumed != 0 || merchant.calls.Load() != 0 {
							t.Fatal("confirmed unused expiry did not release the original hold")
						}
					case "unknown":
						if journal.reserved != 1 || journal.consumed != 0 || merchant.calls.Load() != 0 {
							t.Fatal("unknown original payment escaped its hold")
						}
					}
				})
			}
		}
	}
}
