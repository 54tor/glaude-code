# Changelog

Format : [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/), versions : [SemVer](https://semver.org/lang/fr/).

## [Non publié]

### Corrigé
- Fusion de mémoires ou de snippets lancée dans un dossier non approuvé : la modale « faire confiance à ce dossier » s'affiche maintenant, au lieu de laisser la session attendre dans un terminal détaché.

### Modifié
- Règles livrées par gld : elles ne sont plus écrites dans `~/.claude/rules` (chargé par toutes les sessions) mais transmises en prompt système (`--append-system-prompt-file`) aux seules sessions lancées par gld, chat et terminal. Réglage « Règles gld » dans Configuration pour les couper ; lecture seule dans la Mémoire. Les anciennes copies marquées « Managed by gld » sont supprimées au démarrage.
- Notification quand Claude Code installé n'est pas à jour (comparé à la dernière version npm) ; `GET /v1/update` renvoie `claudeCurrent`, `claudeLatest` et `claudeNewer`.
- Titre automatique d'une session ouverte : il est reporté à la fermeture du processus au lieu d'être refusé.

## [0.8.0] - 2026-10-30

### Ajouté
- Page Plugins dédiée (`/plugins`), avec ses entrées dans la barre latérale.
- Sauvegarde (onglet « Export » de Configuration ; `/sauvegarde` y redirige) : exporter la configuration Claude en zip (skills, commandes et snippets, plugins et marketplaces, réglages et permissions, mémoire et serveurs MCP) et l'importer par catégories, avec un aperçu avant application. Les secrets sont exclus de l'export.
- Mémoire : exporter une entrée, une sélection ou toutes les entrées de mémoire des projets en JSON, et les importer en choisissant le projet cible de chaque groupe et la résolution de chaque conflit de nom.
- Accueil (`/accueil`) : sessions ouvertes (celles qui attendent d'abord), dernières fermées encore au cache, état des serveurs MCP et mise à jour disponible.
- Palette de commandes (Ctrl+K) : aller à une page, une session, un skill ou un réglage.
- Extensions : un onglet par type (Plugins, Skills, Hooks, MCP, Éditeur) avec compteurs, et une seule recherche par onglet qui couvre les installés et le catalogue (marketplaces, npx). Les variables d'environnement des serveurs MCP sont affichées.
- Configuration : onglets regroupés en trois groupes (Claude Code, Diagnostic, Glaude Code), recherche d'un réglage par nom dans l'onglet Claude CLI, onglet « Sauvegardes » renommé « Historique ». Les onglets Hooks et MCP, doublons d'Extensions, sont retirés.
- Session partagée en invité : badge « Lecture seule » dans l'en-tête quand l'accès ne permet pas d'écrire.
- Gestes tactiles : balayer depuis le bord gauche ouvre le menu sur petit écran, ou referme la session ouverte ; balayer le menu vers la gauche le referme ; tirer une liste de sessions vers le bas la recharge.
- Réponses `/btw` affichées dans le panneau de la session, avec copie, bifurcation et fermeture ; la surcouche est fermée avant l'envoi d'un message.
- Notification quand une version plus récente est publiée sur GitHub.
- Pendant un outil en cours, libellés d'attente variés (1000 statuts par langue), tirés au hasard par créneau de 6 secondes.
- Alerte (toast) quand le cache de prompt d'une session ouverte est sur le point d'expirer.
- Règle intégrée qui range les tâches à faire dans le bloc-notes de gld.
- IDE : l'éditeur Monaco s'ouvre en lecture seule sur écran tactile.
- Notes : historique par note des éléments envoyés à une session.
- Session : modèle affiché dans l'en-tête, état du Remote control dans la fenêtre d'informations, globe vert sur les sessions liées à claude.ai, titre automatique après le premier message (un titre choisi n'est jamais écrasé).
- Accueil : bouton « Vérifier » pour interroger GitHub tout de suite (au plus une fois toutes les 30 s) ; la version courante et le lien de mise à jour s'affichent en pied de barre latérale.
- Barre latérale : sections « Projets » et « Local » repliables, sessions tièdes (fermées mais au cache chaud) séparées des sessions ouvertes, projets et dossiers triés par nom.
- Les listes de sessions se trient sur le dernier message de l'utilisateur et non sur la dernière écriture, qui bouge aussi à chaque réponse de l'agent.
- Configuration : bouton « Lancer /doctor » ; l'onglet des réglages s'appelle « Claude CLI ».
- Snippets : lancer, modifier, améliorer, renommer et supprimer depuis le menu contextuel d'une ligne.
- Police « Comic Sans (Comic Relief) » ; la taille de police choisie agrandit toute l'interface et non plus seulement les conversations.
- Tâches de fond : les sous-agents lancés en arrière-plan sont listés, et marqués terminés même sans notification, d'après leur fichier de métadonnées.

### Corrigé
- Sécurité : les chemins de code de l'IDE exigent `config_write` à la passerelle, à la suppression et à la restauration depuis la corbeille ; les clés de projet de la mémoire sont limitées aux projets connus (globs bornés, noms de correspondances masqués sans `fs_read`) ; les arguments, variables d'environnement et commandes de hooks MCP sont masqués sans `config_write`, la profondeur et la taille des JSON sont bornées ; les URL de dépôt illisibles sont masquées par défaut ; les droits de l'appelant sont transmis aux homes enfants (`fs_read`, `config_write`) et vides quand la passerelle n'a pas de magasin.
- Sécurité : le journal des fichiers chargés de la mémoire est lu en fichier ordinaire de taille bornée ; la liste des équipes est écrite par les fonctions sûres face aux liens symboliques ; les identifiants de sessions enfants sont validés ; les brouillons de l'IDE sont effacés à la déconnexion et au changement de compte.
- Équipes : une boîte de dialogue de permission tronquée est signalée et son approbation depuis le web refusée.
- Réaffectation d'une session (`/cd`) : un dossier de session déjà présent à destination (reste d'un déplacement interrompu) est fusionné au lieu de bloquer.
- iOS : icône d'écran d'accueil et de partage servie à la racine (`apple-touch-icon`, 180 px et 1024 px), manifeste à icônes opaques et couleur de thème sombre pour que l'icône ne soit plus verte, plus de rebond élastique, balayage du bord gauche qui ouvre le menu dans la PWA, contrôles de saisie de session sur deux rangées sur téléphone.
- Page de saisie du jeton : le champ respecte la largeur du conteneur.
- Deux expressions régulières de l'interface (mots avec trait d'union, images envoyées) bornées contre un retour arrière quadratique.
- Sécurité : un invité ne lit que les fichiers cités par le transcript de la session partagée ; l'arborescence n'affiche que ces fichiers, les segments `..` sont refusés et les liens symboliques résolus avant la lecture.
- Tactile : zones de clic de 44 px, texte secondaire à 12 px, cases de sélection des sessions masquées jusqu'au début d'une sélection, boutons de menus et d'info-bulles élargis, lecteur de session en plein écran seulement sur téléphone, en-tête sous la barre d'état de l'iPhone, boîtes de dialogue et tiroir pleine hauteur, lignes de plugins contenues dans la carte Extensions.
- Un retour de sous-agent quitte la file d'attente dès que son retrait aboutit.
- Les sous-sessions sont supprimées avec leur session parente.
- Les commandes de fond tuées hors `TaskStop` sont signalées d'après leur marqueur de sortie ; celles arrêtées par `TaskStop` sont marquées tuées.
- La page occupe jusqu'à 1920 px de large ; l'éditeur de skills reste dans la largeur de la page.
- Le dossier personnel (`~`) et `~/.claude` ne sont plus traités comme des projets : leurs skills, commandes et réglages apparaissaient en double sous un groupe sans nom.
- Accessibilité : contrastes relevés au niveau WCAG AA. Texte atténué du thème sombre plus clair (4,4:1 → 5,5:1 sur les panneaux), onglets inactifs du thème clair (3,9:1 → 5,2:1), bordures des champs et cases à cocher à 3:1 au moins, anneau de focus plus marqué (opacité 80 %, teinte du thème clair assombrie : 1,3:1 → 4,1:1), boutons désactivés à 70 % d'opacité au lieu de 50 %, zone de clic des cases à cocher et petits boutons portée à 24 px pour tous les pointeurs.
- Accessibilité : les onglets de la page Skills, Extensions et Plugins défilent sur petit écran au lieu d'élargir la page ; le bouton d'ouverture du menu ne référence plus un tiroir absent (`aria-controls`).
- Accessibilité : lien « Aller au contenu » en tête de page pour sauter la navigation au clavier ; bordure du filtre de fichiers de l'IDE et des zones de texte à 3:1.
- Mode sans comptes : l'interface n'interroge plus les alertes de bannissement toutes les 15 s (deux erreurs 404 en console à chaque chargement).
- Accessibilité : page de saisie du jeton, bouton « Ouvrir » (3,4:1 → 5,2:1), bordures du champ et lien du thème clair relevés à plus de 3:1.
- Sécurité : en mode multi-utilisateur, un jeton `Authorization: Bearer` faux compte désormais pour l'adresse (10 par minute, puis `429`), comme en mode ouvert ; il se devinait auparavant sans limite.
- Sécurité : `/assets/` et `/icons/` ne listent plus leur contenu (`404`) ; les fichiers restent servis.
- Sécurité : le plafond global de 60 tentatives de connexion par minute n'arrête plus un compte qui revient depuis une adresse où il s'est déjà connecté ; des noms inventés envoyés depuis de nombreuses adresses bloquaient toutes les connexions.
- Sécurité (`GLD_RUN_AS`, expérimental) : les fichiers d'agents des équipes (`agents/<id>.md`) sont écrits sans suivre de lien symbolique posé par le compte cible. Le suivi `docs/auth-followup-openroot.md` liste quatre autres sites relevés par l'audit.
- Documentation : `auto` et `keep` sur `PATCH /v1/sessions/{id}` ; remerciements à jour (dépendances Go et web, RealFaviconGenerator pour les icônes).
- Documentation : `mem_write` permet d'orienter les sessions des autres comptes du home (CLAUDE.md et règles chargés par toutes les sessions).

## [0.7.1] - 2026-10-29

### Corrigé
- Windows : la reprise d'une session avec un message fonctionne quand `claude` est un exécutable natif (`.exe`).
- Équipes d'agents : sous Windows, gld indique que tmux est requis au lieu d'afficher l'erreur de reprise.

### Documentation
- Captures d'écran rafraîchies en 1920×1080 (git, revues, notes, plugins, snippets) et ajout des vues session partagée, multi-home et multi-utilisateur.

## [0.7.0] - 2026-10-29

### Ajouté
- Workflows : le détail d'une tâche de workflow affiche ses phases, ses agents (état, tokens, durée, dernier outil) et ses totaux, relus par `GET /v1/sessions/{id}/workflows`.
- Snippets : cocher plusieurs commandes puis les fusionner dans une session Claude (`POST /v1/snippets/merges`), comme la fusion de mémoire.
- Mémoire : renommer un fichier depuis son menu contextuel.
- Notes : clic droit sur l'en-tête pour changer le dossier de notes ; le menu d'envoi se limite aux sessions de ce dossier.
- Tâches de fond et sous-agents : bouton d'arrêt dans les listes en direct ; les messages de sous-agent se replient à 5 lignes ; les messages entre sessions renvoient vers la session émettrice.

### Modifié
- **Rupture** : toute l'API passe en REST sous `/v1` (ressources au pluriel, `GET`/`POST`/`PUT`/`PATCH`/`DELETE`, identifiants en chemin, suppressions multiples par `?item=` répété). Les anciennes routes (`/api`, `/session-send`, `/fs-save`, `/auth/login`, `/admin/users`…) sont supprimées, sans alias. La correspondance complète est dans `docs/reference/openapi.yaml`.
- **Rupture** : les réponses perdent l'enveloppe `{ok, out}`. Un succès renvoie la ressource en JSON (ou `204`), un échec renvoie `{"error":{"code","message"}}` avec le vrai statut HTTP (400, 401, 403, 404, 409, 413, 415, 429, 5xx). Les flux gardent leur type (NDJSON du chat, SSE de `/v1/fs/watch`, WebSocket du terminal).
- **Rupture** : un home enfant (gld d'une version antérieure) n'est plus joignable depuis une passerelle à jour, et inversement : mettre à jour tous les gld d'un même ensemble en même temps.
- Les onglets déjà ouverts et les PWA installées doivent être rechargés (Ctrl+Maj+R) : l'ancienne interface appelle des routes qui n'existent plus.
- L'interface lève une `ApiError` (statut, code, message) au lieu de lire `{ok, out}` ; la couche de compatibilité `Result`, `HttpError` et `api.post` est supprimée.

- Les suppressions multiples (`DELETE /v1/fs/entries`, `/v1/fs/trash`, `/v1/memory/files`, `/v1/skills`, `/v1/snippets`) traitent chaque élément seul et répondent `{done, failed}` (200 dès qu'un élément a abouti, l'erreur uniforme sinon) ; le plafond d'une requête multiple passe à 100 éléments et l'interface découpe les grandes listes en lots.
- La recherche mémoire passe de `GET /v1/memory/files?q=` à `GET /v1/memory/search?q=` ; le 409 de `PUT /v1/fs/entries/content` renvoie aussi `etag` et `deleted`.
- Les actions qui demandent une réauthentification dans un corps deviennent des `POST` : `/v1/auth/factors/{factor}/removals`, `/v1/auth/sessions/{sid}/revocations`, `/v1/auth/sessions/revocations`, `/v1/admin/users/{user}/mfa-resets`, `/v1/admin/sessions/revocations` (des proxys retirent le corps des `DELETE`).

### Ajouté
- `docs/reference/openapi.yaml` : contrat OpenAPI 3 de l'API `/v1`, vérifié contre les routes enregistrées et leurs droits par un test.
- Tests de contrat : toute route hors pages est sous `/v1`, les erreurs d'un échantillon de routes ont la forme uniforme, aucun `http.Error` ne subsiste dans un handler `/v1`.

### Corrigé
- Un lien de session partagée déjà utilisé rouvre la session sur le navigateur autorisé.
- La reprise des sessions coupées en plein tour envoie un message « continue ».
- La recherche de la barre latérale ne filtre plus que ses propres entrées, indépendamment de la barre du haut.
- Les infobulles de la barre d'outils de l'IDE s'affichent sous les boutons ; l'indication de compaction se place entre la barre de session et le champ de saisie.

## [0.6.3] - 2026-10-29

### Ajouté
- Fenêtre « Nouvelle session » (et toutes les fenêtres de saisie : reprise, groupe, envoi à tous les dossiers) : pièces jointes par collage, glisser-déposer ou bouton trombone, avec vignettes.

### Corrigé
- Fenêtre « Nouvelle session » : boutons réorganisés (outils sous le champ, modèle et effort sur une ligne, Annuler et Lancer à droite).
- Messages d'agent ou de session en file d'attente : rendus en carte au lieu de la balise brute, sans coupe à 400 caractères (plafond de 64 000) ; un prompt qui commence par `<` reste du texte brut, coupé comme avant ; le cadre pointillé ne double plus celui de la carte.
- Hauteurs alignées entre listes déroulantes, champs et boutons dans le composeur, Config et Skills (nouvelle taille `xs` des listes).
- Fenêtre d'un sous-agent : la note devient « Lecture seule », en bas.
- « Reprendre les récentes » (`gld-matin`) ne rouvre plus les sessions d'équipiers, qui restent ouvrables par leur identifiant ; la sauvegarde des onglets (`--resume`, « Rouvrir les dernières ») les écarte aussi, y compris dans les sauvegardes existantes.
- Barre d'état de la session : plus collée en bas au défilement, sans espace réservé quand il n'y a rien à montrer.

## [0.6.2] - 2026-10-29

### Ajouté
- Les blocs `<agent-message>` (rapport d'un sous-agent) s'affichent en carte « Message du sous-agent ».
- Touche `Backspace` ajoutée aux touches permises de `POST /session-key`.

### Corrigé
- Question du terminal à onglets : une réponse libre déjà donnée (« 3. texte ✔ ») se modifie au retour, le curseur rejoint la ligne sans valider et le champ reprend l'ancien texte ; le chiffre revalidait la réponse et passait à l'onglet suivant.
- Question du terminal : le contexte affiché ne reprend plus le bandeau de Claude ni la saisie du tour précédent.

## [0.6.1] - 2026-10-28

### Corrigé
- Le test de la file suspendue du composeur comptait l'appel `/config` comme un envoi : la CI de release de 0.6.0 échouait, aucun binaire n'a été publié. Aucun changement de comportement.

## [0.6.0] - 2026-10-28

### Ajouté
- Partage de session avec des invités : lien d'invitation à usage unique, cookie d'invité par partage (en-tête `X-Gld-Share`, plusieurs sessions partagées dans un même navigateur), plusieurs invitations par session avec libellé, mode et file propres, accès lecture seule ou écriture. Vue `/guest` : conversation avec historique complet, navigateur de fichiers en lecture seule du dossier de la session (liste de refus des secrets élargie, évasions de chemin testées), écran brut jamais exposé. Côté hôte : entrée « Session partagée » dans la barre latérale (pastille de compteur), panneau d'invitations par session, menu « Partager… ».
- File d'envoi côté serveur : le serveur vide la file des messages (messages d'invités étiquetés pour ne jamais lancer une commande Claude, recontrôle de l'écran sans cache avant d'appuyer sur Entrée), compare-and-set persistant pour l'état « envoi en cours », file en pause pour `claude -p`, éléments « incertains ».
- Vue d'ensemble d'un projet : une carte par dossier avec totaux et dépôts git de chaque dossier ; envoi d'un même prompt à tous les dossiers d'un projet depuis son menu ; ajout d'un dossier par glisser-déposer sur les sessions locales, bouton « ajouter tous les sous-dossiers » (`POST` côté serveur) dans le sélecteur de dossier.
- Sessions : menu de dossier « Fermer les sessions ouvertes (N) » ; clic droit sur un message « Supprimer la suite de la conversation » (`POST /conv-rewind`, tronque le transcript) ; invite à ouvrir une nouvelle session quand le cache de prompt a expiré ; sous-sessions d'équipe imbriquées sous leur session parente (sessions ouvertes et favoris).
- Conversation : Markdown rendu dans les messages utilisateur, les messages d'équipe et les notifications de tâche ; messages d'équipe JSON décodés en liste de champs ; messages entre sessions rendus en carte d'expéditeur avec réponse depuis la carte ; balises `teammate` et `task` trouvées en milieu de message rendues.
- Pièces jointes nommées `<id-session>-<date>-<aléa>` et supprimées à la purge de la session.
- Revues de sécurité automatiques : vue dédiée en bas de la barre latérale, masquées des listes de sessions, exclues de la restauration des onglets et de `gld-matin`.
- Écran de connexion sur le lien local : un indice `cloudflared` remplace le QR code.
- IDE : `.git` masqué dans l'arbre de fichiers.
- Carrousel : flèches précédent et suivant dans la barre.
- Menu contextuel d'un message : copier le message, un bloc de code ou un lien.
- Images jointes : affichées dans les messages utilisateur à la place de leur chemin, servies par nom et confinées au dossier privé.
- Sélecteur de modèle : affiche le modèle par défaut effectivement utilisé.
- Chemins affichés : le dossier personnel est masqué.
- README : tunnel rapide `trycloudflare` pour essayer le partage de session.
- Documentation : installation depuis le binaire de release dans le README.
- CI : les binaires de release portent une attestation de provenance GitHub.

### Modifié
- Spinners : icône `LoaderPinwheel` partout, trait de 1 px.
- Barre latérale : l'entrée « Récents » est retirée ; la pastille de sous-sessions passe à droite de la session.
- `check-ui` vérifie les types avec `tsc -b`.
- Indice « cache froid » en bleu.

### Corrigé
- Invités : cookie effacé seulement sur refus définitif et jamais sur un fichier statique cacheable, contrôles de cookie bornés, `LastSeen` silencieux, caractères de format retirés des titres de partage, fichier de file contenant `null` rouvert sans panique, file purgée quand le partage passe en lecture seule, disparaît ou que le lien est régénéré, plafond d'éléments par invité, éléments des autres invités masqués, 503 quand un enfant ne peut pas juger, `Flush` conservé sous `no-store`, seuls les comptes avec `exec` sur le home du partage comptent comme hôte, une invitation exige un `POST`.
- Vue invité : plus d'écran blanc avec `scrollIntoView` de Chrome, fin de partage détectée sur un envoi en échec, images chargées par la poignée du partage, scroll qui ne saute plus vers la fin pendant la lecture, vue collée à la fin quand la barre d'état change de hauteur.
- Barre latérale : sessions chaudes contenues dans la largeur, bascule des sous-sessions près de l'icône, titres et icônes alignés.
- Sessions de revue de sécurité acceptées depuis les onglets sauvegardés (`isAuto`).
- Une commande slash vide d'abord la ligne de saisie du TUI avant l'envoi.
- Les sessions tmux détachées prennent une taille de 160x48.
- Un prompt déjà pris par Claude ne reste plus épinglé comme « en file ».
- Le texte des messages de la transcription est de nouveau sélectionnable.

## [0.5.0] - 2026-10-27

### Ajouté
- Équipes d'agents (page `/equipes`, [docs/teams.md](docs/teams.md)) : équipes natives de Claude Code pilotées depuis gld. Catalogue de 21 agents et 11 équipes (identifiant et libellé de rôle, sans prénom), prénoms tirés avec `gofakeit` à chaque lancement (nom d'exécution en minuscules, roster `<CLAUDE_CONFIG_DIR>/gld-teams-journal/<équipe>.roster.json` supprimé à la fermeture), affichage « Prénom · Rôle », composition par union des équipes cochées sans doublon (`GET /teams-catalog`, `POST /teams-compose`). Lancement dans tmux avec `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` (champ `Env` de `tabSpec`, ce seul couple admis), `--teammate-mode=tmux` et `--session-id`, permissions par défaut (`POST /teams-launch`) ; suivi (`GET /teams`, `GET /team`, `GET /team-screen`), messages directs au chef ou à un coéquipier avec leur auteur, mis en file (20 par équipe) si la cible n'est pas disponible (`POST /team-send`), approbation des demandes de permission avec confirmation du texte vu côté serveur (`POST /team-key`), fermeture (`POST /team-close`). Éditeurs d'agents et d'équipes personnels (`POST /teams-agent-save`, `/teams-agent-rm`, `/teams-team-save`, `/teams-team-rm`). Droits : `sessions_read` (lecture), `exec` (lancer, envoyer, touche, fermer), `config_write` (éditeurs). En-tête `X-Gld-User` : la passerelle y pose l'auteur vers les homes enfants, toujours effacé côté client, cru par l'enfant avec son seul jeton.
- Jumelage d'un appareil : dialogue « Connecter un appareil » (QR code, liste des appareils), code à usage unique ou à huit chiffres tapé sur « Connecter cet appareil » ; l'appareil reçoit une session en mode jeton. `--public-url` / `GLD_PUBLIC_URL` fixe l'adresse portée par le QR. Un jumelage de son propre compte ouvre une alerte de sécurité dans les sessions connectées.
- PWA : page de saisie du jeton avec icône et manifeste, installable sur iPhone en plein écran (barre d'état, Dynamic Island) et reconnectable sans cookie ; service worker avec cache des ressources statiques, fichiers d'interface hachés et cachés un an.
- Notifications du navigateur quand Claude demande une réponse ou a terminé ; un clic ouvre la session.
- Options `--token` (jeton d'accès fixé) et `--tls-cert` / `--tls-key` (`GLD_WEB_TLS_CERT`, `GLD_WEB_TLS_KEY`) pour servir en HTTPS ; le cookie du jeton est alors marqué `Secure`.
- Page Skills et plugins (entrée unique de la barre latérale) : plugins, skills, hooks et MCP par portée, serveurs MCP de plugins et de claude.ai, skills et MCP du projet activables par portée. Un seul interrupteur par plugin, skill et MCP, portée d'écriture choisie à part. Installation de plugins et de marketplaces (installés, disponibles groupés par marketplace, sous-onglets). Éditeur de skills intégré, tokens par skill (description en contexte, `SKILL.md` au lancement). Plugins sur deux colonnes, skills d'un plugin repliés, sections en cartes.
- Réglages et permissions propres au projet, avec lien vers les permissions globales ; bouton Réinitialiser avec aperçu avant écriture.
- Règles utilisateur intégrées (`gld-askuser`, `gld-output-format`, `gld-long-output`) installées dans la config au démarrage ; une règle retirée n'est supprimée que si elle porte la marque « Managed by gld ».
- Mémoire : bascule chargement à chaque session ou à la demande dans le menu d'un fichier, liste en arbre, icône, libellé et couleur du type pour la mémoire de projet.
- Conversation : mode terminal sous la transcription au lieu de la remplacer ; messages en file envoyés fenêtre fermée (préfixés pour garder les questions pour la fin) ; sous-agents en cours listés au bas de la transcription ; cartes pour les balises `teammate-message` ; panneau à chasse fixe pour les aperçus de question côte à côte ; documents écrits par Claude ou cités par chemin (md, html, images, pdf) affichés dans la conversation ; spec et plan superpowers cités par un appel d'outil rendus en Markdown. Les avertissements informatifs de Claude Code (par exemple une session signalée par ses garde-fous) s'affichent comme note dans la conversation. La ligne d'outil en cours est mise en évidence dans la transcription et les tâches en cours s'affichent dans la barre d'état live.
- Pièces jointes : tout fichier se joint à un message, pas seulement une image ; bouton « joindre une image » dans les zones de saisie.
- Espaces de travail sauvegardés et libellés de navigation des extensions : renommage dans l'interface. Onglets : « tout fermer » et rouvrir le dernier jeu enregistré (les sessions de revue de sécurité automatiques sont ignorées). Notes : raccourci dans la barre latérale, compteur et icône de bloc-notes du projet. Extraits : renommage.
- Skills : prise en charge de `npx skills` (paquet `skills@1.7.4` épinglé). Les skills du fichier de verrouillage affichent leur source, leur mode (lien ou copie) et un badge « mise à jour » (comparaison du hash du dossier avec GitHub, sans jeton : 60 requêtes par heure, cache de 6 h) ; la page Skills cherche (`GET /skills-find`), installe, met à jour et retire (`POST /skill-npx`, `config_write`) après confirmation de la commande exacte. Une seule action à la fois, 5 min au plus.
  Mettre à jour et Retirer ne sont proposés que pour un skill relié au dossier `.agents/skills` (un skill écrit à la main de même nom n'est jamais touché) ; `remove` passe `-a claude-code`. Le badge « mise à jour » n'existe qu'en portée globale (le verrou de projet ne porte pas de hash de dossier) ; « vérifier » forcé au plus une fois par 30 s.
- Connexion : une adresse extérieure qui accumule 8 échecs en 15 min (tous noms confondus) est bannie 1 h ; le poste, les réseaux privés et le lien local en sont exempts.
- Connexion : un ban ouvre une alerte dans chaque session connectée (`GET /auth/alerts`).
- Mémoire : la fusion accepte jusqu'à 100 fichiers (20 auparavant) et le document de sortie se choisit parmi les fichiers sélectionnés, le `CLAUDE.md` global ou le `CLAUDE.md` / `AGENTS.md` du projet (`out` de `POST /memory-merge`).

### Modifié
- Build de l'interface : le garde de taille des chunks (`check-chunks.mjs`) et l'avertissement de vite passent de 700 à 750 ko ; l'entrée `app-*.js` pèse 700 ko.
- Fenêtre de contexte déduite du modèle (1 M pour la famille 5).
- Barre latérale : tailles disque des sessions et des revues de sécurité masquées retirées, « masquer les sessions groupées » au-dessus de la liste, icône shredder pour toute destruction de session, bouton MCP retiré de l'en-tête, widget git retiré de la fenêtre « nouvelle session » (qui s'ouvre en modale, l'en-tête de dossier n'a plus de champ de prompt).
- Mémoire de projet repliée, recherche sur la page Mémoire, ajout et tri des sections regroupés en un menu.

### Corrigé
- Sécurité : `POST /memory-merge` avec `launch` exige `exec` en plus de `mem_write` ; `POST /vgroups` exige `exec` pour ajouter un dossier enregistré (racines de l'IDE).
- Sécurité : `GLD_AUTH_KEY` n'est plus transmise aux sessions claude, au terminal web ni aux homes enfants ; le garde Bash refuse toute commande citant `GLD_AUTH_*`.
- Sécurité : écrire dans `.claude/`, `.git/`, `.mcp.json`, `.gitattributes` ou `.gitmodules` depuis l'IDE (`fs-save`, `fs-new`, `fs-rename`) exige `config_write` en plus de `fs_write` ; `GET /session-loaded-file` ne lit que les `.md` de la config Claude ou les fichiers d'un dossier connu.
- Sécurité : la liste des serveurs MCP masque les paramètres d'adresse et les valeurs des drapeaux de type clé ou jeton ; `POST /pair-code` émet au plus 5 codes en attente par compte (200 au total).
- Sécurité : un jeton opérateur faux (cookie, Bearer, `?token=`) compte dans les essais par adresse, comme le formulaire.
- Sécurité : sous Linux, un gld enfant se marque non « dumpable » (`PR_SET_DUMPABLE`) : son jeton de passerelle n'est plus lisible dans `/proc/<pid>/environ` par les sessions du même compte.
- Sécurité : quand `GLD_AUTH_KEY` vient de l'environnement, gld se marque non « dumpable » sous Linux ; le garde refuse la lecture de `/proc/*/environ`.
- Synchronisation entre homes et copie de skills : les scripts gardent leur bit exécutable.
- Sécurité : une session ouverte par code de jumelage expire au plus tard avec la session émettrice et tombe avec elle (migration `auth.db` 0009, appliquée au démarrage).
- Sécurité : une source de marketplace vers la machine, un réseau privé ou le lien local est refusée.
- Sécurité (`GLD_RUN_AS`) : les écritures de la passerelle dans la config (journal, corbeilles, réglages, sauvegardes, fichiers de données, transcripts déplacés, images, skills) ne suivent plus un lien symbolique qui sort du dossier ; temporaires à nom aléatoire, mode posé sur le descripteur.
- Sécurité (`GLD_RUN_AS`) : les lectures de la passerelle (réglages, mémoire, transcripts, corbeilles, gardes de droits) ne suivent plus un lien symbolique posé par le compte cible hors du dossier de confiance.
- Connexion : un cookie de jeton périmé est effacé à son premier refus, au lieu d'épuiser les essais de l'adresse à chaque relevé de l'interface.
- File d'envoi : un message ne quitte la file qu'une fois l'envoi confirmé ; un envoi sans réponse s'affiche « envoi incertain » avec Renvoyer.
- Connexion : les échecs de second facteur se comptent par compte et par adresse (5, remis à zéro par une connexion réussie) ; le plafond du compte toutes adresses confondues passe à 15 et n'est jamais remis à zéro.
- Mobile et iPhone : copie dans le presse-papiers en HTTP sur le réseau local, app installée hors de l'indicateur d'accueil et de l'encoche, pas de zoom au focus des champs, exports fiables sur Safari, modales en feuille basse avec zone de sécurité, Entrée va à la ligne au clavier tactile, icône PWA opaque (manifeste avec `id` et `purposes`), menus sans chevauchement, icônes masquées sur les boutons texte.
- Interface : le bloc-notes d'un dossier s'ouvre par défaut, la description des tâches de fond est réservée aux lecteurs d'écran, l'écran brut s'affiche au-dessus de son bouton, les icônes de la barre latérale sont alignées, la phrase d'aide du carrousel disparaît, la zone de sécurité du bas n'ajoute plus de marge à la racine de l'application, l'état de question du terminal n'est signalé qu'à son changement, et l'échec d'un `fetch` ne laisse plus une vue en chargement ni une promesse rejetée sans traitement.
- Conversation : une session rouverte reste ancrée au bas, plus d'aimant vers la fin de la transcription, relecture finale au repos ; la file d'envoi se synchronise entre onglets (un seul la vide) et rejoue toute la transcription au lieu d'une queue de 512 Ko ; un message finissant par `@chemin` reçoit une espace pour que le menu de fichiers n'avale pas Entrée ; erreur 429/529 de nouveau reconnue ; une session de commande seule renvoie une liste vide.
- Extensions : erreur de chargement affichée au lieu d'un squelette infini, recherche de plugin limitée à la dernière requête, MCP et confiance du projet lus dans le `.claude.json` du home actif, état MCP par dossier avec cache des échecs, seuls les serveurs du `.mcp.json` du projet courant listés.
- Skills : les skills liés par lien symbolique sont listés, leur suppression est refusée, supprimer un skill contenant des liens ou dossiers vides garde `SKILL.md`.
- Notes : quitter la vue sans éditer ne supprime plus la note ; déplacer une note sur une clé existante fusionne les éléments (refusé au-delà de la taille maximale).
- Mémoire : un fichier de mémoire ou un extrait existant mais illisible n'est jamais écrasé, `setType` ne touche que le bloc `metadata`, nom et description lus dans le front matter parsé, le contrôle d'index ignore les URL externes, le contrôle de hook ne signale plus les chemins relatifs, la troncature du journal des fichiers chargés ne panique plus.
- Synchronisation entre homes : tout est validé et sauvegardé avant écriture, un dossier de sauvegarde par exécution ; commandes, sync et mémoire suivent une racine symlinkée hors `GLD_RUN_AS`.
- Divers : purge de corbeille qui vérifie les sessions vivantes avant de restaurer (et épargne la corbeille de sessions qui exige `exec`), un tampon tmux distinct par envoi (plus de collage croisé), le watcher IDE réabonne un dossier recréé, l'index de recherche est tenu par home, les titres longs passent à la ligne dans les dialogues, les notifications restent cliquables sous une modale, le relevé de mémoire ne remplace plus le fichier ouvert entre-temps.
- Droits : les brouillons de mémoire et de skill exigent `mem_read` en plus de leur droit d'écriture ; `mem_write` et `config_write` ne s'accordent pas sans `mem_read`.
- Sécurité : l'alerte de ban ne montre l'adresse qu'aux administrateurs, une adresse de bouclage IPv6 n'est plus bannie, les alertes sont relevées par curseur de ligne (aucun événement de la même seconde n'est manqué).
- Sécurité : les actions GitHub de la CI sont épinglées par SHA et le jeton de la CI est en lecture seule ; les skills `npx` refusent un `node_modules/skills` fourni par le projet (dans tous les dossiers parents) et vident `NODE_OPTIONS`.

## [0.4.7] - 2026-10-26

### Corrigé
- Tests : `TestPanneClaudeBloque` rétablit le cache de `claude agents` de production, sinon `/api` relançait chaque relevé (environ 5 s, au-delà du seuil de 4 s).
- Tests : le test de la poignée de l'éditeur attend que l'onglet `b.txt` soit actif avant de cliquer (course en CI).

## [0.4.6] - 2026-10-26

### Corrigé
- Windows : la compilation échouait sur `syscall.Stat_t` et `syscall.O_NOFOLLOW` dans la lecture des tâches de fond. Le contrôle de propriété passe par `ownedByMe` et `O_NOFOLLOW` devient une constante par plateforme (`fsflags_unix.go`, `fsflags_windows.go`).

## [0.4.5] - 2026-10-26

### Corrigé
- Sécurité : `dompurify` est forcé en 3.4.17 (`overrides`) ; la version 3.4.15 embarquée par `monaco-editor` avait une faille critique et un audit de dépendances échouait en CI.

## [0.4.4] - 2026-10-26

### Ajouté
- **Plugins** : un bouton « Plugins » dans la page Skills ouvre une fenêtre qui liste les plugins installés (toutes portées), les active ou les désactive, les met à jour et les désinstalle, et propose les plus installés des marketplaces avec recherche, pour l'utilisateur ou pour un projet (`GET /plugins`, `POST /plugin-act`, via `claude plugin`).
- **Marketplaces** : la même fenêtre liste les marketplaces configurés, en ajoute (dépôt `owner/repo` ou URL https, jamais un chemin local), les met à jour et les retire (`GET /marketplaces`, `POST /marketplace-act`). Lecture avec le droit de lecture, modifications avec le droit d'écriture de configuration.

### Modifié
- Les dossiers de « Projets » et de « Sessions locales » reprennent le style des sessions ouvertes : trait de guidage à gauche des enfants et lignes compactes (28 px, texte discret).

### Corrigé
- CI : trois faux positifs gitleaks (deux jetons de test, `argon2.IDKey`) sont ignorés par empreinte dans `.gitleaksignore`.

## [0.4.3] - 2026-10-26

### Ajouté
- **Lecteur, en-tête en deux colonnes** : à gauche le titre puis le dossier du projet (bouton), la date du dernier message, les sous-agents (bouton grisé s'il n'y en a pas) et l'état git ; à droite « Maintenir au chaud », l'état du cache (froid en bleu glace, chaud en minutes restantes), ouverte ou fermée, puis « Stats », « Nouvelle session » et le menu « … » (« Reprendre » et « Pré-Prompt » y sont).
- **Fenêtre Stats** : réunit les informations de la session (clic droit → Infos ouvre la même fenêtre), le cache et le maintien au chaud, la consommation, le dernier appel et la liste des sous-agents, dont chacun ouvre sa conversation.
- **Pré-Prompt** : chaque fichier chargé par Claude s'ouvre dans une fenêtre au-dessus de la liste (`GET /session-loaded-file`, limité aux fichiers que le hook a vus charger dans cette session, lecture bornée à 1 Mio).
- **Outils masqués** : une ligne repliée « N outils » remplace les appels masqués ; un clic relit et déplie ces messages (`GET /conv-tools`, 1 000 messages au plus). Le bouton « Outils masqués » passe au-dessus de la saisie, à côté de « debug » (grisé tant que la session n'est pas ouverte).
- **Session ouverte** : un bouton de mode (Maj+Tab) lit le mode de permission sur l'écran tmux et passe au suivant ; rangée de commandes réordonnée (modèle et effort à gauche, commandes, fermer, arrêter, envoyer à droite).
- **File d'attente** : annuler un message en file le remet dans la saisie.
- **Compactage** : un bandeau conseille de compacter au-dessus de la saisie quand le contexte dépasse 60 % de la fenêtre (1 M de tokens si le modèle porte `[1m]` ou si le contexte a dépassé 200 k, sinon 200 k).
- **Maintien au chaud** : une session au cache froid peut être réchauffée par un premier ping avant le maintien (explicite, le menu prévient du coût ; `reheat` sur `POST /keepwarm`).
- **Tâches de fond** : pastille « tâche de fond » et arrêt par `claude stop` depuis la liste et l'en-tête du lecteur ; message dédié sous la saisie ; maintien au chaud grisé.
- **Navigation du lecteur** : « Aller au début » apparaît dès qu'on défile, comme « Aller à la fin », et les deux sont relus à l'ouverture et quand le contenu change.

### Modifié
- Recherche : la bascule « Contenu » est dans le champ de recherche ; la barre latérale ne dépend plus de la recherche et les sessions groupées restent toujours trouvées.
- Le cœur de soutien passe en haut de la barre latérale ; le menu « … » de la vue projet est retiré ; cœur, compte et menu utilisateur restent sur la première ligne de l'en-tête sur petit écran.
- Toutes les actions « Reprendre » utilisent l'icône `refresh-ccw-dot` ; « Instructions » devient « Pré-Prompt » ; l'icône du maintien au chaud est le micro-ondes dans tous ses états.

### Supprimé
- **Étiquettes de session** : l'entrée « Étiquettes… », les pastilles `#étiquette`, le filtre `#prod` et le champ `tags` du magasin. Les étiquettes déjà enregistrées sont ignorées puis effacées à la prochaine sauvegarde du magasin.

### Corrigé
- Anneau de focus du bouton dossier rogné par la troncature ; première ligne de l'en-tête non alignée sur la croix de fermeture.
- Les routes `GET /conv-tools` et `GET /session-loaded-file` sont déclarées dans la table des droits (`sessions_read`).
- Build : avertissement `debugName` du groupe de découpage Monaco.

## [0.4.2] - 2026-10-24

### Ajouté
- **Recherche** : un champ de recherche dans chaque vue de sessions, par nom, et dans le contenu en mode avancé.
- **Configuration** : les options de Claude passent dans l'onglet Réglages ; l'onglet Interface réunit les raccourcis puis l'apparence (couleurs et raccourcis en sections).

### Modifié
- Barre latérale : les groupes de sessions s'affichent sous les dossiers d'un projet ; le bouton engrenage est retiré (les réglages sont dans la page Configuration).

### Corrigé
- La CI échouait sur `TestRightsRowCopy` : la migration des droits `fs_read`/`fs_write` de la 0.4.0 avait ajouté deux colonnes à `home_acl`, et le test insérait ses lignes par position sans voir l'échec de l'insertion. Le test nomme maintenant ses colonnes et vérifie l'insertion ; la détection d'une ligne de droits copiée vers un autre utilisateur ou un autre home est vérifiée pour de bon (aucun changement du code de production).

## [0.4.1] - 2026-10-24

### Ajouté
- **Sessions ouvertes** : la barre latérale liste les sessions ouvertes « froides » avec les chaudes ; un double clic ouvre le dossier de la session, et la fenêtre du lecteur gagne un bouton dossier.
- **Bloc-notes** : « tout sélectionner » et envoi des items vers une session fermée.
- **Réglages** : les réglages du dossier sont réunis dans la fenêtre des Réglages (ils ne sont plus dans l'en-tête de page).
- **Compte** : « Révoquer les autres sessions » ferme toutes les sessions de connexion du compte sauf la courante (`POST /auth/sessions/revoke-others`, réauthentification demandée, journalisé `session_revoke`).

### Corrigé
- Le menu de poussée n'affiche que le nom du remote, plus son adresse.
- La vérification de l'interface (`make check-ui`, règle ESLint `react-hooks/refs`) échouait sur `Conversation.tsx` : l'identifiant de la session lu par le gestionnaire de défilement est mis à jour dans un effet et non plus pendant le rendu. Aucun changement de comportement.

## [0.4.0] - 2026-10-24

### Ajouté
- **Multi-utilisateur (optionnel)** : comptes locaux créés par `gld user add --admin`, connexion par mot de passe plus second facteur obligatoire (TOTP, clés de sécurité WebAuthn, codes de secours), sessions à cookie et jeton CSRF. Sans compte, gld se comporte comme avant. Guide : `docs/auth.md`.
- **Droits par home** : neuf droits indépendants (`mem_read`, `mem_write`, `mem_sync`, `usage_read`, `sessions_read`, `config_write`, `exec`, `fs_read`, `fs_write`), refus par défaut de toute route non classée, snippets et restauration de skills réservés à `config_write` (jamais de contenu exécutable avec `mem_write`), interface et API d'administration (comptes, droits, journal, sessions). `exec` et `config_write` équivalent au compte système de gld.
- **Défense contre les essais répétés** : délai progressif, verrou de compte, suite d'échecs consécutifs (30 min au 10e, 24 h au 20e), limite du second facteur (qui bloque aussi le mot de passe du compte), plafond global, journal d'événements limité et purgé à 90 jours, adresse du client via `GLD_TRUSTED_PROXIES`.
- **Base signée** : seuls les secrets TOTP sont chiffrés ; les lignes sensibles (comptes, droits, facteurs, sessions) et le marqueur des comptes sont signés avec une clé dérivée de la clé maîtresse (`GLD_AUTH_KEY`, coffre-fort de l'OS ou `auth.key`). Une ligne altérée est refusée et signalée ; des comptes supprimés ferment le service (503) au lieu de rouvrir le mode ouvert (sauf base entièrement recréée : voir `docs/auth.md`).
- **Garde-fou des sessions** : règles de refus, hook et bac à sable injectés dans les sessions lancées par gld ; `gld auth managed-settings` génère le fichier à installer par root.
- **Espace de travail** (`/espace/<dossier>`) : arborescence chargée dossier par dossier avec marqueurs git, éditeur Monaco à onglets, enregistrement atomique avec détection de conflit (recharger, écraser, voir les différences), ouverture rapide `Ctrl+P` sur un index borné (20 000 entrées, profondeur 12, 5 s ; ignore les noms en `.`, `vendor`, `node_modules` et les liens), sources TypeScript du projet données à Monaco, rafraîchissement par `fsnotify` (repli en sondage), corbeille `.gld-trash/` ignorée par git, terminal branché sur une session Claude vivante du dossier. Deux droits, `fs_read` et `fs_write`, jamais accordés par défaut ; pas de liste de refus (`fs_read` lit `.env` et les clés) ; le home, la config Claude de chaque home et le dossier d'auth de gld sont refusés comme racine ; chaque écriture réussie, sur tout home, est journalisée (`ide_write`, sans contenu) dans les événements d'administration. `fs_write` n'implique pas `fs_read`. `GET /git-changes` renvoie aussi `prefix`. Nouvelles dépendances : `monaco-editor`, `github.com/fsnotify/fsnotify`. Guide : `docs/ide.md`.
- **Espace de travail, barres d'outils** : menu (Fichier, Édition, Rechercher, Affichage, Aller à, Terminal) avec raccourcis affichés et `Alt+lettre`, barre d'actions de l'éditeur (enregistrer, tout enregistrer, annuler, rétablir, rechercher, remplacer, aller à la ligne, palette, retour à la ligne, taille du texte), barre de l'arborescence (nouveau fichier et dossier à la racine, actualiser, tout replier, ouverture rapide, filtre sur les entrées chargées, corbeille avec restauration et purge), barre d'état (position, sélection, fins de ligne, langage, indentation, encodage, branche, surveillance, index, onglets modifiés). Une action impossible est désactivée avec sa raison, jamais masquée. Préférences d'affichage par navigateur (`gld.ide.view.*`). Zone terminal toujours visible avec son état (dont l'absence du droit `sessions_read`, nécessaire avec `exec` pour lister les sessions) ; repliée jusqu'à ce que l'utilisateur l'ouvre (l'attache peut réduire la fenêtre tmux de l'hôte), puis ouverte en lecture seule et sans voler le focus, choix mémorisé. Raccourcis affichés dans la langue de l'interface. Une page qui ne se charge pas (erreur de rendu, chunk introuvable) propose « Réessayer » sans recharger, en plus de « Recharger la page ». Aucun shell libre, aucune nouvelle route serveur. Le flux de surveillance du disque se reconnecte seul (5 s, 15 s, puis 30 s) et son état s'affiche ; il ne s'arrête plus en silence. Guide : `docs/ide.md`.
- **Poids** : Monaco et ses workers ajoutent ~13 Mo non compressés à `ui/dist` (dont ~7 Mo pour le worker TypeScript), chargés seulement à l'ouverture de l'espace de travail ; le bundle principal ne change pas, mais le binaire Go, qui embarque `ui/dist`, grossit d'autant.
- **Retour arrière** : la migration qui re-signe `home_acl` (droits `fs_read`/`fs_write`) rend chaque ligne de droits « altérée » pour un binaire plus ancien, qui refuse alors l'accès (le service reste fermé) ; revenir à une version antérieure exige de restaurer `auth.db`.
- **`GLD_RUN_AS` (expérimental)** : fait tourner claude, tmux et les gld enfants sous un autre compte système. Ce n'est pas encore une frontière de sécurité (voir `docs/auth.md`) ; exige `GLD_RUN_AS_EXPERIMENTAL=1` et root.
- **Build de l'interface** : Monaco est découpé en 8 morceaux d'environ 500 ko chargés avec l'éditeur (la limite d'avertissement de 700 ko est conservée) ; `ui/scripts/check-chunks.mjs`, lancé par `npm run build`, fait échouer la construction sur un cycle entre morceaux, un morceau trop gros (hors workers) ou un `app.js` qui n'est plus l'entrée, et le Makefile reconstruit l'interface quand le garde ou la configuration change. Un découpage naïf (`maxSize`) avait produit des morceaux qui s'importent mutuellement et une page blanche à l'ouverture d'un fichier : le garde l'attrape.
- **Lecteur de session** : la transcription s'aimante à la fin en descendant (à moins d'une demi-page de la fin, y compris à la réouverture), et la position de lecture est mémorisée par session à la réouverture de la fenêtre.
- **Bloc-notes** : notes en liste d'items cochables retirés une fois envoyés (y compris après l'envoi par nouvelle session), `Ctrl+Entrée` pour envoyer, état d'enregistrement par ligne ; navigation entre les onglets des questions de Claude. Fenêtre et semaine dans l'en-tête de la fiche Compte, spinner quand les chiffres sont périmés ; mémoire affichée en blocs.
- **Sidebar** : une icône de micro-ondes orange animée remplace le feu pour une session maintenue au chaud.
- **Erreurs amont** : les 429/529 et les nouvelles tentatives de Claude s'affichent dans la conversation.

### Corrigé
- `/rename` renomme la session au lieu d'être envoyé à Claude.
- Lecteur : les lignes d'activité ne sont plus collées au bas de la fenêtre de la transcription et la zone d'activité reprend le fond de la transcription ; l'aimant n'agit qu'en fin de défilement.
- Une page qui ne se charge pas (erreur de rendu, morceau de l'interface introuvable après une mise à jour) ne laisse plus un écran blanc : un message propose « Réessayer » ou « Recharger la page ».

## [0.3.1] - 2026-10-23

### Corrigé
- Go 1.26.10 minimum (au lieu de 1.26.0) : correctifs de la bibliothèque standard (`net/http`, dont GO-2026-6617, et `crypto/x509`) signalés par `govulncheck`.

## [0.3.0] - 2026-10-23

### Ajouté
- **Deux langues** (français, anglais) : interface, textes du serveur, ligne de commande, consignes des aides à Claude et 500 verbes d'attente par langue. La langue suit le réglage `language` de Claude ; liste déroulante dans Réglages, onglet Apparence.
- **Corbeille des sessions** : supprimer déplace la session dans la corbeille (bouton Annuler), restauration, suppression définitive et purge à 30 jours ; accessible depuis « Toutes les sessions ». Avertissement 24 h avant que Claude Code n'efface des sessions (`cleanupPeriodDays`).
- **Lecteur de session** : fiche d'infos au clic droit, sous-agents ouverts en modale (lecture seule), état git du dossier avec poussée, liste des fichiers modifiés dans une fenêtre, `@` pour mentionner une autre session, identifiant de la session copiable depuis le menu (à la place de la commande `claude --resume`).
- **Sessions ouvertes** : sélecteurs de modèle et d'effort avec le modèle en cours ; les messages écrits pendant que la session travaille ou pose une question partent dès qu'elle attend ; spinner pour une session qui travaille et « i » clignotant pour une question en attente.
- **Questions et autorisations de Claude** : récapitulatif et état des onglets des questions à onglets, commande ou diff affichés avec la demande d'autorisation, réponse au dialogue de confiance d'un dossier depuis l'interface.
- **Mode carrousel** sur les sessions en attente de réponse, menu contextuel sur les dossiers du cache chaud, cache chaud rangé par dossier, une session y remonte dès qu'un prompt lui est envoyé.
- **Dossiers** : ajout d'un dossier sans session pour y lancer un prompt, bouton Permissions sur les réglages du projet, prompt lancé dans le dossier choisi parmi ceux du projet.
- **Configuration** : choix de l'emplacement (utilisateur, projet, local) puis du dossier ; l'éditeur texte des permissions montre les différences et avertissements en direct et couvre les dossiers supplémentaires ; suggestions de règles tirées aussi des sous-commandes des commandes Bash composées.
- **Mobile** : lecteur compact calé sur le clavier virtuel, barre du lecteur sur une ligne avec commandes secondaires dans le menu « … », tiroir de navigation en dialogue Radix.
- 15 palettes de couleurs (Dracula, Nord, Solarized, Gruvbox, Monokai, Tokyo Night, One, Catppuccin, Ayu, Material, GitHub, Rosé Pine, Everforest, Kanagawa, Night Owl), avec couleurs d'état contrastées.
- Mémoire : sections repliables, repliées par défaut ; les blocs repliables gardent leur état.
- Dialogue Crédits et soutien (licence, bibliothèques, adresse XMR avec QR code).
- **Pousser** : dans la vue dossier, bouton « Pousser » (commits en avance ou branche sans amont) qui propose les remotes du dépôt ; jamais de `--force`, identifiants des URL masqués (y compris un jeton placé à la place du nom), amont posé à la première poussée.
- **Bloc-notes** par projet et par dossier, enregistré automatiquement (`gld-web-notes.json`) : le texte peut partir vers une nouvelle session ou vers une session ouverte dans tmux ; la note suit le projet quand on le renomme.
- **Snippets** dans le champ de nouvelle session de la vue dossier, comme dans la fenêtre de saisie.
- `GLD_WEB_TOKEN` est aussi exigé en écoute locale quand il est fixé (machine partagée entre plusieurs comptes).

### Modifié
- **Maintenir au chaud** ne concerne plus que les sessions ouvertes dans un terminal : un ping `claude -p` ne relit pas le cache du mode interactif (mesuré : 0 token lu, tout le contexte réécrit). Le bouton est grisé sur une session fermée, et le maintien s'arrête si la session est fermée. Le coût cumulé n'est plus affiché (non mesurable par le terminal).

### Corrigé
- Une modification non enregistrée (mémoire, skill, snippet, réglages JSON) n'est plus perdue : le navigateur demande confirmation avant de quitter, et le brouillon est retrouvé après un rechargement.
- Une aide de modèle qui échoue (brouillon, lint, skill) affiche la raison donnée par `claude`, plus seulement « exit status 1 ».
- Renommer : en cas d'échec, le dialogue reste ouvert avec la saisie ; après un dialogue ouvert depuis le menu d'une session, le focus revient sur sa ligne (ou sur la voisine après une suppression).
- Accessibilité : champ de saisie nommé, erreur JSON reliée au champ, titres de section et boutons « Nouvelle entrée » nommés par section dans Mémoire, « Fermer » au lieu de « Close ».
- Suppression : « 1 session supprimée » au lieu de « 1 supprimée(s), 0 vivante(s) épargnée(s) ».
- Un message démesuré (plusieurs dizaines de Mo) ne fige plus le lecteur : la page n'en donne que les 64 premiers Ko, et « Afficher la suite » charge les morceaux suivants (`GET /conv-text`).
- Écran étroit : le menu ⋯ du lecteur reste accessible à 375 px, Mémoire, Skills et Snippets n'affichent leurs deux panneaux qu'à partir de 1024 px (plus de défilement horizontal à 768 px), dates et compteurs ne débordent plus des lignes, les onglets de Configuration signalent qu'ils défilent.
- Fermer le lecteur rend le focus clavier à la ligne de la session ; les animations respectent `prefers-reduced-motion` ; sur écran tactile, cases à cocher et petits boutons d'icône reçoivent le doigt sur 24 px.
- Les entrées de la navigation latérale sont de vrais liens : clic du milieu ou Ctrl+clic ouvrent la vue dans un autre onglet.
- La recherche ne bloque plus une minute sur un mot fréquent dans un gros transcript : l'extrait n'est calculé que pour le meilleur message de chaque session.
- Au plus 2 appels à Claude d'aide (brouillons de mémoire, explication des permissions, titres, comptage de tokens) tournent à la fois : une rafale ne lance plus autant de processus et d'appels facturés que de clics.
- Enregistrer ou copier une mémoire vers un dossier de projet absent (ou « . ») est refusé, au lieu de créer un dossier parasite dans `projects/`.
- Les jetons d'accès (`GLD_WEB_TOKEN`, jeton des gld enfants) ne sont plus transmis à `claude`, à tmux ni aux onglets lancés : un `env` exécuté par Claude ne les écrit plus dans un transcript.
- Une session dont le maintien au chaud est arrêté pendant un passage n'est plus pinguée.
- Le terminal web s'ouvre dans un dossier de configuration secondaire (l'origine du WebSocket est vérifiée par la passerelle).
- La question de confiance d'un dossier n'est détectée que dans un panneau où tourne Claude Code, pas dans un `cat` ou un éditeur qui affiche le même texte.
- « Remplacer » un skill dans la synchronisation retire de la cible les fichiers absents de la source (ils vont dans la sauvegarde).
- Un `claude` qui ne répond plus (mise à jour, invite) ne fige plus la liste des sessions : `claude agents` est abandonné au bout de 10 s, les sessions ouvertes sont alors inconnues et les suppressions refusées.
- Un serveur tmux figé ne bloque plus la question de confiance ni l'envoi à une session : chaque appel à tmux est abandonné au bout de 10 s.
- Un dépôt git qui ne répond plus (montage réseau figé) ne bloque plus la liste des remotes : les lectures de git sont abandonnées au bout de 10 s.
- Synchronisation : deux listes de permissions qui ne diffèrent que par un doublon (`[a, b]` contre `[a, a]`) ne sont plus déclarées identiques.
- Dates et compteurs aux bornes : « il y a 1 heure » au lieu de « il y a 60 minutes », « 1 M » au lieu de « 1 000 k ».
- Avec `--listen`, un client qui envoie ses en-têtes au compte-gouttes est déconnecté au bout de 10 s (slowloris), avant même la vérification du jeton.
- Les fichiers réécrits par remplacement (réglages, `history.jsonl`, transcripts réassignés, groupes, onglets) sont écrits sur disque (`fsync`) avant de remplacer l'original : une coupure de courant ne peut plus laisser un fichier vide.
- La liste des fichiers en cours de modification affiche les chemins accentués ou avec guillemet tels quels, au lieu de l'échappement de git (`"\303\251t\303\251.txt"`).
- Le focus clavier, les titres de section (`h2`) et les infobulles des boutons d'icône sont conformes (Radix) ; les fenêtres ne s'élargissent plus au contenu sur petit écran ; la corbeille ne déborde plus.
- Un échec réseau ou un serveur injoignable est signalé au lieu de squelettes muets ; les erreurs de lecture ne se glissent plus dans le texte éditable.
- La conversation reste collée à sa fin quand la bande d'état change de hauteur, ne se vide plus quand la liste se relit sans elle, et une relecture lente ne la remplace plus par une version plus ancienne.
- Le carrousel ne parcourt que les sessions actives, écarte le cache froid et passe à la suivante quand la session affichée est supprimée ; après une suppression, le focus va à la session voisine.
- Dialogue de confiance d'un dossier : bonnes touches, curseur vérifié avant Entrée, seul le bloc d'options est lu.
- Les questions longues, à cases à cocher ou à onglets s'affichent en entier avec leur description.
- Le décompte des commandes pour les suggestions de règles est gardé 60 s ; le cache du tampon des transcripts et celui des textes de recherche sont bornés (1 s, 256 Mo) ; l'index de recherche ne relit pas un transcript modifié depuis moins de 30 s.
- Un `cleanupPeriodDays` très grand n'est plus ramené à 30 jours.
- Plus d'effet de bord sur les requêtes GET : l'expiration de la corbeille tourne en tâche de fond, l'actualisation du compte se fait par `POST /account-refresh`. Les mutations de sessions (suppression, corbeille, réassignation) sont sous verrou, la réassignation est atomique, et le démarrage d'un gld enfant ne bloque plus les autres.
- Un jeton fourni par `GLD_WEB_TOKEN` n'est plus imprimé sur stderr ; l'exploration des dossiers ne descend plus dans `/proc`, `/sys`, `/dev`, `/run`, `/mnt` ni `/media`.
- `/conv` sans `limit` rend des pages de 1000 messages ; l'export Markdown parcourt les pages jusqu'au bout.
- La page d'un projet est servie par le serveur (plus de 404 au rechargement), et les dossiers d'un projet ne sont plus répétés sous « Sessions locales ».

## [0.2.0] - 2026-10-22

### Ajouté
- **Projets** : ensembles nommés de dossiers de travail, au-dessus de l'arbre renommé « Sessions locales » ; un clic droit sur un dossier l'ajoute à un projet, un clic sur un projet réunit les sessions de ses dossiers.
- **État git** : navigation compacte (changements en attente avec l'icône git, nombre de sessions avec un robot jaune quand l'une est ouverte), dépôts sous un dossier de sessions en second niveau replié, et dans la vue dossier la branche, l'avance et le retard, et la liste des fichiers en cours de modification. Relu passivement toutes les 30 s.
- **Nouvelle session depuis la vue dossier** : champ de prompt avec choix du modèle et de l'effort.
- **Plusieurs dossiers de configuration Claude** (`CLAUDE_CONFIG_DIR`, compte pro et perso) : sélecteur en haut de la barre latérale, un gld enfant par dossier derrière une passerelle protégée par un jeton interne.
- **Onglet « Synchroniser »** : compare et copie réglages, règles de permission, `CLAUDE.md`, règles, commandes et skills d'un dossier de configuration à l'autre, élément par élément (fusionner ou remplacer), sans jamais copier de secrets et avec sauvegarde de ce qui est remplacé.
- **Maintenir au chaud** : un ping automatique garde le cache des prompts quand il reste moins de 5 minutes, avec plafond, bandeau de coût toujours visible et statistiques de la session en haut à droite.
- **Permissions** : explication de chaque règle par Haiku, édition de toutes les règles en texte avec vérification de la syntaxe, et serveurs MCP utilisés proposés en suggestion.
- **File d'attente** : les messages écrits pendant une réponse partent à la suite ; bouton « Interrompre » sur une session ouverte.
- Clic droit sur les sessions de la liste « Cache chaud ».
- Licence MIT.
- Préparation d'une architecture multi-fournisseurs : interface `Provider` et capacités, la session porte son fournisseur (Claude Code reste le seul).

### Modifié
- Le bouton des réglages est une roue crantée, en haut à droite du logo.
- La vue ne descend plus qu'à l'ouverture d'une session et à l'envoi d'un message, plus à chaque mise à jour.
- Le champ de saisie garde le focus après chaque message.

### Corrigé
- Le cache chaud se calcule depuis le dernier message de la session, plus depuis la date du fichier.
- Une session lancée depuis l'interface web tourne dans tmux, quel que soit l'émulateur, donc elle accepte des messages d'ici.
- Un message non délivré est remis en file ; brouillons et files sont gardés par session, même fenêtre fermée ou page rechargée.
- Le champ d'envoi à une session ouverte ne se verrouille plus, et l'envoi s'abandonne au bout de 20 s.
- Le composeur ne bascule plus vers l'envoi tmux pendant un tour lancé depuis le web.
- La conversation ne se redessine plus à chaque seconde, ce qui gênait la saisie.
- Barre de défilement parasite sur les onglets de configuration.
- `~/.claude` et `~` ne sont plus listés comme projets de configuration.
- Reprendre une session dit pourquoi rien ne s'ouvre (déjà ouverte, introuvable) au lieu de « 0 onglet lancé ».

## [0.1.3] - 2026-10-22

### Ajouté
- Questions du terminal prises en charge dans la conversation : confirmations risquées, choix simples ou multiples avec cases et validation, écran de relecture, approbation de plan, édition de fichier, réponse libre ; détection couverte par des tests sur captures réelles et bouton « Écran brut » pour déboguer.
- Quand la session attend, la question posée par Claude s'affiche dans le bandeau, et sa dernière phrase est citée sous l'état.
- Nouvelle session dans n'importe quel dossier.
- Entrée envoie le message, Maj+Entrée va à la ligne.
- Snippets : copie du contenu, de la commande ou du chemin au clic droit.

### Corrigé
- Les messages injectés par les skills ne s'affichent plus dans la conversation ni ne servent de titre.
- La sélection du menu des commandes slash suit les flèches du clavier.
- Les boutons de réponse attendent la mise à jour de l'écran avant un nouveau clic (plus de désynchronisation).
- Le message « en attente de lecture » a la même géométrie qu'un message réel (léger décalage au survol).
- Retrait de la phrase d'explication sous le champ de saisie.

## [0.1.2] - 2026-10-22

### Ajouté
- Terminal intégré à la session, ajusté à la zone et répercuté sur tmux, saisie active par défaut ; le mode visuel ou terminal est mémorisé dans le navigateur.
- « Renommer auto » au clic droit d'une session, et titre automatique proposé dans la fenêtre de renommage.
- Mémoire : changement du type d'une entrée au clic droit, correction automatique d'un front matter invalide depuis la santé de la mémoire.
- La fusion de mémoire passe par la fenêtre de saisie du prompt.

### Corrigé
- Confirmations risquées détectées ; réponse libre aux options « Type something » et « Chat about this ».
- La vue ne défile plus d'elle-même quand Claude attend une réponse.
- La fusion de mémoire globale démarre dans `~/.claude` et non dans `~`.
- La fenêtre des instructions chargées ne déborde plus.

## [0.1.1] - 2026-10-22

### Ajouté
- Captures d'écran dans le README, avec un jeu de données fictif pour les refaire.

### Corrigé
- Le dernier outil appelé n'est plus affiché quand la session attend un message.
- Le terminal web tient 70 minutes sans saisie, au-delà du cache de prompt.
- Le rafraîchissement des limites du compte ne provoque plus de course ni de journal égaré.

## [0.1.0] - 2026-10-22

Première version : `gld`, interface web locale pour parcourir, piloter et configurer ses sessions Claude Code.

### Ajouté

**Sessions et conversations**
- Arbre des projets, lecteur de conversation en grand format, rendu Markdown, groupes, favoris, corbeille.
- Recherche plein texte indexée, tokens par session, étiquettes.
- Nouvelle session (y compris depuis une session, dans le même projet), déplacement, suppression, nettoyage.
- Chat dans le lecteur, choix du modèle et de l'effort par prompt, images collées et affichées.
- Résumé de `/compact` affiché en carte ; messages en attente de lecture visibles sous la transcription.
- Transcription qui ne saute plus à la fin d'elle-même, bouton « Aller à la fin ».

**Sessions en direct (tmux)**
- État en direct, sous-agents, dernier outil appelé, contexte et tokens, modale de statistiques.
- Envoi de messages à une session ouverte, fermeture depuis le web, réponses aux dialogues du terminal.
- Menu `/` : commandes de Claude Code, de plugins, de skills et personnelles.
- Les sessions lancées depuis le web s'ouvrent dans un terminal de la machine, avec tmux.
- Terminal intégré (xterm), désactivé par défaut, en lecture seule jusqu'à la prise de main.
- Verbes de réflexion variés (réglage `spinnerVerbs` repris), notification d'attente.
- Suivi des processus sous Linux, Windows et macOS.

**Mémoire et règles**
- Mémoires de projet : liste, édition (menu contextuel ou clic), aperçu Markdown, tri, création.
- Rédaction et amélioration par Haiku, consolidation dans `CLAUDE.md`, analyse des doublons et contradictions.
- Décompte des tokens, instructions réellement chargées par session.

**Configuration de Claude Code**
- Page dédiée : précédence, santé, hooks, MCP, permissions, réglages, sauvegardes.
- Permissions : édition, tri, transfert projet → global, import/export, dossiers supplémentaires.
- Éditeur JSON brut des réglages, avec validation, aperçu et sauvegarde.
- Skills (personnels, de projet, de plugins) avec origine et filtre ; réglages et hooks par projet.

**Compte et interface**
- Fenêtre du compte : limites rafraîchies automatiquement, graphique d'activité.
- Interface adaptée au mobile (menu en tiroir, pages liste/détail).
- Vues chargées à la demande ; écoute hors localhost possible.

**Distribution**
- Binaire unique (interface embarquée), commandes `gld`, `gld-web` et `gld-matin`.
- CI GitHub et binaires de release multi-arch (Linux, macOS, Windows) avec `SHA256SUMS`.

### Sécurité
- Écritures de réglages sérialisées, sans lien symbolique, fichiers de données en `0600`.
- Terminal intégré désactivé par défaut ; lecture bornée des fichiers de commandes.
- Refus d'agir quand les sessions ouvertes sont indéterminées.

[Non publié]: https://github.com/54tor/glaude-code/compare/v0.4.4...HEAD
[0.4.4]: https://github.com/54tor/glaude-code/compare/v0.4.3...v0.4.4
[0.4.3]: https://github.com/54tor/glaude-code/compare/v0.4.2...v0.4.3
[0.4.2]: https://github.com/54tor/glaude-code/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/54tor/glaude-code/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/54tor/glaude-code/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/54tor/glaude-code/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/54tor/glaude-code/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/54tor/glaude-code/compare/v0.1.3...v0.2.0
[0.1.3]: https://github.com/54tor/glaude-code/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/54tor/glaude-code/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/54tor/glaude-code/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/54tor/glaude-code/releases/tag/v0.1.0
