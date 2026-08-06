package presets

import (
	"context"
	"testing"

	"focusguard/internal/ipc"
	"focusguard/internal/preset"
)

type fakeCatalog struct {
	list []preset.Preset
}

func (f *fakeCatalog) List() []preset.Preset { return f.list }

func (f *fakeCatalog) Add(p preset.Preset) error { f.list = append(f.list, p); return nil }

func (f *fakeCatalog) Remove(name string) error {
	var out []preset.Preset
	for _, p := range f.list {
		if p.Name != name {
			out = append(out, p)
		}
	}
	f.list = out
	return nil
}

func TestPresetsList_OK(t *testing.T) {
	h := NewList(&fakeCatalog{list: []preset.Preset{{Name: "social"}}})
	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "presets"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success || len(resp.Presets) != 1 {
		t.Fatalf("esperava 1 preset, got %+v", resp)
	}
}

func TestPresetAdd_OK(t *testing.T) {
	c := &fakeCatalog{}
	h := NewAdd(c)
	resp, err := h.Handle(context.Background(), &ipc.Request{
		Action: "preset-add", PresetName: "trabalho", PresetDomains: []string{"jira.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("esperava sucesso, got %+v", resp)
	}
	if resp.Message != "Preset trabalho criado (1 domínios)" {
		t.Fatalf("mensagem inesperada: %q", resp.Message)
	}
	if len(c.list) != 1 || c.list[0].Name != "trabalho" {
		t.Fatalf("Add não foi chamado com o preset, got %+v", c.list)
	}
}

func TestPresetAdd_SemCatalog(t *testing.T) {
	h := NewAdd(nil)
	_, err := h.Handle(context.Background(), &ipc.Request{Action: "preset-add", PresetName: "x"})
	if err == nil {
		t.Fatal("esperava erro de catálogo ausente")
	}
}

func TestPresetRemove_OK(t *testing.T) {
	c := &fakeCatalog{list: []preset.Preset{{Name: "trabalho"}}}
	h := NewRemove(c)
	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "preset-remove", PresetName: "trabalho"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("esperava sucesso, got %+v", resp)
	}
	if len(c.list) != 0 {
		t.Fatalf("Remove não removeu, got %+v", c.list)
	}
}
