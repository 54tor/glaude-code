# Changelog

Format : [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/), versions : [SemVer](https://semver.org/lang/fr/).

## [Non publié]

### Ajouté
- **Pousser** : dans la vue dossier, bouton « Pousser » (commits en avance ou branche sans amont) qui propose les remotes du dépôt ; jamais de `--force`, identifiants des URL masqués (y compris un jeton placé à la place du nom), amont posé à la première poussée.
- **Bloc-notes** par projet et par dossier, enregistré automatiquement (`gld-web-notes.json`) : le texte peut partir vers une nouvelle session ou vers une session ouverte dans tmux ; la note suit le projet quand on le renomme.
- **Snippets** dans le champ de nouvelle session de la vue dossier, comme dans la fenêtre de saisie.
- `GLD_WEB_TOKEN` est aussi exigé en écoute locale quand il est fixé (machine partagée entre plusieurs comptes).

### Modifié
- **Maintenir au chaud** ne concerne plus que les sessions ouvertes dans un terminal : un ping `claude -p` ne relit pas le cache du mode interactif (mesuré : 0 token lu, tout le contexte réécrit). Le bouton est grisé sur une session fermée, et le maintien s'arrête si la session est fermée. Le coût cumulé n'est plus affiché (non mesurable par le terminal).

### Corrigé
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
