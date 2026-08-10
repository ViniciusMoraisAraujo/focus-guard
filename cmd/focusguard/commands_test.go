package main

import "testing"

// TestSplitBlockFlags_DurationAnywhere cobre o bug real: "block <dominio>
// --duration 10m" falhava com "Duration invalid" porque o flag package do Go
// para no primeiro argumento posicional e --duration virava Arg(1). O split
// agora extrai a duração (e --extend/--replace) de qualquer posição.
func TestSplitBlockFlags_DurationAnywhere(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantOut []string
		wantExt bool
		wantRep bool
		wantDur string
	}{
		{
			name:    "duration depois do dominio (o bug)",
			args:    []string{"youtube.com", "--duration", "10m"},
			wantOut: []string{"youtube.com"},
			wantDur: "10m",
		},
		{
			name:    "duration antes do dominio",
			args:    []string{"--duration", "10m", "youtube.com"},
			wantOut: []string{"youtube.com"},
			wantDur: "10m",
		},
		{
			name:    "duration com sinal de igual",
			args:    []string{"youtube.com", "--duration=1h30m"},
			wantOut: []string{"youtube.com"},
			wantDur: "1h30m",
		}, {
			name:    "shorthand -d",
			args:    []string{"youtube.com", "-d", "30m"},
			wantOut: []string{"youtube.com"},
			wantDur: "30m",
		},
		{
			name:    "traco simples -duration",
			args:    []string{"youtube.com", "-duration", "30m"},
			wantOut: []string{"youtube.com"},
			wantDur: "30m",
		},
		{
			name:    "traco simples -duration com igual",
			args:    []string{"youtube.com", "-duration=45m"},
			wantOut: []string{"youtube.com"},
			wantDur: "45m",
		},
		{
			name:    "tudo junto posicional + flags no fim",
			args:    []string{"twitter.com", "--duration", "30m", "--extend"},
			wantOut: []string{"twitter.com"},
			wantExt: true,
			wantDur: "30m",
		},
		{
			name:    "replace no fim",
			args:    []string{"twitter.com", "--replace", "--duration=4h"},
			wantOut: []string{"twitter.com"},
			wantRep: true,
			wantDur: "4h",
		},
		{
			name:    "sem flags",
			args:    []string{"youtube.com", "30m"},
			wantOut: []string{"youtube.com", "30m"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, ext, rep, dur := splitBlockFlags(tt.args)
			if len(out) != len(tt.wantOut) {
				t.Fatalf("out = %v, want %v", out, tt.wantOut)
			}
			for i := range out {
				if out[i] != tt.wantOut[i] {
					t.Fatalf("out[%d] = %q, want %q", i, out[i], tt.wantOut[i])
				}
			}
			if ext != tt.wantExt || rep != tt.wantRep || dur != tt.wantDur {
				t.Errorf("extend=%v replace=%v duration=%q — want %v/%v/%q",
					ext, rep, dur, tt.wantExt, tt.wantRep, tt.wantDur)
			}
		})
	}
}

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
