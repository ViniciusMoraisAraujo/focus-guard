package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"focusguard/internal/ipc"
	"focusguard/internal/schedule"
)

// handleScheduleCommand dispatches the schedule subcommands
// (add/import/list/remove). With no subcommand it lists the current rules.
func handleScheduleCommand(client *ipc.Client, args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "add":
			handleScheduleAddCommand(client, args[1:])
			return
		case "import":
			handleScheduleImportCommand(client, args[1:])
			return
		case "remove", "rm":
			handleScheduleRemoveCommand(client, args[1:])
			return
		}
	}
	handleScheduleListCommand(client)
}

// handleScheduleImportCommand imports weekly events from an .ics calendar file
// as recurring block rules: focusguard schedule import --file <arquivo.ics> --preset <categoria>
func handleScheduleImportCommand(client *ipc.Client, args []string) {
	impCmd := flag.NewFlagSet("schedule-import", flag.ExitOnError)
	presetFlag := impCmd.String("preset", "", "Categoria a bloquear (ex: social, video)")
	fileFlag := impCmd.String("file", "", "Caminho do arquivo .ics")
	_ = impCmd.Parse(args)

	path := *fileFlag
	if path == "" {
		fmt.Println("Erro: Informe o arquivo .ics (--file <arquivo.ics>).")
		fmt.Println("Uso: focusguard schedule import --file <arquivo.ics> --preset <categoria>")
		osExit(1)
	}
	if *presetFlag == "" {
		fmt.Println("Erro: Informe o preset (ex: --preset social).")
		fmt.Println("Uso: focusguard schedule import --file <arquivo.ics> --preset <categoria>")
		osExit(1)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("Erro ao ler %s: %v\n", path, err)
		osExit(1)
	}

	resp, err := client.Send(ipc.Request{Action: "schedule-import", ICSContent: string(data), ICSPreset: *presetFlag})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao importar calendário: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}

// scheduleDayNames maps English and Portuguese weekday abbreviations to
// time.Weekday values (0=Sunday), case-insensitively.
var scheduleDayNames = map[string]int{
	"sun": 0, "dom": 0,
	"mon": 1, "seg": 1,
	"tue": 2, "ter": 2,
	"wed": 3, "qua": 3,
	"thu": 4, "qui": 4,
	"fri": 5, "sex": 5,
	"sat": 6, "sab": 6,
}

// parseScheduleDays converts a comma-separated list of day abbreviations into
// time.Weekday ints (0=Sunday), rejecting unknown names.
func parseScheduleDays(raw string) ([]int, error) {
	var days []int
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(strings.ToLower(tok))
		d, ok := scheduleDayNames[tok]
		if !ok {
			return nil, fmt.Errorf("dia inválido %q (use mon..sun ou seg..dom)", tok)
		}
		days = append(days, d)
	}
	return days, nil
}

// handleScheduleAddCommand creates a recurring block rule for a preset.
// Example: focusguard schedule add --preset social --days mon-fri --start 08:00 --end 12:00
func handleScheduleAddCommand(client *ipc.Client, args []string) {
	addCmd := flag.NewFlagSet("schedule-add", flag.ExitOnError)
	presetFlag := addCmd.String("preset", "", "Categoria a bloquear (ex: social, video)")
	daysFlag := addCmd.String("days", "", "Dias da semana (ex: mon,tue,wed ou seg,ter,qua)")
	startFlag := addCmd.String("start", "", "Início no formato HH:MM (ex: 08:00)")
	endFlag := addCmd.String("end", "", "Fim no formato HH:MM (ex: 12:00)")
	windowsFlag := addCmd.String("windows", "", "Janelas múltiplas HH:MM-HH:MM separadas por vírgula (ex: 08:00-12:00,14:00-18:00)")
	labelFlag := addCmd.String("label", "", "Rótulo opcional (ex: Estudo matinal)")

	_ = addCmd.Parse(args)

	if *presetFlag == "" || *daysFlag == "" || (*windowsFlag == "" && (*startFlag == "" || *endFlag == "")) {
		fmt.Println("Erro: Informe --preset, --days e (--start/--end OU --windows).")
		fmt.Println("Uso: focusguard schedule add --preset <categoria> --days <dias> --start HH:MM --end HH:MM [--label \"...\"]")
		fmt.Println("     focusguard schedule add --preset <categoria> --days <dias> --windows 08:00-12:00,14:00-18:00 [--label \"...\"]")
		osExit(1)
	}

	days, err := parseScheduleDays(*daysFlag)
	if err != nil {
		fmt.Printf("Erro: %v\n", err)
		osExit(1)
	}

	rule := schedule.Rule{
		Preset:  *presetFlag,
		Label:   *labelFlag,
		Days:    days,
		Start:   *startFlag,
		End:     *endFlag,
		Enabled: true,
	}
	if *windowsFlag != "" {
		rule.Windows = strings.Split(*windowsFlag, ",")
	}

	req := ipc.Request{
		Action:       "schedule-add",
		ScheduleRule: rule,
	}

	resp, err := client.Send(req)
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao criar agendamento: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}

// handleScheduleListCommand prints the recurring rules.
func handleScheduleListCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "schedule-list"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao listar agendamentos: %s\n", resp.Message)
		osExit(1)
	}

	if len(resp.Schedules) == 0 {
		fmt.Println("Nenhum agendamento recorrente configurado.")
		fmt.Println("Crie um com: focusguard schedule add --preset <categoria> --days <dias> --start HH:MM --end HH:MM")
		return
	}

	fmt.Println("📅 Agendamentos recorrentes:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tPRESET\tDIAS\tJANELA")
	fmt.Fprintln(w, "--\t------\t----\t------")
	for _, r := range resp.Schedules {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s%s\n", r.ID, r.Preset, scheduleDaysString(r.Days), scheduleWindowString(r), scheduleStateSuffix(r))
	}
	w.Flush()
}

// scheduleWindowString renders the rule's time window(s): the Windows list
// when present, otherwise the legacy "HH:MM-HH:MM" from Start/End.
func scheduleWindowString(r schedule.Rule) string {
	if len(r.Windows) > 0 {
		return strings.Join(r.Windows, ", ")
	}
	return r.Start + "-" + r.End
}

// scheduleDaysString renders weekday ints as abbreviations (seg..dom).
func scheduleDaysString(days []int) string {
	names := []string{"dom", "seg", "ter", "qua", "qui", "sex", "sab"}
	var out []string
	for _, d := range days {
		if d >= 0 && d < 7 {
			out = append(out, names[d])
		}
	}
	return strings.Join(out, ",")
}

func scheduleStateSuffix(r schedule.Rule) string {
	if !r.Enabled {
		return " (desativada)"
	}
	return ""
}

// handleScheduleRemoveCommand deletes a recurring rule by ID.
func handleScheduleRemoveCommand(client *ipc.Client, args []string) {
	if len(args) < 1 {
		fmt.Println("Erro: Informe o ID da regra.")
		fmt.Println("Uso: focusguard schedule remove <id>")
		osExit(1)
	}

	resp, err := client.Send(ipc.Request{Action: "schedule-remove", ScheduleID: args[0]})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao remover agendamento: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}
