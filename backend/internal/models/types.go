package models

import "time"

type NodeType string
type EdgeKind string

const (
	NodeTypeTrigger  NodeType = "trigger"
	NodeTypeAgent    NodeType = "agent"
	NodeTypeProvider NodeType = "provider"
	NodeTypeTool     NodeType = "tool"
	NodeTypeTool402  NodeType = "tool402"
	NodeTypeAction   NodeType = "action"
	NodeTypeEnd      NodeType = "end"
)

const (
	EdgeKindFlow   EdgeKind = "flow"
	EdgeKindAttach EdgeKind = "attach"
)

type ParamDef struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Default     string `json:"default,omitempty"`
}

type WorkflowNode struct {
	ID           string   `json:"id"`
	Type         NodeType `json:"type"`
	Template     string   `json:"template,omitempty"`
	X            float64  `json:"x,omitempty"`
	Y            float64  `json:"y,omitempty"`
	Name         string   `json:"name,omitempty"`
	Label        string   `json:"label,omitempty"`
	Icon         string   `json:"icon,omitempty"`
	SystemPrompt string   `json:"systemPrompt,omitempty"`
	Wallet       string   `json:"wallet,omitempty"`
	Balance      string   `json:"balance,omitempty"`
	APIKey       string   `json:"apiKey,omitempty"`
	Model        string   `json:"model,omitempty"`
	URL          string   `json:"url,omitempty"`
	Method       string   `json:"method,omitempty"`
	Endpoint     string   `json:"endpoint,omitempty"`
	Price        string   `json:"price,omitempty"`
	Unit         string   `json:"unit,omitempty"`
	Provider     string   `json:"provider,omitempty"`
	Source       string   `json:"source,omitempty"`
	// email action fields
	EmailTo       string `json:"emailTo,omitempty"`
	EmailFrom     string `json:"emailFrom,omitempty"`
	EmailSubject  string `json:"emailSubject,omitempty"`
	EmailBody     string `json:"emailBody,omitempty"`
	EmailAPIKey   string `json:"emailApiKey,omitempty"`
	EmailProvider string `json:"emailProvider,omitempty"`
	// x402 tool discovered params (populated by frontend discover)
	DiscoveredParams []ParamDef `json:"discoveredParams,omitempty"`
	Description      string     `json:"description,omitempty"`
	// Secrets holds per-connector credential values for connectors added after the
	// original dedicated fields (APIKey, EmailAPIKey, ...). Each value is encrypted
	// independently, exactly like EmailAPIKey, via encryptNodes/maskNodes/decryptNodes.
	Secrets map[string]string `json:"secrets,omitempty"`
	// Config holds per-connector non-secret settings (list IDs, project keys, channel
	// names, etc.) for the same connectors. Never encrypted.
	Config map[string]string `json:"config,omitempty"`
}

type WorkflowEdge struct {
	ID     string   `json:"id"`
	From   string   `json:"from"`
	To     string   `json:"to"`
	Kind   EdgeKind `json:"kind"`
	ToPort string   `json:"toPort,omitempty"`
}

type WorkflowGraph struct {
	Nodes []WorkflowNode `json:"nodes"`
	Edges []WorkflowEdge `json:"edges"`
}

type WorkflowStatus string

const (
	WorkflowStatusDraft    WorkflowStatus = "draft"
	WorkflowStatusDeployed WorkflowStatus = "deployed"
	WorkflowStatusError    WorkflowStatus = "error"
)

type Workflow struct {
	ID          string         `json:"id"`
	UserID      string         `json:"userId,omitempty"`
	Name        string         `json:"name"`
	Status      WorkflowStatus `json:"status"`
	Nodes       []WorkflowNode `json:"nodes"`
	Edges       []WorkflowEdge `json:"edges"`
	DeployedAt  *time.Time     `json:"deployedAt,omitempty"`
	RunEndpoint string         `json:"runEndpoint,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Agents      int            `json:"agents,omitempty"`
	Runs        int            `json:"runs,omitempty"`
	Spend       string         `json:"spend,omitempty"`
	Updated     string         `json:"updated,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type RunStatus string

const (
	RunStatusRunning RunStatus = "running"
	RunStatusSuccess RunStatus = "success"
	RunStatusFailed  RunStatus = "failed"
	RunStatusStopped RunStatus = "stopped"
)

type Run struct {
	ID           string     `json:"id"`
	WorkflowID   string     `json:"workflowId"`
	TriggeredBy  string     `json:"triggeredBy"`
	Status       RunStatus  `json:"status"`
	StartedAt    time.Time  `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	InputContext any        `json:"inputContext,omitempty"`
}

type LogStatus string

const (
	LogStatusPending LogStatus = "pending"
	LogStatusRunning LogStatus = "running"
	LogStatusSuccess LogStatus = "success"
	LogStatusFailed  LogStatus = "failed"
)

type RunLog struct {
	ID         string    `json:"id"`
	RunID      string    `json:"runId"`
	StepIndex  int       `json:"stepIndex"`
	NodeID     string    `json:"nodeId"`
	NodeType   NodeType  `json:"nodeType"`
	Status     LogStatus `json:"status"`
	Input      any       `json:"input,omitempty"`
	Output     any       `json:"output,omitempty"`
	DurationMs int       `json:"durationMs,omitempty"`
	Ts         time.Time `json:"ts"`
}

type AgentWallet struct {
	ID                string `json:"id"`
	WorkflowID        string `json:"workflowId"`
	AgentNodeID       string `json:"agentNodeId"`
	Address           string `json:"address"`
	EncryptedMnemonic string `json:"-"`
	Network           string `json:"network"`
}

type LogEvent struct {
	StepIndex  int       `json:"stepIndex"`
	NodeID     string    `json:"nodeId"`
	NodeType   NodeType  `json:"nodeType"`
	Status     LogStatus `json:"status"`
	Output     any       `json:"output,omitempty"`
	DurationMs int       `json:"durationMs,omitempty"`
	Ts         time.Time `json:"ts"`
}

// AttachConfig holds the provider and tools attached to an agent node via "attach" edges.
type AttachConfig struct {
	Provider *WorkflowNode
	Tools    []WorkflowNode
}

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

// CreditTransaction is one row of the append-only credit_ledger table.
type CreditTransaction struct {
	ID                string     `json:"id"`
	UserID            string     `json:"userId"`
	Provider          string     `json:"provider"`
	ProviderOrderID   string     `json:"providerOrderId"`
	ProviderPaymentID *string    `json:"providerPaymentId,omitempty"`
	Status            string     `json:"status"`
	AmountINRPaise    int64      `json:"amountInrPaise"`
	FXRateUSDPerINR   float64    `json:"fxRateUsdPerInr"`
	CreditUSDMicros   int64      `json:"creditUsdMicros"`
	CreatedAt         time.Time  `json:"createdAt"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
}

// DebitEntry is one row of the append-only debit_ledger table — a platform
// fee charged against a user's credit balance for a metered action inside
// a workflow run.
type DebitEntry struct {
	ID              string    `json:"id"`
	UserID          string    `json:"userId"`
	WorkflowID      string    `json:"workflowId"`
	RunID           string    `json:"runId"`
	NodeID          string    `json:"nodeId"`
	Kind            string    `json:"kind"`
	AmountUSDMicros int64     `json:"amountUsdMicros"`
	CreatedAt       time.Time `json:"createdAt"`
}

const (
	DebitKindByokFlatFee     = "byok_flat_fee"
	DebitKindX402PlatformFee = "x402_platform_fee"
)
