# Équipes d'agents

Les équipes d'agents natives de Claude Code, pilotées depuis la page `/equipes` de gld : un chef (Atlas) et des coéquipiers. Gld fournit un catalogue de 21 agents et 11 équipes. La composition lancée est l'union des équipes cochées, sans doublon.

Fonction expérimentale de Claude Code. Rien dans ce document n'a été vérifié de bout en bout, voir [Non vérifié](#non-vérifié).

## Lancer

`POST /teams-launch` (dossier absolu, objectif de 4000 caractères au plus, ids d'équipes) :

1. écrit les définitions d'agents des coéquipiers dans `<CLAUDE_CONFIG_DIR>/agents/<id>.md` ;
2. génère un uuid de session ; le nom d'équipe est `session-` + ses 8 premiers caractères ;
3. ouvre la session du chef dans tmux avec `--teammate-mode=tmux --session-id=<uuid>` et un prompt qui liste les coéquipiers à créer.

Le mode de permissions est celui par défaut de Claude Code : rien n'est approuvé automatiquement.

`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` est posée dans l'environnement du chef par le champ `tabSpec.Env`. Seul ce couple exact est admis (`applyTabEnv`, `term.go`), et `--args` ne peut jamais porter de variable (`matin.go`).

Un lanceur avec tmux est requis (`tmuxLauncher`, `claude.go`) ; sans lui le lancement est refusé. Sont retenus, dans cet ordre : `terminator-tmux` (`terminator` et `tmux` dans le PATH) ; sinon, si `tmux` est dans le PATH, le premier disponible parmi `kitty` (dans le PATH, `KITTY_LISTEN_ON` posée), `wezterm` (dans le PATH, `WEZTERM_PANE` posée), `gnome-terminal`, `konsole`, `xfce4-terminal`, `tilix` (dans le PATH), lancé avec tmux. Sans bureau sous le compte cible (`tmuxDetached`), aucun n'est retenu. Windows est refusé.

Chaque fichier d'agent commence, après le front matter, par la ligne `<!-- gld:managed -->`. Un fichier de même nom sans ce marqueur n'est jamais écrasé : l'identifiant est listé dans `conflicts` et le lancement est refusé.

Agents « lecture » (`read`) : outils `Read, Grep, Glob`. Agents « écriture » (`write`) : outils par défaut.

## Droits

| Droit | Routes |
|---|---|
| `sessions_read` | `GET /teams-catalog`, `POST /teams-compose`, `GET /teams`, `GET /team`, `GET /team-screen` |
| `exec` | `POST /teams-launch`, `POST /team-send`, `POST /team-key`, `POST /team-close` |
| `config_write` | `POST /teams-agent-save`, `POST /teams-agent-rm`, `POST /teams-team-save`, `POST /teams-team-rm` |

Table : `gld/auth_acl.go`.

## Messages et approbations

`POST /team-send` envoie un message direct au chef (par défaut) ou à un coéquipier. Il apparaît pour tous, avec son auteur humain.

- Envoyé tout de suite seulement si la cible est inactive et qu'aucun dialogue n'est en attente dans son pane (l'écran est recontrôlé sous verrou juste avant le collage). Sinon le message est mis en file : 20 au plus par équipe, l'ordre d'envoi par cible est conservé.
- Les demandes de permission d'un coéquipier s'affichent dans le pane du chef (titre `· from the <nom> agent`). `POST /team-key` y répond par une touche autorisée. Le serveur exige toujours un dialogue en attente dans le pane (`screenAsks`) : sans lui la touche est refusée. Le texte affiché (`expect`) n'est comparé à l'écran courant que si le client l'envoie non vide ; en cas de différence la touche est refusée. L'interface fournie envoie toujours `expect` (`teamKey(name, lead, k, view.Permission)`, `ui/src/components/teams/TeamRun.tsx`) et n'envoie rien quand aucune demande n'est affichée. Un client de l'API qui omet `expect` n'a pas la garde de texte.

Auteur d'un message : le nom du principal authentifié (`token` pour un jeton, `local` sans garde), jamais lu dans le corps. Sur un home enfant, la passerelle pose l'en-tête `X-Gld-User` après avoir effacé celui du client. Un enfant ne le croit qu'avec son jeton d'enfant valide (`teamAuthor`, `routes_teams.go` ; pose dans `homes.go`).

## Fichiers lus

Lecture seule, sans documentation officielle de Claude Code. Le lecteur tolère les écarts : un fichier illisible, trop gros (1 Mio), ou au format inattendu est signalé dans `Unknown` au lieu de faire échouer la vue.

- `<CLAUDE_CONFIG_DIR>/teams/<nom>/config.json` : membres, `leadSessionId`.
- `<CLAUDE_CONFIG_DIR>/teams/<nom>/inboxes/<membre>.json` (64 fichiers au plus).
- `<CLAUDE_CONFIG_DIR>/tasks/<nom>/` (200 fichiers au plus).

Constats du spike (Claude Code 2.1.311, `gld/testdata/teams/NOTES.md`) :

- Le pane du chef n'est pas dans `config.json` (`tmuxPaneId: "leader"`). Gld le retrouve par l'identifiant de session.
- Les boîtes sont vides au repos : Claude Code retire les messages lus. Gld garde en mémoire ceux qu'il a vus, 500 par équipe.
- Le format des tâches n'a pas été observé (dossier resté vide) : la liste peut rester vide.
- Un ancien dossier d'équipe peut survivre à sa session. `GET /teams` ne liste que les équipes dont la session du chef a un pane tmux.

## État gardé par gld

| Fichier | Contenu | Limites |
|---|---|---|
| `<CLAUDE_CONFIG_DIR>/gld-teams-journal/<équipe>.jsonl` | messages directs humains | 4 Mio, 500 derniers affichés, supprimé à la fermeture |
| `<CLAUDE_CONFIG_DIR>/gld-teams.json` | agents et équipes personnels | 200 agents, 100 équipes, prompt de 8000 octets |

Modifier un élément fourni crée une copie personnelle ; les éléments fournis ne sont jamais écrits. Un agent utilisé par une équipe ne peut pas être supprimé.

La file de messages et le log des boîtes sont en mémoire : perdus au redémarrage de gld. La file n'est vidée que lorsque quelqu'un interroge `GET /team`.

`POST /team-close` ferme la session du chef, puis supprime le journal, le log et la file de l'équipe.

## Limites de Claude Code

- Fonction expérimentale.
- Une équipe par session ; elle prend fin avec la session du chef.
- Pas d'équipes imbriquées.
- 3 à 5 coéquipiers conseillés. L'interface avertit au-delà de 5 (`tooMany`), sans bloquer.
- Le coût en jetons croît avec le nombre de coéquipiers.

## Non vérifié

À contrôler par l'opérateur :

- [ ] Claude Code accepte `--teammate-mode=tmux` sous la forme avec `=` (le spike a utilisé la forme à espace, que `--args` refuse).
- [ ] Les panes des coéquipiers héritent de la variable d'activation quand elle n'est posée que dans le processus du chef.
- [ ] Le titre de permission `<Outil> · from the <nom> agent` n'est connu que par le spike.
- [ ] `GET /teams` masque bien les équipes périmées : jamais vu dans un lancement réel.
- [ ] Lancement complet de l'équipe `feature` sur un dépôt jetable : un onglet tmux s'ouvre, le chef crée les coéquipiers, leurs panes apparaissent, `~/.claude/teams/session-<8hex>` existe.
