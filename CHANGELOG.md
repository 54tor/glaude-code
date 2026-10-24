# Changelog

Format : [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/), versions : [SemVer](https://semver.org/lang/fr/).

## [Non publié]

### Ajouté
- **Multi-utilisateur (optionnel)** : comptes locaux créés par `gld user add --admin`, connexion par mot de passe plus second facteur obligatoire (TOTP, clés de sécurité WebAuthn, codes de secours), sessions à cookie et jeton CSRF. Sans compte, gld se comporte comme avant. Guide : `docs/auth.md`.
- **Droits par home** : sept droits indépendants (`mem_read`, `mem_write`, `mem_sync`, `usage_read`, `sessions_read`, `config_write`, `exec`), refus par défaut de toute route non classée, snippets et restauration de skills réservés à `config_write` (jamais de contenu exécutable avec `mem_write`), interface et API d'administration (comptes, droits, journal, sessions). `exec` et `config_write` équivalent au compte système de gld.
- **Défense contre les essais répétés** : délai progressif, verrou de compte, suite d'échecs consécutifs (30 min au 10e, 24 h au 20e), limite du second facteur (qui bloque aussi le mot de passe du compte), plafond global, journal d'événements limité et purgé à 90 jours, adresse du client via `GLD_TRUSTED_PROXIES`.
- **Base signée** : seuls les secrets TOTP sont chiffrés ; les lignes sensibles (comptes, droits, facteurs, sessions) et le marqueur des comptes sont signés avec une clé dérivée de la clé maîtresse (`GLD_AUTH_KEY`, coffre-fort de l'OS ou `auth.key`). Une ligne altérée est refusée et signalée ; des comptes supprimés ferment le service (503) au lieu de rouvrir le mode ouvert (sauf base entièrement recréée : voir `docs/auth.md`).
- **Garde-fou des sessions** : règles de refus, hook et bac à sable injectés dans les sessions lancées par gld ; `gld auth managed-settings` génère le fichier à installer par root.
- **Espace de travail** (`/espace/<dossier>`) : arborescence chargée dossier par dossier avec marqueurs git, éditeur Monaco à onglets, enregistrement atomique avec détection de conflit (recharger, écraser, voir les différences), ouverture rapide `Ctrl+P` sur un index borné (20 000 entrées, profondeur 12, 5 s ; ignore les noms en `.`, `vendor`, `node_modules` et les liens), sources TypeScript du projet données à Monaco, rafraîchissement par `fsnotify` (repli en sondage), corbeille `.gld-trash/` ignorée par git, terminal branché sur une session Claude vivante du dossier. Deux droits, `fs_read` et `fs_write`, jamais accordés par défaut ; pas de liste de refus (`fs_read` lit `.env` et les clés) ; le home, la config Claude de chaque home et le dossier d'auth de gld sont refusés comme racine ; chaque écriture réussie, sur tout home, est journalisée (`ide_write`, sans contenu) dans les événements d'administration. `fs_write` n'implique pas `fs_read`. `GET /git-changes` renvoie aussi `prefix`. Nouvelles dépendances : `monaco-editor`, `github.com/fsnotify/fsnotify`. Guide : `docs/ide.md`.
- **Poids** : Monaco et ses workers ajoutent ~13 Mo non compressés à `ui/dist` (dont ~7 Mo pour le worker TypeScript), chargés seulement à l'ouverture de l'espace de travail ; le bundle principal ne change pas, mais le binaire Go, qui embarque `ui/dist`, grossit d'autant.
- **Retour arrière** : la migration qui re-signe `home_acl` (droits `fs_read`/`fs_write`) rend chaque ligne de droits « altérée » pour un binaire plus ancien, qui refuse alors l'accès (le service reste fermé) ; revenir à une version antérieure exige de restaurer `auth.db`.
- **`GLD_RUN_AS` (expérimental)** : fait tourner claude, tmux et les gld enfants sous un autre compte système. Ce n'est pas encore une frontière de sécurité (voir `docs/auth.md`) ; exige `GLD_RUN_AS_EXPERIMENTAL=1` et root.

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

[Non publié]: https://github.com/54tor/glaude-code/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/54tor/glaude-code/compare/v0.1.3...v0.2.0
[0.1.3]: https://github.com/54tor/glaude-code/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/54tor/glaude-code/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/54tor/glaude-code/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/54tor/glaude-code/releases/tag/v0.1.0
