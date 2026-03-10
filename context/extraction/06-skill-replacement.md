## Skill Replacement Analysis Summary

### Skills Analyzed

**GSD (Get Shit Done)**

- Does today: Full project lifecycle — research dispatch, requirements extraction, roadmap/plan creation, wave-based parallel execution, state tracking, session resume, requirement traceability, progress metrics
- Prompt engineering (stays client): ~65% — 12 agent personas, plan/task XML format, Nyquist verification discipline, fresh-context sub-agent architecture, slash command orchestration
- State management (moves to server): ~35% — STATE.md, ROADMAP.md, REQUIREMENTS.md, phase/plan/task status, session pause/resume, progress metrics, config, dependency waves

**Superpowers**

- Does today: Skill discovery/shadowing, session-start hooks, workflow pipeline (brainstorm→plan→execute→finish), 14 behavioral skill definitions, subagent dispatch coordination, plan progress tracking, skill update detection
- Prompt engineering (stays client): ~85% — all 14 skill definitions, anti-rationalization design, activation mandate, platform hooks, workflow pipeline orchestration
- State management (moves to server): ~15% — plan task completion, skill registry with priority ordering, agent lifecycle tracking, skill update versioning

**Allium**

- Does today: Behavioral specification language (parse, validate, store .allium files), Tend/Weed agents for spec authoring and drift detection, elicitation/distillation methodologies, library spec registry, modular composition with import resolution
- Prompt engineering (stays client): ~75% — specification language itself, Tend/Weed agent personas, elicitation/distillation methodologies, specification-first development workflow
- State management (moves to server): ~10-25% — AST storage, drift report tracking, library spec registry, import/dependency resolution, cross-spec impact queries

---

### Server-Side Capabilities Required

**Data Storage Needs**

- Projects: id, name, config JSON, status
- Phases: project_id, order/number, status
- Plans: phase_id, XML content, status enum (PENDING/CLAIMED/IN_PROGRESS/DONE/FAILED)
- Tasks: plan_id, name, status, timestamps
- Dependencies: plan_id → plan_id adjacency table (wave computation)
- Requirements: id, text, project_id; join tables to phases/plans/tasks
- Sessions: agent_id, project_id, state snapshot JSONB, resume context
- Skills: name, content, source_priority, version, description
- Specs (.allium): id, content, AST JSONB, project_id
- DriftReports: spec_id, entity/rule ref, classification, status, timestamps
- Artifacts (searchable): type, content, project_id, created_at

**API Operations Needed**

- `POST /projects` / `GET /projects/:id/state` — project CRUD + state read
- `PUT /tasks/:id/status` — atomic status transitions (serializable isolation for concurrent agents)
- `GET /projects/:id/waves` — compute dependency wave groups from adjacency graph
- `GET /projects/:id/progress` — completion %, velocity metrics
- `POST /sessions` / `GET /sessions/:id` — pause/resume session state
- `GET /requirements/:id/traceability` — full chain: req→phases→plans→tasks
- `GET /skills?q=<description>` — skill discovery by natural language (Meilisearch)
- `PUT /skills/:name` — upsert skill with priority (personal > built-in)
- `GET /skills/updates` — version check endpoint
- `POST /specs` / `GET /specs/:id/ast` — store and retrieve parsed allium ASTs
- `GET /specs/:id/impact?entity=<name>` — cross-spec back-link traversal
- `POST /specs/:id/drift` — record drift report
- `GET /search?q=<query>&type=<artifact|skill|spec>` — Meilisearch full-text search across all artifacts

---

### What Stays Client-Side

- GSD: 12 agent personas (researcher, planner, executor, verifier, debugger, etc.), XML task format, Nyquist verification philosophy, fresh-context sub-agent spawn strategy, 32 slash commands, wave orchestration logic
- Superpowers: All 14 skill definitions (brainstorming, TDD, verification, debugging, etc.), anti-rationalization prompt design, "1% rule" activation mandate, session-start/plan-mode hooks, platform plugin manifests
- Allium: Specification language syntax/semantics, Tend agent (spec authoring), Weed agent (drift judgment and classification), elicitation methodology (stakeholder interviews), distillation methodology (extract specs from code), specification-first workflow

---

### Key Insight

Skills are 60-85% prompt engineering and LLM reasoning that no server can replicate; hive-server replaces only the fragile markdown-based state layer (STATE.md, plan checklists, .allium file scatter). "Skill replacement" means the skills become stateless prompt clients delegating persistence, search, and graph queries to a proper backend — not that they become unnecessary.
