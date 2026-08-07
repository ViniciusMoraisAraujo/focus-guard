package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"focusguard/internal/transport/ipc"
)

// handleMetricsCommand imprime a latência por ação do daemon (Fase 8 — C3):
// chamadas, média, p50/p95 e máximo em ms. --reset zera o registro antes do
// snapshot — o início de uma janela de medição (rode, use o sistema, rode de
// novo para comparar).
func handleMetricsCommand(client *ipc.Client, args []string) {
	metricsCmd := flag.NewFlagSet("metrics", flag.ExitOnError)
	reset := metricsCmd.Bool("reset", false, "Zerar as métricas antes de mostrar")
	_ = metricsCmd.Parse(args)

	resp, err := client.Send(ipc.Request{Action: "metrics", Reset: *reset})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao obter métricas: %s\n", resp.Message)
		osExit(1)
	}

	fmt.Println("📊 Latência por ação (daemon):")
	if len(resp.Metrics) == 0 {
		fmt.Println("Nenhuma ação medida ainda — execute alguns comandos e tente de novo.")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "AÇÃO\tCHAMADAS\tMÉDIA\tP50\tP95\tMÁX")
	fmt.Fprintln(w, "----\t--------\t-----\t---\t---\t---")
	for _, st := range resp.Metrics {
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\n",
			st.Action, st.Count, durString(st.Avg), durString(st.P50),
			durString(st.P95), durString(st.Max))
	}
	w.Flush()
}

// durString renderiza uma duração com precisão de µs para a tabela (0s para
// ações abaixo disso — o snapshots JSON usa nanosegundos).
func durString(d time.Duration) string {
	return d.Round(time.Microsecond).String()
}
