package ui

import (
	"context"
	"embed"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/christmas-island/hive-server/internal/model"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Store interface matches the one from handlers package
type Store interface {
	ListMemory(ctx context.Context, f model.MemoryFilter) ([]*model.MemoryEntry, error)
	ListTasks(ctx context.Context, f model.TaskFilter) ([]*model.Task, error)
	ListAgents(ctx context.Context) ([]*model.Agent, error)
	ListClaims(ctx context.Context, f model.ClaimFilter) ([]*model.Claim, error)
	ListTodos(ctx context.Context, f model.TodoFilter) ([]*model.Todo, error)
	ListCapturedSessions(ctx context.Context, f model.SessionFilter) ([]*model.CapturedSession, error)
}

// UI holds the UI dependencies
type UI struct {
	store Store
	pages map[string]*template.Template
}

// New creates a new UI handler
func New(store Store) *UI {
	ui := &UI{
		store: store,
		pages: make(map[string]*template.Template),
	}

	// Helper functions for templates
	funcMap := template.FuncMap{
		"timeAgo":     timeAgo,
		"formatTime":  formatTime,
		"truncate":    truncate,
		"statusClass": statusClass,
		"now":         time.Now,
	}

	// Pre-parse templates for each page to avoid "content" block overwrite issues
	// when multiple templates are parsed into the same set.
	pageTemplates := []string{
		"dashboard.html",
		"agents.html",
		"tasks.html",
		"claims.html",
		"memory.html",
		"sessions.html",
	}

	for _, page := range pageTemplates {
		tmpl := template.New(page).Funcs(funcMap)
		// Parse layout first, then the specific page
		tmpl = template.Must(tmpl.ParseFS(templateFS, "templates/layout.html", "templates/"+page))
		ui.pages[page] = tmpl
	}

	return ui
}

// Routes sets up the UI routes
func (ui *UI) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	// Static files
	r.Handle("/static/*", http.FileServer(http.FS(staticFS)))

	// UI pages
	r.Get("/", ui.dashboard)
	r.Get("/agents", ui.agents)
	r.Get("/tasks", ui.tasks)
	r.Get("/claims", ui.claims)
	r.Get("/memory", ui.memory)
	r.Get("/sessions", ui.sessions)

	return r
}

// render is a helper to execute the correct page template
func (ui *UI) render(w http.ResponseWriter, page string, data interface{}) {
	tmpl, ok := ui.pages[page]
	if !ok {
		http.Error(w, "Template not found", http.StatusNotFound)
		return
	}

	if err := tmpl.ExecuteTemplate(w, page, data); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// dashboard shows the overview page
func (ui *UI) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get overview data
	agents, err := ui.store.ListAgents(ctx)
	if err != nil {
		http.Error(w, "Failed to fetch agents", http.StatusInternalServerError)
		return
	}

	tasks, err := ui.store.ListTasks(ctx, model.TaskFilter{Limit: 10})
	if err != nil {
		http.Error(w, "Failed to fetch tasks", http.StatusInternalServerError)
		return
	}

	claims, err := ui.store.ListClaims(ctx, model.ClaimFilter{Status: "active", Limit: 10})
	if err != nil {
		http.Error(w, "Failed to fetch claims", http.StatusInternalServerError)
		return
	}

	memory, err := ui.store.ListMemory(ctx, model.MemoryFilter{Limit: 10})
	if err != nil {
		http.Error(w, "Failed to fetch memory", http.StatusInternalServerError)
		return
	}

	data := struct {
		Title        string
		Agents       []*model.Agent
		Tasks        []*model.Task
		Claims       []*model.Claim
		Memory       []*model.MemoryEntry
		OnlineAgents int
		OpenTasks    int
		ActiveClaims int
	}{
		Title:        "Hive Dashboard",
		Agents:       agents,
		Tasks:        tasks,
		Claims:       claims,
		Memory:       memory,
		ActiveClaims: len(claims),
	}

	// Count online agents and open tasks
	for _, agent := range agents {
		if agent.Status == model.AgentStatusOnline {
			data.OnlineAgents++
		}
	}
	for _, task := range tasks {
		if task.Status == model.TaskStatusOpen {
			data.OpenTasks++
		}
	}

	ui.render(w, "dashboard.html", data)
}

// agents shows the agents list
func (ui *UI) agents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agents, err := ui.store.ListAgents(ctx)
	if err != nil {
		http.Error(w, "Failed to fetch agents", http.StatusInternalServerError)
		return
	}

	data := struct {
		Title  string
		Agents []*model.Agent
	}{
		Title:  "Agents",
		Agents: agents,
	}

	ui.render(w, "agents.html", data)
}

// tasks shows the task list
func (ui *UI) tasks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse filters from query params
	filter := model.TaskFilter{}
	if status := r.URL.Query().Get("status"); status != "" {
		filter.Status = status
	}
	if assignee := r.URL.Query().Get("assignee"); assignee != "" {
		filter.Assignee = assignee
	}
	if creator := r.URL.Query().Get("creator"); creator != "" {
		filter.Creator = creator
	}
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			filter.Limit = limit
		}
	}
	if filter.Limit == 0 {
		filter.Limit = 50 // default
	}

	tasks, err := ui.store.ListTasks(ctx, filter)
	if err != nil {
		http.Error(w, "Failed to fetch tasks", http.StatusInternalServerError)
		return
	}

	data := struct {
		Title  string
		Tasks  []*model.Task
		Filter model.TaskFilter
	}{
		Title:  "Tasks",
		Tasks:  tasks,
		Filter: filter,
	}

	ui.render(w, "tasks.html", data)
}

// claims shows the claims list
func (ui *UI) claims(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse filters from query params
	filter := model.ClaimFilter{}
	if status := r.URL.Query().Get("status"); status != "" {
		filter.Status = status
	}
	if agentID := r.URL.Query().Get("agent"); agentID != "" {
		filter.AgentID = agentID
	}
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			filter.Limit = limit
		}
	}
	if filter.Limit == 0 {
		filter.Limit = 50 // default
	}

	claims, err := ui.store.ListClaims(ctx, filter)
	if err != nil {
		http.Error(w, "Failed to fetch claims", http.StatusInternalServerError)
		return
	}

	data := struct {
		Title  string
		Claims []*model.Claim
		Filter model.ClaimFilter
	}{
		Title:  "Claims",
		Claims: claims,
		Filter: filter,
	}

	ui.render(w, "claims.html", data)
}

// memory shows the memory entries list
func (ui *UI) memory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse filters from query params
	filter := model.MemoryFilter{}
	if agent := r.URL.Query().Get("agent"); agent != "" {
		filter.Agent = agent
	}
	if prefix := r.URL.Query().Get("prefix"); prefix != "" {
		filter.Prefix = prefix
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		filter.Tag = tag
	}
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			filter.Limit = limit
		}
	}
	if filter.Limit == 0 {
		filter.Limit = 50 // default
	}

	memory, err := ui.store.ListMemory(ctx, filter)
	if err != nil {
		http.Error(w, "Failed to fetch memory", http.StatusInternalServerError)
		return
	}

	data := struct {
		Title  string
		Memory []*model.MemoryEntry
		Filter model.MemoryFilter
	}{
		Title:  "Memory",
		Memory: memory,
		Filter: filter,
	}

	ui.render(w, "memory.html", data)
}

// sessions shows the captured sessions list
func (ui *UI) sessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse filters from query params
	filter := model.SessionFilter{}
	if agent := r.URL.Query().Get("agent"); agent != "" {
		filter.AgentID = agent
	}
	if repo := r.URL.Query().Get("repo"); repo != "" {
		filter.Repo = repo
	}
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			filter.Limit = limit
		}
	}
	if filter.Limit == 0 {
		filter.Limit = 25 // default
	}

	sessions, err := ui.store.ListCapturedSessions(ctx, filter)
	if err != nil {
		http.Error(w, "Failed to fetch sessions", http.StatusInternalServerError)
		return
	}

	data := struct {
		Title    string
		Sessions []*model.CapturedSession
		Filter   model.SessionFilter
	}{
		Title:    "Sessions",
		Sessions: sessions,
		Filter:   filter,
	}

	ui.render(w, "sessions.html", data)
}

// Helper functions for templates

func timeAgo(t time.Time) string {
	diff := time.Since(t)
	if diff < time.Minute {
		return "just now"
	} else if diff < time.Hour {
		return strconv.Itoa(int(diff.Minutes())) + "m ago"
	} else if diff < 24*time.Hour {
		return strconv.Itoa(int(diff.Hours())) + "h ago"
	} else {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return strconv.Itoa(days) + " days ago"
	}
}

func formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func statusClass(status string) string {
	switch status {
	case "online":
		return "text-green-400"
	case "offline":
		return "text-red-400"
	case "idle":
		return "text-yellow-400"
	case "open":
		return "text-blue-400"
	case "in_progress":
		return "text-orange-400"
	case "done":
		return "text-green-400"
	case "failed":
		return "text-red-400"
	case "active":
		return "text-green-400"
	case "expired":
		return "text-red-400"
	default:
		return "text-gray-400"
	}
}
