//go:build windows

package tray

import "testing"

// O binário de teste (tray.test.exe) não embute resource.syso, então não há
// RT_GROUP_ICON disponível: esses testes cobrem o comportamento sem recursos
// embutidos (não há mais fallback de renderização em runtime).

func TestEmbeddedIcon_ErrorWhenNoResources(t *testing.T) {
	if _, err := embeddedIcon(); err == nil {
		t.Error("esperava erro ao procurar ícone embutido no binário de teste (sem recursos)")
	}
}

func TestPlatformIcon_NoEmbeddedReturnsNil(t *testing.T) {
	if data := platformIcon(); data != nil {
		t.Errorf("esperava nil sem ícone embutido, got %d bytes", len(data))
	}
}
