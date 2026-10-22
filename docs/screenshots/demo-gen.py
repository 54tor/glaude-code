#!/usr/bin/env python3
"""Jeu de données fictif pour les captures du README (usage : python3 demo-gen.py /tmp/gld-demo, puis HOME=/tmp/gld-demo GLD_WEB_PORT=8823 gld) : un HOME factice avec trois projets, des sessions, de la mémoire,
des skills et des réglages. Rien ne vient des vrais fichiers de l'utilisateur."""
import json, os, sys, time, uuid, datetime

HOME = sys.argv[1]
CL = os.path.join(HOME, ".claude")
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
}

sessions = {
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
    "site-vitrine": [
        ("MEMORY.md", None, "# Mémoire du projet\n\n- [Ton éditorial](ton-editorial.md)\n"),
        ("ton-editorial", "feedback", "Tutoyer les visiteurs, phrases courtes, pas de jargon.\n\n**Why:** public d'artisans, peu à l'aise avec le vocabulaire technique.\n\n**How to apply:** relire chaque texte à voix haute."),
    ],
}

for name, label in projects.items():
    cwd = os.path.join(HOME, "Projets", name)
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
    "permissions": {
        "allow": ["Bash(git status:*)", "Bash(git diff:*)", "Bash(ls:*)", "Read(~/Projets/**)"],
        "ask": ["Bash(git push:*)"],
        "deny": ["Read(./.env)", "Bash(rm -rf:*)"],
    },
    "hooks": {"PostToolUse": [{"matcher": "Edit|Write", "hooks": [{"type": "command", "command": "gofmt -l ."}]}]},
}, indent=2, ensure_ascii=False))
w(os.path.join(HOME, "Projets", "boutique", ".claude", "settings.json"), json.dumps({
    "permissions": {"allow": ["Bash(php bin/phpunit:*)", "Bash(composer install:*)", "Bash(php bin/console cache:clear:*)"], "deny": ["Bash(php bin/console doctrine:schema:drop:*)"]}
}, indent=2))
w(os.path.join(HOME, "Projets", "api-meteo", ".claude", "settings.json"), json.dumps({
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
print("ok", HOME)
