package main

import (
	"flag"
	"fmt"

	"focusguard/internal/ipc"
)

func handleUpdateCommand(client *ipc.Client, args []string) {
	updateCmd := flag.NewFlagSet("update", flag.ExitOnError)
	channelFlag := updateCmd.String("channel", "", "Canal de release: stable (padrão) ou beta (prereleases)")
	_ = updateCmd.Parse(args)

	req := ipc.Request{
		Action:  "update",
		Channel: *channelFlag,
	}

	resp, err := client.Send(req)
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}

	if !resp.Success {
		fmt.Printf("Falha ao atualizar: %s\n", resp.Message)
		osExit(1)
	}

	// PendingReboot vem ANTES de UpdateAvailable: o server mantém Available=true
	// no fallback move-on-reboot (o update existe, só será aplicado no boot) —
	// se invertesse a ordem, diria "aplicada" indevidamente.
	if resp.UpdatePendingReboot {
		// Fallback move-on-reboot: um binário em execução estava travado e a
		// troca da suíte foi agendada para o próximo boot (MoveFileEx).
		fmt.Printf("✔ Atualização preparada: %s → %s\n", resp.CurrentVersion, resp.UpdateVersion)
		fmt.Println("  Os binários em uso serão substituídos no próximo reinício do computador.")
		return
	}
	if resp.UpdateAvailable {
		fmt.Printf("✔ Atualização aplicada: %s → %s\n", resp.CurrentVersion, resp.UpdateVersion)
		fmt.Println("  A nova versão será usada na próxima reinicialização do daemon.")
		return
	}

	fmt.Printf("✔ Você já está na versão mais recente (%s).\n", resp.CurrentVersion)
}
