package nodes

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"hash"
	"strconv"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

// executeSet builds an object from the node's `setFields` JSON, expanding
// {{ }} references in every string value. It returns a map rather than a
// string so downstream nodes can address individual fields with
// {{ node.<id>.<field> }}.
func executeSet(node models.WorkflowNode, rc RunContexter) (any, error) {
	raw := configVal(node, "setFields", "")
	if raw == "" {
		return nil, errors.New("set: no fields configured — set `setFields` to a JSON object")
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, fmt.Errorf("set: `setFields` is not a valid JSON object: %w", err)
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		if s, ok := v.(string); ok {
			out[k] = resolveTemplate(s, rc)
			continue
		}
		out[k] = v
	}
	return out, nil
}

// executeJSONExtract parses the upstream output as JSON and walks a dot path
// into it. Numeric segments index arrays: "data.items.0.name".
func executeJSONExtract(node models.WorkflowNode, rc RunContexter) (any, error) {
	path := configVal(node, "jsonPath", "")
	if path == "" {
		return nil, errors.New("json_extract: no `jsonPath` configured")
	}
	var doc any
	if err := json.Unmarshal([]byte(rc.Message()), &doc); err != nil {
		return nil, fmt.Errorf("json_extract: upstream output is not valid JSON: %w", err)
	}
	cur := doc
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return nil, fmt.Errorf("json_extract: no value at path %q (missing key %q)", path, seg)
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil {
				return nil, fmt.Errorf("json_extract: path %q indexes an array with non-numeric segment %q", path, seg)
			}
			if i < 0 || i >= len(node) {
				return nil, fmt.Errorf("json_extract: index %d out of range at path %q (length %d)", i, path, len(node))
			}
			cur = node[i]
		default:
			return nil, fmt.Errorf("json_extract: path %q descends past a scalar at %q", path, seg)
		}
	}
	return cur, nil
}

// executeCrypto hashes / encodes the upstream output. Pure stdlib; no network.
//
// md5 and sha1 are included because real third-party APIs still require them
// for signature schemes (e.g. legacy webhook verification). They are not
// offered as a security recommendation.
func executeCrypto(node models.WorkflowNode, rc RunContexter) (any, error) {
	in := rc.Message()
	action := configVal(node, "cryptoAction", "sha256")

	var h hash.Hash
	switch action {
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	case "sha1":
		h = sha1.New()
	case "md5":
		h = md5.New()
	case "hmac-sha256":
		secret := secretVal(node, "cryptoSecret")
		if secret == "" {
			return nil, errors.New("crypto: hmac-sha256 needs `cryptoSecret` set")
		}
		h = hmac.New(sha256.New, []byte(secret))
	case "base64":
		return base64.StdEncoding.EncodeToString([]byte(in)), nil
	case "base64decode":
		b, err := base64.StdEncoding.DecodeString(in)
		if err != nil {
			return nil, fmt.Errorf("crypto: input is not valid base64: %w", err)
		}
		return string(b), nil
	default:
		return nil, fmt.Errorf("crypto: unsupported action %q "+
			"(want sha256, sha512, sha1, md5, hmac-sha256, base64, base64decode)", action)
	}
	h.Write([]byte(in))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// executeDateTime renders the current time. With no config it returns
// UTC RFC3339 — byte-identical to the previous hardcoded behaviour, so
// existing workflows using the datetime tool are unaffected.
func executeDateTime(node models.WorkflowNode) (any, error) {
	now := time.Now()

	if off := configVal(node, "dtOffset", ""); off != "" {
		d, err := time.ParseDuration(off)
		if err != nil {
			return nil, fmt.Errorf("datetime: `dtOffset` %q is not a duration (try -24h, 30m): %w", off, err)
		}
		now = now.Add(d)
	}

	loc := time.UTC
	if zone := configVal(node, "dtZone", ""); zone != "" {
		l, err := time.LoadLocation(zone)
		if err != nil {
			return nil, fmt.Errorf("datetime: unknown timezone %q (want an IANA name like Asia/Kolkata): %w", zone, err)
		}
		loc = l
	}
	now = now.In(loc)

	switch f := configVal(node, "dtFormat", "rfc3339"); f {
	case "rfc3339":
		return now.Format(time.RFC3339), nil
	case "unix":
		return strconv.FormatInt(now.Unix(), 10), nil
	case "date":
		return now.Format("2006-01-02"), nil
	case "time":
		return now.Format("15:04:05"), nil
	default:
		// Anything else is treated as a literal Go layout string.
		return now.Format(f), nil
	}
}

// executeXMLToJSON converts the upstream output from XML into a JSON-shaped
// map. Attributes become "@name" keys; repeated child elements collapse into a
// slice; leaf elements become their trimmed character data.
func executeXMLToJSON(rc RunContexter) (any, error) {
	dec := xml.NewDecoder(strings.NewReader(rc.Message()))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("xml: could not parse input: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue // skip prolog, comments, leading whitespace
		}
		val, err := decodeXMLNode(dec, start)
		if err != nil {
			return nil, fmt.Errorf("xml: could not parse input: %w", err)
		}
		return val, nil
	}
}

// decodeXMLNode reads one element's subtree. Shared with the RSS connector —
// keep it package-level rather than inlining into executeXMLToJSON.
func decodeXMLNode(d *xml.Decoder, start xml.StartElement) (any, error) {
	node := map[string]any{}
	for _, a := range start.Attr {
		node["@"+a.Name.Local] = a.Value
	}
	var text strings.Builder

	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			child, err := decodeXMLNode(d, t)
			if err != nil {
				return nil, err
			}
			key := t.Name.Local
			switch existing := node[key].(type) {
			case nil:
				node[key] = child
			case []any:
				node[key] = append(existing, child)
			default:
				node[key] = []any{existing, child}
			}
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			trimmed := strings.TrimSpace(text.String())
			// A leaf with no attributes and no children is just its text.
			if len(node) == 0 {
				return trimmed, nil
			}
			if trimmed != "" {
				node["#text"] = trimmed
			}
			return node, nil
		}
	}
}

// executeTemplate renders free text with {{ }} references expanded.
func executeTemplate(node models.WorkflowNode, rc RunContexter) (any, error) {
	tpl := configVal(node, "templateText", "")
	if tpl == "" {
		return nil, errors.New("template: no `templateText` configured")
	}
	return resolveTemplate(tpl, rc), nil
}
