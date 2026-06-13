package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/henry-insomniac/dev-time-server/internal/db"
)

func (server server) handleInternalProjectStatus(response http.ResponseWriter, request *http.Request) {
	bundle, ok := server.loadInternalEvidenceBundle(response, request)
	if !ok {
		return
	}

	topRiskReason := "暂无活跃风险信号"
	evidenceRefs := []string{}
	if len(bundle.Signals) > 0 {
		topRiskReason = bundle.Signals[0].Reason
		evidenceRefs = bundle.Signals[0].EvidenceRefs
	}
	writeJSON(response, http.StatusOK, struct {
		Project       db.ProjectSummary `json:"project"`
		Assessment    db.RiskAssessment `json:"assessment"`
		TopRiskReason string            `json:"top_risk_reason"`
		EvidenceRefs  []string          `json:"evidence_refs"`
	}{
		Project:       bundle.Project,
		Assessment:    bundle.Assessment,
		TopRiskReason: topRiskReason,
		EvidenceRefs:  evidenceRefs,
	})
}

func (server server) handleInternalCIChecks(response http.ResponseWriter, request *http.Request) {
	bundle, ok := server.loadInternalEvidenceBundle(response, request)
	if !ok {
		return
	}

	checks := []internalCheckRun{}
	for _, event := range bundle.Events {
		if event.EventType != "check_run" {
			continue
		}
		var payload struct {
			CheckRun struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				HTMLURL    string `json:"html_url"`
			} `json:"check_run"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{
				"error": "decode check_run event failed",
			})
			return
		}
		checks = append(checks, internalCheckRun{
			EvidenceRef: event.ID,
			Name:        payload.CheckRun.Name,
			Status:      payload.CheckRun.Status,
			Conclusion:  payload.CheckRun.Conclusion,
			URL:         payload.CheckRun.HTMLURL,
		})
	}

	writeJSON(response, http.StatusOK, struct {
		Checks []internalCheckRun `json:"checks"`
	}{
		Checks: checks,
	})
}

func (server server) handleInternalPullRequests(response http.ResponseWriter, request *http.Request) {
	bundle, ok := server.loadInternalEvidenceBundle(response, request)
	if !ok {
		return
	}

	pullRequests := []internalPullRequest{}
	for _, event := range bundle.Events {
		if event.EventType != "pull_request" {
			continue
		}
		var payload struct {
			PullRequest struct {
				Number  int    `json:"number"`
				Title   string `json:"title"`
				State   string `json:"state"`
				HTMLURL string `json:"html_url"`
			} `json:"pull_request"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{
				"error": "decode pull_request event failed",
			})
			return
		}
		pullRequests = append(pullRequests, internalPullRequest{
			EvidenceRef: event.ID,
			Number:      payload.PullRequest.Number,
			Title:       payload.PullRequest.Title,
			State:       payload.PullRequest.State,
			URL:         payload.PullRequest.HTMLURL,
		})
	}

	writeJSON(response, http.StatusOK, struct {
		PullRequests []internalPullRequest `json:"pull_requests"`
	}{
		PullRequests: pullRequests,
	})
}

func (server server) handleInternalCreateActionSuggestion(
	response http.ResponseWriter,
	request *http.Request,
) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	var input struct {
		ProjectID    string   `json:"project_id"`
		ActionType   string   `json:"action_type"`
		TargetRef    string   `json:"target_ref"`
		DraftBody    string   `json:"draft_body"`
		EvidenceRefs []string `json:"evidence_refs"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON request body",
		})
		return
	}
	if input.ProjectID == "" || input.ActionType == "" ||
		input.TargetRef == "" || input.DraftBody == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "project_id, action_type, target_ref, and draft_body are required",
		})
		return
	}

	suggestion, err := server.store.CreateActionSuggestion(
		request.Context(),
		db.ActionSuggestionInput{
			ProjectID:    input.ProjectID,
			ActionType:   input.ActionType,
			TargetRef:    input.TargetRef,
			DraftBody:    input.DraftBody,
			EvidenceRefs: input.EvidenceRefs,
		},
	)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "create action suggestion failed",
		})
		return
	}

	writeJSON(response, http.StatusCreated, suggestion)
}

func (server server) loadInternalEvidenceBundle(
	response http.ResponseWriter,
	request *http.Request,
) (db.EvidenceBundle, bool) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return db.EvidenceBundle{}, false
	}

	assessmentID := chi.URLParam(request, "assessmentID")
	if assessmentID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "assessment id is required",
		})
		return db.EvidenceBundle{}, false
	}

	bundle, err := server.store.GetEvidenceBundle(request.Context(), assessmentID)
	if err != nil {
		writeJSON(response, http.StatusNotFound, map[string]string{
			"error": "evidence bundle not found",
		})
		return db.EvidenceBundle{}, false
	}

	return bundle, true
}

type internalCheckRun struct {
	EvidenceRef string `json:"evidence_ref"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	URL         string `json:"url"`
}

type internalPullRequest struct {
	EvidenceRef string `json:"evidence_ref"`
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	URL         string `json:"url"`
}
