# make install : construit l'interface (node requis), compile gld et le relie dans ~/.local/bin, sous son nom (gld) et sous ceux de gld-web et gld-matin (alias)
# make ui      : reconstruit l'interface React/shadcn vers gld/ui/dist (non versionné : embarqué dans le binaire par go:embed)
# make commonpw : régénère gld/auth_commonpw.txt depuis @zxcvbn-ts/language-common (à relancer après une mise à jour du paquet)
# make windows : gld.exe pour Windows 64 bits (sans bash, sans jq ; l'interface est construite avant)
# make macos   : gld-darwin-arm64 pour macOS Apple Silicon (M1 et suivants ; l'interface est construite avant, sans cgo)
# make test    : tests Go (le dossier dist peut être vide : seul l'interface en dépend)
# make check   : go vet + tests Go (-race) + tsc + eslint + tests de l'interface (rien n'est modifié) ; check-ui : la partie interface seule
# make cover   : tests Go sous -race avec couverture ; échoue sous COVER_MIN % (relevé : 80,7 % le 2026-10-22)
# make fuzz    : chaque cible de fuzzing de gld pendant FUZZTIME (60s par défaut) ; corpus dans le cache de go
BIN ?= $(HOME)/.local/bin
INDEX := gld/ui/dist/index.html
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# L'interface n'est reconstruite que si son code ou ses dépendances ont changé.
$(INDEX): $(shell find gld/ui/src gld/ui/public -type f 2>/dev/null) gld/ui/package-lock.json gld/ui/index.html gld/ui/vite.config.ts gld/ui/scripts/check-chunks.mjs
	cd gld/ui && { [ -d node_modules ] || npm ci; } && npm run build && touch dist/.gitkeep

ui: $(INDEX)

commonpw:
	cd gld/ui && { [ -d node_modules ] || npm ci; } && node scripts/gen-commonpw.mjs

install: ui
	cd gld && go build -ldflags "$(LDFLAGS)" -o gld .
	ln -sf $(CURDIR)/gld/gld $(BIN)/gld
	ln -sf $(CURDIR)/gld/gld $(BIN)/gld-web
	ln -sf $(CURDIR)/gld/gld $(BIN)/gld-matin

windows: ui
	cd gld && GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o gld.exe .

macos: ui
	cd gld && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o gld-darwin-arm64 .

test:
	cd gld && go test -count=1 .

COVER_MIN ?= 80
cover: ui
	cd gld && go test -race -count=1 -covermode=atomic -coverprofile=cover.out . > cover.txt 2>&1; st=$$?; cat cover.txt; exit $$st
	@awk -v min=$(COVER_MIN) 'match($$0, /coverage: [0-9.]+%/) { c = substr($$0, RSTART + 10, RLENGTH - 11) } END { print "couverture : " c " % (plancher " min " %)"; exit (c == "" || c + 0 < min + 0) }' gld/cover.txt

FUZZTIME ?= 60s
fuzz:
	cd gld && for f in $$(go test -list '^Fuzz' . | grep '^Fuzz'); do go test -run '^$$' -fuzz "^$$f$$" -fuzztime $(FUZZTIME) -parallel 4 . || exit 1; done

check: ui check-ui
	cd gld && go vet ./... && go test -race -count=1 .

# La partie interface de check (la CI la lance seule : les tests Go y passent déjà sous -race dans cover).
check-ui:
	cd gld/ui && { [ -d node_modules ] || npm ci; } && npx tsc -b && npx eslint . && npm test

.PHONY: ui commonpw install windows macos test check check-ui fuzz cover
