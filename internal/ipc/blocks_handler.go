package ipc

import (
	"context"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// block / block-all
// ---------------------------------------------------------------------------
//
// Os dois bloqueios rodam direto no scheduler (nunca nil — definido em
// NewServer), reproduzindo 1:1 o switch legado. O conflito ask-first não é
// erro: devolve a Response com Conflict:true + ConflictBlock (como o switch
// faz hoje), para o roteador a encodar como resposta normal.

// handleBlock bloqueia um domínio (ou um preset inteiro) por um período.
func (s *Server) handleBlock(_ context.Context, req *Request) (*Response, error) {
	d, err := time.ParseDuration(req.Duration)
	// d <= 0 também é rejeitado: um bloqueio de 0s expiraria imediatamente
	// (bloqueio sem efeito que ainda aplica/remove regras de firewall).
	if err != nil || d <= 0 {
		return nil, Err(CodeDurationInvalid, "Duration invalid. Ex: --duration 4h, 30m")
	}
	if req.Preset != "" {
		p, perr := s.catalog().Resolve(req.Preset)
		if perr != nil {
			return nil, perr
		}
		blocks, berr := s.scheduler.BlockDomains(p.Domains, d)
		if berr != nil {
			return nil, berr
		}
		if len(blocks) == 0 {
			// Defensivo: nunca indexar blocks[0] — evita pânico no servidor se
			// o scheduler um dia retornar sucesso sem blocos.
			return &Response{Success: true, Message: fmt.Sprintf("Preset %s: nenhum domínio novo bloqueado", p.Name)}, nil
		}
		return &Response{
			Success: true,
			Message: fmt.Sprintf("Preset %s bloqueado (%d domínios) até %s", p.Name, len(blocks), blocks[0].ExpiresAt.Local().Format("15:04:05 02/01/2006")),
		}, nil
	}
	if req.Domain == "" {
		return nil, Err(CodeDomainRequired, "Informe um domínio ou --preset para bloquear.")
	}
	// --extend: soma a duração ao bloqueio ativo (ou cria um novo se não
	// houver). Não passa pela detecção de conflito.
	if req.Extend {
		block, err := s.scheduler.ExtendBlock(req.Domain, d)
		if err != nil {
			return nil, err
		}
		return &Response{Success: true, Message: fmt.Sprintf("Domain %s extended until %s", block.Domain, block.ExpiresAt.Local().Format("15:04:05 02/01/2006"))}, nil
	}
	// Comportamento padrão (ask-first): um domínio já bloqueado é um
	// CONFLITO a ser resolvido pelo usuário (somar/substituir), não um
	// sobrescrita silenciosa. --replace pula o conflito e reinicia a janela.
	if !req.Replace {
		if existing := s.scheduler.ActiveBlock(req.Domain); existing != nil {
			return &Response{
				Success:       false,
				Code:          CodeDomainConflict,
				Conflict:      true,
				ConflictBlock: existing,
				Message: fmt.Sprintf("Domínio já bloqueado até %s. Use --extend para somar ou --replace para reiniciar.",
					existing.ExpiresAt.Local().Format("15:04:05 02/01/2006")),
			}, nil
		}
	}
	block, err := s.scheduler.Block(req.Domain, d)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Domain %s blocked  %s", block.Domain, block.ExpiresAt.Local().Format("15:04:05 02/01/2006"))}, nil
}

// handleBlockAll bloqueia toda a internet (panic mode) ou tudo exceto a
// allowlist (deep-focus mode).
func (s *Server) handleBlockAll(_ context.Context, req *Request) (*Response, error) {
	d, err := time.ParseDuration(req.Duration)
	if err != nil || d <= 0 {
		return nil, Err(CodeDurationInvalid, "Duration invalid. Ex: --duration 4h, 30m")
	}
	block, err := s.scheduler.BlockAllInternet(req.Allowlist, d)
	if err != nil {
		return nil, err
	}
	return &Response{
		Success: true,
		Message: fmt.Sprintf("Internet bloqueada até %s%s", block.ExpiresAt.Local().Format("15:04:05 02/01/2006"), blockAllModeSuffix(req.Allowlist)),
	}, nil
}
