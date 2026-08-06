package main

import (
	"fmt"
	"os"

	"focusguard/internal/ipc"
)

// osExit é injetável nos testes (osExit mockado vira panic para capturar o
// código de saída sem derrubar o processo).
var osExit = os.Exit

func main() {
	if len(os.Args) < 2 {
		// Sem argumentos, o FocusGuard abre a interface web no navegador (a
		// antiga TUI interativa foi removida em favor da UI).
		handleWebCommand()
		return
	}

	client := ipc.NewClient()
	command := os.Args[1]

	switch command {
	case "help", "-h", "--help":
		printUsage()
		return
	}

	if c, ok := commands[command]; ok {
		c.Run(client, os.Args[2:])
		return
	}

	fmt.Printf("Comando desconhecido: %s\n\n", command)
	printUsage()
	osExit(1)
}
