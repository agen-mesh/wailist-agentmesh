package purchasesim

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type attribute struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}
type offerRevision struct {
	Block  uint64
	Empty  bool
	Entity entity
}
type entity struct {
	Key        string      `json:"key"`
	Owner      string      `json:"owner"`
	CreatedAt  string      `json:"createdAt"`
	UpdatedAt  string      `json:"updatedAt"`
	ExpiresAt  string      `json:"expiresAt"`
	Attributes []attribute `json:"attributes"`
}

func (f *Fixture) OfferKey() string { return fixtureHash("offer:" + f.identity()).Hex() }

func (f *Fixture) reviseOffer(mode string) {
	f.state.ArkivHead++
	head := f.state.ArkivHead
	owner, endpoint := f.config.Payee, f.config.Resource
	version, expires, until := uint64(1), head+300, uint64(f.config.Now().Unix())+3600
	switch mode {
	case "changed", "changed_version":
		version = 2
	case "changed_owner":
		owner = strings.ToLower(common.BytesToAddress(fixtureHash("other-owner").Bytes()).Hex())
	case "changed_payload":
		endpoint += "/changed"
	case "expired", "native_expired":
		expires = head
	case "utc_expired":
		until = uint64(f.config.Now().Unix()) - 1
	}
	payload := struct {
		Endpoint string `json:"endpoint"`
		Method   string `json:"method"`
		Payee    string `json:"payee"`
		Schema   any    `json:"input_schema"`
	}{endpoint, "POST", f.config.Payee, map[string]any{"type": "object", "properties": map[string]any{"text": map[string]string{"type": "string"}}, "required": []string{"text"}, "additionalProperties": false}}
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic("static simulation offer is not serializable")
	}
	e := entity{Key: f.OfferKey(), Owner: owner, CreatedAt: hexQuantity(4096), UpdatedAt: hexQuantity(head), ExpiresAt: hexQuantity(expires), Attributes: []attribute{
		{"arkiv.schema", "str", "agentmesh.service-offer.v2"},
		{"project", "str", "agentmesh-mumbai"}, {"kind", "str", "service-offer-v2"}, {"capability", "str", "text.digest.v1"},
		{"settlement_chain", "u64", hexQuantity(ArcChainID)}, {"asset", "addr", Asset}, {"price_micros", "u64", hexQuantity(f.price.Uint64())},
		{"version", "u64", hexQuantity(version)}, {"status", "str", "active"}, {"valid_until", "u64", hexQuantity(until)},
		{"content_type", "str", "application/json"}, {"payload", "bytes", hexutil.Encode(encoded)},
	}}
	f.state.Offers = append(f.state.Offers, offerRevision{head, mode == "empty", e})
}

func (f *Fixture) offerAt(block uint64) *entity {
	for i := len(f.state.Offers) - 1; i >= 0; i-- {
		r := f.state.Offers[i]
		if r.Block > block {
			continue
		}
		expires, _ := blockTag(r.Entity.ExpiresAt, f.state.ArkivHead)
		if r.Empty || expires <= block {
			return nil
		}
		e := r.Entity
		return &e
	}
	return nil
}

func (f *Fixture) arkivRPC(method string, params []json.RawMessage) (any, error) {
	switch method {
	case "eth_chainId":
		return hexQuantity(ArkivChainID), nil
	case "eth_blockNumber":
		return hexQuantity(f.state.ArkivHead), nil
	case "arkiv_query":
		f.obs.ArkivQueries++
		expression, err := textParam(params, 0)
		if err != nil || len(params) > 2 || len(expression) > 4096 {
			return nil, errParams
		}
		var options struct {
			Select map[string]json.RawMessage `json:"select"`
			Limit  string                     `json:"limit"`
			Offset string                     `json:"offset"`
		}
		if len(params) == 2 && string(params[1]) != "null" {
			d := json.NewDecoder(bytes.NewReader(params[1]))
			d.DisallowUnknownFields()
			if d.Decode(&options) != nil {
				return nil, errParams
			}
		}
		limit, offset, at := uint64(20), uint64(0), f.state.ArkivHead
		if options.Limit != "" {
			limit, err = blockTag(options.Limit, f.state.ArkivHead)
			if err != nil || limit == 0 || limit > 50 {
				return nil, errParams
			}
		}
		if options.Offset != "" {
			offset, err = blockTag(options.Offset, f.state.ArkivHead)
			if err != nil {
				return nil, errParams
			}
		}
		predicates, err := parsePredicates(expression)
		if err != nil {
			return nil, err
		}
		// Validate projection even when no offer matches, so malformed queries fail.
		if _, err := projectEntity(entity{}, options.Select); err != nil {
			return nil, err
		}
		data := []any{}
		if e := f.offerAt(at); e != nil && offset == 0 && matchesPredicates(*e, predicates) {
			row, err := projectEntity(*e, options.Select)
			if err != nil {
				return nil, err
			}
			data = append(data, row)
		}
		return map[string]any{"data": data, "blockNumber": hexQuantity(at)}, nil
	default:
		return nil, errors.New("unsupported simulation Arkiv method; writes are not implemented")
	}
}

type predicate struct{ name, op, kind, value string }

var predicateRE = regexp.MustCompile(`^([a-z_][a-z0-9_.]*|\$(?:key|owner|expiresAt|createdAt))\s*(=|<=|>=|<|>)\s*(str|u64|addr|key)\((.*)\)$`)

func parsePredicates(expression string) ([]predicate, error) {
	expression = strings.TrimSuffix(expression, " ORDER BY $key ASC")
	if expression == "*" {
		return nil, nil
	}
	parts := strings.Split(expression, " AND ")
	if len(parts) > 16 {
		return nil, errParams
	}
	result := make([]predicate, 0, len(parts))
	for _, part := range parts {
		match := predicateRE.FindStringSubmatch(strings.TrimSpace(part))
		if match == nil {
			return nil, errors.New("unsupported simulation query predicate")
		}
		p := predicate{match[1], match[2], match[3], match[4]}
		switch p.kind {
		case "str":
			if len(p.value) < 2 || p.value[0] != '\'' || p.value[len(p.value)-1] != '\'' || strings.ContainsAny(p.value[1:len(p.value)-1], "'\\") || p.op != "=" {
				return nil, errParams
			}
			p.value = p.value[1 : len(p.value)-1]
		case "u64":
			n, ok := new(big.Int).SetString(p.value, 10)
			if !ok || !n.IsUint64() || n.String() != p.value {
				return nil, errParams
			}
		case "addr":
			if !common.IsHexAddress(p.value) || p.op != "=" {
				return nil, errParams
			}
		case "key":
			if !isHash(p.value) || p.op != "=" {
				return nil, errParams
			}
		}
		result = append(result, p)
	}
	return result, nil
}

func matchesPredicates(e entity, predicates []predicate) bool {
	values := map[string]attribute{"$key": {"", "key", e.Key}, "$owner": {"", "addr", e.Owner}, "$expiresAt": {"", "u64", e.ExpiresAt}, "$createdAt": {"", "u64", e.CreatedAt}}
	for _, a := range e.Attributes {
		values[a.Name] = a
	}
	for _, p := range predicates {
		a, ok := values[p.name]
		if !ok || a.Type != p.kind {
			return false
		}
		if p.kind == "u64" {
			actual, ok := new(big.Int).SetString(strings.TrimPrefix(a.Value, "0x"), 16)
			if !ok {
				return false
			}
			want, _ := new(big.Int).SetString(p.value, 10)
			c := actual.Cmp(want)
			if (p.op == "=" && c != 0) || (p.op == "<=" && c > 0) || (p.op == ">=" && c < 0) || (p.op == "<" && c >= 0) || (p.op == ">" && c <= 0) {
				return false
			}
		} else if (p.kind == "addr" || p.kind == "key") && !strings.EqualFold(a.Value, p.value) {
			return false
		} else if p.kind == "str" && a.Value != p.value {
			return false
		}
	}
	return true
}

func projectEntity(e entity, selects map[string]json.RawMessage) (map[string]any, error) {
	fields := map[string]any{"key": e.Key, "owner": e.Owner, "createdAt": e.CreatedAt, "updatedAt": e.UpdatedAt, "expiresAt": e.ExpiresAt, "attributes": e.Attributes}
	if selects == nil {
		return map[string]any{"key": e.Key}, nil
	}
	out := map[string]any{}
	for name, raw := range selects {
		value, ok := fields[name]
		if !ok {
			return nil, errParams
		}
		var yes bool
		if json.Unmarshal(raw, &yes) == nil {
			if yes {
				out[name] = value
			}
			continue
		}
		if name != "attributes" {
			return nil, errParams
		}
		var names map[string]bool
		if json.Unmarshal(raw, &names) != nil || names == nil {
			return nil, errParams
		}
		attrs := []attribute{}
		for _, a := range e.Attributes {
			if names[a.Name] {
				attrs = append(attrs, a)
			}
		}
		out[name] = attrs
	}
	return out, nil
}
