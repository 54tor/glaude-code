#!/usr/bin/env python3
"""Assemble le site : site/content/<langue>/<page>.html (corps seul) -> <sortie>/ avec menu, TOC et navigation."""
import html
import re
import shutil
import sys
from pathlib import Path

SITE = Path(__file__).resolve().parent
SHOTS = SITE.parent / "docs" / "screenshots"
BASE = "https://54tor.github.io/glaude-code/"
PAGES = ["index", "demarrer", "sessions", "partage", "memoire", "reglages",
         "espace-de-travail", "mobile", "equipes", "multi-utilisateur", "faq"]

LANGS = {
    "fr": {
        "prefix": "", "other": "en", "lang_label": "Langue", "menu": "Navigation",
        "toc": "Sur cette page", "prev": "Précédent", "next": "Suivant",
        "footer": "Glaude Code — licence MIT", "doc_label": "Manuel",
        "titles": {
            "index": "Accueil", "demarrer": "Installer et démarrer", "sessions": "Sessions",
            "partage": "Partager une session", "memoire": "Mémoire et règles",
            "reglages": "Skills, snippets et réglages", "espace-de-travail": "Espace de travail",
            "mobile": "Téléphone et accès distant", "equipes": "Équipes d'agents",
            "multi-utilisateur": "Comptes et multi-utilisateur", "faq": "Questions fréquentes",
        },
        "descs": {
            "index": "Retrouvez, relisez et reprenez vos sessions Claude Code depuis le navigateur ou le téléphone, partagez-les et gardez la main sur mémoire, règles et réglages. Tout reste sur votre machine.",
            "demarrer": "Installer Glaude Code (binaire, sources, WSL2), le lancer, choisir la langue et résoudre les problèmes de port.",
            "sessions": "Retrouver, relire, reprendre et écrire à une session Claude Code ; envoyer des messages, rouvrir ses sessions après un redémarrage, raccourcis clavier.",
            "partage": "Partager une session avec un collègue par lien d'invitation : lecture seule ou écriture, file d'envoi, révocation, limites et précautions.",
            "memoire": "Lire, ranger et alléger CLAUDE.md, règles et mémoire de projet : coût en tokens, santé, corbeille, export et import.",
            "reglages": "Skills, snippets, permissions et réglages de Claude Code en cases à cocher, avec aperçu et sauvegarde avant chaque écriture.",
            "espace-de-travail": "Arborescence git, éditeur Monaco, enregistrement et terminal en lecture seule pour relire et corriger le code d'un projet dans le navigateur.",
            "mobile": "Utiliser Glaude Code depuis un téléphone ou un autre poste : jeton, jumelage par code, HTTPS, notifications push, restriction de l'explorateur.",
            "equipes": "Lancer des équipes d'agents de Claude Code (chef de projet et coéquipiers) depuis le navigateur, les piloter et répondre à leurs demandes.",
            "multi-utilisateur": "Comptes, second facteur obligatoire et neuf droits indépendants par dossier de configuration Claude.",
            "faq": "Données, sécurité, sauvegardes, port, coûts : les réponses aux questions les plus fréquentes.",
        },
    },
    "en": {
        "prefix": "../", "other": "fr", "lang_label": "Language", "menu": "Navigation",
        "toc": "On this page", "prev": "Previous", "next": "Next",
        "footer": "Glaude Code — MIT license", "doc_label": "Manual",
        "titles": {
            "index": "Home", "demarrer": "Install and start", "sessions": "Sessions",
            "partage": "Share a session", "memoire": "Memory and rules",
            "reglages": "Skills, snippets and settings", "espace-de-travail": "Workspace",
            "mobile": "Phone and remote access", "equipes": "Agent teams",
            "multi-utilisateur": "Accounts and multi-user", "faq": "FAQ",
        },
        "descs": {
            "index": "Find, reread and resume your Claude Code sessions from your browser or phone, share them, and stay in control of memory, rules and settings. Everything stays on your machine.",
            "demarrer": "Install Glaude Code (binary, sources, WSL2), start it, pick the language and fix port problems.",
            "sessions": "Find, reread, resume and write to a Claude Code session; send messages, reopen your sessions after a restart, keyboard shortcuts.",
            "partage": "Share a session with a colleague through an invitation link: read-only or write access, send queue, revocation, limits and precautions.",
            "memoire": "Read, tidy and slim down CLAUDE.md, rules and project memory: token cost, health checks, trash, export and import.",
            "reglages": "Skills, snippets, permissions and Claude Code settings as checkboxes, with a preview and a backup before every write.",
            "espace-de-travail": "Git tree, Monaco editor, saving and a read-only terminal to reread and fix a project's code in the browser.",
            "mobile": "Use Glaude Code from a phone or another computer: token, pairing code, HTTPS, push notifications, folder-browser restriction.",
            "equipes": "Launch Claude Code agent teams (project lead and teammates) from the browser, drive them and answer their requests.",
            "multi-utilisateur": "Accounts, mandatory second factor and nine independent rights per Claude configuration folder.",
            "faq": "Data, security, backups, port, costs: answers to the most common questions.",
        },
    },
}

TEMPLATE = """<!doctype html>
<html lang="{lang}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title}</title>
<meta name="description" content="{desc}">
<link rel="canonical" href="{canonical}">
<link rel="alternate" hreflang="fr" href="{url_fr}">
<link rel="alternate" hreflang="en" href="{url_en}">
<link rel="alternate" hreflang="x-default" href="{url_fr}">
<link rel="icon" href="{prefix}logo.png">
<link rel="stylesheet" href="{prefix}style.css">
</head>
<body class="{kind}">
<header class="wrap top">
  <a class="brand" href="{home}"><img src="{prefix}logo.png" alt="">Glaude Code</a>
  <nav class="lang" aria-label="{lang_label}">{lang_switch}</nav>
</header>
<nav class="wrap pills" aria-label="{menu}">{pills}</nav>
<div class="wrap layout">
{side}<main id="main">
{body}
{pager}</main>
</div>
<footer>
  <div class="wrap">
    <span>{footer}</span>
    <a href="https://github.com/54tor/glaude-code">github.com/54tor/glaude-code</a>
  </div>
</footer>
</body>
</html>
"""


def page_href(slug):
    return "./" if slug == "index" else f"{slug}.html"


def links(lang, current):
    texts = LANGS[lang]["titles"]
    out = []
    for slug in PAGES:
        mark = ' aria-current="page"' if slug == current else ""
        out.append(f'<a href="{page_href(slug)}"{mark}>{html.escape(texts[slug])}</a>')
    return out


def table_of_contents(body, label):
    entries = re.findall(r'<h2 id="([^"]+)">(.*?)</h2>', body)
    if len(entries) < 3:
        return ""
    items = "".join(f'<li><a href="#{anchor}">{text}</a></li>' for anchor, text in entries)
    return f'<nav class="toc" aria-label="{label}"><strong>{label}</strong><ul>{items}</ul></nav>\n'


def pager(lang, slug):
    if slug == "index":
        return ""
    config = LANGS[lang]
    position = PAGES.index(slug)
    parts = []
    if position > 1:
        before = PAGES[position - 1]
        parts.append(f'<a class="prev" href="{page_href(before)}"><small>{config["prev"]}</small>{html.escape(config["titles"][before])}</a>')
    if position < len(PAGES) - 1:
        after = PAGES[position + 1]
        parts.append(f'<a class="next" href="{page_href(after)}"><small>{config["next"]}</small>{html.escape(config["titles"][after])}</a>')
    return f'<nav class="pager">{"".join(parts)}</nav>\n'


def render(lang, slug, used_shots):
    config = LANGS[lang]
    source = (SITE / "content" / lang / f"{slug}.html").read_text(encoding="utf-8")
    body = source.replace("{{R}}", config["prefix"])
    used_shots.update(re.findall(r'shots/([\w.-]+\.png)', body))
    is_home = slug == "index"
    if not is_home:
        lead_end = body.find("</p>", body.find('class="lead"'))
        toc = table_of_contents(body, config["toc"])
        if lead_end != -1 and toc:
            lead_end += len("</p>")
            body = body[:lead_end] + "\n" + toc + body[lead_end:]
        body = f'<article class="doc">\n{body}\n</article>'
    other = config["other"]
    here_url = {"fr": BASE + ("" if is_home else f"{slug}.html"),
                "en": BASE + "en/" + ("" if is_home else f"{slug}.html")}
    other_href = ("../" if lang == "en" else "en/") + ("" if is_home else f"{slug}.html")
    if lang == "fr":
        lang_switch = f'<span aria-current="true">FR</span><a href="{other_href}" hreflang="en" lang="en">EN</a>'
    else:
        lang_switch = f'<a href="{other_href}" hreflang="fr" lang="fr">FR</a><span aria-current="true">EN</span>'
    link_list = links(lang, slug)
    side = ""
    if not is_home:
        side = (f'<nav class="side" aria-label="{config["menu"]}"><strong>{config["doc_label"]}</strong>'
                + "".join(link_list) + "</nav>\n")
    title = config["titles"][slug]
    full_title = f"{title} — Glaude Code"
    if is_home:
        full_title = {"fr": "Glaude Code — le poste de pilotage web de Claude Code",
                      "en": "Glaude Code — the web cockpit for Claude Code"}[lang]
    return TEMPLATE.format(
        lang=lang, title=html.escape(full_title), desc=html.escape(config["descs"][slug]),
        canonical=here_url[lang], url_fr=here_url["fr"], url_en=here_url["en"],
        prefix=config["prefix"], kind="home" if is_home else "doc", home=page_href("index"),
        lang_label=config["lang_label"], lang_switch=lang_switch, menu=config["menu"],
        pills="".join(link_list), side=side, body=body, pager=pager(lang, slug), footer=config["footer"],
    )


def main():
    out = Path(sys.argv[1] if len(sys.argv) > 1 else "_site")
    out.mkdir(parents=True, exist_ok=True)
    used_shots = set()
    for lang in LANGS:
        directory = out if lang == "fr" else out / lang
        directory.mkdir(parents=True, exist_ok=True)
        for slug in PAGES:
            (directory / f"{slug}.html").write_text(render(lang, slug, used_shots), encoding="utf-8")
    for static in ("style.css", "logo.png"):
        shutil.copy(SITE / static, out / static)
    (out / "shots").mkdir(exist_ok=True)
    for name in sorted(used_shots):
        shutil.copy(SHOTS / name, out / "shots" / name)
    print(f"{len(PAGES) * len(LANGS)} pages, {len(used_shots)} captures -> {out}")


if __name__ == "__main__":
    main()
