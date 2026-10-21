# make install : construit l'interface (node requis), compile gld et le relie dans ~/.local/bin, sous son nom (gld) et sous ceux de gld-web et gld-matin (alias)
# make ui      : reconstruit l'interface React/shadcn vers gld/ui/dist (non versionné : embarqué dans le binaire par go:embed)
# make windows : gld.exe pour Windows 64 bits (sans bash, sans jq ; l'interface est construite avant)
# make test    : tests Go (le dossier dist peut être vide : seul l'interface en dépend)
# make check   : go vet + tests Go + tsc + eslint (rien n'est modifié)
BIN ?= $(HOME)/.local/bin
INDEX := gld/ui/dist/index.html

# L'interface n'est reconstruite que si son code ou ses dépendances ont changé.
$(INDEX): $(shell find gld/ui/src gld/ui/public -type f 2>/dev/null) gld/ui/package-lock.json gld/ui/index.html gld/ui/vite.config.ts
	cd gld/ui && { [ -d node_modules ] || npm ci; } && npm run build && touch dist/.gitkeep

ui: $(INDEX)

install: ui
	cd gld && go build -ldflags '-s -w' -o gld .
	ln -sf $(CURDIR)/gld/gld $(BIN)/gld
	ln -sf $(CURDIR)/gld/gld $(BIN)/gld-web
	ln -sf $(CURDIR)/gld/gld $(BIN)/gld-matin

windows: ui
	cd gld && GOOS=windows GOARCH=amd64 go build -ldflags '-s -w' -o gld.exe .

test:
	cd gld && go test -count=1 .

check:
	cd gld && go vet ./... && go test -count=1 .
	cd gld/ui && { [ -d node_modules ] || npm ci; } && npx tsc --noEmit && npx eslint .

.PHONY: ui install windows test check
