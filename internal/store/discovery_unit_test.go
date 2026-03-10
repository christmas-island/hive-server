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

// channelColumns are the columns returned by discovery_channels queries.
var channelColumns = []string{"id", "name", "discord_id", "purpose", "category", "members", "created_at", "updated_at"}

// roleColumns are the columns returned by discovery_roles queries.
var roleColumns = []string{"id", "name", "discord_id", "members", "created_at", "updated_at"}

// discoveryAgentColumns are the columns returned by discovery agent queries.
var discoveryAgentColumns = []string{
	"id", "name", "status", "capabilities", "last_heartbeat", "registered_at",
	"discord_user_id", "home_channel", "mention_format", "channels",
}

// sampleChannelRow returns a single channel row.
func sampleChannelRow(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows(channelColumns).AddRow(
		"general", "General", "123456", "General chat", "Text",
		`["agent1","agent2"]`,
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
}

// sampleRoleRow returns a single role row.
func sampleRoleRow(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows(roleColumns).AddRow(
		"admin", "Admin", "789012",
		`["agent1"]`,
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
}

// sampleDiscoveryAgentRow returns a single discovery agent row.
func sampleDiscoveryAgentRow(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows(discoveryAgentColumns).AddRow(
		"agent1", "Agent One", "online", `["tasks"]`,
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		"discord-user-1", "home-channel", "@Agent One",
		`["ch1","ch2"]`,
	)
}

// --- GetChannel ---

func TestGetChannel_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, purpose, category, members, created_at, updated_at
		 FROM discovery_channels WHERE id = $1`,
	)).WithArgs("general").WillReturnRows(sampleChannelRow(now))

	got, err := s.GetChannel(context.Background(), "general")
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if got.ID != "general" {
		t.Errorf("ID = %q, want general", got.ID)
	}
	if got.Name != "General" {
		t.Errorf("Name = %q, want General", got.Name)
	}
	if len(got.Members) != 2 {
		t.Errorf("Members len = %d, want 2", len(got.Members))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetChannel_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, purpose, category, members, created_at, updated_at
		 FROM discovery_channels WHERE id = $1`,
	)).WithArgs("missing").WillReturnRows(sqlmock.NewRows(channelColumns))

	_, err := s.GetChannel(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetChannel_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("db error")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, purpose, category, members, created_at, updated_at
		 FROM discovery_channels WHERE id = $1`,
	)).WithArgs("ch1").WillReturnError(dbErr)

	_, err := s.GetChannel(context.Background(), "ch1")
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

// --- ListChannels ---

func TestListChannels_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows(channelColumns).
		AddRow("ch1", "Channel 1", "111", "", "", `[]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)).
		AddRow("ch2", "Channel 2", "222", "Purpose", "Cat", `["a1"]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, purpose, category, members, created_at, updated_at
		 FROM discovery_channels ORDER BY id ASC`,
	)).WillReturnRows(rows)

	got, err := s.ListChannels(context.Background())
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestListChannels_Empty(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, purpose, category, members, created_at, updated_at
		 FROM discovery_channels ORDER BY id ASC`,
	)).WillReturnRows(sqlmock.NewRows(channelColumns))

	got, err := s.ListChannels(context.Background())
	if err != nil {
		t.Fatalf("ListChannels empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestListChannels_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("query error")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, purpose, category, members, created_at, updated_at
		 FROM discovery_channels ORDER BY id ASC`,
	)).WillReturnError(dbErr)

	_, err := s.ListChannels(context.Background())
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

func TestListChannels_ScanError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows([]string{"id"}).AddRow("ch1")

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, purpose, category, members, created_at, updated_at
		 FROM discovery_channels ORDER BY id ASC`,
	)).WillReturnRows(rows)

	_, err := s.ListChannels(context.Background())
	if err == nil {
		t.Error("expected scan error, got nil")
	}
}

// --- DeleteChannel ---

func TestDeleteChannel_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM discovery_channels WHERE id = $1`)).
		WithArgs("general").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.DeleteChannel(context.Background(), "general"); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestDeleteChannel_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM discovery_channels WHERE id = $1`)).
		WithArgs("missing").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := s.DeleteChannel(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteChannel_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("exec error")
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM discovery_channels WHERE id = $1`)).
		WithArgs("ch1").
		WillReturnError(dbErr)

	err := s.DeleteChannel(context.Background(), "ch1")
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

// --- UpsertChannel ---

func TestUpsertChannel_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO discovery_channels`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, purpose, category, members, created_at, updated_at
		 FROM discovery_channels WHERE id = $1`,
	)).WithArgs("general").WillReturnRows(sampleChannelRow(now))

	ch := &model.DiscoveryChannel{
		ID:        "general",
		Name:      "General",
		DiscordID: "123456",
		Members:   []string{"agent1", "agent2"},
	}
	got, err := s.UpsertChannel(context.Background(), ch)
	if err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if got.ID != "general" {
		t.Errorf("ID = %q, want general", got.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestUpsertChannel_NilMembers(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO discovery_channels`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, purpose, category, members, created_at, updated_at
		 FROM discovery_channels WHERE id = $1`,
	)).WithArgs("ch1").WillReturnRows(
		sqlmock.NewRows(channelColumns).AddRow(
			"ch1", "Ch1", "", "", "", `[]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		),
	)

	ch := &model.DiscoveryChannel{ID: "ch1", Name: "Ch1", Members: nil}
	got, err := s.UpsertChannel(context.Background(), ch)
	if err != nil {
		t.Fatalf("UpsertChannel nil members: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestUpsertChannel_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("exec failed")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO discovery_channels`)).
		WillReturnError(dbErr)
	mock.ExpectRollback()

	ch := &model.DiscoveryChannel{ID: "ch1", Name: "Ch1", Members: []string{}}
	_, err := s.UpsertChannel(context.Background(), ch)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- GetRole ---

func TestGetRole_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, members, created_at, updated_at
		 FROM discovery_roles WHERE id = $1`,
	)).WithArgs("admin").WillReturnRows(sampleRoleRow(now))

	got, err := s.GetRole(context.Background(), "admin")
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	if got.ID != "admin" {
		t.Errorf("ID = %q, want admin", got.ID)
	}
	if len(got.Members) != 1 {
		t.Errorf("Members len = %d, want 1", len(got.Members))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetRole_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, members, created_at, updated_at
		 FROM discovery_roles WHERE id = $1`,
	)).WithArgs("missing").WillReturnRows(sqlmock.NewRows(roleColumns))

	_, err := s.GetRole(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetRole_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("db error")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, members, created_at, updated_at
		 FROM discovery_roles WHERE id = $1`,
	)).WithArgs("r1").WillReturnError(dbErr)

	_, err := s.GetRole(context.Background(), "r1")
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

// --- ListRoles ---

func TestListRoles_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows(roleColumns).
		AddRow("admin", "Admin", "111", `["a1"]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)).
		AddRow("mod", "Mod", "222", `[]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, members, created_at, updated_at
		 FROM discovery_roles ORDER BY id ASC`,
	)).WillReturnRows(rows)

	got, err := s.ListRoles(context.Background())
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestListRoles_Empty(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, members, created_at, updated_at
		 FROM discovery_roles ORDER BY id ASC`,
	)).WillReturnRows(sqlmock.NewRows(roleColumns))

	got, err := s.ListRoles(context.Background())
	if err != nil {
		t.Fatalf("ListRoles empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestListRoles_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("query error")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, members, created_at, updated_at
		 FROM discovery_roles ORDER BY id ASC`,
	)).WillReturnError(dbErr)

	_, err := s.ListRoles(context.Background())
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

func TestListRoles_ScanError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows([]string{"id"}).AddRow("r1")

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, members, created_at, updated_at
		 FROM discovery_roles ORDER BY id ASC`,
	)).WillReturnRows(rows)

	_, err := s.ListRoles(context.Background())
	if err == nil {
		t.Error("expected scan error, got nil")
	}
}

// --- DeleteRole ---

func TestDeleteRole_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM discovery_roles WHERE id = $1`)).
		WithArgs("admin").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.DeleteRole(context.Background(), "admin"); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
}

func TestDeleteRole_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM discovery_roles WHERE id = $1`)).
		WithArgs("missing").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := s.DeleteRole(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteRole_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("exec error")
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM discovery_roles WHERE id = $1`)).
		WithArgs("r1").
		WillReturnError(dbErr)

	err := s.DeleteRole(context.Background(), "r1")
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

// --- UpsertRole ---

func TestUpsertRole_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO discovery_roles`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, members, created_at, updated_at
		 FROM discovery_roles WHERE id = $1`,
	)).WithArgs("admin").WillReturnRows(sampleRoleRow(now))

	role := &model.DiscoveryRole{
		ID:        "admin",
		Name:      "Admin",
		DiscordID: "789012",
		Members:   []string{"agent1"},
	}
	got, err := s.UpsertRole(context.Background(), role)
	if err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if got.ID != "admin" {
		t.Errorf("ID = %q, want admin", got.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestUpsertRole_NilMembers(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO discovery_roles`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, discord_id, members, created_at, updated_at
		 FROM discovery_roles WHERE id = $1`,
	)).WithArgs("r1").WillReturnRows(
		sqlmock.NewRows(roleColumns).AddRow("r1", "R1", "", `[]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)),
	)

	role := &model.DiscoveryRole{ID: "r1", Name: "R1", Members: nil}
	got, err := s.UpsertRole(context.Background(), role)
	if err != nil {
		t.Fatalf("UpsertRole nil members: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil role")
	}
}

func TestUpsertRole_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("exec failed")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO discovery_roles`)).
		WillReturnError(dbErr)
	mock.ExpectRollback()

	role := &model.DiscoveryRole{ID: "r1", Name: "R1", Members: []string{}}
	_, err := s.UpsertRole(context.Background(), role)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- GetDiscoveryAgent ---

func TestGetDiscoveryAgent_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, status, capabilities, last_heartbeat, registered_at,
		       discord_user_id, home_channel, mention_format, channels
		FROM agents WHERE id = $1`)).
		WithArgs("agent1").
		WillReturnRows(sampleDiscoveryAgentRow(now))

	got, err := s.GetDiscoveryAgent(context.Background(), "agent1")
	if err != nil {
		t.Fatalf("GetDiscoveryAgent: %v", err)
	}
	if got.ID != "agent1" {
		t.Errorf("ID = %q, want agent1", got.ID)
	}
	if got.DiscordUserID != "discord-user-1" {
		t.Errorf("DiscordUserID = %q, want discord-user-1", got.DiscordUserID)
	}
	if len(got.Channels) != 2 {
		t.Errorf("Channels len = %d, want 2", len(got.Channels))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetDiscoveryAgent_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, status, capabilities, last_heartbeat, registered_at,
		       discord_user_id, home_channel, mention_format, channels
		FROM agents WHERE id = $1`)).
		WithArgs("missing").
		WillReturnRows(sqlmock.NewRows(discoveryAgentColumns))

	_, err := s.GetDiscoveryAgent(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetDiscoveryAgent_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("db error")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, status, capabilities, last_heartbeat, registered_at,
		       discord_user_id, home_channel, mention_format, channels
		FROM agents WHERE id = $1`)).
		WithArgs("a1").
		WillReturnError(dbErr)

	_, err := s.GetDiscoveryAgent(context.Background(), "a1")
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

// --- ListDiscoveryAgents ---

func TestListDiscoveryAgents_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows(discoveryAgentColumns).
		AddRow("a1", "A1", "online", `[]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "du1", "hc1", "@A1", `[]`).
		AddRow("a2", "A2", "offline", `["cap"]`, now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour).Format(time.RFC3339Nano), "", "", "", `["ch1"]`)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, status, capabilities, last_heartbeat, registered_at,
		       discord_user_id, home_channel, mention_format, channels
		FROM agents ORDER BY id ASC`)).
		WillReturnRows(rows)

	got, err := s.ListDiscoveryAgents(context.Background())
	if err != nil {
		t.Fatalf("ListDiscoveryAgents: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
	if got[0].ID != "a1" {
		t.Errorf("got[0].ID = %q, want a1", got[0].ID)
	}
}

func TestListDiscoveryAgents_Empty(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, status, capabilities, last_heartbeat, registered_at,
		       discord_user_id, home_channel, mention_format, channels
		FROM agents ORDER BY id ASC`)).
		WillReturnRows(sqlmock.NewRows(discoveryAgentColumns))

	got, err := s.ListDiscoveryAgents(context.Background())
	if err != nil {
		t.Fatalf("ListDiscoveryAgents empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestListDiscoveryAgents_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("query error")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, status, capabilities, last_heartbeat, registered_at,
		       discord_user_id, home_channel, mention_format, channels
		FROM agents ORDER BY id ASC`)).
		WillReturnError(dbErr)

	_, err := s.ListDiscoveryAgents(context.Background())
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

func TestListDiscoveryAgents_ScanError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows([]string{"id"}).AddRow("a1")

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, status, capabilities, last_heartbeat, registered_at,
		       discord_user_id, home_channel, mention_format, channels
		FROM agents ORDER BY id ASC`)).
		WillReturnRows(rows)

	_, err := s.ListDiscoveryAgents(context.Background())
	if err == nil {
		t.Error("expected scan error, got nil")
	}
}

// --- UpsertAgentMeta ---

func TestUpsertAgentMeta_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE agents SET
				discord_user_id = $2,
				home_channel    = $3,
				mention_format  = $4,
				channels        = $5
			WHERE id = $1`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// GetDiscoveryAgent after upsert
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, status, capabilities, last_heartbeat, registered_at,
		       discord_user_id, home_channel, mention_format, channels
		FROM agents WHERE id = $1`)).
		WithArgs("agent1").
		WillReturnRows(sampleDiscoveryAgentRow(now))

	meta := &model.DiscoveryAgentMeta{
		DiscordUserID: "discord-user-1",
		HomeChannel:   "home-channel",
		MentionFormat: "@Agent One",
		Channels:      []string{"ch1", "ch2"},
	}
	got, err := s.UpsertAgentMeta(context.Background(), "agent1", meta)
	if err != nil {
		t.Fatalf("UpsertAgentMeta: %v", err)
	}
	if got.ID != "agent1" {
		t.Errorf("ID = %q, want agent1", got.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestUpsertAgentMeta_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE agents SET
				discord_user_id = $2,
				home_channel    = $3,
				mention_format  = $4,
				channels        = $5
			WHERE id = $1`)).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected
	mock.ExpectCommit()

	meta := &model.DiscoveryAgentMeta{Channels: []string{}}
	_, err := s.UpsertAgentMeta(context.Background(), "missing", meta)
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpsertAgentMeta_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("exec error")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE agents SET
				discord_user_id = $2,
				home_channel    = $3,
				mention_format  = $4,
				channels        = $5
			WHERE id = $1`)).
		WillReturnError(dbErr)
	mock.ExpectRollback()

	meta := &model.DiscoveryAgentMeta{Channels: []string{}}
	_, err := s.UpsertAgentMeta(context.Background(), "a1", meta)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpsertAgentMeta_NilChannels(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE agents SET
				discord_user_id = $2,
				home_channel    = $3,
				mention_format  = $4,
				channels        = $5
			WHERE id = $1`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, status, capabilities, last_heartbeat, registered_at,
		       discord_user_id, home_channel, mention_format, channels
		FROM agents WHERE id = $1`)).
		WithArgs("a1").
		WillReturnRows(sampleDiscoveryAgentRow(now))

	meta := &model.DiscoveryAgentMeta{Channels: nil}
	got, err := s.UpsertAgentMeta(context.Background(), "a1", meta)
	if err != nil {
		t.Fatalf("UpsertAgentMeta nil channels: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil agent")
	}
}
