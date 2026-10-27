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
- **npx skills** : les skills posés par `npx skills` sont reconnus (source, lien ou copie, mise à jour disponible) ; on peut en chercher, installer, mettre à jour et retirer depuis la page Skills. Chaque action affiche la commande exacte avant de la lancer ; elle exige `config_write`.
- **Snippets** : vos commandes `/nom` de Claude Code, à créer, modifier, importer ou exporter, et à insérer dans un message.
- **Configuration** : permissions, réglages et dossiers supplémentaires de Claude Code, en listes et cases à cocher.
  Chaque modification montre ce qui change avant d'être écrite, et une sauvegarde est faite à chaque fois.
- **Compte** (en haut à droite) : vos limites d'usage et votre consommation des 14 derniers jours.

### Espace de travail

Le bouton **Espace de travail** de la vue dossier ouvre le dossier dans le navigateur : arborescence avec marqueurs
git, éditeur à onglets (Monaco, le moteur de VS Code, servi par gld sans CDN), `Ctrl+S` pour enregistrer, `Ctrl+P`
pour ouvrir un fichier par son nom, et un bandeau qui demande quoi faire quand l'agent a modifié le fichier entre-temps
(recharger, écraser, comparer). Un menu (Fichier, Édition, Rechercher, Affichage, Aller à, Terminal), une barre d'actions
de l'éditeur, une barre d'outils de l'arborescence (filtre, corbeille avec restauration et purge) et une barre d'état
(curseur, fins de ligne, branche, état de la surveillance) donnent accès à tout sans connaître les raccourcis.
Supprimer envoie dans une corbeille `.gld-trash/` ignorée par git. gld ne propose volontairement aucun shell : la zone
terminal, toujours visible, s'attache en lecture seule à une session Claude vivante du dossier (`GLD_WEB_TERMINAL=1`),
seulement après que vous l'avez ouverte une première fois.

En multi-utilisateur, il faut les droits `fs_read` (lire) et `fs_write` (écrire), plus `exec` et `sessions_read` pour le terminal ; aucun
n'est donné par défaut. Il n'y a pas de liste de fichiers interdits : `fs_read` lit aussi `.env` et les clés. Routes,
limites et sécurité : [docs/ide.md](docs/ide.md).

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

Glaude Code affiche alors une adresse contenant un **jeton secret** : ouvrez-la telle quelle. Sans cette adresse, la page
demande le jeton ; elle porte l'icône et le manifeste, donc l'ajout à l'écran d'accueil d'un iPhone fonctionne depuis elle. La connexion n'est pas
chiffrée : passez par un tunnel (SSH, Tailscale…) et **ne l'exposez jamais directement sur Internet**, car la page peut
lire et supprimer vos sessions et lancer Claude.

Pour connecter un téléphone sans recopier le jeton, ouvrez **Connecter un appareil** (icône de téléphone en haut de la
barre latérale) sur un appareil déjà connecté : il affiche un code à huit chiffres, valable deux minutes et une seule
fois. Sur le téléphone, ouvrez Glaude Code (l'application de l'écran d'accueil ou le navigateur), touchez **Connecter cet
appareil** sous le champ du jeton ou du mot de passe, puis tapez le code : la connexion se fait là où il est tapé, donc
dans l'application. Le même code existe en QR, mais l'appareil photo ouvre alors le navigateur, pas l'application.
Le téléphone reçoit sa propre session : en mode jeton, elle dure tant qu'il revient au moins une fois tous les 90 jours,
se révoque depuis la même fenêtre, et tombe si le jeton change ; avec des comptes, c'est une session du compte qui a
affiché le code, visible dans Profil › Sessions. Dix codes faux par minute bloquent l'adresse une minute.

Le QR code reprend l'adresse de la page qui l'affiche. Si vous l'affichez depuis `127.0.0.1` ou l'IP locale alors que le
téléphone passe par un nom de domaine (proxy, tunnel), donnez cette adresse publique :

```sh
gld --listen 0.0.0.0 --public-url https://gld.exemple.org   # ou GLD_PUBLIC_URL=https://gld.exemple.org
```

Sur iPhone et iPad, l'application installée (« Sur l'écran d'accueil ») n'a ni notifications, ni copie native, ni clé
de sécurité tant qu'elle est servie en HTTP. gld peut servir lui-même en HTTPS avec un certificat auquel l'appareil fait
confiance, par exemple créé avec [mkcert](https://github.com/FiloSottile/mkcert) (installez son autorité racine sur
l'iPhone, puis activez-la dans Réglages › Général › Informations › Réglages de confiance des certificats) :

```sh
mkcert 192.168.0.10 mon-pc.local
gld --listen 0.0.0.0 --tls-cert 192.168.0.10+1.pem --tls-key 192.168.0.10+1-key.pem
```

Les variables `GLD_WEB_TLS_CERT` et `GLD_WEB_TLS_KEY` équivalent aux deux options. Les clés de sécurité (WebAuthn)
exigent en plus un nom de domaine, pas une adresse IP.

Sur une machine partagée avec d'autres comptes, l'adresse locale est joignable par eux aussi. Fixez alors un jeton
(16 caractères au moins) : il est exigé même sans `--listen`, et gld affiche l'adresse à ouvrir avec `$GLD_WEB_TOKEN`
à la place du jeton, pour qu'il ne finisse pas dans les journaux : remplacez-le par votre valeur.

```sh
GLD_WEB_TOKEN=$(openssl rand -hex 24) gld
```

`gld --token=<jeton>` fait la même chose, mais l'argument est visible de tous dans `ps` : préférez la variable sur une machine partagée.

## Multi-utilisateur (optionnel)

Par défaut, un seul jeton donne accès à tout. Pour que plusieurs personnes se connectent chacune avec leur compte, créez
le premier administrateur : `gld user add --admin <nom>`. Dès lors, la connexion (mot de passe plus second facteur :
TOTP, clé de sécurité ou codes de secours) remplace le jeton pour le navigateur, et chaque compte n'a accès qu'aux homes
et aux droits que vous lui donnez (`gld user grant`, ou l'écran Utilisateurs). Sans compte, rien ne change. Le jeton ne vaut alors plus qu'en en-tête `Authorization: Bearer`, et seulement s'il est fixé par `GLD_WEB_TOKEN`.

**Attention** : les droits `exec` (chat, terminal) et `config_write` (réglages, skills) équivalent au compte système qui
fait tourner gld, et les snippets (commandes slash) demandent `config_write`. Ne les donnez qu'à des personnes de confiance. Guide complet, variables, limites :
[docs/auth.md](docs/auth.md).

## Langue

Glaude Code parle français et anglais. La langue vient du réglage `language` de `~/.claude/settings.json`, le même que
celui de Claude Code, modifiable dans Configuration. `French` ou `fr` donne le français, `English` ou `en` l'anglais.
Toute autre langue donne l'anglais pour l'interface, et les titres automatiques, explications et rédactions demandés à
Claude suivent votre réglage. Sans réglage, l'interface suit la langue du navigateur, les commandes `gld-*` suivent
`LC_ALL` puis `LANG`, et le serveur parle français. Le contenu de vos sessions, mémoires, skills et snippets n'est jamais
traduit.

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
