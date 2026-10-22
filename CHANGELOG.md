# Changelog

Format : [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/), versions : [SemVer](https://semver.org/lang/fr/).

## [Non publié]

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

[Non publié]: https://github.com/54tor/glaude-code/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/54tor/glaude-code/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/54tor/glaude-code/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/54tor/glaude-code/releases/tag/v0.1.0
