// Tipos que espelham o contrato IPC do daemon (internal/ipc). Mantenha em
// sincronia com os structs Go: Request/Response, policy.Block, preset.Preset,
// pomodoro.State, analytics.Stats, schedule.Rule e tamper.Event.

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

export interface LabelStat {
  label: string;
  duration: number; // nanosegundos
  sessions: number;
}

// FocusSession espelha analytics.Session — uma sessão de foco concluída.
export interface FocusSession {
  start: string; // RFC3339
  end: string; // RFC3339
  preset: string;
  label?: string;
  domains: string[];
  work_min: number;
  rest_min: number;
  cycles: number;
  focus: number; // nanosegundos
  strict: boolean;
}

export interface ScheduleRule {
  id: string;
  label?: string;
  preset: string;
  days: number[]; // 0 = domingo
  start: string; // "HH:MM"
  end: string; // "HH:MM"
  windows?: string[]; // ["HH:MM-HH:MM", ...]
  enabled: boolean;
}

export interface TamperEvent {
  at: string; // RFC3339
  source: "hosts" | "state";
  action: "restore" | "reconcile";
  detail?: string;
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
  work_min?: number;
  rest_min?: number;
  cycles?: number;
  strict?: boolean;
  save?: boolean;
  channel?: string;
  preset_name?: string;
  preset_label?: string;
  preset_domains?: string[];
  schedule_rule?: ScheduleRule;
  schedule_id?: string;
  ics_content?: string;
  ics_preset?: string;
  extend?: boolean;
  replace?: boolean;
  // Usuários (user-add / user-remove / user-set-password)
  user_name?: string;
  user_password?: string;
  // Upstream DNS (dns-set-upstream)
  upstream?: string;
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
  // Fallback move-on-reboot: a troca dos binários foi agendada para o próximo
  // reinício (binário em uso travado no Windows) — o daemon segue na versão
  // antiga até lá.
  update_pending_reboot?: boolean;
  stats?: Stats;
  apps?: string[];
  doh_active?: boolean;
  expected_doh?: boolean;
  firewall_rules?: number;
  protection_error?: string;
  schedules?: ScheduleRule[];
  tamper_log?: TamperEvent[];
  label_stats?: LabelStat[];
  sessions?: FocusSession[];
  pomodoro_work?: number;
  pomodoro_rest?: number;
  pomodoro_cycles?: number;
  conflict?: boolean;
  conflict_block?: Block;
  // Servidor DNS sinkhole ("Rei da Rede"): espelha dnsserver.Status + o flag
  // persistido dns_enabled (vem no status quando o daemon tem o controller).
  dns_enabled?: boolean;
  dns_listening?: boolean;
  dns_addr?: string;
  dns_upstream?: string;
  dns_queries?: number;
  dns_blocked?: number;
  dns_bind_error?: string;
  // Usuários da interface web (user-list) — nomes apenas, nunca hashes.
  users?: string[];
}
