package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/agentmesh/backend/internal/api/handlers"
)

func NewRouter(d *handlers.Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(corsMiddleware)

	// Public routes — no JWT required
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	r.Post("/auth/signup", d.SignUp)
	r.Post("/auth/signin", d.SignIn)
	r.Post("/auth/signout", d.SignOut)
	r.Get("/auth/oauth/{provider}", d.OAuthStart)
	r.Get("/auth/oauth/{provider}/callback", d.OAuthCallback)
	r.Post("/waitlist", d.JoinWaitlist)
	r.Post("/run/{workflowId}", d.PublicTrigger)
	// Called by Razorpay's servers, not the browser — authenticated via HMAC signature
	// (X-Razorpay-Signature), not a session cookie, so it must sit outside the JWT group.
	r.Post("/payments/razorpay/webhook", d.RazorpayWebhook)
	// Called by NOWPayments' servers, not the browser — authenticated via HMAC signature
	// (x-nowpayments-sig), not a session cookie, so it must sit outside the JWT group.
	r.Post("/payments/nowpayments/webhook", d.NOWPaymentsWebhook)

	// Protected routes — JWT required
	r.Group(func(r chi.Router) {
		r.Use(NewAuthMiddleware(d.JWTSecret))

		r.Get("/auth/me", d.Me)

		r.Get("/workflows", d.ListWorkflows)
		r.Post("/workflows", d.CreateWorkflow)
		r.Get("/workflows/{id}", d.GetWorkflow)
		r.Put("/workflows/{id}", d.UpdateWorkflow)
		r.Delete("/workflows/{id}", d.DeleteWorkflow)

		r.Post("/workflows/{id}/deploy", d.Deploy)
		r.Get("/workflows/{id}/agents/{agentId}/balance", d.AgentBalance)
		r.Post("/workflows/{id}/agents/{agentId}/fund", d.FundAgent)

		r.Post("/workflows/{id}/run", d.TriggerRun)
		r.Post("/workflows/{id}/stop", d.StopWorkflow)
		r.Get("/runs/{runId}", d.GetRun)
		r.Get("/runs/{runId}/stream", d.StreamRun)

		r.Post("/tools/x402/quote", d.X402Quote)

		r.Post("/payments/razorpay/order", d.CreateRazorpayOrder)
		r.Post("/payments/razorpay/verify", d.VerifyRazorpayPayment)
		r.Post("/payments/nowpayments/invoice", d.CreateCryptoInvoice)
		r.Get("/credits/balance", d.GetCreditBalance)

		r.Get("/connectors/oauth/{provider}/start", d.ConnectorOAuthStart)
		r.Get("/connectors/oauth/{provider}/callback", d.ConnectorOAuthCallback)
	})

	return r
}
