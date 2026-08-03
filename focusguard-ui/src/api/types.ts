// Tipos que espelham o contrato IPC do daemon (internal/ipc). Mantenha em
// sincronia com os structs Go: Request/Response, policy.Block, preset.Preset,
// pomodoro.State e analytics.Stats.

export interface Block {
  domain: string;
  started_at: string; // RFC3339
  expires_at: string; // RFC3339
  resolved_ips?: string[];
}

export interface Preset {
  name: string;
  label: string;
  domains: string[];
}

export interface PomodoroState {
  active: boolean;
  preset?: string;
  phase?: "work" | "rest";
  cycle?: number;
  cycles?: number;
  started_at?: string;
  phase_until?: string;
}

export interface DayStat {
  day: string;
  duration: number; // nanosegundos
  sessions: number;
}

export interface DomainStat {
  domain: string;
  duration: number; // nanosegundos
}

export interface Stats {
  total_sessions: number;
  total_focus: number; // nanosegundos
  per_day: DayStat[];
  per_domain: DomainStat[];
  streak: number;
}

export interface ApiRequest {
  action: string;
  domain?: string;
  duration?: string;
  preset?: string;
  allowlist?: string[];
  goal_minutes?: number;
  app_name?: string;
  mission?: string;
  label?: string;
}

export interface ApiResponse {
  success: boolean;
  message?: string;
  blocks?: Block[];
  presets?: Preset[];
  pomodoro?: PomodoroState;
  goal?: number; // nanosegundos
  current_version?: string;
  update_available?: boolean;
  update_version?: string;
  stats?: Stats;
  apps?: string[];
  doh_active?: boolean;
  expected_doh?: boolean;
  firewall_rules?: number;
  protection_error?: string;
}
