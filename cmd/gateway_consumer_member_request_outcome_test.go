package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type requestOutcomeTeamStore struct {
	store.TeamStore
	tasks    map[uuid.UUID]*store.TeamTaskData
	comments []store.TeamTaskCommentData
}

func (s *requestOutcomeTeamStore) GetTask(_ context.Context, id uuid.UUID) (*store.TeamTaskData, error) {
	return s.tasks[id], nil
}

func (s *requestOutcomeTeamStore) AddTaskComment(_ context.Context, c *store.TeamTaskCommentData) error {
	s.comments = append(s.comments, *c)
	return nil
}

func newRequestOutcomeStore(taskType, status string, parent *uuid.UUID, result string) (*requestOutcomeTeamStore, uuid.UUID) {
	id := uuid.New()
	owner := uuid.New()
	return &requestOutcomeTeamStore{tasks: map[uuid.UUID]*store.TeamTaskData{
		id: {
			BaseModel:    store.BaseModel{ID: id},
			Subject:      "Need the production tarball",
			Status:       status,
			TaskType:     taskType,
			TaskNumber:   15,
			ParentID:     parent,
			Result:       &result,
			OwnerAgentID: &owner,
		},
	}}, id
}

func TestCommentMemberRequestOutcomeOnParent_PostsResultOnParent(t *testing.T) {
	parent := uuid.New()
	s, id := newRequestOutcomeStore("request", store.TeamTaskStatusCompleted, &parent, "Uploaded inbox/cafe-landing-production.tgz")

	commentMemberRequestOutcomeOnParent(context.Background(), s, id, "tech-lead")

	if len(s.comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(s.comments))
	}
	c := s.comments[0]
	if c.TaskID != parent {
		t.Fatalf("comment posted on %s, want parent %s", c.TaskID, parent)
	}
	for _, want := range []string{"#15", "completed", "tech-lead", "cafe-landing-production.tgz"} {
		if !strings.Contains(c.Content, want) {
			t.Fatalf("comment %q missing %q", c.Content, want)
		}
	}
}

func TestCommentMemberRequestOutcomeOnParent_IgnoresOtherTasks(t *testing.T) {
	parent := uuid.New()
	cases := map[string]struct {
		taskType string
		status   string
		parent   *uuid.UUID
	}{
		"general task":       {"general", store.TeamTaskStatusCompleted, &parent},
		"request no parent":  {"request", store.TeamTaskStatusCompleted, nil},
		"request still open": {"request", store.TeamTaskStatusInProgress, &parent},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, id := newRequestOutcomeStore(tc.taskType, tc.status, tc.parent, "done")
			commentMemberRequestOutcomeOnParent(context.Background(), s, id, "tech-lead")
			if len(s.comments) != 0 {
				t.Fatalf("unexpected comment: %+v", s.comments)
			}
		})
	}
}

func TestCommentMemberRequestOutcomeOnParent_TruncatesLongResultByRunes(t *testing.T) {
	parent := uuid.New()
	s, id := newRequestOutcomeStore("request", store.TeamTaskStatusCompleted, &parent, strings.Repeat("ả", memberRequestOutcomeMaxRunes+50))

	commentMemberRequestOutcomeOnParent(context.Background(), s, id, "tech-lead")

	if len(s.comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(s.comments))
	}
	if !strings.Contains(s.comments[0].Content, "[truncated, full result on request #15]") {
		t.Fatal("long result was not truncated")
	}
	if !strings.Contains(s.comments[0].Content, strings.Repeat("ả", 10)) || strings.ContainsRune(s.comments[0].Content, '�') {
		t.Fatal("truncation broke UTF-8")
	}
}
