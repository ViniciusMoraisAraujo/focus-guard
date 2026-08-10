package main

import (
	"fmt"
	"strconv"

	"focusguard/internal/domain/reports"
	"focusguard/internal/transport/ipc"
)

// handleReportCommandDispatch roteia os subcomandos de report: sem subcomando
// imprime o resumo semanal (comportamento histórico); `now` gera na hora;
// `auto` gerencia o agendamento (Fase 5.1).
func handleReportCommandDispatch(client *ipc.Client, args []string) {
	if len(args) == 0 {
		handleReportCommand(client)
		return
	}
	switch args[0] {
	case "now":
		handleReportNowCommand(client, args[1:])
	case "auto":
		handleReportAutoCommand(client, args[1:])
	default:
		handleReportCommand(client)
	}
}

// handleReportAutoCommand liga/desliga o relatório semanal automático (Fase
// 5.1) e ajusta o agendamento: `focusguard report auto on --day 0 --hour 23
// --minute 59 [--path <pasta>]` / `focusguard report auto off` /
// `focusguard report auto status`.
func handleReportAutoCommand(client *ipc.Client, args []string) {
	if len(args) == 0 {
		printReportAutoUsage()
		osExit(1)
	}
	switch args[0] {
	case "on":
		handleReportAutoSet(client, args[1:], true)
	case "off":
		handleReportAutoSet(client, args[1:], false)
	case "status":
		handleReportStatusCommand(client)
	default:
		printReportAutoUsage()
		osExit(1)
	}
}

func printReportAutoUsage() {
	fmt.Println("Uso: focusguard report auto on|off|status [--day 0-6] [--hour 0-23] [--minute 0-59] [--path <pasta>]")
	fmt.Println("  --day   0=domingo … 6=sábado (padrão 0)")
	fmt.Println("  --hour  hora da geração (padrão 23)")
	fmt.Println("  --minute minuto da geração (padrão 59)")
	fmt.Println("  --path  pasta de export (padrão ~/FocusGuardReports)")
}

// handleReportAutoSet persiste o agendamento (ativando ou desativando).
func handleReportAutoSet(client *ipc.Client, args []string, enabled bool) {
	cfg := reports.DefaultConfig()
	cfg.Enabled = enabled
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--day":
			if i+1 < len(args) {
				i++
				if n, err := strconv.Atoi(args[i]); err == nil {
					cfg.DayOfWeek = n
				}
			}
		case "--hour":
			if i+1 < len(args) {
				i++
				if n, err := strconv.Atoi(args[i]); err == nil {
					cfg.Hour = n
				}
			}
		case "--minute":
			if i+1 < len(args) {
				i++
				if n, err := strconv.Atoi(args[i]); err == nil {
					cfg.Minute = n
				}
			}
		case "--path":
			if i+1 < len(args) {
				i++
				cfg.ExportPath = args[i]
			}
		}
	}
	if err := cfg.Valid(); err != nil {
		fmt.Printf("Agendamento inválido: %v\n", err)
		osExit(1)
	}
	resp, err := client.Send(ipc.Request{Action: "reports-config-set", ReportConfig: &cfg})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao configurar o relatório: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
	handleReportStatusCommand(client)
}

// handleReportStatusCommand mostra o agendamento atual.
func handleReportStatusCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "reports-config-get"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao obter o agendamento: %s\n", resp.Message)
		osExit(1)
	}
	if resp.ReportConfig == nil {
		fmt.Println("Sem agendamento configurado.")
		return
	}
	cfg := *resp.ReportConfig
	days := []string{"domingo", "segunda", "terça", "quarta", "quinta", "sexta", "sábado"}
	day := "?"
	if cfg.DayOfWeek >= 0 && cfg.DayOfWeek <= 6 {
		day = days[cfg.DayOfWeek]
	}
	path := cfg.ExportPath
	if path == "" {
		path = reports.DefaultExportPath
	}
	fmt.Println("Relatório semanal:")
	fmt.Printf("  Estado: %s\n", map[bool]string{true: "ativo", false: "desativado"}[cfg.Enabled])
	fmt.Printf("  Agenda: %s às %02d:%02d\n", day, cfg.Hour, cfg.Minute)
	fmt.Printf("  Pasta:  %s\n", path)
}

// handleReportNowCommand gera o relatório imediatamente (Fase 5.1): `focusguard
// report now [--path <pasta>]`.
func handleReportNowCommand(client *ipc.Client, args []string) {
	req := ipc.Request{Action: "reports-generate"}
	for i := 0; i < len(args); i++ {
		if args[i] == "--path" && i+1 < len(args) {
			i++
			req.ReportExportPath = args[i]
		}
	}
	resp, err := client.Send(req)
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao gerar o relatório: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}
