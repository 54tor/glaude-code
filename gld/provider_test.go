package main

import "testing"

func TestProviderClaude(t *testing.T) {
	provider, ok := providerByID("claude")
	if !ok || provider.Name() != "Claude Code" || provider.ResumeCommand(id1) != "claude --resume "+id1 {
		t.Fatalf("%v %v", provider, ok)
	}
	if caps := provider.Caps(); !caps.KeepWarm || !caps.HotCache || !caps.Memory {
		t.Fatalf("%+v", caps)
	}
	if _, ok := providerByID("inconnu"); ok {
		t.Fatal("fournisseur inconnu accepté")
	}
	if infos := providerInfos(); len(infos) != 1 || infos[0].ID != "claude" {
		t.Fatalf("%+v", infos)
	}
}

// Les sessions et la réponse de /v1/state portent leur fournisseur ; les identifiants ne changent pas.
func TestSessionPorteSonFournisseur(t *testing.T) {
	handler, _, _ := web(t)
	lookup := status(t, handler, "GET", "/v1/state", "", 200)
	pv, _ := lookup["providers"].([]any)
	if len(pv) != 1 || pv[0].(map[string]any)["id"] != "claude" {
		t.Fatalf("%v", lookup["providers"])
	}
	localLookup := lookup["groups"].([]any)[0].(map[string]any)["sessions"].([]any)[0].(map[string]any)
	if localLookup["provider"] != "claude" || localLookup["id"] != id1 {
		t.Fatalf("%v", localLookup)
	}
}
