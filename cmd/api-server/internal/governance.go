package internal

import (
	"errors"
	"sort"
	"sync"
	"time"
)

type AgentRecord struct {
	AgentID     string    `json:"agentId"`
	Type        string    `json:"type"`
	Model       string    `json:"model"`
	Capability  []string  `json:"capability"`
	BudgetLimit float64   `json:"budgetLimit"`
	CreatedAt   time.Time `json:"createdAt"`
}

type WorkflowSpec struct {
	WorkflowID   string   `json:"workflowId"`
	Version      string   `json:"version"`
	Dependencies []string `json:"dependencies"`
}

type TaskRecord struct {
	TaskID           string       `json:"taskId"`
	Objective        string       `json:"objective"`
	Status           string       `json:"status"`
	Priority         string       `json:"priority"`
	Deadline         string       `json:"deadline,omitempty"`
	RiskLevel        string       `json:"riskLevel"`
	ApprovalRequired bool         `json:"approvalRequired"`
	AssignedAgentID  string       `json:"assignedAgentId"`
	Workflow         WorkflowSpec `json:"workflow"`
	CreatedAt        time.Time    `json:"createdAt"`
	UpdatedAt        time.Time    `json:"updatedAt"`
}

type BudgetLedgerRecord struct {
	LedgerID  string    `json:"ledgerId"`
	Scope     string    `json:"scope"`
	Planned   float64   `json:"planned"`
	Used      float64   `json:"used"`
	Remaining float64   `json:"remaining"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type AuditEventRecord struct {
	EventID    string                 `json:"eventId"`
	Actor      string                 `json:"actor"`
	Action     string                 `json:"action"`
	Input      map[string]interface{} `json:"input,omitempty"`
	Output     map[string]interface{} `json:"output,omitempty"`
	Result     string                 `json:"result"`
	TaskID     string                 `json:"taskId,omitempty"`
	OccurredAt time.Time              `json:"occurredAt"`
}

type DecisionCardRecord struct {
	CardID            string    `json:"cardId"`
	TaskID            string    `json:"taskId"`
	Summary           string    `json:"summary"`
	Options           []string  `json:"options"`
	RecommendedAction string    `json:"recommendedAction"`
	CreatedAt         time.Time `json:"createdAt"`
}

type CreateAgentRequest struct {
	AgentID     string   `json:"agentId"`
	Type        string   `json:"type"`
	Model       string   `json:"model"`
	Capability  []string `json:"capability"`
	BudgetLimit float64  `json:"budgetLimit"`
	Actor       string   `json:"actor"`
}

type CreateTaskRequest struct {
	TaskID          string   `json:"taskId"`
	Objective       string   `json:"objective"`
	Priority        string   `json:"priority"`
	Deadline        string   `json:"deadline"`
	AssignedAgentID string   `json:"assignedAgentId"`
	EstimatedCost   float64  `json:"estimatedCost"`
	Actor           string   `json:"actor"`
	WorkflowVersion string   `json:"workflowVersion"`
	Dependencies    []string `json:"dependencies"`
}

type GovernanceService struct {
	mu            sync.RWMutex
	agents        map[string]AgentRecord
	tasks         map[string]TaskRecord
	budgetLedgers map[string]BudgetLedgerRecord
	auditEvents   []AuditEventRecord
	decisionCards []DecisionCardRecord
	nextID        int
}

func NewGovernanceService() *GovernanceService {
	now := time.Now().UTC()
	return &GovernanceService{
		agents: map[string]AgentRecord{
			"agent-risk-reviewer": {
				AgentID:     "agent-risk-reviewer",
				Type:        "approval",
				Model:       "gpt-4.1",
				Capability:  []string{"policy-check", "risk-labeling"},
				BudgetLimit: 300,
				CreatedAt:   now,
			},
		},
		tasks: map[string]TaskRecord{},
		budgetLedgers: map[string]BudgetLedgerRecord{
			"org-default": {
				LedgerID:  "org-default",
				Scope:     "organization/default",
				Planned:   5000,
				Used:      420,
				Remaining: 4580,
				UpdatedAt: now,
			},
		},
		auditEvents: []AuditEventRecord{},
		nextID:      1,
	}
}

func (s *GovernanceService) ListAgents() []AgentRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AgentRecord, 0, len(s.agents))
	for _, a := range s.agents {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *GovernanceService) CreateAgent(req CreateAgentRequest) (AgentRecord, error) {
	if req.AgentID == "" || req.Type == "" || req.Model == "" {
		return AgentRecord{}, errors.New("agentId, type, model are required")
	}
	if req.BudgetLimit <= 0 {
		return AgentRecord{}, errors.New("budgetLimit must be > 0")
	}
	now := time.Now().UTC()
	rec := AgentRecord{
		AgentID:     req.AgentID,
		Type:        req.Type,
		Model:       req.Model,
		Capability:  req.Capability,
		BudgetLimit: req.BudgetLimit,
		CreatedAt:   now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.agents[req.AgentID]; exists {
		return AgentRecord{}, errors.New("agent already exists")
	}
	s.agents[req.AgentID] = rec
	s.appendAuditLocked(AuditEventRecord{
		Actor:      defaultActor(req.Actor),
		Action:     "agent.registered",
		Result:     "success",
		OccurredAt: now,
		Input: map[string]interface{}{
			"agentId": req.AgentID,
			"model":   req.Model,
		},
	})
	return rec, nil
}

func (s *GovernanceService) ListTasks() []TaskRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TaskRecord, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *GovernanceService) CreateTask(req CreateTaskRequest) (TaskRecord, error) {
	if req.TaskID == "" || req.Objective == "" || req.AssignedAgentID == "" {
		return TaskRecord{}, errors.New("taskId, objective, assignedAgentId are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tasks[req.TaskID]; exists {
		return TaskRecord{}, errors.New("task already exists")
	}
	agent, ok := s.agents[req.AssignedAgentID]
	if !ok {
		return TaskRecord{}, errors.New("assigned agent does not exist")
	}

	ledger := s.budgetLedgers["org-default"]
	if req.EstimatedCost <= 0 {
		return TaskRecord{}, errors.New("estimatedCost must be > 0")
	}
	if req.EstimatedCost > ledger.Remaining {
		return TaskRecord{}, errors.New("estimated cost exceeds remaining budget")
	}

	riskLevel := "low"
	approvalRequired := false
	if req.EstimatedCost >= 500 || req.Priority == "p0" {
		riskLevel = "high"
		approvalRequired = true
	}

	now := time.Now().UTC()
	task := TaskRecord{
		TaskID:           req.TaskID,
		Objective:        req.Objective,
		Status:           map[bool]string{true: "pending_approval", false: "queued"}[approvalRequired],
		Priority:         normalizePriority(req.Priority),
		Deadline:         req.Deadline,
		RiskLevel:        riskLevel,
		ApprovalRequired: approvalRequired,
		AssignedAgentID:  req.AssignedAgentID,
		Workflow: WorkflowSpec{
			WorkflowID:   "wf-" + req.TaskID,
			Version:      nonEmpty(req.WorkflowVersion, "v1"),
			Dependencies: req.Dependencies,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.tasks[task.TaskID] = task

	ledger.Used += req.EstimatedCost
	ledger.Remaining -= req.EstimatedCost
	ledger.UpdatedAt = now
	s.budgetLedgers[ledger.LedgerID] = ledger

	s.appendAuditLocked(AuditEventRecord{
		Actor:      defaultActor(req.Actor),
		Action:     "task.created",
		TaskID:     task.TaskID,
		Result:     "success",
		OccurredAt: now,
		Input: map[string]interface{}{
			"objective":     req.Objective,
			"estimatedCost": req.EstimatedCost,
			"agentType":     agent.Type,
		},
		Output: map[string]interface{}{
			"status":           task.Status,
			"approvalRequired": task.ApprovalRequired,
		},
	})

	if approvalRequired {
		s.decisionCards = append(s.decisionCards, DecisionCardRecord{
			CardID:            "card-" + task.TaskID,
			TaskID:            task.TaskID,
			Summary:           "高风险任务等待审批：" + task.Objective,
			Options:           []string{"批准并执行", "驳回并回滚预算", "转人工处理"},
			RecommendedAction: "批准并执行",
			CreatedAt:         now,
		})
	}

	return task, nil
}

func (s *GovernanceService) ApproveTask(taskID, actor string) (TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return TaskRecord{}, errors.New("task not found")
	}
	if !task.ApprovalRequired {
		return TaskRecord{}, errors.New("task does not require approval")
	}
	if task.Status != "pending_approval" {
		return TaskRecord{}, errors.New("task is not pending approval")
	}
	task.Status = "queued"
	task.UpdatedAt = time.Now().UTC()
	s.tasks[taskID] = task
	s.appendAuditLocked(AuditEventRecord{
		Actor:      defaultActor(actor),
		Action:     "task.approved",
		TaskID:     taskID,
		Result:     "success",
		OccurredAt: task.UpdatedAt,
		Output: map[string]interface{}{
			"status": task.Status,
		},
	})
	return task, nil
}

func (s *GovernanceService) ListAuditEvents() []AuditEventRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AuditEventRecord, len(s.auditEvents))
	copy(out, s.auditEvents)
	sort.Slice(out, func(i, j int) bool { return out[i].OccurredAt.After(out[j].OccurredAt) })
	return out
}

func (s *GovernanceService) ListDecisionCards() []DecisionCardRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DecisionCardRecord, len(s.decisionCards))
	copy(out, s.decisionCards)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *GovernanceService) ListBudgetLedgers() []BudgetLedgerRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]BudgetLedgerRecord, 0, len(s.budgetLedgers))
	for _, l := range s.budgetLedgers {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func (s *GovernanceService) appendAuditLocked(evt AuditEventRecord) {
	evt.EventID = "evt-" + time.Now().UTC().Format("20060102150405") + "-" + itoa(s.nextID)
	s.nextID++
	s.auditEvents = append(s.auditEvents, evt)
}

func normalizePriority(priority string) string {
	if priority == "" {
		return "p2"
	}
	switch priority {
	case "p0", "p1", "p2", "p3":
		return priority
	default:
		return "p2"
	}
}

func defaultActor(actor string) string {
	if actor == "" {
		return "system"
	}
	return actor
}

func nonEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
