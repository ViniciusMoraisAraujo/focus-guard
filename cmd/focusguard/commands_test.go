package main

import "testing"

// TestCommandTable_ConsistentWithUsage fecha o gap OCP da tabela de comandos:
// um comando registrado na map com Usage DEVE aparecer na usageOrder (senão
// some do help em silêncio), e todo nome da usageOrder DEVE existir na map.
// Também garante que Name bate com a chave e que nenhum Run está nil.
func TestCommandTable_ConsistentWithUsage(t *testing.T) {
	for _, name := range usageOrder {
		c, ok := commands[name]
		if !ok {
			t.Errorf("usageOrder %q não está na tabela commands (comando some do help)", name)
			continue
		}
		if c.Name != name {
			t.Errorf("Name=%q não bate com a chave %q", c.Name, name)
		}
		if len(c.Usage) == 0 {
			t.Errorf("comando %q na usageOrder sem Usage (help vazio)", name)
		}
		if c.Run == nil {
			t.Errorf("comando %q com Run nil (dispatch quebra)", name)
		}
	}

	for name, c := range commands {
		if c.Run == nil {
			t.Errorf("comando %q com Run nil", name)
		}
		if len(c.Usage) == 0 {
			continue // alias sem entrada no help (ex.: missions)
		}
		found := false
		for _, n := range usageOrder {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("comando %q tem Usage mas não está na usageOrder — some do help", name)
		}
	}
}
