package main

import "testing"

func TestSubagentsFromEventsTracksStartAndStop(t *testing.T) {
	events := []stateEvent{
		{Session: "s1", Event: "SessionStart", Subagents: unknownSubCount},
		{Session: "s1", Event: "SubagentStart", Agent: "a1", Subagents: unknownSubCount},
		{Session: "s2", Event: "SubagentStart", Agent: "b1", Subagents: unknownSubCount},
	}
	if running, known := subagentsFromEvents(events, "s1"); !running || !known {
		t.Fatalf("valeurAttendue running=true known=true, valeurObtenue running=%v known=%v", running, known)
	}
	events = append(events, stateEvent{Session: "s1", Event: "SubagentStop", Agent: "a1", Subagents: unknownSubCount})
	if running, _ := subagentsFromEvents(events, "s1"); running {
		t.Fatal("sous-agent arrêté encore compté")
	}
	if _, known := subagentsFromEvents(events, "inconnue"); known {
		t.Fatal("session sans événement prise pour connue")
	}
}

func TestSubagentsFromEventsClearedByStopListingNone(t *testing.T) {
	events := []stateEvent{
		{Session: "s1", Event: "SubagentStart", Agent: "a1", Subagents: unknownSubCount},
		{Session: "s1", Event: "Stop", Subagents: 0},
	}
	if running, _ := subagentsFromEvents(events, "s1"); running {
		t.Fatal("compteur bloqué non remis à zéro par un Stop sans sous-agent")
	}
}
