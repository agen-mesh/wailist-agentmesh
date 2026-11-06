package merchant

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

type AcceptRequest struct {
	Snapshot Snapshot `json:"snapshot"`
	Quote    Quote    `json:"quote"`
}

type FulfillRequest struct {
	OrderID    string       `json:"orderId"`
	IntentHash string       `json:"intentHash"`
	Payment    PaymentEvent `json:"payment"`
}

type ResultView struct {
	OrderID                string          `json:"orderId"`
	IntentHash             string          `json:"intentHash"`
	State                  State           `json:"state"`
	Paid                   bool            `json:"paid"`
	Result                 json.RawMessage `json:"result,omitempty"`
	FailureCode            string          `json:"failureCode,omitempty"`
	ReconciliationRequired bool            `json:"reconciliationRequired"`
}

func View(r Record) ResultView {
	if r.State != Completed {
		r.Result = nil
	}
	return ResultView{
		OrderID: r.Snapshot.OrderID, IntentHash: r.Snapshot.IntentHash, State: r.State,
		Paid: r.Payment != nil, Result: r.Result, FailureCode: r.FailureCode,
		ReconciliationRequired: r.State == Started || r.State == Unknown,
	}
}

// The bearer belongs only to the central server, which authenticates account ownership.
func NewHandler(s *Service, internalBearer string) (http.Handler, error) {
	if s == nil || len(internalBearer) < 32 || len(internalBearer) > 256 {
		return nil, ErrInvalid
	}
	for _, c := range internalBearer {
		if c < 33 || c > 126 {
			return nil, ErrInvalid
		}
	}
	expected := sha256.Sum256([]byte("Bearer " + internalBearer))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /merchant/quote", func(w http.ResponseWriter, r *http.Request) {
		var input QuoteRequest
		if !readBody(w, r, &input) {
			return
		}
		q, err := s.Quote(r.Context(), input)
		if err != nil {
			writeError(w, err, Record{})
			return
		}
		writeJSON(w, http.StatusOK, q)
	})
	mux.HandleFunc("POST /merchant/accept", func(w http.ResponseWriter, r *http.Request) {
		var input AcceptRequest
		if !readBody(w, r, &input) {
			return
		}
		result, err := s.Accept(r.Context(), input.Snapshot, input.Quote)
		if err != nil {
			writeError(w, err, result)
			return
		}
		writeJSON(w, http.StatusOK, View(result))
	})
	mux.HandleFunc("POST /merchant/fulfill", func(w http.ResponseWriter, r *http.Request) {
		var input FulfillRequest
		if !readBody(w, r, &input) {
			return
		}
		result, err := s.Fulfill(r.Context(), input.OrderID, input.IntentHash, input.Payment)
		if err != nil {
			writeError(w, err, result)
			return
		}
		writeJSON(w, stateStatus(result.State), View(result))
	})
	mux.HandleFunc("GET /merchant/results/{orderID}", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if len(query) != 1 || len(query["intentHash"]) != 1 {
			writeError(w, ErrInvalid, Record{})
			return
		}
		result, err := s.Result(r.Context(), r.PathValue("orderID"), query.Get("intentHash"))
		if err != nil {
			writeError(w, err, result)
			return
		}
		writeJSON(w, stateStatus(result.State), View(result))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", "application/json")
		value := r.Header.Values("Authorization")
		actual := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if len(value) != 1 || subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		mux.ServeHTTP(w, r)
	}), nil
}

func readBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" || (r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity") {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "json_required"})
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2*MaxRequestBytes))
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "body_limit"})
		return false
	}
	if err := strictJSON(raw, dst, 2*MaxRequestBytes); err != nil {
		writeError(w, err, Record{})
		return false
	}
	return true
}

func stateStatus(state State) int {
	if state == Pending || state == Started || state == Unknown {
		return http.StatusAccepted
	}
	return http.StatusOK
}

func writeError(w http.ResponseWriter, err error, r Record) {
	status, code := http.StatusServiceUnavailable, "merchant_unavailable"
	switch {
	case errors.Is(err, ErrInvalid):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, ErrConflict):
		status, code = http.StatusConflict, "intent_or_payment_conflict"
	case errors.Is(err, ErrNotFound):
		status, code = http.StatusNotFound, "unaccepted"
	case errors.Is(err, ErrExpired):
		status, code = http.StatusGone, "quote_expired"
	case errors.Is(err, ErrPaymentPending):
		status, code = http.StatusAccepted, "payment_pending"
	case errors.Is(err, ErrPayment):
		status, code = http.StatusUnprocessableEntity, "invalid_payment_evidence"
	case errors.Is(err, ErrUnknown):
		status, code = http.StatusAccepted, "fulfillment_unknown"
	case errors.Is(err, ErrService):
		status, code = http.StatusBadGateway, "paid_service_failed"
	}
	body := map[string]any{"error": code}
	if r.Snapshot.OrderID != "" {
		v := View(r)
		if status >= 400 || strings.Contains(code, "unknown") {
			v.Result = nil
		}
		body["order"] = v
	}
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		status, b = http.StatusInternalServerError, []byte(`{"error":"response_encoding"}`)
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}
