package main

import (
	"flag"
	"fmt"
	"strings"

	"focusguard/internal/transport/ipc"
)

// splitBlockFlags removes the position-independent flags (--extend/--replace
// and --duration/--d with their values) from anywhere in args and returns the
// remaining tokens plus the extracted values. Go's flag package stops parsing
// at the first positional argument, so a user writing "focusguard block
// twitter.com --duration 30m --extend" would never have the flags parsed
// (they would become Arg(1)+ and be mistaken for the duration); extracting
// them before Parse makes the flags position-independent. Both "--duration
// 30m" and "--duration=30m" forms are accepted.
func splitBlockFlags(args []string) (out []string, extend, replace bool, duration string) {
	out = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--extend":
			extend = true
		case a == "--replace":
			replace = true
		case a == "--duration" || a == "-duration" || a == "--d" || a == "-d":
			if i+1 < len(args) {
				duration = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--duration=") || strings.HasPrefix(a, "-duration="):
			duration = strings.TrimPrefix(strings.TrimPrefix(a, "--duration="), "-duration=")
		case strings.HasPrefix(a, "--d=") || strings.HasPrefix(a, "-d="):
			duration = strings.TrimPrefix(strings.TrimPrefix(a, "--d="), "-d=")
		default:
			out = append(out, a)
		}
	}
	return out, extend, replace, duration
}

func handleBlockCommand(client *ipc.Client, args []string) {
	blockCmd := flag.NewFlagSet("block", flag.ExitOnError)
	durationFlag := blockCmd.String("duration", "", "Duração do bloqueio (ex: 4h, 30m, 1h30m)")
	durationShortFlag := blockCmd.String("d", "", "Duração do bloqueio (shorthand)")
	presetFlag := blockCmd.String("preset", "", "Bloquear uma categoria inteira (ex: social, video, news, games)")
	internetFlag := blockCmd.Bool("internet", false, "Bloquear toda a internet (modo pânico) por um período")
	allowFlag := blockCmd.String("allow", "", "No modo --internet: domínios permitidos (allowlist), separados por vírgula")
	extendFlag := blockCmd.Bool("extend", false, "Somar à duração do bloqueio já ativo do domínio (em vez de perguntar)")
	replaceFlag := blockCmd.Bool("replace", false, "Reiniciar o bloqueio do domínio a partir de agora, descartando o anterior")

	// Go's flag package para de parsear no primeiro argumento posicional —
	// "focusguard block <dominio> --duration 30m --extend" deixaria os flags
	// sem efeito. Extrai esses flags de qualquer posição antes do Parse.
	args, argExtend, argReplace, argDuration := splitBlockFlags(args)
	_ = blockCmd.Parse(args)
	extend := *extendFlag || argExtend
	replace := *replaceFlag || argReplace

	domain := blockCmd.Arg(0)
	if domain == "" && *presetFlag == "" && !*internetFlag {
		fmt.Println("Erro: Informe um domínio, --preset ou --internet para bloquear.")
		fmt.Println("Uso: focusguard block <dominio> --duration <tempo>  |  focusguard block --preset <categoria> --duration <tempo>  |  focusguard block --internet [--allow <dominios>] --duration <tempo>")
		osExit(1)
	}
	if (extend || replace) && (domain == "" || *presetFlag != "" || *internetFlag) {
		fmt.Println("Erro: --extend e --replace só se aplicam a um domínio específico.")
		osExit(1)
	}

	durationStr := *durationFlag
	if durationStr == "" {
		durationStr = *durationShortFlag
	}
	if durationStr == "" {
		durationStr = argDuration
	}

	if durationStr == "" && blockCmd.NArg() > 1 {
		durationStr = blockCmd.Arg(1)
	}

	if durationStr == "" {
		fmt.Println("Erro: A duração do bloqueio deve ser informada (ex: --duration 4h).")
		osExit(1)
	}

	req := ipc.Request{
		Action:   "block",
		Domain:   domain,
		Preset:   *presetFlag,
		Duration: durationStr,
		Extend:   extend,
		Replace:  replace,
	}
	if *internetFlag {
		req.Action = "block-all"
		if *allowFlag != "" {
			for _, d := range strings.Split(*allowFlag, ",") {
				if d = strings.TrimSpace(d); d != "" {
					req.Allowlist = append(req.Allowlist, d)
				}
			}
		}
	}

	resp, err := client.Send(req)
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}

	if resp.Conflict {
		fmt.Printf("Domínio já bloqueado até %s.\n", resp.ConflictBlock.ExpiresAt.Local().Format("15:04:05 02/01/2006"))
		fmt.Println("Use --extend para somar a duração atual ou --replace para reiniciar o bloqueio.")
		osExit(1)
	}

	if !resp.Success {
		fmt.Printf("Falha ao aplicar bloqueio: %s\n", resp.Message)
		osExit(1)
	}

	fmt.Printf("✔ %s\n", resp.Message)
}
