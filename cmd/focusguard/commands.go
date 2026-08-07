package main

import (
	"focusguard/internal/transport/ipc"
)

// Command descreve um subcomando do FocusGuard. A tabela commands é o único
// ponto de registro (B5/OCP): comando novo = novo arquivo com o handler +
// uma entrada aqui (e a posição no usageOrder para o help) — nada mais muda.
type Command struct {
	Name  string
	Run   func(*ipc.Client, []string)
	Usage []string
}

// commands é a tabela de comandos (map por nome — aliases apontam para o
// mesmo Command). O main() resolve o dispatch aqui; o printUsage consome o
// campo Usage (na ordem de usageOrder) para gerar a seção "Uso:" do help.
var commands = map[string]Command{
	"block": {
		Name: "block",
		Run:  handleBlockCommand,
		Usage: []string{
			"  focusguard block <dominio> --duration <tempo>",
			"  focusguard block --preset <categoria> --duration <tempo>",
			"  focusguard block --internet [--allow <dominios>] --duration <tempo>   Modo pânico / allowlist",
		},
	},
	"presets": {
		Name: "presets",
		Run:  func(c *ipc.Client, _ []string) { handlePresetsCommand(c) },
		Usage: []string{
			"  focusguard presets                     Listar categorias de bloqueio",
		},
	},
	"preset": {
		Name: "preset",
		Run:  handlePresetCommand,
		Usage: []string{
			"  focusguard preset add <nome> <dominio...>   Criar preset personalizado",
			"  focusguard preset remove <nome>         Remover preset personalizado",
		},
	},
	"schedule": {
		Name: "schedule",
		Run:  handleScheduleCommand,
		Usage: []string{
			"  focusguard schedule add --preset <cat> --days <dias> --start HH:MM --end HH:MM",
			"  focusguard schedule import --file <arquivo.ics> --preset <cat>   Importar calendário (eventos semanais)",
			"  focusguard schedule list                Listar agendamentos recorrentes",
			"  focusguard schedule remove <id>         Remover um agendamento",
		},
	},
	"apps": {
		Name: "apps",
		Run:  handleAppsCommand,
		Usage: []string{
			"  focusguard apps [list]                  Listar processos da denylist",
			"  focusguard apps add <processo>          Encerrar processo durante sessões de foco",
			"  focusguard apps remove <processo>       Parar de encerrar um processo",
		},
	},
	"dns": {
		Name: "dns",
		Run:  handleDNSCommand,
		Usage: []string{
			"  focusguard dns start                    Iniciar o servidor DNS sinkhole (porta 53)",
			"  focusguard dns stop                     Desligar o servidor DNS sinkhole",
			"  focusguard dns status                   Mostrar o status do servidor DNS",
			"  focusguard dns upstream <host[:porta]>  Alterar o upstream DNS (ex: 9.9.9.9)",
		},
	},
	"pomodoro": {
		Name: "pomodoro",
		Run:  handlePomodoroCommand,
		Usage: []string{
			"  focusguard pomodoro --preset <categoria> [--work 25] [--rest 5] [--cycles 4] [--strict] [--save] [--label \"missão\"]",
		},
	},
	"pomodoro-defaults": {
		Name: "pomodoro-defaults",
		Run:  func(c *ipc.Client, _ []string) { handlePomodoroDefaultsCommand(c) },
		Usage: []string{
			"  focusguard pomodoro-defaults          Mostrar os padrões salvos do pomodoro",
		},
	},
	"mission":  missionCommand,
	"missions": {Name: "missions", Run: missionCommand.Run}, // alias (sem Usage no help)
	"pomodoro-stop": {
		Name: "pomodoro-stop",
		Run:  func(c *ipc.Client, _ []string) { handlePomodoroStopCommand(c) },
		Usage: []string{
			"  focusguard pomodoro-stop               Encerrar a sessão pomodoro",
		},
	},
	"stats": {
		Name: "stats",
		Run:  handleStatsCommand,
		Usage: []string{
			"  focusguard stats [--export csv|json|html] [--mission <nome>]   Gráfico de foco / exportar relatório",
		},
	},
	"report": {
		Name: "report",
		Run:  func(c *ipc.Client, _ []string) { handleReportCommand(c) },
		Usage: []string{
			"  focusguard report                     Resumo semanal de foco",
		},
	},
	"tamper-log": {
		Name: "tamper-log",
		Run:  func(c *ipc.Client, _ []string) { handleTamperLogCommand(c) },
		Usage: []string{
			"  focusguard tamper-log                 Histórico de tentativas de burla",
		},
	},
	"goal": {
		Name: "goal",
		Run:  handleGoalCommand,
		Usage: []string{
			"  focusguard goal                        Mostrar a meta diária de foco",
			"  focusguard goal set <duracao>         Definir a meta diária (ex: 4h)",
		},
	},
	"status": {
		Name: "status",
		Run:  func(c *ipc.Client, _ []string) { handleStatusCommand(c) },
		Usage: []string{
			"  focusguard status",
		},
	},
	"metrics": {
		Name: "metrics",
		Run:  handleMetricsCommand,
		Usage: []string{
			"  focusguard metrics [--reset]          Latência por ação (medir o daemon)",
		},
	},
	"update": {
		Name: "update",
		Run:  handleUpdateCommand,
		Usage: []string{
			"  focusguard update [--channel beta]  Verificar e aplicar atualizações do daemon",
		},
	},
	"web": {
		Name: "web",
		Run:  func(_ *ipc.Client, _ []string) { handleWebCommand() },
		Usage: []string{
			"  focusguard web                     Abrir a interface web no navegador",
		},
	},
	"install": {
		Name: "install",
		Run:  func(_ *ipc.Client, _ []string) { handleInstallCommand() },
		Usage: []string{
			"  focusguard install                 Instalar daemon + tray + watchdog",
		},
	},
	"uninstall": {
		Name: "uninstall",
		Run:  func(_ *ipc.Client, _ []string) { handleUninstallCommand() },
		Usage: []string{
			"  focusguard uninstall               Remover daemon da inicialização",
		},
	},
	"install-watchdog": {
		Name: "install-watchdog",
		Run:  func(_ *ipc.Client, _ []string) { handleInstallWatchdogCommand() },
		Usage: []string{
			"  focusguard install-watchdog         Instalar watchdog externo (Windows)",
		},
	},
	"uninstall-watchdog": {
		Name: "uninstall-watchdog",
		Run:  func(_ *ipc.Client, _ []string) { handleUninstallWatchdogCommand() },
		Usage: []string{
			"  focusguard uninstall-watchdog       Remover watchdog externo",
		},
	},
	"install-tray": {
		Name: "install-tray",
		Run:  func(_ *ipc.Client, _ []string) { handleInstallTrayCommand() },
		Usage: []string{
			"  focusguard install-tray             Iniciar o tray com o Windows (HKCU Run)",
		},
	},
	"uninstall-tray": {
		Name: "uninstall-tray",
		Run:  func(_ *ipc.Client, _ []string) { handleUninstallTrayCommand() },
		Usage: []string{
			"  focusguard uninstall-tray           Remover o tray da inicialização",
		},
	},
}

// missionCommand é o Command canônico da missão; o alias "missions" aponta o
// mesmo Run, sem entrada própria no help.
var missionCommand = Command{
	Name: "mission",
	Run:  func(c *ipc.Client, _ []string) { handleMissionCommand(c) },
	Usage: []string{
		"  focusguard mission                    Resumo de foco por missão nomeada",
	},
}

// usageOrder preserva a ordem de exibição da seção "Uso:" do help (mapas não
// têm ordem). Nomes canônicos — aliases ficam de fora (mission é o canônico).
var usageOrder = []string{
	"block", "presets", "preset", "schedule", "apps", "dns", "pomodoro",
	"pomodoro-defaults", "mission", "pomodoro-stop", "stats", "report",
	"tamper-log", "goal", "status", "metrics", "web", "update", "install", "uninstall",
	"install-watchdog", "uninstall-watchdog", "install-tray", "uninstall-tray",
}
