# make install : compile gld-web et le relie dans ~/.local/bin, sous son nom et sous celui de gld-matin
# make ui      : recompile l'interface React/shadcn (node requis) vers gld-web/ui/dist, qui est versionné
# make windows : gld-web.exe pour Windows 64 bits (sans node, sans bash, sans jq)
BIN ?= $(HOME)/.local/bin

install:
	cd gld-web && go build -ldflags '-s -w' -o gld-web .
	ln -sf $(CURDIR)/gld-web/gld-web $(BIN)/gld-web
	ln -sf $(CURDIR)/gld-web/gld-web $(BIN)/gld-matin

ui:
	cd gld-web/ui && npm ci && npm run build

windows:
	cd gld-web && GOOS=windows GOARCH=amd64 go build -ldflags '-s -w' -o gld-web.exe .

test:
	cd gld-web && go test -count=1 .
