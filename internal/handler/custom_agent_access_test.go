package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// stubAccessMemberService answers GetMembership from a fixed row so the
// ListAgents access filter can be exercised without a database.
type stubAccessMemberService struct {
	interfaces.TenantMemberService
	member *types.TenantMember
}

func (s *stubAccessMemberService) GetMembership(ctx context.Context, userID string, tenantID uint64) (*types.TenantMember, error) {
	return s.member, nil
}

// stubAccessListAgentService returns a fixed agent list (built-in + two customs).
type stubAccessListAgentService struct {
	interfaces.CustomAgentService
}

func (s *stubAccessListAgentService) ListAgents(ctx context.Context) ([]*types.CustomAgent, error) {
	return []*types.CustomAgent{
		{ID: "builtin-quick-answer", TenantID: 1, IsBuiltin: true},
		{ID: "builtin-smart-reasoning", TenantID: 1, IsBuiltin: true},
		{ID: "agent-a", TenantID: 1, CreatedBy: "owner"},
		{ID: "agent-b", TenantID: 1, CreatedBy: "owner"},
	}, nil
}

// stubDisabledRepo returns no disabled agents; ListAgents touches it after
// the access filter, so a nil repo would panic the handler under test.
type stubDisabledRepo struct {
	interfaces.TenantDisabledSharedAgentRepository
}

func (s *stubDisabledRepo) ListDisabledOwnAgentIDs(ctx context.Context, tenantID uint64) ([]string, error) {
	return nil, nil
}

func newListAgentsAccessHandler(t *testing.T, member *types.TenantMember) *CustomAgentHandler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return &CustomAgentHandler{
		service:       &stubAccessListAgentService{},
		memberService: &stubAccessMemberService{member: member},
		disabledRepo:  &stubDisabledRepo{},
	}
}

func runListAgents(t *testing.T, h *CustomAgentHandler) []string {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	c.Request = c.Request.WithContext(context.Background())
	c.Set(types.TenantIDContextKey.String(), uint64(1))
	c.Set(types.UserIDContextKey.String(), "u1")
	h.ListAgents(c)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents status = %d, want 200", w.Code)
	}
	var body struct {
		Data []*types.CustomAgent `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	ids := make([]string, 0, len(body.Data))
	for _, a := range body.Data {
		ids = append(ids, a.ID)
	}
	return ids
}

func TestListAgents_MemberAccessFilter(t *testing.T) {
	cases := []struct {
		name    string
		member  *types.TenantMember
		wantIDs []string
	}{
		{
			name:    "no membership row stays unfiltered",
			member:  nil,
			wantIDs: []string{"builtin-quick-answer", "builtin-smart-reasoning", "agent-a", "agent-b"},
		},
		{
			name:    "null allowlist (unrestricted) stays unfiltered",
			member:  &types.TenantMember{UserID: "u1", TenantID: 1, AllowedAgentIDs: nil},
			wantIDs: []string{"builtin-quick-answer", "builtin-smart-reasoning", "agent-a", "agent-b"},
		},
		{
			name: "allowlist keeps builtins plus exactly the allowed customs",
			member: &types.TenantMember{UserID: "u1", TenantID: 1,
				AllowedAgentIDs: types.AgentIDList{"agent-b"}},
			wantIDs: []string{"builtin-quick-answer", "builtin-smart-reasoning", "agent-b"},
		},
		{
			name: "empty allowlist leaves only builtins",
			member: &types.TenantMember{UserID: "u1", TenantID: 1,
				AllowedAgentIDs: types.AgentIDList{}},
			wantIDs: []string{"builtin-quick-answer", "builtin-smart-reasoning"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newListAgentsAccessHandler(t, tc.member)
			got := runListAgents(t, h)
			if len(got) != len(tc.wantIDs) {
				t.Fatalf("ids = %v, want %v", got, tc.wantIDs)
			}
			for i := range got {
				if got[i] != tc.wantIDs[i] {
					t.Fatalf("ids = %v, want %v", got, tc.wantIDs)
				}
			}
		})
	}
}
