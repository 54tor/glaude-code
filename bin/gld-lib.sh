# gld-lib.sh — briques communes aux commandes cc-* (gld-matin, gld-nommer, gld-rm,
# gld-trier). Sourcé, jamais exécuté. Portable bash 3.2+, GNU/BSD/macOS.

export LC_ALL=C   # tri et encodage indépendants de la locale

# Racine de la configuration Claude. Aucune commande cc-* n'efface hors d'ici.
GLD_ROOT="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"

# Minutes d'immunité d'un fichier tout frais : une session qui vient de naître
# n'a pas encore écrit son transcript, ses fichiers ne sont pas des restes.
GLD_GRACE_MIN=${GLD_GRACE:-${CC_GRACE:-60}}

# Répertoire de travail temporaire, créé à la demande, effacé à la sortie.
# À appeler depuis le shell principal : dans un sous-shell, GLD_TMP serait perdu.
GLD_TMP=""
cc_tmp() {
  [ -n "$GLD_TMP" ] && return 0
  GLD_TMP=$(mktemp -d "${TMPDIR:-/tmp}/cc.XXXXXX") || exit 1
  trap 'rm -rf "$GLD_TMP"' EXIT INT TERM
}

# Échappe pour insertion dans une commande shell entre quotes simples.
shq() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }

# Taille en octets : wc -c est POSIX, contrairement à stat -c/-f.
taille() { wc -c < "$1" | tr -d ' '; }

# Date de modification lisible : GNU `date -r`, sinon BSD `stat -f`.
mdate() {
  date -r "$1" +%m-%d_%H:%M 2>/dev/null \
    || stat -f '%Sm' -t '%m-%d_%H:%M' "$1" 2>/dev/null \
    || echo '?'
}

# Répertoire des transcripts d'un projet.
# Encodage Claude : tout caractère non alphanumérique devient '-'.
# Transformation exacte, jamais un préfixe : «…-crawl-center» préfixe
# «…-crawl-center-agent», deux projets bien distincts.
transcripts_de() {
  printf '%s/projects/%s' \
    "$GLD_ROOT" \
    "$(printf '%s' "$1" | sed 's/[^a-zA-Z0-9]/-/g')"
}

# Titre explicite d'une session. Le dernier écrit fait foi : Claude réémet
# la ligne custom-title en continu (des centaines par transcript), et un
# renommage s'ajoute à la fin sans effacer les précédents.
titre_explicite() {
  grep '"custom-title"' "$1" 2>/dev/null \
    | tail -1 \
    | jq -r '.customTitle // empty' 2>/dev/null
}

# Repli pour les sessions jamais nommées : le premier vrai prompt utilisateur.
# On écarte le bruit d'amorçage (system-reminder, attachments, <command-*>,
# résultats d'outils) pour ne garder que ce que vous avez tapé.
etiquette_deduite() {
  head -200 "$1" \
    | jq -r 'select(.type=="user")
             | .message.content
             | if type == "string" then .
               else ([.[]? | select(.type == "text") | .text] | join(" "))
               end' 2>/dev/null \
    | tr '\t' ' ' \
    | grep -v '^[[:space:]]*$' \
    | grep -v '^<' \
    | grep -vi '^caveat:' \
    | head -1 \
    | cut -c1-45
}

# IDs des sessions actuellement vivantes (interactives comme background).
# Échoue (code 1) si la liste est indéterminée : l'appelant doit alors refuser d'agir.
sessions_vivantes() {
  _sv=$(claude agents --json 2>/dev/null) || return 1
  printf '%s' "$_sv" | jq -r '.[].sessionId' 2>/dev/null
}

# Résout le projet cible ($1 ou $PWD) et pose PROJET / TRANSCRIPTS.
resoudre_projet() {
  command -v jq >/dev/null || { echo "${0##*/}: jq requis" >&2; exit 1; }
  command -v claude >/dev/null || { echo "${0##*/}: claude introuvable" >&2; exit 1; }

  PROJET="$(cd "${1:-$PWD}" 2>/dev/null && pwd)" \
    || { echo "${0##*/}: projet introuvable: ${1:-$PWD}" >&2; exit 1; }
  TRANSCRIPTS="$(transcripts_de "$PROJET")"

  # Un répertoire de travail additionnel n'a pas de transcript à lui : Claude y
  # dépose un .session-aliases qui pointe le répertoire canonique. On le suit,
  # sinon on croirait le projet sans session.
  # shellcheck disable=SC2034
  TRANSCRIPTS_ALIAS=""
  if [ -f "$TRANSCRIPTS/.session-aliases" ] \
     && ! ls "$TRANSCRIPTS"/*.jsonl >/dev/null 2>&1; then
    canon=$(head -1 "$TRANSCRIPTS/.session-aliases" 2>/dev/null)
    if [ -n "$canon" ] && [ -d "$canon" ]; then
      TRANSCRIPTS="$canon"
      # shellcheck disable=SC2034
      TRANSCRIPTS_ALIAS="$canon"
    fi
  fi

  [ -d "$TRANSCRIPTS" ] || {
    echo "${0##*/}: aucune session Claude pour $PROJET" >&2
    echo "          (cherché dans $TRANSCRIPTS)" >&2
    exit 1
  }
}

# --- suppression complète d'une session --------------------------------------
#
# Un transcript n'est que la partie visible : Claude écrit aussi dans
# file-history/, session-env/, tasks/, teams/, jobs/, uploads/, debug/,
# paste-cache/, shell-snapshots/ et history.jsonl. Effacer le seul .jsonl
# laisse tout cela derrière — c'est ce qui remplit ~/.claude au fil des mois.

# Efface un chemin, avec garde-fou : rien hors de $GLD_ROOT, jamais un memory/
# (la mémoire longue durée de Claude, qui survit aux sessions).
effacer() {
  case "$1" in
    "$GLD_ROOT"/?*) ;;
    *) printf "cc: refus d'effacer hors de %s : %s\n" "$GLD_ROOT" "$1" >&2; return 1 ;;
  esac
  case "$1" in
    *..*|*/memory|*/memory/*)
      printf "cc: chemin protégé, non effacé : %s\n" "$1" >&2; return 1 ;;
  esac
  rm -rf -- "$1"
}

# Un id de session est un UUID : 8-4-4-4-12.
est_id() {
  case "$1" in
    ????????-????-????-????-????????????) return 0 ;;
    *) return 1 ;;
  esac
}

# Tout ce qu'une session possède AILLEURS que dans son transcript.
# file-history/ session-env/ uploads/ debug/ portent l'id entier ;
# tasks/ teams/ jobs/ n'en gardent que les huit premiers caractères.
satellites_de() {
  id="$1"; court=$(printf '%.8s' "$id")
  printf '%s\n' \
    "$GLD_ROOT/file-history/$id" \
    "$GLD_ROOT/session-env/$id" \
    "$GLD_ROOT/uploads/$id" \
    "$GLD_ROOT/debug/$id.txt" \
    "$GLD_ROOT/tasks/session-$court" \
    "$GLD_ROOT/teams/session-$court"

  # jobs/ : huit caractères pourraient désigner une autre session, state.json
  # tranche. Un job sans state.json lisible appartient à celle-ci par défaut.
  job="$GLD_ROOT/jobs/$court"
  if [ -d "$job" ]; then
    sid=$(jq -r '.sessionId // empty' "$job/state.json" 2>/dev/null)
    if [ -z "$sid" ] || [ "$sid" = "$id" ]; then printf '%s\n' "$job"; fi
  fi
}

# Supprime une session : transcript, sous-agents, satellites. $1 répertoire du
# projet, $2 id. Écrit un chemin par ligne — l'appelant compte ou affiche.
# Les restes partagés (collages, instantanés, history.jsonl) se traitent en fin
# de course, quand on sait ce qui n'est plus cité par personne.
purger_session() {
  rep="$1"; id="$2"
  est_id "$id" || { echo "cc: id de session invalide: $id" >&2; return 1; }
  { printf '%s\n%s\n' "$rep/$id.jsonl" "$rep/$id"; satellites_de "$id"; } \
  | while IFS= read -r p; do
      [ -e "$p" ] || continue
      effacer "$p" && printf '%s\n' "$p"
    done
}

# Instantanés de shell cités par une session (transcript et sous-agents) : à
# relever AVANT de l'effacer, le nom du fichier ne se déduit de rien d'autre.
snapshots_cites() {
  grep -rhoE 'snapshot-[a-zA-Z]+-[0-9]+-[a-zA-Z0-9]+\.sh' "$@" 2>/dev/null | sort -u
}

# Fabrique les motifs « "sessionId":"…" » d'une liste d'ids lue sur stdin.
# $1 = fichier de sortie.
motifs_sessions() {
  awk 'NF {printf "\"sessionId\":\"%s\"\n", $1}' > "$1"
}

# Hachages de collage cités par les lignes d'history.jsonl de ces sessions.
# $1 = fichier de motifs.
hashes_history() {
  [ -s "$1" ] && [ -s "$GLD_ROOT/history.jsonl" ] || return 0
  grep -F -f "$1" "$GLD_ROOT/history.jsonl" 2>/dev/null \
    | jq -r '.pastedContents[]?.contentHash // empty' 2>/dev/null | sort -u
}

# Retire d'history.jsonl les prompts de ces sessions et écrit le nombre de
# lignes ôtées. Fichier temporaire puis renommage : jamais de troncature si la
# commande est interrompue.
elaguer_history() {
  h="$GLD_ROOT/history.jsonl"
  [ -s "$1" ] && [ -s "$h" ] || { echo 0; return; }
  avant=$(wc -l < "$h" | tr -d ' ')
  taille0=$(taille "$h")
  tmp="$h.cc.$$"
  grep -F -v -f "$1" "$h" > "$tmp" 2>/dev/null
  [ $? -le 1 ] || { rm -f "$tmp"; echo 0; return; }
  # Une session ouverte a pu ajouter une ligne pendant la réécriture : on abandonne plutôt que de la perdre.
  [ "$(taille "$h")" = "$taille0" ] || {
    rm -f "$tmp"
    echo "  history.jsonl   : modifié pendant l'opération, lignes de prompt non retirées (réessaie)" >&2
    echo 0; return
  }
  apres=$(wc -l < "$tmp" | tr -d ' ')
  mv -f "$tmp" "$h" 2>/dev/null || { rm -f "$tmp"; echo 0; return; }
  echo $((avant - apres))
}

# Tout ce que les données restantes citent encore : instantanés de shell (dans
# les transcripts) et hachages de collage (history.jsonl et transcripts). Sert
# à décider si un fichier partagé est devenu un déchet.
references_vivantes() {
  grep -rhoE 'snapshot-[a-zA-Z]+-[0-9]+-[a-zA-Z0-9]+\.sh|[0-9a-f]{16}' \
    "$GLD_ROOT/projects" 2>/dev/null
  jq -r '.pastedContents[]?.contentHash // empty' "$GLD_ROOT/history.jsonl" 2>/dev/null
}

# Nombre de lignes d'un fichier, 0 s'il n'existe pas.
nblignes() {
  [ -f "$1" ] || { echo 0; return; }
  wc -l < "$1" | tr -d ' '
}

# Filtre : écarte les chemins touchés depuis moins de $GLD_GRACE_MIN minutes.
sans_les_recents() {
  while IFS= read -r p; do
    [ -e "$p" ] || continue
    [ -n "$(find "$p" -maxdepth 0 -mmin +"$GLD_GRACE_MIN" 2>/dev/null)" ] || continue
    printf '%s\n' "$p"
  done
}

# Efface les chemins listés dans le fichier $1 et écrit le compte.
effacer_liste() {
  n=0
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    effacer "$p" && n=$((n + 1))
  done < "$1"
  echo "$n"
}

# Solde d'une suppression : lignes de history.jsonl, collages et instantanés de
# shell des sessions effacées. Un collage ou un instantané ne part que si plus
# rien ne le cite — deux sessions peuvent partager le même contenu.
# $1 = fichier des ids supprimés, $2 = fichier des instantanés relevés avant
# destruction. Écrit un compte rendu de trois lignes au plus.
solder() {
  cc_tmp
  grep -v '^$' "$1" | motifs_sessions "$GLD_TMP/motifs"
  hashes_history "$GLD_TMP/motifs" > "$GLD_TMP/hashes"
  nh=$(elaguer_history "$GLD_TMP/motifs")

  references_vivantes | sort -u > "$GLD_TMP/refs"

  sort -u "$GLD_TMP/hashes" 2>/dev/null | comm -13 "$GLD_TMP/refs" - \
    | sed "s|^|$GLD_ROOT/paste-cache/|; s|\$|.txt|" | sans_les_recents > "$GLD_TMP/pastes"
  sort -u "$2" 2>/dev/null | grep -v '^$' | comm -13 "$GLD_TMP/refs" - \
    | sed "s|^|$GLD_ROOT/shell-snapshots/|" | sans_les_recents > "$GLD_TMP/snaps"

  np=$(effacer_liste "$GLD_TMP/pastes")
  ns=$(effacer_liste "$GLD_TMP/snaps")

  [ "$nh" -gt 0 ] && printf '  history.jsonl   : %s ligne(s) de prompt retirée(s)\n' "$nh"
  [ "$np" -gt 0 ] && printf '  paste-cache     : %s collage(s) effacé(s)\n' "$np"
  [ "$ns" -gt 0 ] && printf '  shell-snapshots : %s instantané(s) effacé(s)\n' "$ns"
  return 0
}

# Somme en Ko des chemins listés dans le fichier $1.
poids_ko() {
  [ -s "$1" ] || { echo 0; return; }
  tr '\n' '\0' < "$1" | xargs -0 du -sk 2>/dev/null | awk '{s += $1} END {print s+0}'
}

# Ko → lecture humaine.
humain() {
  if [ "$1" -ge 1048576 ]; then printf '%s Go' $(( $1 / 1048576 ))
  elif [ "$1" -ge 1024 ]; then printf '%s Mo' $(( $1 / 1024 ))
  else printf '%s Ko' "$1"
  fi
}
