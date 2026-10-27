Textes visibles : côté Go via `L("srv.<fichier>.<clé>")`, clés dans `gld/locales/fr.json` ET `en.json` (les tests `i18n_keys_test.go` échouent sinon) ; côté UI, via le système i18n existant, jamais de chaîne en dur.

Commentaires : permis dans le code source s'ils sont succincts et énoncent un *pourquoi* non évident. Aucun commentaire dans ce qui part au client (HTML, bundles, réponses d'API, statiques).

**Why:** le binaire sert l'UI embarquée ; tout commentaire ou texte en dur y fuit ou y échappe à la traduction.
