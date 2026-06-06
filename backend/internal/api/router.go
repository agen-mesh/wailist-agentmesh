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
	r.Post("/waitlist", d.JoinWaitlist)
	r.Post("/run/{workflowId}", d.PublicTrigger)

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
	})

	return r
}
