package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type supplementCLIStore struct {
	store.SecureCLIStore
	bins      []store.SecureCLIBinary
	labels    []store.SecureCLICredentialLabel
	labelsErr error
	query     store.SecureCLICredentialLabelQuery
}

func (s *supplementCLIStore) ListForAgent(context.Context, uuid.UUID) ([]store.SecureCLIBinary, error) {
	out := make([]store.SecureCLIBinary, len(s.bins))
	copy(out, s.bins)
	return out, nil
}

func (s *supplementCLIStore) ListEnabled(ctx context.Context) ([]store.SecureCLIBinary, error) {
	return s.ListForAgent(ctx, uuid.Nil)
}

func (s *supplementCLIStore) ListCredentialLabels(_ context.Context, q store.SecureCLICredentialLabelQuery) ([]store.SecureCLICredentialLabel, error) {
	s.query = q
	return s.labels, s.labelsErr
}

func (s *supplementCLIStore) ResolveLabeledCredential(context.Context, uuid.UUID, *uuid.UUID, string, string) (*store.SecureCLIResolvedCredential, error) {
	return nil, nil
}
func (s *supplementCLIStore) GetUserCredentialByLabel(context.Context, uuid.UUID, string, string) (*store.SecureCLIUserCredential, error) {
	return nil, nil
}
func (s *supplementCLIStore) SetUserCredentialLabeled(context.Context, uuid.UUID, string, store.SecureCLICredentialWrite) error {
	return nil
}
func (s *supplementCLIStore) DeleteUserCredentialByLabel(context.Context, uuid.UUID, string, string) error {
	return nil
}
func (s *supplementCLIStore) GetAgentCredentialByLabel(context.Context, uuid.UUID, uuid.UUID, string) (*store.SecureCLIAgentCredential, error) {
	return nil, nil
}
func (s *supplementCLIStore) SetAgentCredentialLabeled(context.Context, uuid.UUID, uuid.UUID, store.SecureCLICredentialWrite, string) error {
	return nil
}
func (s *supplementCLIStore) DeleteAgentCredentialByLabel(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

type plainCLIStore struct {
	store.SecureCLIStore
	bins []store.SecureCLIBinary
}

func (s *plainCLIStore) ListForAgent(context.Context, uuid.UUID) ([]store.SecureCLIBinary, error) {
	return s.bins, nil
}

func newSupplementBins() (store.SecureCLIBinary, store.SecureCLIBinary) {
	orca := store.SecureCLIBinary{BinaryName: "orca", Description: "Orca CLI"}
	orca.ID = uuid.New()
	gh := store.SecureCLIBinary{BinaryName: "gh", Description: "GitHub CLI"}
	gh.ID = uuid.New()
	return orca, gh
}

func TestBuildCredentialCLIContext_GroupsLabelsByBinaryAndScopesQuery(t *testing.T) {
	orca, gh := newSupplementBins()
	agentID := uuid.New()
	st := &supplementCLIStore{
		bins: []store.SecureCLIBinary{orca, gh},
		labels: []store.SecureCLICredentialLabel{
			{BinaryID: orca.ID, Label: "build-vm", Description: "CI build VM"},
			{BinaryID: gh.ID, Label: ""},
		},
	}
	l := &Loop{secureCLIStore: st, agentUUID: agentID}
	ctx := store.WithUserID(context.Background(), "u1")

	out := l.buildCredentialCLIContext(ctx)

	if !strings.Contains(out, "- build-vm — CI build VM") {
		t.Fatalf("missing orca server:\n%s", out)
	}
	if strings.Count(out, "Servers (") != 1 {
		t.Fatalf("servers block must render only for orca:\n%s", out)
	}
	if st.query.UserID != "u1" || st.query.AgentID == nil || *st.query.AgentID != agentID || st.query.BinaryID != nil {
		t.Fatalf("unexpected label query: %+v", st.query)
	}
}

func TestBuildCredentialCLIContext_LabelListErrorRendersWithoutServers(t *testing.T) {
	orca, _ := newSupplementBins()
	st := &supplementCLIStore{
		bins:      []store.SecureCLIBinary{orca},
		labels:    []store.SecureCLICredentialLabel{{BinaryID: orca.ID, Label: "build-vm"}},
		labelsErr: errors.New("boom"),
	}
	l := &Loop{secureCLIStore: st, agentUUID: uuid.New()}

	out := l.buildCredentialCLIContext(context.Background())

	if !strings.Contains(out, "**orca**") || strings.Contains(out, "Servers (") {
		t.Fatalf("expected base context without servers:\n%s", out)
	}
}

func TestBuildCredentialCLIContext_StoreWithoutLabelSupportUnchanged(t *testing.T) {
	orca, _ := newSupplementBins()
	l := &Loop{secureCLIStore: &plainCLIStore{bins: []store.SecureCLIBinary{orca}}, agentUUID: uuid.New()}

	out := l.buildCredentialCLIContext(context.Background())

	if !strings.Contains(out, "**orca**") || strings.Contains(out, "Servers (") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestBuildCredentialCLIContext_NoAgentOmitsAgentScope(t *testing.T) {
	orca, _ := newSupplementBins()
	st := &supplementCLIStore{bins: []store.SecureCLIBinary{orca}}
	l := &Loop{secureCLIStore: st}

	l.buildCredentialCLIContext(store.WithUserID(context.Background(), "u2"))

	if st.query.AgentID != nil || st.query.UserID != "u2" {
		t.Fatalf("unexpected label query: %+v", st.query)
	}
}
