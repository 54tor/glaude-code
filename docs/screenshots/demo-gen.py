#!/usr/bin/env python3
"""Jeu de données fictif pour les captures du README (usage : python3 demo-gen.py /tmp/gld-demo, puis HOME=/tmp/gld-demo GLD_WEB_PORT=8823 gld) : un HOME factice avec trois projets, des sessions, de la mémoire,
des skills et des réglages. Rien ne vient des vrais fichiers de l'utilisateur."""
import json, os, sys, time, uuid, datetime

HOME = sys.argv[1]
ONLY = sys.argv[2].split(",") if len(sys.argv) > 2 else ["boutique", "api-meteo", "site-vitrine"]  # « compta-interne » : second home
CL = os.path.join(HOME, ".claude")
CWD_HOME = os.environ.get("DEMO_CWD_HOME", HOME)  # base des dossiers de projet (second home : celle du home principal)
now = time.time()


def iso(t):
    return datetime.datetime.fromtimestamp(t, datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.000Z")


def w(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    open(path, "w").write(text)


projects = {
    "boutique": "Boutique en ligne (Symfony)",
    "api-meteo": "API météo (Go)",
    "site-vitrine": "Site vitrine (Astro)",
    "compta-interne": "Outil de comptabilité interne (Rails)",
}

sessions = {
    "compta-interne": [
        ("Générer le rapport de TVA trimestriel", 6, [
            ("user", "Génère le rapport de TVA du trimestre au format CSV, une ligne par taux."),
            ("text", "Je regroupe les écritures par taux et par pays, puis j'exporte avec le séparateur attendu par le comptable."),
            ("tool", "Bash", {"command": "bin/rails reports:vat QUARTER=2026-Q3", "description": "Génère le rapport"}, "Rapport écrit : tmp/tva-2026-Q3.csv (214 lignes)"),
            ("text", "Le rapport est prêt : 214 lignes, totaux cohérents avec le grand livre."),
        ]),
        ("Rapprochement bancaire automatique", 30, [
            ("user", "Propose un rapprochement automatique entre relevés bancaires et factures."),
            ("text", "Je rapproche par montant et référence, puis par date à trois jours près, et je laisse le reste à valider à la main."),
        ]),
    ],
    "boutique": [
        ("Ajouter le paiement par carte avec Stripe", 3, [
            ("user", "Ajoute le paiement par carte avec Stripe sur la page de commande. Il faut gérer l'échec et le retour sur la boutique."),
            ("text", "Je commence par regarder comment la commande est construite aujourd'hui, puis j'ajoute un service de paiement."),
            ("tool", "Read", {"file_path": "src/Controller/CheckoutController.php"}, "<?php\nclass CheckoutController { /* … */ }"),
            ("tool", "Bash", {"command": "composer require stripe/stripe-php", "description": "Installe le SDK Stripe"}, "Using version ^15.0 for stripe/stripe-php"),
            ("text", "Le SDK est installé. Je crée `PaymentService` avec la création de session de paiement et un webhook pour confirmer la commande.\n\n- `src/Service/PaymentService.php` : création de la session\n- `src/Controller/StripeWebhookController.php` : confirmation côté serveur\n- une migration pour stocker `payment_status`"),
            ("tool", "Write", {"file_path": "src/Service/PaymentService.php"}, "Fichier créé"),
            ("tool", "Bash", {"command": "php bin/phpunit tests/Service/PaymentServiceTest.php", "description": "Lance les tests du paiement"}, "OK (4 tests, 11 assertions)"),
            ("text", "Les tests passent. Le retour de paiement affiche la commande confirmée, et un échec ramène au panier avec un message clair."),
        ]),
        ("Corriger le calcul de TVA sur les frais de port", 26, [
            ("user", "Les frais de port ne sont pas soumis à la bonne TVA pour les commandes vers la Belgique."),
            ("text", "Je cherche où la TVA est calculée."),
            ("tool", "Grep", {"pattern": "vatRate", "path": "src"}, "src/Pricing/VatCalculator.php:42"),
            ("text", "Le taux est pris sur le pays de facturation, pas sur celui de livraison. Je corrige `VatCalculator` et j'ajoute un test pour la Belgique."),
        ]),
        ("", 30, [
            ("user", "Perform a read-only security review of this codebase and report confirmed vulnerabilities only."),
            ("tool", "Grep", {"pattern": "createQueryBuilder|executeQuery", "path": "src"}, "src/Repository/OrderRepository.php:58"),
            ("text", "**1 constat confirmé** — `src/Repository/OrderRepository.php:58` : le filtre `status` est concaténé dans la requête DQL (injection SQL, gravité haute). Correctif : paramètre nommé `:status`.\n\n2 pistes écartées : le webhook Stripe vérifie bien la signature, et l'upload d'images valide le type MIME."),
        ]),
        ("Découper PaymentService et committer", 4, [
            ("user", "Découpe PaymentService en deux classes puis commite."),
            ("tool", "Task", {"description": "Chercher les usages de PaymentService", "subagent_type": "Explore", "prompt": "Liste les appels à PaymentService"}, "7 usages dans 4 fichiers"),
            ("tool", "Bash", {"command": "git add -A && git commit -m \"refactor(payment): split PaymentService\"", "description": "Commit"}, "[main 3fa91c2] refactor(payment): split PaymentService\n 3 files changed, 84 insertions(+), 51 deletions(-)"),
            ("text", "Le service est découpé en `CheckoutSession` et `PaymentWebhook`, et le commit est fait (3 fichiers). Il reste deux commits à pousser."),
        ]),
        ("Migrer vers PostgreSQL 17", 70, [
            ("user", "Prépare la migration de MySQL vers PostgreSQL 17 : liste ce qui casse."),
            ("text", "Je passe en revue les requêtes natives et les types de colonnes. Trois points à traiter : les `GROUP BY` implicites, `ENUM`, et les `LIMIT` dans les sous-requêtes."),
        ]),
    ],
    "api-meteo": [
        ("Mettre en cache les prévisions à l'heure", 5, [
            ("user", "Les appels au fournisseur coûtent cher : mets en cache les prévisions par station, une heure."),
            ("text", "Un cache mémoire par station avec expiration à l'heure ronde suffit ici. J'utilise `sync.Map` et une goroutine de nettoyage."),
            ("tool", "Edit", {"file_path": "internal/forecast/cache.go"}, "Fichier modifié"),
            ("tool", "Bash", {"command": "go test ./... -race", "description": "Tests avec détecteur de courses"}, "ok  \tmeteo/internal/forecast\t1.2s"),
            ("text", "C'est en place : 95 % des appels répétés sont servis par le cache, et le détecteur de courses ne signale rien."),
        ]),
        ("Ajouter l'alerte orage par SMS", 50, [
            ("user", "Ajoute une alerte orage envoyée par SMS quand la probabilité dépasse 70 %."),
            ("text", "Je propose un petit module `alert` avec un seuil configurable, indépendant du fournisseur de SMS."),
        ]),
    ],
    "site-vitrine": [
        ("Passer les images en AVIF", 9, [
            ("user", "Convertis les images du site en AVIF avec un repli WebP."),
            ("text", "Astro sait le faire à la construction avec `<Picture>`. Je remplace les `<img>` de la page d'accueil et je règle les largeurs."),
        ]),
        ("", 200, [
            ("user", "Review this change for security vulnerabilities in the diff below."),
            ("text", "Aucun problème confirmé : les liens externes portent `rel=\"noopener\"` et le formulaire de contact est protégé par un jeton CSRF."),
        ]),
        ("Rédiger la page « À propos »", 120, [
            ("user", "Écris la page « À propos » : deux paragraphes, ton chaleureux."),
            ("text", "Voici une première version, avec l'histoire de l'atelier et ses valeurs. Dis-moi ce que tu veux ajuster."),
        ]),
    ],
}

memory = {
    "boutique": [
        ("MEMORY.md", None, "# Mémoire du projet\n\n- [Paiements en test](paiements-test.md) — clés de test seulement\n- [Choix de PostgreSQL](decision-postgres.md) — migration prévue\n- [Préférences de revue](preferences-revue.md)\n- [Équipe](equipe.md) — qui fait quoi\n"),
        ("paiements-test", "project", "Ne jamais utiliser les clés Stripe de production en développement : les clés de test commencent par `sk_test_`.\n\n**Why:** une commande réelle a déjà été facturée par erreur.\n\n**How to apply:** vérifier le préfixe de la clé avant tout appel à l'API."),
        ("decision-postgres", "project", "La base passe de MySQL à PostgreSQL 17 d'ici la fin du trimestre.\n\n**Why:** les jointures JSON et les index partiels.\n\n**How to apply:** écrire toute nouvelle requête native compatible PostgreSQL."),
        ("preferences-revue", "feedback", "Faire des revues courtes : trois remarques maximum, les plus importantes d'abord.\n\n**Why:** les longues listes ne sont pas lues.\n\n**How to apply:** regrouper les remarques mineures en une ligne."),
        ("equipe", "user", "Léa gère le front, Marc s'occupe de la base de données et du déploiement."),
    ],
    "api-meteo": [
        ("MEMORY.md", None, "# Mémoire du projet\n\n- [Fournisseur météo](fournisseur.md)\n"),
        ("fournisseur", "reference", "Documentation du fournisseur : https://exemple.test/docs. Limite : 60 appels par minute."),
    ],
    "compta-interne": [
        ("MEMORY.md", None, "# Mémoire du projet\n\n- [Clôture](cloture.md)\n"),
        ("cloture", "project", "La clôture mensuelle se fait le 3 de chaque mois ouvré.\n\n**Why:** le comptable externe exporte ce jour-là.\n\n**How to apply:** ne jamais lancer de migration de données comptables entre le 1er et le 3."),
    ],
    "site-vitrine": [
        ("MEMORY.md", None, "# Mémoire du projet\n\n- [Ton éditorial](ton-editorial.md)\n"),
        ("ton-editorial", "feedback", "Tutoyer les visiteurs, phrases courtes, pas de jargon.\n\n**Why:** public d'artisans, peu à l'aise avec le vocabulaire technique.\n\n**How to apply:** relire chaque texte à voix haute."),
    ],
}

for name, label in projects.items():
    if name not in ONLY:
        continue
    cwd = os.path.join(CWD_HOME, "Projets", name)
    os.makedirs(os.path.join(cwd, ".claude"), exist_ok=True)
    enc = "-" + cwd.strip("/").replace("/", "-").replace(".", "-").replace("_", "-")
    pdir = os.path.join(CL, "projects", enc)
    for title, hours, steps in sessions[name]:
        sid = str(uuid.uuid4())
        t = now - hours * 3600
        lines, parent = [], None
        n = 0

        def add(d):
            global n
            n += 1
            d.update({"sessionId": sid, "cwd": cwd, "uuid": str(uuid.uuid4()), "timestamp": iso(t + n * 7), "isSidechain": False, "userType": "external"})
            lines.append(json.dumps(d, ensure_ascii=False, separators=(",", ":")))

        pending = None
        for s in steps:
            if s[0] == "user":
                add({"type": "user", "message": {"role": "user", "content": s[1]}})
            elif s[0] == "text":
                add({"type": "assistant", "message": {"role": "assistant", "model": "claude-opus-5-5", "content": [{"type": "text", "text": s[1]}], "stop_reason": "end_turn",
                                                     "usage": {"input_tokens": 1800 + n * 40, "output_tokens": 220 + n * 9, "cache_read_input_tokens": 41000 + n * 900, "cache_creation_input_tokens": 1200}}})
            else:
                tid = "toolu_" + uuid.uuid4().hex[:20]
                add({"type": "assistant", "message": {"role": "assistant", "model": "claude-opus-5-5", "content": [{"type": "tool_use", "id": tid, "name": s[1], "input": s[2]}], "stop_reason": "tool_use",
                                                     "usage": {"input_tokens": 900, "output_tokens": 80, "cache_read_input_tokens": 40000, "cache_creation_input_tokens": 600}}})
                add({"type": "user", "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": tid, "content": s[3]}]}})
        if title:
            lines.append(json.dumps({"type": "custom-title", "customTitle": title, "sessionId": sid}, ensure_ascii=False, separators=(",", ":")))
        w(os.path.join(pdir, sid + ".jsonl"), "\n".join(lines) + "\n")
        os.utime(os.path.join(pdir, sid + ".jsonl"), (t + 120, t + 120))
    for fname, typ, body in memory[name]:
        if fname == "MEMORY.md":
            w(os.path.join(pdir, "memory", "MEMORY.md"), body)
        else:
            fm = f"---\nname: {fname}\ndescription: {json.dumps(body.splitlines()[0][:90], ensure_ascii=False)}\nmetadata:\n  type: {typ}\n---\n\n{body}\n"
            w(os.path.join(pdir, "memory", fname + ".md"), fm)
    w(os.path.join(cwd, "CLAUDE.md"), f"# {label}\n\nConventions du projet : tests avant tout commit, messages de commit en français.\n")

# réglages personnels et de projet
w(os.path.join(CL, "settings.json"), json.dumps({
    "model": "opus",
    "enabledPlugins": {"outils-revue@atelier": True, "doc-express@atelier": False},
    "permissions": {
        "allow": ["Bash(git status:*)", "Bash(git diff:*)", "Bash(ls:*)", "Read(~/Projets/**)"],
        "ask": ["Bash(git push:*)"],
        "deny": ["Read(./.env)", "Bash(rm -rf:*)"],
    },
    "hooks": {"PostToolUse": [{"matcher": "Edit|Write", "hooks": [{"type": "command", "command": "gofmt -l ."}]}]},
}, indent=2, ensure_ascii=False))
if "boutique" in ONLY:
  w(os.path.join(CWD_HOME, "Projets", "boutique", ".claude", "settings.json"), json.dumps({
    "permissions": {"allow": ["Bash(php bin/phpunit:*)", "Bash(composer install:*)", "Bash(php bin/console cache:clear:*)"], "deny": ["Bash(php bin/console doctrine:schema:drop:*)"]}
}, indent=2))
if "api-meteo" in ONLY:
  w(os.path.join(CWD_HOME, "Projets", "api-meteo", ".claude", "settings.json"), json.dumps({
    "permissions": {"allow": ["Bash(go test:*)", "Bash(go vet:*)"], "additionalDirectories": ["../shared-proto"]}
}, indent=2))

# skills
w(os.path.join(CL, "skills", "revue-de-code", "SKILL.md"), "---\nname: revue-de-code\ndescription: Relit un diff et signale au plus trois problèmes, les plus importants d'abord\n---\n\nRelis le diff demandé. Signale trois remarques au maximum, classées par gravité.\n")
w(os.path.join(CL, "skills", "message-de-commit", "SKILL.md"), "---\nname: message-de-commit\ndescription: Rédige un message de commit en français au format conventionnel\n---\n\nFormat : `type(portée): description`, sans corps sauf si nécessaire.\n")
w(os.path.join(CL, "skills", "notes-de-version", "SKILL.md"), "---\nname: notes-de-version\ndescription: Prépare les notes de version à partir de l'historique git\n---\n\nRegroupe les commits par type et rédige des notes lisibles par un utilisateur final.\n")

# règles globales
w(os.path.join(CL, "CLAUDE.md"), "# Règles personnelles\n\nRépondre en français, de façon concise.\n")
w(os.path.join(CL, "rules", "commits.md"), "Messages de commit en français, format conventionnel, sans ligne d'attribution.\n")
w(os.path.join(CL, "rules", "tests.md"), "Lancer les tests ciblés avant de proposer un commit.\n")
# git : un dépôt par projet (remote local, commits en avance, fichiers modifiés et non suivis)
import subprocess
GE = dict(os.environ, GIT_AUTHOR_NAME="alice", GIT_AUTHOR_EMAIL="alice@example.com", GIT_COMMITTER_NAME="alice", GIT_COMMITTER_EMAIL="alice@example.com", HOME=HOME)


def git(cwd, *a):
    subprocess.run(["git", *a], cwd=cwd, env=GE, check=True, capture_output=True)


for name, ahead, dirty, untracked in (("boutique", 2, 3, 2), ("api-meteo", 1, 1, 0), ("site-vitrine", 0, 0, 0), ("compta-interne", 0, 2, 0)):
    if name not in ONLY:
        continue
    cwd = os.path.join(CWD_HOME, "Projets", name)
    rem = os.path.join(CWD_HOME, "remotes", name + ".git")
    os.makedirs(rem, exist_ok=True)
    git(rem, "init", "--bare", "-b", "main")
    git(cwd, "init", "-b", "main")
    for f in ("README.md", "src/app.txt", "src/a.txt", "src/b.txt"):
        w(os.path.join(cwd, f), f"{f}\n")
    git(cwd, "add", "-A")
    git(cwd, "commit", "-m", "chore(init): first commit")
    git(cwd, "remote", "add", "origin", rem)
    git(cwd, "push", "-u", "origin", "main")
    for i in range(ahead):
        w(os.path.join(cwd, f"src/feat{i}.txt"), "x\n")
        git(cwd, "add", "-A")
        git(cwd, "commit", "-m", f"feat(app): step {i + 1}")
    for f in ("README.md", "src/app.txt", "src/a.txt")[:dirty]:
        w(os.path.join(cwd, f), "modifié\n")
    for i in range(untracked):
        w(os.path.join(cwd, f"notes{i}.txt"), "à trier\n")

# notes de projet (bloc-notes) et commandes personnalisées (snippets)
w(os.path.join(CL, "gld-web-notes.json"), json.dumps({
    "d/" + os.path.join(CWD_HOME, "Projets", "boutique"): json.dumps(["Relire le webhook Stripe avant la mise en production", "Demander à Marc la date de bascule PostgreSQL", "Ajouter un test pour les remboursements partiels"], ensure_ascii=False),
    "d/" + os.path.join(CWD_HOME, "Projets", "api-meteo"): json.dumps(["Limite fournisseur : 60 appels par minute"], ensure_ascii=False),
}, indent=2, ensure_ascii=False))
for n, d, b in (("deploiement", "Prépare le déploiement : tests, tag, notes de version", "Lance les tests, crée le tag et rédige les notes de version."),
                ("explique", "Explique un fichier en trois phrases", "Explique le fichier donné en trois phrases, sans jargon.")):
    w(os.path.join(CL, "commands", n + ".md"), f"---\ndescription: {d}\n---\n\n{b}\n")
if "boutique" in ONLY:
  w(os.path.join(CWD_HOME, "Projets", "boutique", ".claude", "commands", "migration.md"), "---\ndescription: Génère une migration Doctrine à partir du diff des entités\n---\n\nGénère la migration sans l'exécuter.\n")

# plugin installé depuis une place de marché fictive
pl = os.path.join(CL, "plugins", "cache", "atelier", "outils-revue", "1.2.0")
w(os.path.join(pl, "skills", "revue-securite", "SKILL.md"), "---\nname: revue-securite\ndescription: Revue de sécurité en lecture seule d'un diff ou d'un dépôt\n---\n\nLis le code sans le modifier et signale seulement les failles confirmées.\n")
w(os.path.join(CL, "plugins", "installed_plugins.json"), json.dumps({"version": 2, "plugins": {"outils-revue@atelier": [{"scope": "user", "installPath": pl, "version": "1.2.0"}]}}, indent=2))
w(os.path.join(CL, "plugins", "known_marketplaces.json"), json.dumps({"atelier": {"source": {"source": "github", "repo": "atelier/plugins"}, "installLocation": os.path.join(CL, "plugins", "marketplaces", "atelier"), "lastUpdated": iso(now - 86400)}}, indent=2))

# serveurs MCP et workspace sauvegardé
w(os.path.join(HOME, ".claude.json"), json.dumps({"mcpServers": {"documentation": {"type": "http", "url": "https://docs.example.com/mcp"}}}, indent=2))
if "boutique" in ONLY:
  w(os.path.join(CWD_HOME, "Projets", "boutique", ".mcp.json"), json.dumps({"mcpServers": {"base-de-test": {"command": "npx", "args": ["-y", "serveur-sql-lecture-seule"]}}}, indent=2))
print("ok", HOME)
