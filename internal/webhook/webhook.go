// Package webhook implements GitHub webhook handling for event-driven task
// and claim automation. It validates HMAC-SHA256 signatures and dispatches
// events to per-type handlers that manage hive tasks and claims.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/christmas-island/hive-server/internal/log"
	"github.com/christmas-island/hive-server/internal/model"
)

// Store is the subset of the data layer the webhook handler needs.
type Store interface {
	CreateTask(ctx context.Context, t *model.Task) (*model.Task, error)
	ListTasks(ctx context.Context, f model.TaskFilter) ([]*model.Task, error)
	UpdateTask(ctx context.Context, id string, upd model.TaskUpdate) (*model.Task, error)
	ListClaims(ctx context.Context, f model.ClaimFilter) ([]*model.Claim, error)
	ReleaseClaim(ctx context.Context, id string) (*model.ClaimReleaseResult, error)
}

// Handler processes incoming GitHub webhook events.
type Handler struct {
	secret []byte
	store  Store
}

// New creates a webhook handler with the given HMAC secret and store.
func New(secret string, store Store) *Handler {
	return &Handler{
		secret: []byte(secret),
		store:  store,
	}
}

// ServeHTTP validates the webhook signature, parses the event, and dispatches
// to the appropriate handler. Unrecognised events return 200 (no-op).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if len(h.secret) > 0 {
		sig := r.Header.Get("X-Hub-Signature-256")
		if !ValidateSignature(body, sig, h.secret) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}
	}

	event := r.Header.Get("X-GitHub-Event")
	ctx := r.Context()

	switch event {
	case "pull_request":
		h.handlePullRequest(ctx, body)
	case "issues":
		h.handleIssues(ctx, body)
	case "delete":
		h.handleDelete(ctx, body)
	default:
		// No-op for unhandled events.
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// ValidateSignature checks the HMAC-SHA256 signature from GitHub.
// The signature header format is "sha256=<hex>".
func ValidateSignature(payload []byte, signature string, secret []byte) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sigHex := strings.TrimPrefix(signature, "sha256=")
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	expected := mac.Sum(nil)

	return hmac.Equal(sigBytes, expected)
}

// --- GitHub event payload types ---

type pullRequestEvent struct {
	Action      string      `json:"action"`
	PullRequest pullRequest `json:"pull_request"`
}

type pullRequest struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Merged bool   `json:"merged"`
	HTMLURL string `json:"html_url"`
	Head   struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

type issuesEvent struct {
	Action string `json:"action"`
	Issue  issue  `json:"issue"`
}

type issue struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

type deleteEvent struct {
	RefType string `json:"ref_type"`
	Ref     string `json:"ref"`
}

// --- Event handlers ---

func (h *Handler) handlePullRequest(ctx context.Context, body []byte) {
	var ev pullRequestEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		log.Error("webhook: unmarshal pull_request: ", err)
		return
	}

	if ev.Action == "closed" && ev.PullRequest.Merged {
		h.releaseClaims(ctx, ev.PullRequest.Head.Ref)
		h.releaseClaims(ctx, ev.PullRequest.HTMLURL)
	}

	if ev.Action == "opened" {
		h.createTaskFromPR(ctx, &ev)
	}
}

func (h *Handler) handleIssues(ctx context.Context, body []byte) {
	var ev issuesEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		log.Error("webhook: unmarshal issues: ", err)
		return
	}

	switch ev.Action {
	case "opened":
		h.createTaskFromIssue(ctx, &ev)
	case "closed":
		h.completeTaskForIssue(ctx, &ev)
	}
}

func (h *Handler) handleDelete(ctx context.Context, body []byte) {
	var ev deleteEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		log.Error("webhook: unmarshal delete: ", err)
		return
	}

	if ev.RefType == "branch" {
		h.releaseClaims(ctx, ev.Ref)
	}
}

// releaseClaims releases all active claims matching the given resource string.
func (h *Handler) releaseClaims(ctx context.Context, resource string) {
	if resource == "" {
		return
	}
	claims, err := h.store.ListClaims(ctx, model.ClaimFilter{
		Resource: resource,
		Status:   string(model.ClaimStatusActive),
	})
	if err != nil {
		log.Error("webhook: list claims for resource: ", err)
		return
	}
	for _, c := range claims {
		if _, err := h.store.ReleaseClaim(ctx, c.ID); err != nil {
			log.Error(fmt.Sprintf("webhook: release claim %s: %v", c.ID, err))
		}
	}
}

func (h *Handler) createTaskFromPR(ctx context.Context, ev *pullRequestEvent) {
	title := fmt.Sprintf("PR #%d: %s", ev.PullRequest.Number, ev.PullRequest.Title)
	t := &model.Task{
		Title:       title,
		Description: ev.PullRequest.Body,
		Creator:     "github-webhook",
		Tags:        []string{"github", "pull_request"},
	}
	if _, err := h.store.CreateTask(ctx, t); err != nil {
		log.Error("webhook: create task from PR: ", err)
	}
}

func (h *Handler) createTaskFromIssue(ctx context.Context, ev *issuesEvent) {
	title := fmt.Sprintf("Issue #%d: %s", ev.Issue.Number, ev.Issue.Title)
	t := &model.Task{
		Title:       title,
		Description: ev.Issue.Body,
		Creator:     "github-webhook",
		Tags:        []string{"github", "issue"},
	}
	if _, err := h.store.CreateTask(ctx, t); err != nil {
		log.Error("webhook: create task from issue: ", err)
	}
}

func (h *Handler) completeTaskForIssue(ctx context.Context, ev *issuesEvent) {
	prefix := fmt.Sprintf("Issue #%d:", ev.Issue.Number)
	tasks, err := h.store.ListTasks(ctx, model.TaskFilter{})
	if err != nil {
		log.Error("webhook: list tasks for issue close: ", err)
		return
	}
	done := model.TaskStatusDone
	now := time.Now().UTC()
	for _, t := range tasks {
		if strings.HasPrefix(t.Title, prefix) && t.Status != model.TaskStatusDone {
			note := fmt.Sprintf("Closed via GitHub webhook at %s", now.Format(time.RFC3339))
			if _, err := h.store.UpdateTask(ctx, t.ID, model.TaskUpdate{
				Status: &done,
				Note:   &note,
			}); err != nil {
				log.Error(fmt.Sprintf("webhook: complete task %s: %v", t.ID, err))
			}
		}
	}
}
