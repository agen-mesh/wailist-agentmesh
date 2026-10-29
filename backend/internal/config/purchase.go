package config

import (
	"net/url"
	"strconv"
	"strings"
)

type Purchase struct {
	Enabled             bool
	RPCURL              string
	RelayerKey          string
	MaxGasWei           string
	Confirmations       uint64
	ArkivRPCURL         string
	MerchantResource    string
	MerchantBearer      string
	MerchantSigningKey  string
	MerchantPublicKey   string
	MerchantPayee       string
	MerchantPriceMicros string
	FrontendURL         string
	CallbackURL         string
	WorldEnvironment    string
	WorldClientID       string
	WorldClientSecret   string
	WorldAuthMethod     string
	WorldPrivateKeyPEM  string
	WorldPrivateKeyID   string
	WorldACR            string
	EncryptionKey       string
	BlockedReasons      []string
}

func (Purchase) String() string   { return "purchase.Config{private}" }
func (Purchase) GoString() string { return "purchase.Config{private}" }

func LoadPurchase(lookup func(string) string) Purchase {
	cache := map[string]string{}
	get := func(key string) string {
		if v, ok := cache[key]; ok {
			return v
		}
		v := lookup(key)
		cache[key] = v
		return v
	}
	c := Purchase{Enabled: get("ARC_PURCHASE_ENABLED") == "true", RPCURL: get("ARC_PURCHASE_RPC_URL"), RelayerKey: get("ARC_PURCHASE_RELAYER_PRIVATE_KEY"), MaxGasWei: get("ARC_PURCHASE_MAX_GAS_WEI"), ArkivRPCURL: get("ARKIV_RPC_URL"), MerchantResource: get("PURCHASE_MERCHANT_RESOURCE"), MerchantBearer: get("PURCHASE_MERCHANT_BEARER"), MerchantSigningKey: get("PURCHASE_MERCHANT_SIGNING_KEY"), MerchantPublicKey: get("PURCHASE_MERCHANT_PUBLIC_KEY"), MerchantPayee: get("PURCHASE_MERCHANT_PAYEE"), MerchantPriceMicros: get("PURCHASE_MERCHANT_PRICE_MICROS"), FrontendURL: get("FRONTEND_URL"), CallbackURL: get("WORLD_REDIRECT_URI"), WorldEnvironment: get("WORLD_ENVIRONMENT"), WorldClientID: get("WORLD_CLIENT_ID"), WorldClientSecret: get("WORLD_CLIENT_SECRET"), WorldAuthMethod: get("WORLD_CLIENT_AUTH_METHOD"), WorldPrivateKeyPEM: get("WORLD_PRIVATE_KEY_PEM"), WorldPrivateKeyID: get("WORLD_PRIVATE_KEY_ID"), WorldACR: get("WORLD_REQUIRED_ACR"), EncryptionKey: get("ENCRYPTION_KEY")}
	c.Confirmations = 2
	if raw := get("ARC_PURCHASE_CONFIRMATIONS"); raw != "" {
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || n < 2 || n > 100 {
			c.BlockedReasons = append(c.BlockedReasons, "ARC_PURCHASE_CONFIRMATIONS must be between 2 and 100")
		} else {
			c.Confirmations = n
		}
	}
	if !c.Enabled {
		c.BlockedReasons = append(c.BlockedReasons, "ARC_PURCHASE_ENABLED is off")
	}
	for _, item := range []struct{ name, value string }{{"ARC_PURCHASE_RPC_URL", c.RPCURL}, {"ARC_PURCHASE_RELAYER_PRIVATE_KEY", c.RelayerKey}, {"ARC_PURCHASE_MAX_GAS_WEI", c.MaxGasWei}, {"ARKIV_RPC_URL", c.ArkivRPCURL}, {"PURCHASE_MERCHANT_RESOURCE", c.MerchantResource}, {"PURCHASE_MERCHANT_BEARER", c.MerchantBearer}, {"PURCHASE_MERCHANT_PUBLIC_KEY", c.MerchantPublicKey}, {"PURCHASE_MERCHANT_PAYEE", c.MerchantPayee}, {"PURCHASE_MERCHANT_PRICE_MICROS", c.MerchantPriceMicros}, {"FRONTEND_URL", c.FrontendURL}, {"WORLD_REDIRECT_URI", c.CallbackURL}, {"WORLD_ENVIRONMENT", c.WorldEnvironment}, {"WORLD_CLIENT_ID", c.WorldClientID}, {"ENCRYPTION_KEY", c.EncryptionKey}} {
		if strings.TrimSpace(item.value) == "" {
			c.BlockedReasons = append(c.BlockedReasons, item.name+" is missing")
		}
	}
	if c.WorldAuthMethod == "" {
		c.WorldAuthMethod = "client_secret_basic"
	}
	if c.WorldAuthMethod == "private_key_jwt" {
		if c.WorldPrivateKeyPEM == "" || c.WorldPrivateKeyID == "" {
			c.BlockedReasons = append(c.BlockedReasons, "World registered client key is missing")
		}
	} else if c.WorldClientSecret == "" {
		c.BlockedReasons = append(c.BlockedReasons, "WORLD_CLIENT_SECRET is missing")
	}
	for _, raw := range []string{c.RPCURL, c.ArkivRPCURL, c.MerchantResource} {
		if raw != "" {
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
				c.BlockedReasons = append(c.BlockedReasons, "Purchase endpoints require HTTPS")
			}
		}
	}
	return c
}
