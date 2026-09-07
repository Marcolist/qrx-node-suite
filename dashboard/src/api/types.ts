// Mirrors agent/models' JSON shapes (agent/models/availability.go etc).
// Keep in sync with the Go structs by hand -- see contracts/json-schema for
// the machine-checkable version of the same contract.

export type AvailabilityState = 'available' | 'unavailable' | 'unsupported' | 'error';

export interface Value<T> {
  state: AvailabilityState;
  value?: T;
  reason?: string;
}

export function isAvailable<T>(v: Value<T> | undefined): v is Value<T> & { value: T } {
  return !!v && v.state === 'available' && v.value !== undefined;
}

export interface SyncStatus {
  syncing: boolean;
  current_height: number;
  target_height?: number;
  progress_percent?: number;
}

export interface PeerSummary {
  connected: number;
  inbound: number;
  outbound: number;
}

export interface NodeStatus {
  online: boolean;
  network: Value<string>;
  height: Value<number>;
  finalized_height: Value<number>;
  sync: Value<SyncStatus>;
  peers: Value<PeerSummary>;
  mempool_tx_count: Value<number>;
  qrx_version: Value<string>;
  node_uptime_seconds: Value<number>;
  last_successful_poll?: string;
  last_error?: string;
  simulation_mode: boolean;
  adapter_name: string;
}

export interface NetworkStatus {
  network: Value<string>;
  peer_count: Value<number>;
  protocol_version: Value<string>;
  listen_addresses?: Value<string[]>;
}

export interface MempoolStatus {
  tx_count: Value<number>;
  size_bytes: Value<number>;
}

export interface ValidatorStatus {
  active: Value<boolean>;
  address: Value<string>;
  stake: Value<string>;
  delegated_stake: Value<string>;
  voting_weight: Value<string>;
}

export interface BlockProducerStatus {
  producer_status: Value<string>;
  blocks_produced: Value<number>;
  missed_blocks: Value<number>;
}

export interface VelocityStatus {
  engine_status: Value<string>;
  transaction_version: Value<string>;
  scheduler_version: Value<string>;
  parallel_width: Value<number>;
  execution_waves: Value<number>;
  conflicts: Value<number>;
  selective_retries: Value<number>;
}

export interface Lane {
  index: number;
  next_nonce: string;
  in_flight: number;
}

export interface NonceLanes {
  lane_count: Value<number>;
  lanes: Value<Lane[]>;
}

export interface PlatformInfo {
  os: string;
  arch: string;
  model?: string;
  is_raspberry_pi: boolean;
}

export interface SystemStatus {
  cpu_usage_percent: Value<number>;
  cpu_core_count: Value<number>;
  load_average_1: Value<number>;
  load_average_5: Value<number>;
  load_average_15: Value<number>;
  memory_total_bytes: Value<number>;
  memory_used_bytes: Value<number>;
  disk_total_bytes: Value<number>;
  disk_used_bytes: Value<number>;
  disk_free_bytes: Value<number>;
  network_rx_bytes: Value<number>;
  network_tx_bytes: Value<number>;
  system_uptime_seconds: Value<number>;
  process_uptime_seconds: Value<number>;
  qrx_process_cpu_percent: Value<number>;
  qrx_process_memory_bytes: Value<number>;
  temperature_celsius: Value<number>;
  throttled: Value<boolean>;
  platform: PlatformInfo;
}

export interface RecentBlock {
  height: number;
  hash: string;
  timestamp: number;
  tx_count?: number;
  producer?: string;
}

export interface RecentTransaction {
  hash: string;
  height?: number;
  timestamp: number;
  from?: string;
  to?: string;
  value?: string;
}

export type AlertSeverity = 'info' | 'warning' | 'critical';

export interface Alert {
  id: number;
  rule_id: string;
  severity: AlertSeverity;
  title: string;
  message: string;
  created_at: string;
  resolved_at?: string;
  resolved: boolean;
}

export type ServiceState = 'running' | 'stopped' | 'failed' | 'unknown';

export interface ServiceStatus {
  name: string;
  state: ServiceState;
  pid?: number;
  restart_count: number;
  last_restart_at?: string;
}

export type HealthState = 'HEALTHY' | 'DEGRADED' | 'UNHEALTHY' | 'OFFLINE';

export interface StatusResponse {
  health: HealthState;
  node: NodeStatus;
  system: SystemStatus;
  adapter_name: string;
  updated_at: string;
}

export interface VersionInfo {
  suite_version: string;
  agent_version: string;
  dashboard_version: string;
  adapter_name: string;
  adapter_version: string;
  qrx_core_version: string;
  api_version: string;
  telemetry_protocol_version: number;
  config_schema_version: number;
}

// Mirrors agent/updates.CheckResult (agent/updates/manager.go).
export interface UpdateCheckResult {
  component: string;
  channel: string;
  current?: string;
  latest?: string;
  update_available: boolean;
  pinned: boolean;
  compatible: boolean;
  blocked: boolean;
  block_reason?: string;
  restart_required: boolean;
  rollback_available: boolean;
  check_error?: string;
}

// Mirrors agent/storage.UpdateHistoryRecord.
export interface UpdateHistoryRecord {
  id: number;
  component: string;
  from_version: string;
  to_version: string;
  channel: string;
  timestamp: string;
  status: 'started' | 'succeeded' | 'failed' | 'rolled_back' | 'blocked';
  rollback_used: boolean;
  checksum: string;
  manifest_version: number;
  error_message?: string;
}

export interface InstallResult {
  History: UpdateHistoryRecord;
  PendingRestart: boolean;
}
