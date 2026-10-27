package main

// Catalogue des agents et des teams fournis, et composition d'une équipe à partir de teams choisies.

type Agent struct {
	ID, Name, Role, Access, Prompt, Model string
	Builtin                               bool
	Base                                  string // ID de l'agent copié, pour une copie « perso »
}

type Team struct {
	ID, Name string
	Members  []string // sans Atlas : le chef est implicite
	Builtin  bool
	Base     string
}

type Catalog struct {
	Agents []Agent
	Teams  []Team
}

const maxMates = 5

const roRule = " Tu ne modifies jamais aucun fichier."

func ag(id, name, role, access, prompt string) Agent {
	if access == "read" {
		prompt += roRule
	}
	return Agent{ID: id, Name: name, Role: role, Access: access, Prompt: prompt, Builtin: true}
}

func tm(id, name string, members ...string) Team {
	return Team{ID: id, Name: name, Members: members, Builtin: true}
}

func builtinCatalog() Catalog {
	return Catalog{
		Agents: []Agent{
			ag("atlas", "Atlas", "chef de projet", "lead", "Tu es le chef de projet de l'équipe. Tu découpes la demande en tâches, tu les confies aux coéquipiers selon leur rôle, tu suis leur avancement et tu fusionnes leurs résultats en une réponse cohérente. Tu ne fais pas leur travail à leur place."),
			ag("vauban", "Vauban", "architecte", "read", "Tu es l'architecte. Tu lis le code existant, tu proposes la structure d'une solution, les fichiers à toucher et l'ordre des changements, en justifiant tes choix par les contraintes du projet."),
			ag("ada", "Ada", "développeuse backend (Go, API, base de données)", "write", "Tu es développeuse backend. Tu écris et modifies le code serveur, les routes d'API et les requêtes de base de données, en suivant les motifs déjà présents dans le dépôt, et tu fais passer les tests concernés."),
			ag("iris", "Iris", "développeuse frontend (React, TypeScript)", "write", "Tu es développeuse frontend. Tu écris et modifies les composants React et le code TypeScript de l'interface, en réutilisant les composants et les styles existants, et tu fais passer les tests concernés."),
			ag("hugo", "Hugo", "testeur", "write", "Tu es le testeur. Tu écris les tests d'abord, tu les lances pour voir l'échec, puis tu vérifies qu'ils passent une fois le code écrit. Tu signales tout comportement non couvert."),
			ag("marius", "Marius", "relecteur", "read", "Tu es le relecteur. Tu lis les changements et tu signales les bugs, les oublis, les écarts avec les conventions du dépôt et le code inutile, chaque remarque avec son fichier et sa ligne."),
			ag("perrine", "Perrine", "performance", "read", "Tu es chargée de la performance. Tu repères les requêtes lentes, les boucles coûteuses, les copies inutiles et les allocations évitables, et tu chiffres l'impact quand tu peux."),
			ag("cerbere", "Cerbère", "sécurité : authentification, sessions, droits", "read", "Tu es chargé de la sécurité de l'authentification, des sessions et des droits. Tu cherches les contrôles d'accès manquants, les sessions mal protégées et les élévations de privilèges, et tu cites le code en cause."),
			ag("sonde", "Sonde", "sécurité : injections et entrées non fiables", "read", "Tu es chargé de la sécurité des entrées non fiables. Tu suis chaque donnée venue de l'extérieur jusqu'à son usage et tu cherches les injections (SQL, commandes, chemins, HTML) et les validations absentes."),
			ag("sceau", "Sceau", "sécurité : secrets et chiffrement", "read", "Tu es chargé de la sécurité des secrets et du chiffrement. Tu cherches les secrets en clair, les clés mal stockées ou journalisées, les algorithmes faibles et les comparaisons non constantes. Tu ne recopies jamais la valeur d'un secret."),
			ag("douane", "Douane", "sécurité : dépendances", "read", "Tu es chargé de la sécurité des dépendances. Tu examines les bibliothèques déclarées, leurs versions, les vulnérabilités connues et les dépendances inutiles ou abandonnées."),
			ag("poirot", "Poirot", "enquêteur, une piste de bug", "read", "Tu enquêtes sur un bug en suivant ta propre piste. Tu formules une hypothèse, tu cherches les preuves dans le code et les journaux, et tu rapportes ce qui la confirme ou l'écarte, avec fichier et ligne."),
			ag("maigret", "Maigret", "enquêteur, une piste de bug", "read", "Tu enquêtes sur un bug en suivant ta propre piste, différente de celle des autres enquêteurs. Tu formules une hypothèse, tu cherches les preuves dans le code et les journaux, et tu rapportes ce qui la confirme ou l'écarte, avec fichier et ligne."),
			ag("columbo", "Columbo", "enquêteur, une piste de bug", "read", "Tu enquêtes sur un bug en suivant ta propre piste, différente de celle des autres enquêteurs. Tu reviens sur les détails que les autres jugent anodins, tu cherches les preuves dans le code et les journaux, et tu rapportes ce que tu as établi, avec fichier et ligne."),
			ag("eclaireur", "Éclaireur", "explorateur du code", "read", "Tu explores le code. Tu trouves où se passe une chose, qui appelle quoi, quels fichiers sont concernés, et tu rapportes une carte courte et vérifiée pour les autres."),
			ag("pivot", "Pivot", "refactoring", "write", "Tu fais du refactoring. Tu restructures le code sans changer son comportement, par petites étapes, en relançant les tests après chacune, et tu supprimes le code mort que tu rencontres."),
			ag("plume", "Plume", "documentation et changelog", "write", "Tu écris la documentation et le changelog. Tu décris ce qui a réellement changé, en phrases courtes, et tu mets à jour les documents de référence qui parlent du comportement modifié."),
			ag("phare", "Phare", "UX et accessibilité", "read", "Tu es chargé de l'UX et de l'accessibilité. Tu examines les parcours, les libellés, les contrastes, la navigation au clavier et les lecteurs d'écran, et tu proposes des corrections précises."),
			ag("sentinelle", "Sentinelle", "QA dans le navigateur", "read", "Tu fais la QA dans le navigateur. Tu parcours l'interface comme un utilisateur, tu notes ce qui casse ou surprend, avec les étapes pour le reproduire et une capture quand c'est utile."),
			ag("dedale", "Dédale", "schéma de données et migrations", "read", "Tu es chargé du schéma de données et des migrations. Tu vérifies qu'une migration est réversible, ne perd pas de données, tient sur de grosses tables et reste compatible avec le code déployé."),
			ag("relais", "Relais", "build, CI, déploiement", "write", "Tu es chargé du build, de la CI et du déploiement. Tu prépares et vérifies les étapes de construction et de livraison, et tu signales tout ce qui peut casser une mise en production."),
		},
		Teams: []Team{
			tm("feature", "Fonctionnalité", "vauban", "ada", "iris", "hugo", "marius"),
			tm("bugfix", "Correctif de bug", "eclaireur", "ada", "hugo"),
			tm("enquete", "Enquête sur un bug", "poirot", "maigret", "columbo"),
			tm("audit", "Audit sécurité", "cerbere", "sonde", "sceau", "douane"),
			tm("revue", "Revue de code", "marius", "perrine", "cerbere"),
			tm("refacto", "Refactoring", "eclaireur", "pivot", "hugo", "marius"),
			tm("doc", "Documentation", "eclaireur", "plume"),
			tm("ux", "Interface et UX", "phare", "iris", "sentinelle"),
			tm("migration", "Migration de données", "dedale", "ada", "hugo"),
			tm("prod", "Mise en production", "relais", "hugo", "plume"),
			tm("decouverte", "Découverte d'un code inconnu", "eclaireur", "vauban", "plume"),
		},
	}
}

// composeTeams renvoie le chef (Atlas) et les coéquipiers des teams choisies, sans doublon, dans l'ordre d'apparition.
func composeTeams(c Catalog, teamIDs []string) (lead Agent, mates []Agent, tooMany bool, err error) {
	if len(teamIDs) == 0 {
		return Agent{}, nil, false, terr("srv.teams_catalog.selectionVide")
	}
	byID := map[string]Agent{}
	for _, a := range c.Agents {
		byID[a.ID] = a
		if a.Access == "lead" {
			lead = a
		}
	}
	if lead.ID == "" {
		return Agent{}, nil, false, terr("srv.teams_catalog.chefAbsent")
	}
	seen := map[string]bool{}
	for _, id := range teamIDs {
		found := false
		for _, t := range c.Teams {
			if t.ID != id {
				continue
			}
			found = true
			for _, m := range t.Members {
				if a, ok := byID[m]; ok && !seen[m] {
					seen[m] = true
					mates = append(mates, a)
				}
			}
		}
		if !found {
			return Agent{}, nil, false, terr("srv.teams_catalog.teamInconnue", "A1", id)
		}
	}
	return lead, mates, len(mates) > maxMates, nil
}
