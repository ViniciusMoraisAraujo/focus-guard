package main

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"focusguard/internal/ipc"
)

func handleStatusCommand(client *ipc.Client) {
	req := ipc.Request{
		Action: "status",
	}

	resp, err := client.Send(req)
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}

	if !resp.Success {
		fmt.Printf("Falha ao obter status: %s\n", resp.Message)
		osExit(1)
	}

	printProtectionStatus(resp)

	if resp.UpdateAvailable {
		fmt.Printf("🔄 Nova versão disponível: %s → %s (rode 'focusguard update')\n", resp.CurrentVersion, resp.UpdateVersion)
	}

	if len(resp.Blocks) == 0 {
		fmt.Println("Nenhum bloqueio ativo no momento.")
		return
	}

	fmt.Println("\n🔒 Bloqueios Ativos:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "DOMÍNIO\tINÍCIO\tEXPIRA EM\tTEMPO RESTANTE")
	fmt.Fprintln(w, "-------\t------\t---------\t--------------")

	for _, b := range resp.Blocks {
		remaining := time.Until(b.ExpiresAt).Round(time.Second)
		if remaining < 0 {
			remaining = 0
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			b.Domain,
			b.StartedAt.Local().Format("15:04 02/01"),
			b.ExpiresAt.Local().Format("15:04 02/01"),
			remaining.String(),
		)
	}
	w.Flush()
	fmt.Println()
}

func printProtectionStatus(resp *ipc.Response) {
	fmt.Println("🛡 Proteção DoH/DoT:")

	if resp.ProtectionError != "" {
		fmt.Printf("  ⚠ Não foi possível consultar o firewall: %s\n", resp.ProtectionError)
		return
	}

	status := "inativa"
	if resp.DoHActive {
		status = "ATIVA"
	}
	fmt.Printf("  Estado: %s\n", status)
	fmt.Printf("  Regras de firewall (FocusGuard): %d\n", resp.FirewallRules)

	switch {
	case resp.ExpectedDoH && !resp.DoHActive:
		fmt.Println("  ⚠ Atenção: há bloqueios ativos, mas as regras DoH/DoT não foram encontradas.")
	case !resp.ExpectedDoH && resp.DoHActive:
		fmt.Println("  ℹ As regras DoH/DoT estão presentes, mas não há bloqueios ativos.")
	}
}
