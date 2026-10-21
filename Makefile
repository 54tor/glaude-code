# make install : construit l'interface (node requis), compile gld-web et le relie dans ~/.local/bin, sous son nom et sous celui de gld-matin
# make ui      : reconstruit l'interface React/shadcn vers gld-web/ui/dist (non versionné : embarqué dans le binaire par go:embed)
# make windows : gld-web.exe pour Windows 64 bits (sans bash, sans jq ; l'interface est construite avant)
# make test    : tests Go (le dossier dist peut être vide : seul l'interface en dépend)
BIN ?= $(HOME)/.local/bin
INDEX := gld-web/ui/dist/index.html

# L'interface n'est reconstruite que si son code ou ses dépendances ont changé.
$(INDEX): $(shell find gld-web/ui/src gld-web/ui/public -type f 2>/dev/null) gld-web/ui/package-lock.json gld-web/ui/index.html gld-web/ui/vite.config.ts
	cd gld-web/ui && { [ -d node_modules ] || npm ci; } && npm run build && touch dist/.gitkeep

ui: $(INDEX)

install: ui
	cd gld-web && go build -ldflags '-s -w' -o gld-web .
	ln -sf $(CURDIR)/gld-web/gld-web $(BIN)/gld-web
	ln -sf $(CURDIR)/gld-web/gld-web $(BIN)/gld-matin

windows: ui
	cd gld-web && GOOS=windows GOARCH=amd64 go build -ldflags '-s -w' -o gld-web.exe .

test:
	cd gld-web && go test -count=1 .

.PHONY: ui install windows test
