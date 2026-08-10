package ipc_test

// ---------------------------------------------------------------------------
// Wiring test (package ipc_test, externo): compõe os handlers REAIS dos
// pacotes de domínio com o roteador do ipc.Server, exatamente como o daemon
// os registra no composition root (Fase 5). Fecha o gap de drift: os testes
// internos do ipc (package ipc) usam os adapters de referência
// (handlers_ref_test.go), e este exercita o código de PRODUÇÃO dos pacotes de
// domínio contra a superfície do wire — incluindo o fechamento specs↔registry
// que o daemon verifica no boot (ValidateRegistry).
// ---------------------------------------------------------------------------

import (
	"context"
	"strings"
	"testing"
	"time"

	"focusguard/internal/domain/analytics"
	"focusguard/internal/domain/apps"
	"focusguard/internal/domain/blocks"
	"focusguard/internal/domain/goal"
	"focusguard/internal/domain/policy"
	"focusguard/internal/domain/pomodoro"
	"focusguard/internal/domain/preset"
	"focusguard/internal/domain/presets"
	"focusguard/internal/domain/schedule"
	"focusguard/internal/domain/user"
	"focusguard/internal/domain/users"
	"focusguard/internal/infrastructure/dns"
	"focusguard/internal/infrastructure/dnsserver"
	"focusguard/internal/infrastructure/update"
	"focusguard/internal/transport/ipc"
)

type fakeBlocker struct {
	block  *policy.Block
	active *policy.Block
	blocks []policy.Block
	err    error
}

func (f *fakeBlocker) Block(domain string, d time.Duration) (*policy.Block, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.block, nil
}

func (f *fakeBlocker) BlockDomains(domains []string, d time.Duration) ([]policy.Block, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.blocks, nil
}

func (f *fakeBlocker) ExtendBlock(domain string, d time.Duration) (*policy.Block, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.block, nil
}

func (f *fakeBlocker) ActiveBlock(domain string) *policy.Block { return f.active }

func (f *fakeBlocker) BlockAllInternet(allowlist []string, d time.Duration) (*policy.Block, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.block, nil
}

type fakeDNSController struct {
	started  bool
	upstream string
	queries  uint64
	err      error
}

func (f *fakeDNSController) Start() error {
	if f.err != nil {
		return f.err
	}
	f.started = true
	return nil
}

func (f *fakeDNSController) Stop() error {
	f.started = false
	return nil
}

func (f *fakeDNSController) SetUpstream(u string) error {
	if f.err != nil {
		return f.err
	}
	f.upstream = u
	return nil
}

func (f *fakeDNSController) Status() dnsserver.Status {
	return dnsserver.Status{Listening: f.started, Upstream: f.upstream, Queries: f.queries}
}

// mergeDNSWire projeta o Status de domínio do DNS no wire (mesmo helper do
// composition root).
func mergeDNSWire(resp *ipc.Response, st dns.Status) {
	resp.DNSEnabled = st.Enabled
	resp.DNSListening = st.Listening
	resp.DNSAddr = st.Addr
	resp.DNSUpstream = st.Upstream
	resp.DNSQueries = st.Queries
	resp.DNSBlocked = st.Blocked
	resp.DNSBindError = st.BindError
}

type fakeDNSPersister struct {
	enabled  bool
	upstream string
	err      error
}

func (f *fakeDNSPersister) SetDNSEnabled(v bool) error {
	if f.err != nil {
		return f.err
	}
	f.enabled = v
	return nil
}

func (f *fakeDNSPersister) SetDNSUpstream(u string) error {
	if f.err != nil {
		return f.err
	}
	f.upstream = u
	return nil
}

func (f *fakeDNSPersister) DNSEnabled() bool { return f.enabled }

// fakeSessionsProvider é um analytics.Provider de teste (uma sessão pronta
// para as ações stats/missions/sessions).
type fakeSessionsProvider struct{ sessions []analytics.Session }

func (f *fakeSessionsProvider) Sessions() ([]analytics.Session, error) { return f.sessions, nil }

// fakeScheduleStore é um schedule.RuleStore de teste (lista em memória).
type fakeScheduleStore struct {
	rules []schedule.Rule
	err   error
}

func (f *fakeScheduleStore) List() []schedule.Rule { return f.rules }

func (f *fakeScheduleStore) Add(r schedule.Rule) (schedule.Rule, error) {
	if f.err != nil {
		return schedule.Rule{}, f.err
	}
	f.rules = append(f.rules, r)
	return r, nil
}

func (f *fakeScheduleStore) Remove(id string) error {
	if f.err != nil {
		return f.err
	}
	for i, r := range f.rules {
		if r.ID == id {
			f.rules = append(f.rules[:i], f.rules[i+1:]...)
			return nil
		}
	}
	return nil
}

func (f *fakeScheduleStore) ImportICS(_ []byte, _ string) ([]schedule.Rule, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []schedule.Rule{{ID: "ics-1"}}, nil
}

// fakePomodoroRunner é um pomodoro.Runner de teste (estado fixo).
type fakePomodoroRunner struct{ st pomodoro.State }

func (f *fakePomodoroRunner) Start(_ pomodoro.Session) (pomodoro.State, error) { return f.st, nil }
func (f *fakePomodoroRunner) Stop() (pomodoro.State, error)                    { return f.st, nil }

// fakePomodoroPrefs é um pomodoro.PrefsStore de teste com defaults clássicos
// 25/5/4.
type fakePomodoroPrefs struct{ work, rest, cycles int }

func newFakePomodoroPrefs() *fakePomodoroPrefs {
	return &fakePomodoroPrefs{work: 25, rest: 5, cycles: 4}
}

func (f *fakePomodoroPrefs) Resolve(work, rest, cycles int) (int, int, int) {
	if work == 0 {
		work = f.work
	}
	if rest == -1 {
		rest = f.rest
	}
	if cycles == 0 {
		cycles = f.cycles
	}
	return work, rest, cycles
}

func (f *fakePomodoroPrefs) Remember(work, rest, cycles int) {
	f.work, f.rest, f.cycles = work, rest, cycles
}

// fakeWireUpdateChecker é um ipc.UpdateChecker de teste (status fixo no wire).
type fakeWireUpdateChecker struct{ st ipc.UpdateStatus }

func (f *fakeWireUpdateChecker) Check(_ context.Context, _ bool, _ string) (ipc.UpdateStatus, error) {
	return f.st, nil
}

// updateCheckerBridge — espelho do composition root (pós-reorg item 1): adapta
// o ipc.UpdateChecker do wire ao update.Checker do domínio.
type updateCheckerBridge struct{ c ipc.UpdateChecker }

func (b updateCheckerBridge) Check(ctx context.Context, apply bool, channel string) (update.Status, error) {
	st, err := b.c.Check(ctx, apply, channel)
	return update.Status{
		CurrentVersion: st.CurrentVersion,
		NewVersion:     st.NewVersion,
		Available:      st.Available,
		Applied:        st.Applied,
		PendingReboot:  st.PendingReboot,
	}, err
}

// composeTestServer mounts todos os 31 handlers de domínio (como o daemon faz)
// sobre o NewServer (que registra os de nível servidor) — o conjunto completo
// que o ValidateRegistry exige no boot (34 + 12 ações do item 1).
func composeTestServer(t *testing.T) (*ipc.Server, *fakeBlocker, *fakeDNSPersister) {
	t.Helper()
	s := ipc.NewServer(nil)

	cat := preset.NewStore(t.TempDir() + "/presets.json")
	goalStore := goal.NewStore(t.TempDir() + "/goal.json")
	userStore := user.NewStore(t.TempDir() + "/user.json")
	appsStore := apps.NewStore(t.TempDir() + "/apps.json")
	blk := &fakeBlocker{}
	dc := &fakeDNSController{upstream: dnsserver.DefaultUpstream, queries: 7}
	dp := &fakeDNSPersister{}

	// blocks via ipc.DomainAction (mesmo padrão do composition root).
	hBlock := blocks.New(blk, cat)
	s.Register(ipc.DomainAction[blocks.BlockInput, blocks.BlockResult]{
		Name: hBlock.Action(),
		Decode: func(r *ipc.Request) (*blocks.BlockInput, error) {
			return &blocks.BlockInput{Domain: r.Domain, Duration: r.Duration, Preset: r.Preset, Extend: r.Extend, Replace: r.Replace}, nil
		},
		Validate: hBlock.Validate,
		Handle:   hBlock.Handle,
		Encode: func(out *blocks.BlockResult) (*ipc.Response, error) {
			// Success deriva de Code (invariante do domínio: conflito implica
			// código estável) — nunca Success:true + Conflict:true no wire.
			resp := &ipc.Response{Success: out.Code == "", Message: out.Message, Conflict: out.Conflict, ConflictBlock: out.ConflictBlock}
			if out.Code != "" {
				resp.Code = out.Code
			}
			return resp, nil
		},
	}.Handler())
	hBlockAll := blocks.NewBlockAll(blk)
	s.Register(ipc.DomainAction[blocks.BlockAllInput, blocks.BlockAllResult]{
		Name: hBlockAll.Action(),
		Decode: func(r *ipc.Request) (*blocks.BlockAllInput, error) {
			return &blocks.BlockAllInput{Duration: r.Duration, Allowlist: r.Allowlist}, nil
		},
		Validate: hBlockAll.Validate,
		Handle:   hBlockAll.Handle,
		Encode: func(out *blocks.BlockAllResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	// presets/goal via ipc.DomainAction (mesmo padrão do composition root).
	hPresetsList := presets.NewList(cat)
	s.Register(ipc.DomainAction[presets.NoInput, presets.ListResult]{
		Name:   hPresetsList.Action(),
		Decode: ipc.NoInputDecode[presets.NoInput](),
		Handle: hPresetsList.Handle,
		Encode: func(out *presets.ListResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Presets: out.Presets}, nil
		},
	}.Handler())
	hPresetsAdd := presets.NewAdd(cat)
	s.Register(ipc.DomainAction[presets.AddInput, presets.AddResult]{
		Name: hPresetsAdd.Action(),
		Decode: func(r *ipc.Request) (*presets.AddInput, error) {
			return &presets.AddInput{PresetName: r.PresetName, PresetLabel: r.PresetLabel, PresetDomains: r.PresetDomains}, nil
		},
		Handle: hPresetsAdd.Handle,
		Encode: func(out *presets.AddResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	hPresetsRemove := presets.NewRemove(cat)
	s.Register(ipc.DomainAction[presets.RemoveInput, presets.RemoveResult]{
		Name: hPresetsRemove.Action(),
		Decode: func(r *ipc.Request) (*presets.RemoveInput, error) {
			return &presets.RemoveInput{PresetName: r.PresetName}, nil
		},
		Handle: hPresetsRemove.Handle,
		Encode: func(out *presets.RemoveResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	hGoalGet := goal.NewGet(goalStore)
	s.Register(ipc.DomainAction[goal.NoInput, goal.GetResult]{
		Name:   hGoalGet.Action(),
		Decode: ipc.NoInputDecode[goal.NoInput](),
		Handle: hGoalGet.Handle,
		Encode: func(out *goal.GetResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Goal: out.Goal}, nil
		},
	}.Handler())
	hGoalSet := goal.NewSet(goalStore)
	s.Register(ipc.DomainAction[goal.SetInput, goal.SetResult]{
		Name:   hGoalSet.Action(),
		Decode: func(r *ipc.Request) (*goal.SetInput, error) { return &goal.SetInput{GoalMinutes: r.GoalMinutes}, nil },
		Handle: hGoalSet.Handle,
		Encode: func(out *goal.SetResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Goal: out.Goal, Message: out.Message}, nil
		},
	}.Handler())
	// dns via ipc.DomainAction (mesmo padrão do composition root).
	hDNSStart := dns.NewStart(dc, dp, nil)
	s.Register(ipc.DomainAction[dns.NoInput, dns.StartResult]{
		Name:   hDNSStart.Action(),
		Decode: ipc.NoInputDecode[dns.NoInput](),
		Handle: hDNSStart.Handle,
		Encode: func(out *dns.StartResult) (*ipc.Response, error) {
			resp := &ipc.Response{Success: true, Message: out.Message}
			mergeDNSWire(resp, out.Status)
			return resp, nil
		},
	}.Handler())
	hDNSStop := dns.NewStop(dc, dp)
	s.Register(ipc.DomainAction[dns.NoInput, dns.StopResult]{
		Name:   hDNSStop.Action(),
		Decode: ipc.NoInputDecode[dns.NoInput](),
		Handle: hDNSStop.Handle,
		Encode: func(out *dns.StopResult) (*ipc.Response, error) {
			resp := &ipc.Response{Success: true, Message: out.Message}
			mergeDNSWire(resp, out.Status)
			return resp, nil
		},
	}.Handler())
	hDNSStatus := dns.NewStatus(dc, dp)
	s.Register(ipc.DomainAction[dns.NoInput, dns.StatusResult]{
		Name:   hDNSStatus.Action(),
		Decode: ipc.NoInputDecode[dns.NoInput](),
		Handle: hDNSStatus.Handle,
		Encode: func(out *dns.StatusResult) (*ipc.Response, error) {
			resp := &ipc.Response{Success: true}
			mergeDNSWire(resp, out.Status)
			return resp, nil
		},
	}.Handler())
	hDNSSetUpstream := dns.NewSetUpstream(dc, dp)
	s.Register(ipc.DomainAction[dns.SetUpstreamInput, dns.SetUpstreamResult]{
		Name: hDNSSetUpstream.Action(),
		Decode: func(r *ipc.Request) (*dns.SetUpstreamInput, error) {
			return &dns.SetUpstreamInput{Upstream: r.Upstream}, nil
		},
		Handle: hDNSSetUpstream.Handle,
		Encode: func(out *dns.SetUpstreamResult) (*ipc.Response, error) {
			resp := &ipc.Response{Success: true, Message: out.Message}
			mergeDNSWire(resp, out.Status)
			return resp, nil
		},
	}.Handler())
	// users via ipc.DomainAction (mesmo padrão do composition root).
	hUsersList := users.NewList(userStore)
	s.Register(ipc.DomainAction[users.NoInput, users.ListResult]{
		Name:   hUsersList.Action(),
		Decode: ipc.NoInputDecode[users.NoInput](),
		Handle: hUsersList.Handle,
		Encode: func(out *users.ListResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Users: out.Users}, nil
		},
	}.Handler())
	hUsersVerify := users.NewVerify(userStore)
	s.Register(ipc.DomainAction[users.VerifyInput, users.VerifyResult]{
		Name: hUsersVerify.Action(),
		Decode: func(r *ipc.Request) (*users.VerifyInput, error) {
			return &users.VerifyInput{UserName: r.UserName, UserPassword: r.UserPassword}, nil
		},
		Handle: hUsersVerify.Handle,
		Encode: func(out *users.VerifyResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, UserIsAdmin: out.UserIsAdmin}, nil
		},
	}.Handler())
	hUsersAdd := users.NewAdd(userStore)
	s.Register(ipc.DomainAction[users.AddInput, users.AddResult]{
		Name: hUsersAdd.Action(),
		Decode: func(r *ipc.Request) (*users.AddInput, error) {
			return &users.AddInput{UserName: r.UserName, UserPassword: r.UserPassword}, nil
		},
		Handle: hUsersAdd.Handle,
		Encode: func(out *users.AddResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	hUsersRemove := users.NewRemove(userStore)
	s.Register(ipc.DomainAction[users.RemoveInput, users.RemoveResult]{
		Name:   hUsersRemove.Action(),
		Decode: func(r *ipc.Request) (*users.RemoveInput, error) { return &users.RemoveInput{UserName: r.UserName}, nil },
		Handle: hUsersRemove.Handle,
		Encode: func(out *users.RemoveResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	hUsersSetPassword := users.NewSetPassword(userStore)
	s.Register(ipc.DomainAction[users.SetPasswordInput, users.SetPasswordResult]{
		Name: hUsersSetPassword.Action(),
		Decode: func(r *ipc.Request) (*users.SetPasswordInput, error) {
			return &users.SetPasswordInput{UserName: r.UserName, UserPassword: r.UserPassword}, nil
		},
		Validate: hUsersSetPassword.Validate,
		Handle:   hUsersSetPassword.Handle,
		Encode: func(out *users.SetPasswordResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	// apps via ipc.DomainAction (mesmo padrão do composition root — pós-reorg
	// item 2: handlers de domínio com tipos próprios, adaptados ao wire).
	hAppsList := apps.NewList(appsStore)
	s.Register(ipc.DomainAction[apps.NoInput, apps.ListResult]{
		Name:   hAppsList.Action(),
		Decode: ipc.NoInputDecode[apps.NoInput](),
		Handle: hAppsList.Handle,
		Encode: func(out *apps.ListResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Apps: out.Apps}, nil
		},
	}.Handler())
	hAppsAdd := apps.NewAdd(appsStore)
	s.Register(ipc.DomainAction[apps.AddInput, apps.AddResult]{
		Name:   hAppsAdd.Action(),
		Decode: func(r *ipc.Request) (*apps.AddInput, error) { return &apps.AddInput{AppName: r.AppName}, nil },
		Handle: hAppsAdd.Handle,
		Encode: func(out *apps.AddResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	hAppsRemove := apps.NewRemove(appsStore)
	s.Register(ipc.DomainAction[apps.RemoveInput, apps.RemoveResult]{
		Name:   hAppsRemove.Action(),
		Decode: func(r *ipc.Request) (*apps.RemoveInput, error) { return &apps.RemoveInput{AppName: r.AppName}, nil },
		Handle: hAppsRemove.Handle,
		Encode: func(out *apps.RemoveResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	// analytics/stats via ipc.DomainAction (mesmo padrão do composition root —
	// pós-reorg item 1).
	sp := &fakeSessionsProvider{}
	hStats := analytics.NewStatsHandler(sp)
	s.Register(ipc.DomainAction[analytics.StatsInput, analytics.StatsResult]{
		Name: hStats.Action(),
		Decode: func(r *ipc.Request) (*analytics.StatsInput, error) {
			return &analytics.StatsInput{Mission: r.Mission}, nil
		},
		Handle: hStats.Handle,
		Encode: func(out *analytics.StatsResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Stats: out.Stats}, nil
		},
	}.Handler())
	hMissions := analytics.NewMissionsHandler(sp)
	s.Register(ipc.DomainAction[analytics.NoInput, analytics.MissionsResult]{
		Name:   hMissions.Action(),
		Decode: ipc.NoInputDecode[analytics.NoInput](),
		Handle: hMissions.Handle,
		Encode: func(out *analytics.MissionsResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, LabelStats: out.LabelStats}, nil
		},
	}.Handler())
	hSessions := analytics.NewSessionsHandler(sp)
	s.Register(ipc.DomainAction[analytics.NoInput, analytics.SessionsResult]{
		Name:   hSessions.Action(),
		Decode: ipc.NoInputDecode[analytics.NoInput](),
		Handle: hSessions.Handle,
		Encode: func(out *analytics.SessionsResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Sessions: out.Sessions}, nil
		},
	}.Handler())
	// schedule via ipc.DomainAction (pós-reorg item 1). O catálogo de presets
	// real (cat) satisfaz o PresetResolver do serviço.
	ss := &fakeScheduleStore{}
	hScheduleList := schedule.NewListHandler(ss, cat)
	s.Register(ipc.DomainAction[schedule.NoInput, schedule.ListResult]{
		Name:   hScheduleList.Action(),
		Decode: ipc.NoInputDecode[schedule.NoInput](),
		Handle: hScheduleList.Handle,
		Encode: func(out *schedule.ListResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Schedules: out.Schedules}, nil
		},
	}.Handler())
	hScheduleAdd := schedule.NewAddHandler(ss, cat)
	s.Register(ipc.DomainAction[schedule.AddInput, schedule.AddResult]{
		Name:   hScheduleAdd.Action(),
		Decode: func(r *ipc.Request) (*schedule.AddInput, error) { return &schedule.AddInput{Rule: r.ScheduleRule}, nil },
		Handle: hScheduleAdd.Handle,
		Encode: func(out *schedule.AddResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	hScheduleImport := schedule.NewImportHandler(ss, cat)
	s.Register(ipc.DomainAction[schedule.ImportInput, schedule.ImportResult]{
		Name: hScheduleImport.Action(),
		Decode: func(r *ipc.Request) (*schedule.ImportInput, error) {
			return &schedule.ImportInput{ICSContent: r.ICSContent, ICSPreset: r.ICSPreset}, nil
		},
		Handle: hScheduleImport.Handle,
		Encode: func(out *schedule.ImportResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Schedules: out.Schedules, Message: out.Message}, nil
		},
	}.Handler())
	hScheduleRemove := schedule.NewRemoveHandler(ss, cat)
	s.Register(ipc.DomainAction[schedule.RemoveInput, schedule.RemoveResult]{
		Name: hScheduleRemove.Action(),
		Decode: func(r *ipc.Request) (*schedule.RemoveInput, error) {
			return &schedule.RemoveInput{ScheduleID: r.ScheduleID}, nil
		},
		Handle: hScheduleRemove.Handle,
		Encode: func(out *schedule.RemoveResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	// pomodoro via ipc.DomainAction (pós-reorg item 1). O catálogo de presets
	// real (cat) satisfaz o Catalog do serviço.
	pr := &fakePomodoroRunner{}
	pp := newFakePomodoroPrefs()
	hPomodoro := pomodoro.NewStartHandler(pr, pp, cat)
	s.Register(ipc.DomainAction[pomodoro.StartInput, pomodoro.StartResult]{
		Name: hPomodoro.Action(),
		Decode: func(r *ipc.Request) (*pomodoro.StartInput, error) {
			return &pomodoro.StartInput{
				Preset: r.Preset, Label: r.Label, WorkMin: r.WorkMin, RestMin: r.RestMin,
				Cycles: r.Cycles, Strict: r.Strict, Save: r.Save,
			}, nil
		},
		Handle: hPomodoro.Handle,
		Encode: func(out *pomodoro.StartResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message, Pomodoro: &out.State}, nil
		},
	}.Handler())
	hPomodoroDefaults := pomodoro.NewDefaultsHandler(pp)
	s.Register(ipc.DomainAction[pomodoro.NoInput, pomodoro.DefaultsResult]{
		Name:   hPomodoroDefaults.Action(),
		Decode: ipc.NoInputDecode[pomodoro.NoInput](),
		Handle: hPomodoroDefaults.Handle,
		Encode: func(out *pomodoro.DefaultsResult) (*ipc.Response, error) {
			return &ipc.Response{
				Success:       true,
				PomodoroWork:  out.Work,
				PomodoroRest:  out.Rest,
				PomodoroCycle: out.Cycles,
				Message:       out.Message,
			}, nil
		},
	}.Handler())
	hPomodoroStop := pomodoro.NewStopHandler(pr)
	s.Register(ipc.DomainAction[pomodoro.NoInput, pomodoro.StopResult]{
		Name:   hPomodoroStop.Action(),
		Decode: ipc.NoInputDecode[pomodoro.NoInput](),
		Handle: hPomodoroStop.Handle,
		Encode: func(out *pomodoro.StopResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message, Pomodoro: &out.State}, nil
		},
	}.Handler())
	// update/update-check via ipc.DomainAction (pós-reorg item 1). O checker é
	// lido lazy do server (SetUpdateChecker roda depois do registro), com o
	// bridge wire→domínio — espelho exato do composition root.
	s.SetUpdateChecker(&fakeWireUpdateChecker{st: ipc.UpdateStatus{CurrentVersion: "0.16.1"}})
	updateBridge := func() update.Checker {
		c := s.UpdateChecker()
		if c == nil {
			return nil
		}
		return updateCheckerBridge{c: c}
	}
	for _, apply := range []bool{true, false} {
		hUpdate := update.NewUpdateHandler(updateBridge, apply)
		s.Register(ipc.DomainAction[update.UpdateInput, update.Result]{
			Name:   hUpdate.Action(),
			Decode: func(r *ipc.Request) (*update.UpdateInput, error) { return &update.UpdateInput{Channel: r.Channel}, nil },
			Handle: func(ctx context.Context, in *update.UpdateInput) (*update.Result, error) {
				ctx, cancel := context.WithTimeout(ctx, ipc.UpdateTimeout)
				defer cancel()
				return hUpdate.Handle(ctx, in)
			},
			Encode: func(out *update.Result) (*ipc.Response, error) {
				resp := &ipc.Response{
					Success:         true,
					UpdateAvailable: out.Status.Available,
					UpdateVersion:   out.Status.NewVersion,
					CurrentVersion:  out.Status.CurrentVersion,
					Message:         out.Message,
				}
				if apply {
					resp.UpdatePendingReboot = out.Status.PendingReboot
				}
				return resp, nil
			},
		}.Handler())
	}
	return s, blk, dp
}

// TestDomainWiring_ComposesWithRouter cobre o caminho de produção dos handlers
// de domínio pelo roteador real: mensagens, códigos estáveis e o fechamento
// specs↔registry (boot check do daemon).
func TestDomainWiring_ComposesWithRouter(t *testing.T) {
	s, blk, dp := composeTestServer(t)

	if err := s.ValidateRegistry(); err != nil {
		t.Fatalf("ValidateRegistry: %v", err)
	}

	now := time.Now()
	blk.block = &policy.Block{Domain: "x.com", StartedAt: now, ExpiresAt: now.Add(time.Hour)}

	// block → mensagem de sucesso do handler de domínio.
	resp := s.Dispatch(&ipc.Request{Action: "block", Domain: "x.com", Duration: "1h"})
	if !resp.Success || !strings.Contains(resp.Message, "Domain x.com blocked") {
		t.Fatalf("block: success=%v msg=%q", resp.Success, resp.Message)
	}

	// Conflito ask-first (domínio já ativo) → CodeDomainConflict + ConflictBlock.
	blk.active = &policy.Block{Domain: "x.com", StartedAt: now, ExpiresAt: now.Add(time.Hour)}
	resp = s.Dispatch(&ipc.Request{Action: "block", Domain: "x.com", Duration: "1h"})
	if resp.Success || resp.Code != ipc.CodeDomainConflict || !resp.Conflict {
		t.Fatalf("conflito: success=%v code=%q conflict=%v", resp.Success, resp.Code, resp.Conflict)
	}

	// Duração inválida → código estável (mesmo do switch legado).
	resp = s.Dispatch(&ipc.Request{Action: "block", Domain: "x.com", Duration: "bad"})
	if resp.Success || resp.Code != ipc.CodeDurationInvalid {
		t.Fatalf("duração inválida: code=%q", resp.Code)
	}

	// user-set-password → fail-fast do handler de domínio (senha curta).
	resp = s.Dispatch(&ipc.Request{Action: "user-set-password", UserName: "maria", UserPassword: "curta"})
	if resp.Success || resp.Code != ipc.CodeInvalid {
		t.Fatalf("user-set-password curta: code=%q msg=%q", resp.Code, resp.Message)
	}

	// dns-start → persiste o flag e devolve a mensagem de sucesso.
	resp = s.Dispatch(&ipc.Request{Action: "dns-start"})
	if !resp.Success || !dp.enabled || !strings.Contains(resp.Message, "Servidor DNS iniciado") {
		t.Fatalf("dns-start: success=%v enabled=%v msg=%q", resp.Success, dp.enabled, resp.Message)
	}
	// O wire DNS* reflete a projeção (mergeDNSWire) do estado combinado —
	// cobre os campos DNS* contra drift entre domínio e composition root.
	if !resp.DNSListening {
		t.Fatalf("dns-start: DNSListening deveria vir no wire, got %+v", resp)
	}
	resp = s.Dispatch(&ipc.Request{Action: "dns-status"})
	if !resp.Success || !resp.DNSListening || resp.DNSQueries != 7 || resp.DNSUpstream != dnsserver.DefaultUpstream {
		t.Fatalf("dns-status: wire DNS* incompleto, got %+v", resp)
	}

	// goal-set com store real → meta refletida na resposta.
	resp = s.Dispatch(&ipc.Request{Action: "goal-set", GoalMinutes: 120})
	if !resp.Success || resp.Goal != 120*time.Minute {
		t.Fatalf("goal-set: success=%v goal=%v", resp.Success, resp.Goal)
	}

	// Ação desconhecida → CodeUnknownAction + mensagem legada preservada.
	resp = s.Dispatch(&ipc.Request{Action: "nope"})
	if resp.Success || resp.Code != ipc.CodeUnknownAction || resp.Message != "Not supported action: nope" {
		t.Fatalf("desconhecida: success=%v code=%q msg=%q", resp.Success, resp.Code, resp.Message)
	}

	// update-check → mensagem do serviço de domínio (checker fake sem update).
	resp = s.Dispatch(&ipc.Request{Action: "update-check"})
	if !resp.Success || resp.Message != "Nenhuma atualização disponível." {
		t.Fatalf("update-check: success=%v msg=%q", resp.Success, resp.Message)
	}

	// update com atualização aplicada → campos do wire + latch de restart
	// (o wrapper do composition root cacheia o status e arma o latch; o
	// roteador o consome após a resposta — hook ausente aqui é nil-safe).
	s.SetUpdateChecker(&fakeWireUpdateChecker{st: ipc.UpdateStatus{
		CurrentVersion: "0.16.1", NewVersion: "0.17.0", Available: true, Applied: true,
	}})
	resp = s.Dispatch(&ipc.Request{Action: "update"})
	if !resp.Success || !resp.UpdateAvailable || resp.UpdateVersion != "0.17.0" || resp.CurrentVersion != "0.16.1" {
		t.Fatalf("update aplicado: wire incompleto, got %+v", resp)
	}
	if !strings.Contains(resp.Message, "Atualização aplicada") {
		t.Fatalf("update aplicado: msg=%q", resp.Message)
	}
}

// TestDomainWiring_AllActionsDispatch dispara TODAS as 31 ações de domínio
// contra o roteador real (handlers de produção), prendendo o shape do wire de
// cada família — a rede de segurança contra drift entre os adapters de
// referência (testes internos) e os handlers reais (daemon).
func TestDomainWiring_AllActionsDispatch(t *testing.T) {
	s, blk, _ := composeTestServer(t)

	now := time.Now()
	blk.block = &policy.Block{Domain: "x.com", StartedAt: now, ExpiresAt: now.Add(time.Hour)}

	cases := []struct {
		name   string
		req    ipc.Request
		wantOK bool
	}{
		{name: "block", req: ipc.Request{Action: "block", Domain: "x.com", Duration: "1h"}, wantOK: true},
		{name: "block-all", req: ipc.Request{Action: "block-all", Duration: "1h"}, wantOK: true},
		{name: "presets", req: ipc.Request{Action: "presets"}, wantOK: true},
		{name: "preset-add", req: ipc.Request{Action: "preset-add", PresetName: "meu", PresetDomains: []string{"a.com"}}, wantOK: true},
		{name: "preset-remove", req: ipc.Request{Action: "preset-remove", PresetName: "meu"}, wantOK: true},
		{name: "apps-list", req: ipc.Request{Action: "apps-list"}, wantOK: true},
		{name: "apps-add", req: ipc.Request{Action: "apps-add", AppName: "steam.exe"}, wantOK: true},
		{name: "apps-remove", req: ipc.Request{Action: "apps-remove", AppName: "steam.exe"}, wantOK: true},
		{name: "goal-get", req: ipc.Request{Action: "goal-get"}, wantOK: true},
		{name: "goal-set", req: ipc.Request{Action: "goal-set", GoalMinutes: 60}, wantOK: true},
		{name: "user-list", req: ipc.Request{Action: "user-list"}, wantOK: true},
		{name: "user-add", req: ipc.Request{Action: "user-add", UserName: "maria", UserPassword: "senha-forte-1"}, wantOK: true},
		{name: "user-verify-ok", req: ipc.Request{Action: "user-verify", UserName: "maria", UserPassword: "senha-forte-1"}, wantOK: true},
		{name: "user-set-password", req: ipc.Request{Action: "user-set-password", UserName: "maria", UserPassword: "nova-senha-123"}, wantOK: true},
		{name: "user-remove", req: ipc.Request{Action: "user-remove", UserName: "maria"}, wantOK: true},
		{name: "user-verify-fail", req: ipc.Request{Action: "user-verify", UserName: "maria", UserPassword: "senha-forte-1"}, wantOK: false},
		{name: "dns-start", req: ipc.Request{Action: "dns-start"}, wantOK: true},
		{name: "dns-stop", req: ipc.Request{Action: "dns-stop"}, wantOK: true},
		{name: "dns-status", req: ipc.Request{Action: "dns-status"}, wantOK: true},
		{name: "dns-set-upstream", req: ipc.Request{Action: "dns-set-upstream", Upstream: "9.9.9.9"}, wantOK: true},
		{name: "stats", req: ipc.Request{Action: "stats"}, wantOK: true},
		{name: "missions", req: ipc.Request{Action: "missions"}, wantOK: true},
		{name: "sessions", req: ipc.Request{Action: "sessions"}, wantOK: true},
		{name: "schedule-list", req: ipc.Request{Action: "schedule-list"}, wantOK: true},
		{name: "schedule-add", req: ipc.Request{Action: "schedule-add", ScheduleRule: schedule.Rule{Preset: "social"}}, wantOK: true},
		{name: "schedule-import", req: ipc.Request{Action: "schedule-import", ICSContent: "BEGIN:VCALENDAR", ICSPreset: "social"}, wantOK: true},
		{name: "schedule-remove", req: ipc.Request{Action: "schedule-remove", ScheduleID: "r1"}, wantOK: true},
		{name: "pomodoro", req: ipc.Request{Action: "pomodoro", Preset: "social", WorkMin: 25, RestMin: 5, Cycles: 1}, wantOK: true},
		{name: "pomodoro-defaults", req: ipc.Request{Action: "pomodoro-defaults"}, wantOK: true},
		{name: "pomodoro-stop", req: ipc.Request{Action: "pomodoro-stop"}, wantOK: true},
		{name: "update-check", req: ipc.Request{Action: "update-check"}, wantOK: true},
		{name: "update", req: ipc.Request{Action: "update"}, wantOK: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.Dispatch(&tc.req)
			if resp.Success != tc.wantOK {
				t.Fatalf("%s: success=%v msg=%q", tc.name, resp.Success, resp.Message)
			}
		})
	}
}

// TestDomainWiring_NoStoreFailsAsLegacy verifica que os handlers de domínio
// reproduzem os erros "não configurado" do switch legado (store ausente),
// fechando o comportamento que os adapters de referência também testam.
func TestDomainWiring_NoStoreFailsAsLegacy(t *testing.T) {
	// users.NewList(nil): mesmo CodeNotConfigured do adaptador legado.
	h := users.NewList(nil)
	resp, err := h.Handle(context.Background(), &users.NoInput{})
	if err == nil {
		t.Fatal("user-list sem store deveria falhar")
	}
	if resp != nil || err.Error() != "usuários não configurados" {
		t.Fatalf("user-list nil: resp=%v err=%v", resp, err)
	}
}
