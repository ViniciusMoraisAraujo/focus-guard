package main

import (
	"flag"
	"fmt"
	"strings"

	"focusguard/internal/ipc"
)

// splitExtendReplaceArgs removes the --extend/--replace tokens from anywhere in
// args and returns the remaining tokens plus the extracted booleans. Go's flag
// package stops parsing at the first positional argument, so a user writing
// "focusguard block twitter.com --extend 30m" would never have the flag parsed
// (it would become Arg(1) and be mistaken for the duration); extracting it
// before Parse makes the flag position-independent.
func splitExtendReplaceArgs(args []string) ([]string, bool, bool) {
	var extend, replace bool
	out := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--extend":
			extend = true
		case "--replace":
			replace = true
		default:
			out = append(out, a)
		}
	}
	return out, extend, replace
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
	// "focusguard block <dominio> --extend 30m" deixaria --extend sem efeito.
	// Extrai esses flags de qualquer posição antes do Parse.
	args, argExtend, argReplace := splitExtendReplaceArgs(args)
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
