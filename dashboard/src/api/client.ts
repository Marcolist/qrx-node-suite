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
    return localStorage.getItem(ADMIN_TOKEN_KEY) ?? '';
  } catch {
    return '';
  }
}

export function setAdminToken(token: string) {
  try {
    localStorage.setItem(ADMIN_TOKEN_KEY, token);
  } catch {
    // localStorage unavailable (private browsing, etc) -- admin actions
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

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers = new Headers(opts.headers);
  if (opts.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }
  const token = getAdminToken();
  if (token) {
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
    request<InstallResult>('/api/v1/updates/install', {
      method: 'POST',
      body: JSON.stringify({ component, ...opts }),
    }),
  updatesRollback: (component: string) =>
    request<InstallResult>('/api/v1/updates/rollback', { method: 'POST', body: JSON.stringify({ component }) }),
  activateVersion: (component: string, version: string, allowUnsupported = false) =>
    request<{ status: string; component: string }>(`/api/v1/components/${encodeURIComponent(component)}/activate-version`, {
      method: 'POST',
      body: JSON.stringify({ version, allow_unsupported: allowUnsupported }),
    }),
  qrxCoreSwitch: (version: string, profilePath: string, expertOverride = false) =>
    request<unknown>('/api/v1/qrx-core/switch', {
      method: 'POST',
      body: JSON.stringify({ version, profile_path: profilePath, expert_override: expertOverride }),
    }),
  restartService: () => request<{ status: string }>('/api/v1/services/qrx/restart', { method: 'POST' }),
};

export { ApiError };
