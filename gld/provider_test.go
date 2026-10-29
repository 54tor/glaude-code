package main

import "testing"

func TestProviderClaude(t *testing.T) {
	p, ok := providerByID("claude")
	if !ok || p.Name() != "Claude Code" || p.ResumeCommand(id1) != "claude --resume "+id1 {
		t.Fatalf("%v %v", p, ok)
	}
	if c := p.Caps(); !c.KeepWarm || !c.HotCache || !c.Memory {
		t.Fatalf("%+v", c)
	}
	if _, ok := providerByID("inconnu"); ok {
		t.Fatal("fournisseur inconnu accepté")
	}
	if infos := providerInfos(); len(infos) != 1 || infos[0].ID != "claude" {
		t.Fatalf("%+v", infos)
	}
}

// Les sessions et la réponse de /api portent leur fournisseur ; les identifiants ne changent pas.
func TestSessionPorteSonFournisseur(t *testing.T) {
	h, _, _ := web(t)
	m := status(t, h, "GET", "/v1/state", "", 200)
	pv, _ := m["providers"].([]any)
	if len(pv) != 1 || pv[0].(map[string]any)["id"] != "claude" {
		t.Fatalf("%v", m["providers"])
	}
	g := m["groups"].([]any)[0].(map[string]any)["sessions"].([]any)[0].(map[string]any)
	if g["provider"] != "claude" || g["id"] != id1 {
		t.Fatalf("%v", g)
	}
}
