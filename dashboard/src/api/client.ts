import type {
  Alert,
  BlockProducerStatus,
  InstallResult,
  MempoolStatus,
  NetworkStatus,
  NodeStatus,
  NonceLanes,
  RecentBlock,
  RecentTransaction,
  ServiceStatus,
  StatusResponse,
  SystemStatus,
  UpdateCheckResult,
  ValidatorStatus,
  VelocityStatus,
  VersionInfo,
} from './types';

const ADMIN_TOKEN_KEY = 'qrx_admin_token';

export function getAdminToken(): string {
  try {
    // v0.1.1 stored this credential persistently. Remove any stranded
    // legacy copy instead of silently carrying that exposure forward.
    localStorage.removeItem(ADMIN_TOKEN_KEY);
  } catch {
    // A browser policy may disable persistent storage independently.
  }
  try {
    return sessionStorage.getItem(ADMIN_TOKEN_KEY) ?? '';
  } catch {
    return '';
  }
}

export function setAdminToken(token: string) {
  try {
    localStorage.removeItem(ADMIN_TOKEN_KEY);
  } catch {
    // Best-effort cleanup of the v0.1.1 persistent credential.
  }
  try {
    if (token) sessionStorage.setItem(ADMIN_TOKEN_KEY, token);
    else sessionStorage.removeItem(ADMIN_TOKEN_KEY);
  } catch {
    // sessionStorage unavailable (private browsing, etc) -- admin actions
    // will just fail with a clear 401/403 from the Agent instead.
  }
}

class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(
  path: string,
  opts: RequestInit = {},
  admin = false,
  tokenOverride?: string,
): Promise<T> {
  const headers = new Headers(opts.headers);
  if (opts.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }
  const token = tokenOverride ?? getAdminToken();
  if (admin && token) {
    headers.set('Authorization', `Bearer ${token}`);
  }
  const res = await fetch(path, { ...opts, headers });
  if (!res.ok) {
    let message = res.statusText;
    try {
      const body = await res.json();
      if (body && typeof body.error === 'string') message = body.error;
    } catch {
      // non-JSON error body -- keep statusText
    }
    throw new ApiError(res.status, message);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  verifyAdmin: (token: string) =>
    request<{ authenticated: boolean }>('/api/v1/admin/session', {}, true, token),
  health: () => request<{ status: string }>('/health'),
  version: () => request<VersionInfo>('/api/v1/version'),
  versions: () => request<UpdateCheckResult[]>('/api/v1/versions'),
  status: () => request<StatusResponse>('/api/v1/status'),
  node: () => request<NodeStatus>('/api/v1/node'),
  network: () => request<NetworkStatus>('/api/v1/network'),
  mempool: () => request<MempoolStatus>('/api/v1/mempool'),
  validator: () =>
    request<{ validator: ValidatorStatus; block_producer: BlockProducerStatus }>('/api/v1/validator'),
  velocity: () => request<{ velocity: VelocityStatus; nonce_lanes: NonceLanes }>('/api/v1/velocity'),
  system: () => request<SystemStatus>('/api/v1/system'),
  activity: () =>
    request<{ recent_blocks: RecentBlock[]; recent_transactions: RecentTransaction[] }>('/api/v1/activity'),
  alerts: (unresolvedOnly = false) =>
    request<Alert[]>(`/api/v1/alerts${unresolvedOnly ? '?unresolved=true' : ''}`),
  services: () => request<ServiceStatus[]>('/api/v1/services'),
  settings: () => request<Record<string, string>>('/api/v1/settings'),

  updatesStatus: () => request<Record<string, UpdateCheckResult>>('/api/v1/updates'),
  updatesCheck: (component: string) =>
    request<UpdateCheckResult>('/api/v1/updates/check', { method: 'POST', body: JSON.stringify({ component }) }),
  updatesPlan: (components: string[] = []) =>
    request<UpdateCheckResult[]>('/api/v1/updates/plan', { method: 'POST', body: JSON.stringify({ components }) }),
  updatesInstall: (component: string, opts: { channel?: string; allow_downgrade?: boolean; force?: boolean } = {}) =>
    request<InstallResult>(
      '/api/v1/updates/install',
      { method: 'POST', body: JSON.stringify({ component, ...opts }) },
      true,
    ),
  updatesRollback: (component: string) =>
    request<InstallResult>('/api/v1/updates/rollback', { method: 'POST', body: JSON.stringify({ component }) }, true),
  activateVersion: (component: string, version: string, allowUnsupported = false) =>
    request<{ status: string; component: string }>(
      `/api/v1/components/${encodeURIComponent(component)}/activate-version`,
      { method: 'POST', body: JSON.stringify({ version, allow_unsupported: allowUnsupported }) },
      true,
    ),
  qrxCoreSwitch: (version: string, profilePath: string, expertOverride = false) =>
    request<unknown>('/api/v1/qrx-core/switch', {
      method: 'POST',
      body: JSON.stringify({ version, profile_path: profilePath, expert_override: expertOverride }),
    }, true),
  restartService: () => request<{ status: string }>('/api/v1/services/qrx/restart', { method: 'POST' }, true),
};

export { ApiError };
