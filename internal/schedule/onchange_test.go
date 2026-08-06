package schedule

import "testing"

// TestManager_SetOnChange_NotifiesOnMutation verifica o hook de mudança
// (Fase 7 — event hub): cada mutação bem-sucedida do catálogo (Add/Remove)
// dispara o callback registrado, e mutações que falham não notificam.
func TestManager_SetOnChange_NotifiesOnMutation(t *testing.T) {
	m := NewManager("") // em memória — save() é no-op

	calls := 0
	m.SetOnChange(func() { calls++ })

	r, err := m.Add(Rule{Preset: "social", Days: []int{1}, Start: "08:00", End: "12:00"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := m.Remove(r.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if calls != 2 {
		t.Errorf("SetOnChange chamado %d vezes, esperava 2 (Add+Remove)", calls)
	}

	// Mutação que falha não notifica: remover um ID inexistente é erro.
	if err := m.Remove("nope"); err == nil {
		t.Error("Remove de ID inexistente deveria falhar")
	}
	if calls != 2 {
		t.Errorf("falha no Remove notificou: %d chamadas", calls)
	}
}
