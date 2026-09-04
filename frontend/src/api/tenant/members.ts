import { get, post, put, del } from '@/utils/request'

// TenantRole mirrors internal/types/tenant_member.go's four-role enum.
// Keep the string values aligned with the Go constants.
export type TenantRole = 'owner' | 'admin' | 'contributor' | 'viewer'

export type TenantMemberStatus = 'active' | 'invited' | 'suspended'

// TenantMember is the API projection of a (user, tenant) membership row,
// already joined with the user's email/username/avatar by the backend.
export interface TenantMember {
  user_id: string
  email: string
  username: string
  /** 姓名：展示优先于用户名；企业微信等 SSO 账号可能为空待补。 */
  name?: string
  avatar?: string
  role: TenantRole
  status: TenantMemberStatus
  invited_by?: string | null
  joined_at: string
  /**
   * 成员可用的本空间自定义智能体 ID 列表。
   * null = 未限制（全部可用）；数组（含空数组）= 仅列表内可用。
   * 内置与共享智能体不受此限制影响。
   */
  allowed_agent_ids?: string[] | null
}

export interface ListMembersResponse {
  success: boolean
  data?: {
    members: TenantMember[]
    total: number
    page?: number
    page_size?: number
  }
  message?: string
}

export interface ListMembersParams {
  page?: number
  page_size?: number
  /** 按邮箱/用户名筛选（服务端模糊匹配） */
  q?: string
}

function buildMembersQuery(params: ListMembersParams | undefined): string {
  if (!params) return ''
  const u = new URLSearchParams()
  if (params.page != null && params.page > 0) u.set('page', String(params.page))
  if (params.page_size != null && params.page_size > 0) u.set('page_size', String(params.page_size))
  const q = params.q?.trim()
  if (q) u.set('q', q)
  const qs = u.toString()
  return qs ? `?${qs}` : ''
}

export interface AddMemberRequest {
  email: string
  role: TenantRole
}

export interface AddMemberResponse {
  success: boolean
  data?: TenantMember
  message?: string
}

export interface CreateMemberRequest {
  email: string
  username: string
  name: string
  password: string
  role: TenantRole
}

export interface SimpleResponse {
  success: boolean
  message?: string
}

/**
 * 分页列出空间成员。
 * Backend: GET /api/v1/tenants/:id/members (Viewer+)。查询参数：`q`、`page`、`page_size`。
 */
export async function listMembers(
  tenantId: number,
  params: ListMembersParams = {},
): Promise<ListMembersResponse> {
  const qs = buildMembersQuery(params)
  return (await get(
    `/api/v1/tenants/${tenantId}/members${qs}`,
  )) as unknown as ListMembersResponse
}

/**
 * 遍历分页拉取空间的全部成员（每页最大 100，最多 500 页兜底）。
 * 用于「退出空间」等对全量成员的轻量校验；普通表格请直接使用 {@link listMembers} 分页接口。
 */
export async function fetchAllTenantMembers(tenantId: number): Promise<TenantMember[]> {
  const pageSize = 100
  let page = 1
  const out: TenantMember[] = []
  let total = Number.POSITIVE_INFINITY
  for (let guard = 0; guard < 500 && out.length < total; guard++) {
    const resp = await listMembers(tenantId, { page, page_size: pageSize })
    if (!resp.success || !resp.data) break
    total = resp.data.total
    const batch = resp.data.members || []
    if (batch.length === 0 && page >= 2) break
    out.push(...batch)
    if (batch.length < pageSize) break
    page++
  }
  return out
}

/**
 * Invite an existing user (by email) to the tenant with the given role.
 * Backend: POST /api/v1/tenants/:id/members (Owner+).
 *
 * Returns 404 when the email does not match any registered user — the
 * caller should ask the invitee to register first. PR 3 does not yet
 * support email-based invites for users who don't have an account.
 */
export async function addMember(
  tenantId: number,
  body: AddMemberRequest,
): Promise<AddMemberResponse> {
  return (await post(`/api/v1/tenants/${tenantId}/members`, body)) as unknown as AddMemberResponse
}

/**
 * 直接新增成员：为未注册邮箱创建账号（tenantless）并以指定角色加入空间。
 * Backend: POST /api/v1/tenants/:id/members/create (Owner+)。
 *
 * 邮箱已注册时返回 409（改用 addMember 或邀请）；密码须满足 8-32 位
 * 含字母和数字的强度策略。
 */
export async function createMember(
  tenantId: number,
  body: CreateMemberRequest,
): Promise<AddMemberResponse> {
  return (await post(
    `/api/v1/tenants/${tenantId}/members/create`,
    body,
  )) as unknown as AddMemberResponse
}

/**
 * 维护成员姓名（Owner+）。企业微信等 SSO 账号自动取不到姓名时手工补充；
 * 用户管理与使用日报展示均以姓名为准。
 * Backend: PUT /tenants/:id/members/:user_id/profile
 */
export async function updateMemberProfile(
  tenantId: number,
  userId: string,
  name: string,
): Promise<SimpleResponse> {
  return (await put(`/api/v1/tenants/${tenantId}/members/${userId}/profile`, { name })) as unknown as SimpleResponse
}

/**
 * Change an existing member's role.
 * Backend: PUT /api/v1/tenants/:id/members/:user_id (Owner+).
 *
 * Returns 409 when this would demote the last active Owner of the tenant.
 */
export async function updateMemberRole(
  tenantId: number,
  userId: string,
  role: TenantRole,
): Promise<SimpleResponse> {
  return (await put(`/api/v1/tenants/${tenantId}/members/${userId}`, { role })) as unknown as SimpleResponse
}

/**
 * 设置成员在对话中可访问的本空间自定义智能体。
 * Backend: PUT /api/v1/tenants/:id/members/:user_id/agent-access (Owner+)。
 *
 * allowedAgentIds 传 null 表示清除限制（全部可用，写库为 SQL NULL）；
 * 传数组（可为空）表示仅这些智能体可用。内置与共享智能体不受影响。
 */
export async function updateMemberAgentAccess(
  tenantId: number,
  userId: string,
  allowedAgentIds: string[] | null,
): Promise<SimpleResponse> {
  return (await put(
    `/api/v1/tenants/${tenantId}/members/${userId}/agent-access`,
    { allowed_agent_ids: allowedAgentIds },
  )) as unknown as SimpleResponse
}

/**
 * 新成员默认可访问的智能体（租户级）。新加入的非 Owner 成员会在加入时
 * 复制该列表作为自己的 allowed_agent_ids；已存在成员不受影响。
 * Backend: GET /api/v1/tenants/kv/member-agent-defaults (Viewer+)。
 */
export async function getMemberAgentDefaults(): Promise<string[] | null> {
  const resp = (await get('/api/v1/tenants/kv/member-agent-defaults')) as unknown as {
    data?: { default_member_agent_ids?: string[] | null }
  }
  return resp?.data?.default_member_agent_ids ?? null
}

/**
 * 更新新成员默认可访问的智能体。
 * Backend: PUT /api/v1/tenants/kv/member-agent-defaults (Admin+)。
 *
 * ids 传 null 清除默认（新成员不限制）；传非空数组指定默认列表。
 */
export async function updateMemberAgentDefaults(ids: string[] | null): Promise<SimpleResponse> {
  return (await put(
    '/api/v1/tenants/kv/member-agent-defaults',
    { default_member_agent_ids: ids },
  )) as unknown as SimpleResponse
}

/**
 * Remove a member from the tenant.
 * Backend: DELETE /api/v1/tenants/:id/members/:user_id (Owner+).
 *
 * Returns 409 when this would remove the last active Owner.
 */
export async function removeMember(
  tenantId: number,
  userId: string,
): Promise<SimpleResponse> {
  return (await del(`/api/v1/tenants/${tenantId}/members/${userId}`)) as unknown as SimpleResponse
}

/**
 * Quit the tenant on your own. Same last-Owner invariant as
 * removeMember, but does NOT require Owner+ — any active member can
 * call it.
 * Backend: POST /api/v1/tenants/:id/leave (Viewer+).
 */
export async function leaveTenant(tenantId: number): Promise<SimpleResponse> {
  return (await post(`/api/v1/tenants/${tenantId}/leave`)) as unknown as SimpleResponse
}
