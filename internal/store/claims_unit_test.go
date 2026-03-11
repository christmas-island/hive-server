package store

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/christmas-island/hive-server/internal/model"
)

// claimColumns lists the columns returned by claim queries.
var claimColumns = []string{"id", "type", "resource", "agent_id", "status", "metadata", "session_key", "session_id", "channel", "sender_id", "sender_is_owner", "sandboxed", "claimed_at", "expires_at", "updated_at"}

// claimRow builds a sample claim row.
func claimRow(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows(claimColumns).AddRow(
		"claim-1", "conch", "resource-a", "agent1", "active",
		`{"key":"val"}`,
		"", "", "", "", false, false,
		now.Format(time.RFC3339Nano),
		now.Add(time.Hour).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
}

// --- GetClaim ---

func TestGetClaim_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
         FROM claims WHERE id = $1`,
	)).WithArgs("claim-1").WillReturnRows(claimRow(now))

	got, err := s.GetClaim(context.Background(), "claim-1")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if got.ID != "claim-1" {
		t.Errorf("ID = %q, want claim-1", got.ID)
	}
	if got.Resource != "resource-a" {
		t.Errorf("Resource = %q, want resource-a", got.Resource)
	}
	if got.Status != model.ClaimStatusActive {
		t.Errorf("Status = %q, want active", got.Status)
	}
	if got.ClaimedAt.IsZero() {
		t.Error("ClaimedAt should not be zero")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetClaim_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
         FROM claims WHERE id = $1`,
	)).WithArgs("missing").WillReturnRows(sqlmock.NewRows(claimColumns))

	_, err := s.GetClaim(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetClaim_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("db error")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
         FROM claims WHERE id = $1`,
	)).WithArgs("claim-1").WillReturnError(dbErr)

	_, err := s.GetClaim(context.Background(), "claim-1")
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

// --- ListClaims ---

func TestListClaims_NoFilter(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows(claimColumns).
		AddRow("c1", "conch", "r1", "a1", "active", `{}`, "", "", "", "", false, false, now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)).
		AddRow("c2", "issue", "r2", "a2", "expired", `{}`, "", "", "", "", false, false, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
          FROM claims WHERE 1=1 ORDER BY claimed_at DESC LIMIT 50`,
	)).WillReturnRows(rows)

	got, err := s.ListClaims(context.Background(), model.ClaimFilter{})
	if err != nil {
		t.Fatalf("ListClaims: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestListClaims_WithType(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows(claimColumns).
		AddRow("c1", "conch", "r1", "a1", "active", `{}`, "", "", "", "", false, false, now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
          FROM claims WHERE 1=1 AND type = $1 ORDER BY claimed_at DESC LIMIT 50`,
	)).WithArgs("conch").WillReturnRows(rows)

	got, err := s.ListClaims(context.Background(), model.ClaimFilter{Type: "conch"})
	if err != nil {
		t.Fatalf("ListClaims with type: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
}

func TestListClaims_WithAgentID(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows(claimColumns)

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
          FROM claims WHERE 1=1 AND agent_id = $1 ORDER BY claimed_at DESC LIMIT 50`,
	)).WithArgs("agent1").WillReturnRows(rows)

	got, err := s.ListClaims(context.Background(), model.ClaimFilter{AgentID: "agent1"})
	if err != nil {
		t.Fatalf("ListClaims: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestListClaims_WithResource(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows(claimColumns).
		AddRow("c1", "conch", "special-resource", "a1", "active", `{}`, "", "", "", "", false, false, now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
          FROM claims WHERE 1=1 AND resource = $1 ORDER BY claimed_at DESC LIMIT 50`,
	)).WithArgs("special-resource").WillReturnRows(rows)

	got, err := s.ListClaims(context.Background(), model.ClaimFilter{Resource: "special-resource"})
	if err != nil {
		t.Fatalf("ListClaims: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
}

func TestListClaims_WithStatus(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows(claimColumns)

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
          FROM claims WHERE 1=1 AND status = $1 ORDER BY claimed_at DESC LIMIT 50`,
	)).WithArgs("expired").WillReturnRows(rows)

	_, err := s.ListClaims(context.Background(), model.ClaimFilter{Status: "expired"})
	if err != nil {
		t.Fatalf("ListClaims with status: %v", err)
	}
}

func TestListClaims_WithLimitAndOffset(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows(claimColumns)

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
          FROM claims WHERE 1=1 ORDER BY claimed_at DESC LIMIT 5 OFFSET 10`,
	)).WillReturnRows(rows)

	_, err := s.ListClaims(context.Background(), model.ClaimFilter{Limit: 5, Offset: 10})
	if err != nil {
		t.Fatalf("ListClaims with limit/offset: %v", err)
	}
}

func TestListClaims_AllFilters(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows(claimColumns).
		AddRow("c1", "conch", "r1", "agent1", "active", `{}`, "", "", "", "", false, false, now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
          FROM claims WHERE 1=1 AND type = $1 AND agent_id = $2 AND resource = $3 AND status = $4 ORDER BY claimed_at DESC LIMIT 25 OFFSET 5`,
	)).WithArgs("conch", "agent1", "r1", "active").WillReturnRows(rows)

	got, err := s.ListClaims(context.Background(), model.ClaimFilter{
		Type: "conch", AgentID: "agent1", Resource: "r1", Status: "active",
		Limit: 25, Offset: 5,
	})
	if err != nil {
		t.Fatalf("ListClaims all filters: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
}

func TestListClaims_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("query failed")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
          FROM claims WHERE 1=1 ORDER BY claimed_at DESC LIMIT 50`,
	)).WillReturnError(dbErr)

	_, err := s.ListClaims(context.Background(), model.ClaimFilter{})
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

func TestListClaims_ScanError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	// Return wrong column count.
	rows := sqlmock.NewRows([]string{"id"}).AddRow("c1")

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
          FROM claims WHERE 1=1 ORDER BY claimed_at DESC LIMIT 50`,
	)).WillReturnRows(rows)

	_, err := s.ListClaims(context.Background(), model.ClaimFilter{})
	if err == nil {
		t.Error("expected scan error, got nil")
	}
}

// --- CreateClaim ---

func TestCreateClaim_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO claims (id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at)`,
	)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	claim := &model.Claim{
		Type:      model.ClaimTypeConch,
		Resource:  "some-resource",
		AgentID:   "agent1",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		Metadata:  map[string]string{"k": "v"},
	}
	got, err := s.CreateClaim(context.Background(), claim)
	if err != nil {
		t.Fatalf("CreateClaim: %v", err)
	}
	if got.ID == "" {
		t.Error("ID should be set")
	}
	if got.Status != model.ClaimStatusActive {
		t.Errorf("Status = %q, want active", got.Status)
	}
	if got.ClaimedAt.IsZero() {
		t.Error("ClaimedAt should not be zero")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCreateClaim_NilMetadata(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO claims (id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at)`,
	)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	claim := &model.Claim{
		Type:      model.ClaimTypeIssue,
		Resource:  "issue-42",
		AgentID:   "agent1",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		Metadata:  nil,
	}
	got, err := s.CreateClaim(context.Background(), claim)
	if err != nil {
		t.Fatalf("CreateClaim nil metadata: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil claim")
	}
}

func TestCreateClaim_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("exec failed")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO claims (id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at)`,
	)).WillReturnError(dbErr)
	mock.ExpectRollback()

	claim := &model.Claim{
		Type: model.ClaimTypeConch, Resource: "r", AgentID: "a",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	_, err := s.CreateClaim(context.Background(), claim)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- ReleaseClaim ---

func TestReleaseClaim_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE claims SET status = $1, updated_at = $2 WHERE id = $3`,
	)).WillReturnResult(sqlmock.NewResult(0, 1))
	// Fetch released claim inside tx.
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata,
			        session_key, session_id, channel, sender_id, sender_is_owner, sandboxed,
			        claimed_at, expires_at, updated_at
			 FROM claims WHERE id = $1`,
	)).WithArgs("claim-1").WillReturnRows(
		sqlmock.NewRows(claimColumns).AddRow(
			"claim-1", "conch", "r1", "a1", "released", `{}`, "", "", "", "", false, false, now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		),
	)
	// Pop next waiter — queue empty.
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, resource, agent_id, type, metadata,
		        session_key, session_id, channel, sender_id, sender_is_owner, sandboxed,
		        expires_in_sec, queued_at
		 FROM claim_queue WHERE resource = $1
		 ORDER BY queued_at ASC LIMIT 1`,
	)).WithArgs("r1").WillReturnRows(sqlmock.NewRows([]string{}))
	mock.ExpectCommit()

	got, err := s.ReleaseClaim(context.Background(), "claim-1")
	if err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}
	if got.Claim.Status != model.ClaimStatusReleased {
		t.Errorf("Status = %q, want released", got.Claim.Status)
	}
	if got.Next != nil {
		t.Errorf("expected no next waiter, got %+v", got.Next)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestReleaseClaim_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE claims SET status = $1, updated_at = $2 WHERE id = $3`,
	)).WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected
	mock.ExpectRollback()

	_, err := s.ReleaseClaim(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestReleaseClaim_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("exec failed")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE claims SET status = $1, updated_at = $2 WHERE id = $3`,
	)).WillReturnError(dbErr)
	mock.ExpectRollback()

	_, err := s.ReleaseClaim(context.Background(), "claim-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- RenewClaim ---

func TestRenewClaim_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	newExpiry := now.Add(2 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE claims SET expires_at = $1, updated_at = $2 WHERE id = $3 AND status = $4`,
	)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
         FROM claims WHERE id = $1`,
	)).WithArgs("claim-1").WillReturnRows(
		sqlmock.NewRows(claimColumns).AddRow(
			"claim-1", "conch", "r1", "a1", "active", `{}`, "", "", "", "", false, false, now.Format(time.RFC3339Nano), newExpiry.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		),
	)

	got, err := s.RenewClaim(context.Background(), "claim-1", newExpiry)
	if err != nil {
		t.Fatalf("RenewClaim: %v", err)
	}
	if got.Status != model.ClaimStatusActive {
		t.Errorf("Status = %q, want active", got.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRenewClaim_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	newExpiry := time.Now().UTC().Add(2 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE claims SET expires_at = $1, updated_at = $2 WHERE id = $3 AND status = $4`,
	)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	_, err := s.RenewClaim(context.Background(), "missing", newExpiry)
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRenewClaim_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("exec failed")
	newExpiry := time.Now().UTC().Add(2 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE claims SET expires_at = $1, updated_at = $2 WHERE id = $3 AND status = $4`,
	)).WillReturnError(dbErr)
	mock.ExpectRollback()

	_, err := s.RenewClaim(context.Background(), "claim-1", newExpiry)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- ExpireOldClaims ---

func TestExpireOldClaims_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE claims SET status = $1, updated_at = $2 WHERE status = $3 AND expires_at < $4`,
	)).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	n, err := s.ExpireOldClaims(context.Background())
	if err != nil {
		t.Fatalf("ExpireOldClaims: %v", err)
	}
	if n != 3 {
		t.Errorf("count = %d, want 3", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestExpireOldClaims_NoExpired(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE claims SET status = $1, updated_at = $2 WHERE status = $3 AND expires_at < $4`,
	)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	n, err := s.ExpireOldClaims(context.Background())
	if err != nil {
		t.Fatalf("ExpireOldClaims: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

func TestExpireOldClaims_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("exec failed")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE claims SET status = $1, updated_at = $2 WHERE status = $3 AND expires_at < $4`,
	)).WillReturnError(dbErr)
	mock.ExpectRollback()

	_, err := s.ExpireOldClaims(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
