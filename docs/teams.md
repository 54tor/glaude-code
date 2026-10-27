# Équipes d'agents

Les équipes d'agents natives de Claude Code, pilotées depuis la page `/equipes` de gld : un chef de projet et des coéquipiers. Gld fournit un catalogue de 21 agents et 11 équipes. Un agent du catalogue a un identifiant de rôle (`testeur`, `chef`…) et un libellé de rôle (« Testeur », « Chef de projet »), sans prénom. La composition lancée est l'union des équipes cochées, sans doublon.

Fonction expérimentale de Claude Code. Rien dans ce document n'a été vérifié de bout en bout, voir [Non vérifié](#non-vérifié).

## Lancer

`POST /teams-launch` (dossier absolu, objectif de 4000 caractères au plus, ids d'équipes) :

1. tire un prénom par coéquipier avec `gofakeit` (32 coéquipiers au plus par lancement) ;
2. écrit les définitions d'agents des coéquipiers dans `<CLAUDE_CONFIG_DIR>/agents/<id>.md`, `<id>` étant l'identifiant de rôle ;
3. génère un uuid de session ; le nom d'équipe est `session-` + ses 8 premiers caractères ;
4. écrit le roster (prénom -> identifiant de rôle) dans `<CLAUDE_CONFIG_DIR>/gld-teams-journal/<équipe>.roster.json`, avant le lancement ;
5. ouvre la session du chef dans tmux avec `--teammate-mode=tmux --session-id=<uuid>` et un prompt qui liste les coéquipiers à créer.

Prénoms : tirés à chaque lancement, nom d'exécution du coéquipier = 2 à 12 lettres ASCII minuscules, distincts entre eux, des identifiants de rôle et de `team-lead`. Le prompt nomme le fichier d'agent par son identifiant de rôle et le coéquipier par son prénom. La réponse de `POST /teams-launch` liste `Mates` : `{ID, Name}` (identifiant de rôle, prénom). L'interface affiche « Prénom · Libellé de rôle » (le chef : « Chef de projet ») ; les envois, touches et écrans gardent le nom brut. Sans roster (équipe lancée hors gld, fichier absent ou illisible), seul le prénom est affiché.

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
- Les boîtes sont vides au repos : Claude Code retire les messages lus. Gld garde en mémoire ceux qu'il a vus, 500 par équipe. Une notification `idle_notification` n'est gardée que si son champ `result` n'est pas vide : ce texte (compte rendu du coéquipier au chef) s'affiche comme un message de son auteur.
- Le format des tâches n'a pas été observé (dossier resté vide) : la liste peut rester vide.
- Un ancien dossier d'équipe peut survivre à sa session. `GET /teams` ne liste que les équipes dont la session du chef a un pane tmux.

## État gardé par gld

| Fichier | Contenu | Limites |
|---|---|---|
| `<CLAUDE_CONFIG_DIR>/gld-teams-journal/<équipe>.jsonl` | messages directs humains | 4 Mio, 500 derniers affichés, supprimé à la fermeture |
| `<CLAUDE_CONFIG_DIR>/gld-teams-journal/<équipe>.roster.json` | prénom -> identifiant de rôle | 64 entrées, 64 Kio, supprimé à la fermeture et si le lancement échoue |
| `<CLAUDE_CONFIG_DIR>/gld-teams.json` | agents et équipes personnels | 200 agents, 100 équipes, prompt de 8000 octets |

Le roster est un second état ajouté par gld à côté du journal, écrit avant le lancement. Un lancement interrompu avant sa suppression peut laisser un petit fichier orphelin.

Modifier un élément fourni crée une copie personnelle ; les éléments fournis ne sont jamais écrits. Un agent utilisé par une équipe ne peut pas être supprimé.

La file de messages et le log des boîtes sont en mémoire : perdus au redémarrage de gld. La file n'est vidée que lorsque quelqu'un interroge `GET /team`.

`POST /team-close` ferme la session du chef, puis supprime le journal, le roster, le log et la file de l'équipe. Si le chef a déjà quitté (équipe terminée), il n'y a rien à fermer : le nettoyage a lieu quand même, et le bouton reste actif. Si tmux est injoignable, `team-send`, `team-key`, `team-screen` et `team-close` répondent « tmux injoignable » (et non « team inactive ») ; `GET /team` et `GET /teams` répondent normalement, équipe non active.

Agent : le champ `Model` se limite à 64 caractères parmi `A-Za-z0-9._-` (vide admis). Un lancement dont la sortie contient `unknown option`, `unknown argument` ou `unrecognized` est signalé « indisponible : cette version de claude ne gère pas les teams ». L'écran affiche « Répondre dans le terminal » quand le chef attend (ou n'est pas lisible) sans demande lisible.

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
- [ ] `claude agents --json` liste la session démarrée avec un `--session-id` explicite (kind interactive, pid dans le pane) ; sinon `sendable()` ne trouve pas le chef et toute team s'affiche non active.
- [ ] `isActive:false` dans `config.json` veut dire « coéquipier vivant mais au repos » ; s'il veut dire « parti », les messages aux coéquipiers restent en file.
- [ ] SIGTERM du chef ferme les panes des coéquipiers ; sinon des panes tmux orphelins restent.
- [ ] Échap sur la demande d'un coéquipier (option 3, « No, and tell Claude what to do differently ») laisse le chef en attente d'instructions : vérifier que l'écran le montre alors au repos.
- [ ] Claude Code accepte un prénom tiré en minuscules comme nom d'exécution d'un agent défini par fichier. Les lancements enregistrés ont utilisé `ada`, `hugo`, `zoe`, saisis par le chef dans le prompt, jamais tirés.
- [ ] Les coéquipiers démarrés depuis `agents/<id>.md` respectent `tools: Read, Grep, Glob`.
