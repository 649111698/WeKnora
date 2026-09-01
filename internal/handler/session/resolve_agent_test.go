package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type resolveAgentShareStub struct {
	agent *types.CustomAgent
	err   error
}

func (s *resolveAgentShareStub) GetSharedAgentForTenant(
	_ context.Context,
	_ uint64,
	_ types.TenantRole,
	_ string,
	_ ...uint64,
) (*types.CustomAgent, error) {
	return s.agent, s.err
}

func (s *resolveAgentShareStub) ShareAgent(context.Context, string, string, string, uint64, types.OrgMemberRole) (*types.AgentShare, error) {
	panic("not implemented")
}
func (s *resolveAgentShareStub) RemoveShare(context.Context, string, string, uint64) error {
	panic("not implemented")
}
func (s *resolveAgentShareStub) ListSharesByAgent(context.Context, string, uint64) ([]*types.AgentShare, error) {
	panic("not implemented")
}
func (s *resolveAgentShareStub) ListSharesByOrganization(context.Context, string) ([]*types.AgentShare, error) {
	panic("not implemented")
}
func (s *resolveAgentShareStub) ListSharedAgents(context.Context, uint64, types.TenantRole) ([]*types.SharedAgentInfo, error) {
	panic("not implemented")
}
func (s *resolveAgentShareStub) ListSharedAgentsInOrganization(context.Context, string, uint64, types.TenantRole) ([]*types.OrganizationSharedAgentItem, error) {
	panic("not implemented")
}
func (s *resolveAgentShareStub) ListSharedAgentsInOrganizations(context.Context, []string, uint64, types.TenantRole) (map[string][]*types.OrganizationSharedAgentItem, error) {
	panic("not implemented")
}
func (s *resolveAgentShareStub) SetSharedAgentDisabledByMe(context.Context, uint64, string, uint64, bool) error {
	panic("not implemented")
}
func (s *resolveAgentShareStub) TenantCanAccessKBViaSomeSharedAgent(context.Context, uint64, types.TenantRole, *types.KnowledgeBase) (bool, error) {
	panic("not implemented")
}
func (s *resolveAgentShareStub) GetShare(context.Context, string) (*types.AgentShare, error) {
	panic("not implemented")
}
func (s *resolveAgentShareStub) GetShareByAgentAndOrg(context.Context, string, string) (*types.AgentShare, error) {
	panic("not implemented")
}
func (s *resolveAgentShareStub) GetShareByAgentIDForTenant(context.Context, uint64, string, uint64) (*types.AgentShare, error) {
	panic("not implemented")
}
func (s *resolveAgentShareStub) CountByOrganizations(context.Context, []string) (map[string]int64, error) {
	panic("not implemented")
}

type resolveOwnAgentStub struct {
	agent *types.CustomAgent
	err   error
}

func (s *resolveOwnAgentStub) GetAgentByID(context.Context, string) (*types.CustomAgent, error) {
	return s.agent, s.err
}

func (s *resolveOwnAgentStub) CreateAgent(context.Context, *types.CustomAgent) (*types.CustomAgent, error) {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) GetAgentByIDAndTenant(context.Context, string, uint64) (*types.CustomAgent, error) {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) ListAgents(context.Context) ([]*types.CustomAgent, error) {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) UpdateAgent(context.Context, *types.CustomAgent) (*types.CustomAgent, error) {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) DeleteAgent(context.Context, string) error {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) CopyAgent(context.Context, string) (*types.CustomAgent, error) {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) GetSuggestedQuestions(context.Context, string, []string, []string, []types.TagScope, int) ([]types.SuggestedQuestion, error) {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) GetKnowledgeSuggestedQuestions(context.Context, string, []string, []string, []types.TagScope, int) ([]types.SuggestedQuestion, error) {
	panic("not implemented")
}

func newResolveAgentTestContext(tenantID uint64) (*gin.Context, context.Context) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Set(types.UserIDContextKey.String(), "user-1")
	c.Set(types.TenantIDContextKey.String(), tenantID)
	ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, tenantID)
	c.Request = c.Request.WithContext(ctx)
	return c, ctx
}

func TestResolveAgent_RejectsInvalidSharedSelectorWithoutLocalFallback(t *testing.T) {
	c, ctx := newResolveAgentTestContext(7)
	localAgent := &types.CustomAgent{ID: "builtin-smart-reasoning", TenantID: 7, Name: "Local"}
	h := &Handler{
		agentShareService:  &resolveAgentShareStub{},
		customAgentService: &resolveOwnAgentStub{agent: localAgent},
	}

	agent, effectiveTenantID, sharedReadOnly := h.resolveAgent(ctx, c, "builtin-smart-reasoning", 84)

	require.Nil(t, agent)
	require.Equal(t, uint64(0), effectiveTenantID)
	require.False(t, sharedReadOnly)
}

func TestResolveAgent_UsesSharedAgentWhenSourceSelectorMatches(t *testing.T) {
	c, ctx := newResolveAgentTestContext(7)
	sharedAgent := &types.CustomAgent{ID: "builtin-smart-reasoning", TenantID: 84, Name: "Shared"}
	h := &Handler{
		agentShareService:  &resolveAgentShareStub{agent: sharedAgent},
		customAgentService: &resolveOwnAgentStub{agent: &types.CustomAgent{ID: "builtin-smart-reasoning", TenantID: 7}},
	}

	agent, effectiveTenantID, sharedReadOnly := h.resolveAgent(ctx, c, "builtin-smart-reasoning", 84)

	require.Equal(t, sharedAgent, agent)
	require.Equal(t, uint64(84), effectiveTenantID)
	require.True(t, sharedReadOnly)
}

func TestResolveAgent_FallsBackToLocalAgentWithoutSourceSelector(t *testing.T) {
	c, ctx := newResolveAgentTestContext(7)
	localAgent := &types.CustomAgent{ID: "builtin-smart-reasoning", TenantID: 7, Name: "Local"}
	h := &Handler{
		agentShareService:  &resolveAgentShareStub{},
		customAgentService: &resolveOwnAgentStub{agent: localAgent},
	}

	agent, effectiveTenantID, sharedReadOnly := h.resolveAgent(ctx, c, "builtin-smart-reasoning", 0)

	require.Equal(t, localAgent, agent)
	require.Equal(t, uint64(0), effectiveTenantID)
	require.False(t, sharedReadOnly)
}

// resolveMemberStub answers GetMembership with a fixed row so the
// per-member agent access check can be exercised without a database.
type resolveMemberStub struct {
	member *types.TenantMember
}

func (s *resolveMemberStub) AddMember(context.Context, string, uint64, types.TenantRole, *string) (*types.TenantMember, error) {
	panic("not implemented")
}
func (s *resolveMemberStub) EnsureOwner(context.Context, string, uint64) (*types.TenantMember, error) {
	panic("not implemented")
}
func (s *resolveMemberStub) GetMembership(_ context.Context, _ string, _ uint64) (*types.TenantMember, error) {
	return s.member, nil
}
func (s *resolveMemberStub) ListByUser(context.Context, string) ([]*types.TenantMember, error) {
	panic("not implemented")
}
func (s *resolveMemberStub) ListByTenant(context.Context, uint64) ([]*types.TenantMember, error) {
	panic("not implemented")
}
func (s *resolveMemberStub) ListMembersPage(context.Context, uint64, string, int, int) ([]*types.TenantMember, int64, error) {
	panic("not implemented")
}
func (s *resolveMemberStub) HasAnyMembers(context.Context, uint64) (bool, error) {
	panic("not implemented")
}
func (s *resolveMemberStub) UpdateRole(context.Context, string, uint64, types.TenantRole) error {
	panic("not implemented")
}
func (s *resolveMemberStub) UpdateAllowedAgentIDs(context.Context, uint64, string, types.AgentIDList) error {
	panic("not implemented")
}
func (s *resolveMemberStub) RemoveMember(context.Context, string, uint64) error {
	panic("not implemented")
}

func TestResolveAgent_DeniesOwnAgentOutsideMemberAllowlist(t *testing.T) {
	cases := []struct {
		name   string
		member *types.TenantMember
		wantOK bool
	}{
		{
			name:   "no membership row allows",
			member: nil,
			wantOK: true,
		},
		{
			name:   "null allowlist allows",
			member: &types.TenantMember{UserID: "user-1", TenantID: 7},
			wantOK: true,
		},
		{
			name: "allowlist containing the agent allows",
			member: &types.TenantMember{UserID: "user-1", TenantID: 7,
				AllowedAgentIDs: types.AgentIDList{"agent-1"}},
			wantOK: true,
		},
		{
			name: "allowlist without the agent denies",
			member: &types.TenantMember{UserID: "user-1", TenantID: 7,
				AllowedAgentIDs: types.AgentIDList{"other-agent"}},
			wantOK: false,
		},
		{
			name: "empty allowlist denies",
			member: &types.TenantMember{UserID: "user-1", TenantID: 7,
				AllowedAgentIDs: types.AgentIDList{}},
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, ctx := newResolveAgentTestContext(7)
			own := &types.CustomAgent{ID: "agent-1", TenantID: 7, Name: "Own"}
			h := &Handler{
				customAgentService: &resolveOwnAgentStub{agent: own},
				memberService:      &resolveMemberStub{member: tc.member},
			}
			agent, _, _ := h.resolveAgent(ctx, c, "agent-1", 0)
			if tc.wantOK {
				require.NotNil(t, agent)
			} else {
				require.Nil(t, agent)
			}
		})
	}
}

func TestResolveAgent_BuiltinAgentsBypassMemberAllowlist(t *testing.T) {
	c, ctx := newResolveAgentTestContext(7)
	builtin := &types.CustomAgent{ID: "builtin-smart-reasoning", TenantID: 7, IsBuiltin: true}
	h := &Handler{
		customAgentService: &resolveOwnAgentStub{agent: builtin},
		memberService: &resolveMemberStub{member: &types.TenantMember{
			UserID: "user-1", TenantID: 7, AllowedAgentIDs: types.AgentIDList{},
		}},
	}
	agent, _, _ := h.resolveAgent(ctx, c, "builtin-smart-reasoning", 0)
	require.NotNil(t, agent)
}
