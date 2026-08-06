package main

import (
	"flag"
	"fmt"

	"focusguard/internal/ipc"
)

// handlePomodoroCommand starts a pomodoro session over a preset's domains.
// Flags não informadas enviam sentinelas (work=0, rest=-1, cycles=0) para o
// daemon resolver os padrões salvos (--save) ou o clássico 25/5/4. O rest é o
// único que distingue "não informado" (-1) de "sem descanso" (0 explícito).
func handlePomodoroCommand(client *ipc.Client, args []string) {
	pomCmd := flag.NewFlagSet("pomodoro", flag.ExitOnError)
	presetFlag := pomCmd.String("preset", "", "Categoria de domínios (ex: social, video)")
	workFlag := pomCmd.Int("work", 0, "Minutos de trabalho por ciclo (padrão: salvo ou 25)")
	restFlag := pomCmd.Int("rest", -1, "Minutos de descanso entre ciclos (0 = sem descanso; omitido = salvo ou 5)")
	cyclesFlag := pomCmd.Int("cycles", 0, "Número de ciclos (padrão: salvo ou 4)")
	strictFlag := pomCmd.Bool("strict", false, "Sessão estrita (não pode ser encerrada antecipadamente)")
	saveFlag := pomCmd.Bool("save", false, "Salvar estes parâmetros como padrão para as próximas sessões")
	labelFlag := pomCmd.String("label", "", "Nome da missão para o relatório (ex: --label \"Estudar ENEM\")")

	_ = pomCmd.Parse(args)

	if *presetFlag == "" {
		fmt.Println("Erro: Informe um preset (ex: --preset social).")
		fmt.Println("Uso: focusguard pomodoro --preset <categoria> [--work 25] [--rest 5] [--cycles 4] [--strict] [--save] [--label \"missão\"]")
		osExit(1)
	}

	req := ipc.Request{
		Action:  "pomodoro",
		Preset:  *presetFlag,
		WorkMin: *workFlag,
		RestMin: *restFlag,
		Cycles:  *cyclesFlag,
		Strict:  *strictFlag,
		Save:    *saveFlag,
		Label:   *labelFlag,
	}

	resp, err := client.Send(req)
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao iniciar pomodoro: %s\n", resp.Message)
		osExit(1)
	}

	fmt.Printf("✔ %s\n", resp.Message)
}

// handlePomodoroDefaultsCommand shows the current persisted pomodoro defaults
// (or the classic 25/5/4 when none were saved).
func handlePomodoroDefaultsCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "pomodoro-defaults"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao consultar padrões: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("🍅 Padrões atuais do pomodoro: %dm trabalho / %dm descanso / %d ciclos\n",
		resp.PomodoroWork, resp.PomodoroRest, resp.PomodoroCycle)
	fmt.Println("  Salve novos padrões com: focusguard pomodoro --preset <categoria> --work X --rest Y --cycles Z --save")
}

// handlePomodoroStopCommand ends the active pomodoro session.
func handlePomodoroStopCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "pomodoro-stop"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao encerrar pomodoro: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}
