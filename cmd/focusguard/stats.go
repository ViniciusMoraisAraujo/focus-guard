package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"focusguard/internal/analytics"
	"focusguard/internal/ipc"
	"focusguard/internal/tamper"
)

// handleMissionCommand lists the focus totals per named mission (sessions
// started with pomodoro --label "...").
func handleMissionCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "missions"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao obter missões: %s\n", resp.Message)
		osExit(1)
	}

	fmt.Println("🎯 Missões de foco:")
	if len(resp.LabelStats) == 0 {
		fmt.Println("Nenhuma missão nomeada ainda — inicie uma com: focusguard pomodoro --preset <cat> --label \"missão\"")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "MISSÃO\tFOCO\tSESSÕES")
	fmt.Fprintln(w, "------\t----\t-------")
	for _, ls := range resp.LabelStats {
		fmt.Fprintf(w, "%s\t%s\t%d\n", ls.Label, ls.Duration.Round(time.Minute), ls.Sessions)
	}
	w.Flush()
}

// handleStatsCommand fetches the focus analytics and renders the ASCII chart
// (default), or exports the report as CSV/JSON with --export.
func handleStatsCommand(client *ipc.Client, args []string) {
	statsCmd := flag.NewFlagSet("stats", flag.ExitOnError)
	exportFlag := statsCmd.String("export", "", "Exportar como csv ou json")
	missionFlag := statsCmd.String("mission", "", "Filtrar por uma missão (label da sessão)")
	_ = statsCmd.Parse(args)

	resp, err := client.Send(ipc.Request{Action: "stats", Mission: *missionFlag})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao obter estatísticas: %s\n", resp.Message)
		osExit(1)
	}

	if resp.Stats == nil {
		fmt.Println("Sem estatísticas registradas ainda — faça uma sessão de foco primeiro.")
		return
	}

	switch strings.ToLower(*exportFlag) {
	case "csv":
		fmt.Print(analytics.ExportCSV(resp.Stats))
	case "json":
		if out, err := analytics.ExportJSON(resp.Stats); err != nil {
			fmt.Printf("Falha ao exportar JSON: %v\n", err)
			osExit(1)
		} else {
			fmt.Println(out)
		}
	case "html":
		fmt.Print(analytics.ExportHTML(resp.Stats))
	default:
		fmt.Print(analytics.RenderStats(resp.Stats, 30))
	}
}

// handleReportCommand prints a compact weekly focus summary derived from the
// stats report (same IPC action — no new daemon surface needed).
func handleReportCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "stats"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao obter o resumo: %s\n", resp.Message)
		osExit(1)
	}
	if resp.Stats == nil {
		fmt.Println("Sem estatísticas registradas ainda — faça uma sessão de foco primeiro.")
		return
	}
	fmt.Print(analytics.RenderWeeklySummary(resp.Stats))
}

// handleTamperLogCommand prints the detected tamper attempts (external edits
// to the hosts/state files that were detected and restored).
func handleTamperLogCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "tamper-log"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao obter o histórico: %s\n", resp.Message)
		osExit(1)
	}

	fmt.Println("🛡 Histórico de tentativas de burla (adulterações detectadas e revertidas):")
	if len(resp.TamperLog) == 0 {
		fmt.Println("Nenhuma tentativa registrada. 👌")
		return
	}
	for _, e := range resp.TamperLog {
		fmt.Printf("  %s\n", tamper.FormatEvent(e))
	}
}
