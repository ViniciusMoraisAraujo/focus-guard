package main

import "fmt"

// printUsage renderiza o help. A seção "Uso:" é gerada a partir da tabela de
// comandos (commands + usageOrder) — um comando novo aparece no help
// automaticamente; cabeçalho, exemplos e nota ficam aqui, verbatim.
func printUsage() {
	fmt.Println("FocusGuard - CLI para bloqueio focado")
	fmt.Println("\nUso:")
	fmt.Println("  focusguard                        Abre a interface web no navegador")
	for _, name := range usageOrder {
		for _, line := range commands[name].Usage {
			fmt.Println(line)
		}
	}
	fmt.Println("\nExemplos:")
	fmt.Println("  focusguard install")
	fmt.Println("  focusguard install-watchdog")
	fmt.Println("  focusguard block twitter.com --duration 4h")
	fmt.Println("  focusguard block youtube.com 30m")
	fmt.Println("  focusguard block --preset social --duration 2h")
	fmt.Println("  focusguard block --internet --duration 30m")
	fmt.Println("  focusguard block --internet --allow docs.google.com,drive.google.com --duration 2h")
	fmt.Println("  focusguard pomodoro --preset social --work 25 --rest 5 --cycles 4 --strict")
	fmt.Println("  focusguard schedule add --preset social --days seg,ter,qua,qui,sex --start 08:00 --end 12:00")
	fmt.Println("  focusguard schedule")
	fmt.Println("  focusguard stats")
	fmt.Println("  focusguard presets")
	fmt.Println("  focusguard status")
	fmt.Println("  focusguard")
	fmt.Println("\nNota: Não existe comando de unblock manual por design.")
}
