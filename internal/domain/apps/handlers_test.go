package apps

import (
	"context"
	"testing"

	"focusguard/internal/transport/ipc"
)

type fakeStore struct {
	list []string
}

func (f *fakeStore) List() []string { return f.list }

func (f *fakeStore) Add(name string) error { f.list = append(f.list, name); return nil }

func (f *fakeStore) Remove(name string) error {
	var out []string
	for _, x := range f.list {
		if x != name {
			out = append(out, x)
		}
	}
	f.list = out
	return nil
}

func TestAppsList_OK(t *testing.T) {
	h := NewList(&fakeStore{list: []string{"steam"}})
	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "apps-list"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success || len(resp.Apps) != 1 {
		t.Fatalf("esperava 1 app, got %+v", resp)
	}
}

func TestAppsList_SemStore(t *testing.T) {
	h := NewList(nil)
	_, err := h.Handle(context.Background(), &ipc.Request{Action: "apps-list"})
	if err == nil {
		t.Fatal("esperava erro de denylist não configurada")
	}
}

func TestAppsAdd_OK(t *testing.T) {
	st := &fakeStore{}
	h := NewAdd(st)
	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "apps-add", AppName: "discord.exe"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("esperava sucesso, got %+v", resp)
	}
	if len(st.list) != 1 || st.list[0] != "discord.exe" {
		t.Fatalf("Add não foi chamado, got %+v", st.list)
	}
}

func TestAppsRemove_OK(t *testing.T) {
	st := &fakeStore{list: []string{"discord.exe"}}
	h := NewRemove(st)
	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "apps-remove", AppName: "discord.exe"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success || len(st.list) != 0 {
		t.Fatalf("esperava sucesso + lista vazia, got resp=%+v list=%v", resp, st.list)
	}
}
