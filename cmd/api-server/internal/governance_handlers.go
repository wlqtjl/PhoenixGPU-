package internal

import (
	"encoding/json"
	"net/http"
	"strings"
)

type governanceHandlers struct {
	service *GovernanceService
	log     Logger
}

func (h *governanceHandlers) listAgents(w http.ResponseWriter, r *http.Request) {
	okMeta(w, h.service.ListAgents(), len(h.service.ListAgents()))
}

func (h *governanceHandlers) createAgent(w http.ResponseWriter, r *http.Request) {
	var req CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	agent, err := h.service.CreateAgent(req)
	if err != nil {
		errResp(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusCreated, APIResponse{Data: agent, Meta: &APIMeta{Timestamp: agent.CreatedAt}})
}

func (h *governanceHandlers) listTasks(w http.ResponseWriter, r *http.Request) {
	tasks := h.service.ListTasks()
	okMeta(w, tasks, len(tasks))
}

func (h *governanceHandlers) createTask(w http.ResponseWriter, r *http.Request) {
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task, err := h.service.CreateTask(req)
	if err != nil {
		errResp(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusCreated, APIResponse{Data: task, Meta: &APIMeta{Timestamp: task.CreatedAt}})
}

func (h *governanceHandlers) approveTask(w http.ResponseWriter, r *http.Request) {
	taskID, okPath := parseTaskApprovePath(r.URL.Path)
	if !okPath {
		errResp(w, http.StatusBadRequest, "invalid task approve path")
		return
	}
	actor := r.URL.Query().Get("actor")
	task, err := h.service.ApproveTask(taskID, actor)
	if err != nil {
		if err.Error() == "task not found" {
			errResp(w, http.StatusNotFound, err.Error())
			return
		}
		errResp(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, task)
}

func (h *governanceHandlers) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	evts := h.service.ListAuditEvents()
	okMeta(w, evts, len(evts))
}

func (h *governanceHandlers) listDecisionCards(w http.ResponseWriter, r *http.Request) {
	cards := h.service.ListDecisionCards()
	okMeta(w, cards, len(cards))
}

func (h *governanceHandlers) listBudgetLedgers(w http.ResponseWriter, r *http.Request) {
	ledgers := h.service.ListBudgetLedgers()
	okMeta(w, ledgers, len(ledgers))
}

func parseTaskApprovePath(path string) (taskID string, ok bool) {
	parts := splitPath(path)
	if len(parts) == 7 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "control" && parts[3] == "tasks" && parts[5] == "approvals" && parts[6] == "approve" {
		return parts[4], true
	}
	return "", false
}

func withMethods(methods []string, next http.Handler) http.Handler {
	allow := make(map[string]struct{}, len(methods))
	for _, m := range methods {
		allow[m] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow[r.Method]; !ok {
			w.Header().Set("Allow", strings.Join(methods, ", "))
			errResp(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}
