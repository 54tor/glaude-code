<p align="center"><img src="docs/logo.png" alt="Glaude Code" width="120"></p>

# Glaude Code

**Une page web pour retrouver, relire et reprendre vos sessions Claude Code**, et mettre de l'ordre dans sa mémoire,
ses règles, ses skills et ses réglages. Tout reste sur votre machine : Glaude Code lit simplement le dossier `~/.claude`.

## Pour qui ?

Pour vous si vous utilisez Claude Code tous les jours et que :

- vous ne retrouvez plus « la session d'hier où on avait réglé ce bug » ;
- vous avez dix onglets de terminal ouverts et redémarrer la machine vous fait peur ;
- votre mémoire et vos règles ont grossi sans que vous sachiez ce qui est vraiment chargé, ni combien ça coûte en contexte ;
- vous préférez cocher une case plutôt que d'éditer `settings.json` à la main.

## Installation

Il faut [Go](https://go.dev/dl/) 1.25 ou plus récent, [Node.js](https://nodejs.org/) et Claude Code déjà installé.

```sh
git clone <adresse-du-dépôt> glaude-code
cd glaude-code
make install
```

La commande `gld` est installée dans `~/.local/bin` (ce dossier doit être dans votre `PATH`).

## Démarrer

```sh
gld
```

Puis ouvrez **http://127.0.0.1:8799** dans votre navigateur. C'est tout.

## Ce que vous pouvez faire

### Retrouver et reprendre une session

- La page d'accueil montre vos sessions des 7 derniers jours, rangées par projet. Un **point vert** signale celles qui
  sont ouvertes en ce moment.
- La **recherche** (touche `/`) fouille les titres et le texte des conversations.
- Cliquez sur une session pour la **relire** en entier, puis **Reprendre** pour la rouvrir dans un terminal, avec un
  message de départ si vous voulez.
- Renommez, exportez en Markdown, déplacez ou supprimez une ou plusieurs sessions (clic droit, ou Maj+clic pour en
  sélectionner une série).
- La vue **En cours et cache chaud** montre les sessions actives depuis moins d'une heure : les reprendre maintenant
  coûte moins cher.

### Écrire à une session sans ouvrir de terminal

Depuis le lecteur, tapez un message : Claude répond dans la page. Si la session tourne déjà dans un terminal tmux, le
bouton **Envoyer à la session** écrit directement dedans, et vous voyez en direct ce que Claude fait.

> Chaque message envoyé ainsi recharge tout le contexte : comptez environ 1 à 2 $ par message sur une session froide.

### Ne rien perdre au redémarrage

Glaude Code note toutes les 2 minutes les sessions ouvertes. Après un redémarrage, **Tout rouvrir** les relance, chacune
dans son onglet. En ligne de commande :

```sh
gld matin             # rouvre les sessions d'un projet
gld matin --resume    # rouvre la dernière sauvegarde
```

### Ranger la mémoire et les règles

La page **Mémoire** regroupe vos `CLAUDE.md`, vos règles et la mémoire de chaque projet :

- lire, modifier, déplacer, copier, désactiver une règle sans la supprimer ;
- voir **combien de tokens** chaque fichier ajoute à chaque session ;
- l'onglet **Santé** repère les liens cassés, les index trop longs, les doublons et les contradictions ;
- demander à Claude (modèle Haiku, quelques dixièmes de centime) de réécrire ou de fusionner des entrées.

Rien n'est perdu : tout fichier supprimé ou remplacé passe d'abord par une **corbeille** d'où il se restaure en un clic.

### Gérer skills, commandes et réglages

- **Skills** : lire, créer, modifier, copier vers un projet ; la liste signale les skills mal décrits.
- **Snippets** : vos commandes `/nom` de Claude Code, à créer, modifier, importer ou exporter, et à insérer dans un message.
- **Configuration** : permissions, réglages et dossiers supplémentaires de Claude Code, en listes et cases à cocher.
  Chaque modification montre ce qui change avant d'être écrite, et une sauvegarde est faite à chaque fois.
- **Compte** (en haut à droite) : vos limites d'usage et votre consommation des 14 derniers jours.

### Sur téléphone

La page s'adapte aux petits écrans : le menu passe dans le bouton ☰.

## Raccourcis clavier

| Touche | Action |
|---|---|
| `/` | Rechercher |
| `j` / `k` | Session suivante / précédente |
| `Entrée` | Lire |
| `t` | Reprendre dans un terminal |
| `m` | Reprendre avec un message |
| `r` | Renommer |
| `x` | Sélectionner |
| `e` | Exporter en Markdown |
| `Suppr` | Supprimer |
| `Échap` | Vider la sélection |

## Accès depuis un autre appareil

Par défaut, la page n'est visible que depuis votre ordinateur. Pour l'ouvrir depuis un téléphone ou un autre poste :

```sh
gld --listen 0.0.0.0
```

Glaude Code affiche alors une adresse contenant un **jeton secret** : ouvrez-la telle quelle. La connexion n'est pas
chiffrée : passez par un tunnel (SSH, Tailscale…) et **ne l'exposez jamais directement sur Internet**, car la page peut
lire et supprimer vos sessions et lancer Claude.

Sur une machine partagée avec d'autres comptes, l'adresse locale est joignable par eux aussi. Fixez alors un jeton
(16 caractères au moins) : il est exigé même sans `--listen`, et gld affiche l'adresse à ouvrir.

```sh
GLD_WEB_TOKEN=$(openssl rand -hex 24) gld
```

## Windows

`make windows` produit `gld.exe`. Le support Windows est compilé mais pas encore testé sur une vraie machine. Sous
WSL2, la version Linux fonctionne telle quelle.

## Questions fréquentes

**Est-ce que mes conversations partent quelque part ?**
Non. Glaude Code lit les fichiers locaux de Claude Code. Seules les actions qui font appel à Claude (écrire à une
session, réécrire une entrée de mémoire, compter des tokens) passent par votre propre Claude Code.

**Je peux casser ma configuration ?**
Chaque écriture est précédée d'une sauvegarde et d'un aperçu des changements. Les fichiers de mémoire supprimés vont à
la corbeille.

**Le port 8799 est déjà pris.**
Lancez `GLD_WEB_PORT=8800 gld`.

## Licence

MIT, voir [LICENSE](LICENSE).
