package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestPurchaseConfigSnapshotsReadsAndRedactsSecrets(t *testing.T) {
	reads := map[string]int{}
	c := LoadPurchase(func(key string) string {
		reads[key]++
		switch key {
		case "ARKIV_RPC_URL":
			return "https://rpc.example"
		case "WORLD_CLIENT_SECRET", "ARC_PURCHASE_RELAYER_PRIVATE_KEY":
			return "private-sentinel"
		}
		return ""
	})
	if c.Enabled || c.ArkivRPCURL == "" || len(c.BlockedReasons) == 0 {
		t.Fatal("missing configuration did not preserve discovery and disable checkout")
	}
	for key, count := range reads {
		if count != 1 {
			t.Fatalf("%s read %d times", key, count)
		}
	}
	if strings.Contains(fmt.Sprintf("%v %#v", c, c), "private-sentinel") {
		t.Fatal("configuration formatting disclosed a secret")
	}
}
