package main

// Fournisseurs d'agents de code (Claude Code aujourd'hui ; Codex, Gemini… plus tard). Un fournisseur sait lire ses sessions et dire ce
// qu'il sait faire (Caps) ; tout ce qui est commun (arbre, groupes, projets, recherche, git, terminal) ne dépend que de ces deux
// choses. Pour l'instant Claude est le seul fournisseur et le reste du code l'appelle encore directement : cette interface fixe le
// vocabulaire et sert déjà à l'interface web, qui n'affiche une fonction que si le fournisseur de la session la déclare.

// Caps : ce qu'un fournisseur sait faire. L'interface masque tout ce qui n'est pas déclaré.
type Caps struct {
	HotCache   bool `json:"hotCache"`   // cache de prompts à durée connue (« cache chaud »)
	KeepWarm   bool `json:"keepWarm"`   // ping automatique pour garder ce cache
	Config     bool `json:"config"`     // réglages, permissions, hooks lisibles et modifiables
	Memory     bool `json:"memory"`     // fichiers d'instructions (CLAUDE.md, rules…)
	TUIPrompts bool `json:"tuiPrompts"` // questions du terminal détectables et auxquelles répondre
	Resume     bool `json:"resume"`     // reprendre une session par son identifiant
}

type ProviderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Caps Caps   `json:"caps"`
}

type Provider interface {
	ID() string
	Name() string
	Caps() Caps
	Sessions() []Group                          // sessions groupées par dossier de travail
	Transcript(dir, id string) (string, bool)   // chemin du transcript d'une session
	Conversation(path string, tools bool) []Msg // messages d'un transcript
	ResumeCommand(id string) string             // commande qui reprend la session, affichée à l'utilisateur
}

// providerClaude est le fournisseur Claude Code : il délègue aux fonctions existantes (transcript.go, chat.go…).
type providerClaude struct{}

const claudeID = "claude"

func (providerClaude) ID() string   { return claudeID }
func (providerClaude) Name() string { return "Claude Code" }
func (providerClaude) Caps() Caps {
	return Caps{HotCache: true, KeepWarm: true, Config: true, Memory: true, TUIPrompts: true, Resume: true}
}
func (providerClaude) Sessions() []Group                          { return sessions() }
func (providerClaude) Transcript(dir, id string) (string, bool)   { return transcript(dir, id) }
func (providerClaude) Conversation(path string, tools bool) []Msg { return conv(path, tools) }
func (providerClaude) ResumeCommand(id string) string             { return "claude --resume " + id }

// providers : les fournisseurs connus, dans l'ordre d'affichage. Un nouveau fournisseur s'ajoute ici.
var providers = []Provider{providerClaude{}}

func providerByID(id string) (Provider, bool) {
	for _, p := range providers {
		if p.ID() == id {
			return p, true
		}
	}
	return nil, false
}

func providerInfos() []ProviderInfo {
	out := make([]ProviderInfo, 0, len(providers))
	for _, p := range providers {
		out = append(out, ProviderInfo{p.ID(), p.Name(), p.Caps()})
	}
	return out
}
