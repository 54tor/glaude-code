# make install : compile gld-web et relie tout dans ~/.local/bin (remplace les copies existantes)
# make ui      : recompile l'interface React/shadcn (node requis) vers gld-web/ui/dist, qui est versionné
# make windows : gld-web.exe pour Windows 64 bits (sans node, sans bash)
BIN ?= $(HOME)/.local/bin

install:
	cd gld-web && go build -ldflags '-s -w' -o gld-web .
	ln -sf $(CURDIR)/bin/* $(CURDIR)/gld-web/gld-web $(BIN)/

ui:
	cd gld-web/ui && npm ci && npm run build

windows:
	cd gld-web && GOOS=windows GOARCH=amd64 go build -ldflags '-s -w' -o gld-web.exe .

test:
	cd gld-web && go test -count=1 .
