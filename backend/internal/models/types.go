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
	// "use ours" toggles
	UseOurKey   bool `json:"useOurKey,omitempty"`
	UseOurEmail bool `json:"useOurEmail,omitempty"`
	// agent: false = platform wallet pays for x402, true = user funds the agent wallet
	SelfFundWallet bool   `json:"selfFundWallet,omitempty"`
	MaxCostPerRun  string `json:"maxCostPerRun,omitempty"`
	MaxSpendTotal  string `json:"maxSpendTotal,omitempty"`
	// webhook trigger
	WebhookMethod        string `json:"webhookMethod,omitempty"`
	WebhookPayloadSchema string `json:"webhookPayloadSchema,omitempty"`
	WebhookLiveURL       string `json:"webhookLiveURL,omitempty"`
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
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	SourcePublishedID *string    `json:"sourcePublishedId,omitempty"`
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
	Cost         float64    `json:"cost"`
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
	Credits      float64   `json:"credits"`
	CreatedAt    time.Time `json:"createdAt"`
}

type PublishedWorkflow struct {
	ID           string         `json:"id"`
	CreatorID    string         `json:"creatorId"`
	CreatorEmail string         `json:"creatorEmail"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	Tags         []string       `json:"tags"`
	Nodes        []WorkflowNode `json:"nodes,omitempty"`
	Edges        []WorkflowEdge `json:"edges,omitempty"`
	FeePerRun    float64        `json:"feePerRun"`
	RunCount     int            `json:"runCount"`
	UpvoteCount  int            `json:"upvoteCount"`
	PublishedAt  time.Time      `json:"publishedAt"`
}
