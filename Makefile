# make install : compile gld-web et relie tout dans ~/.local/bin (remplace les copies existantes)
BIN ?= $(HOME)/.local/bin

install:
	cd gld-web && go build -ldflags '-s -w' -o gld-web .
	ln -sf $(CURDIR)/bin/* $(CURDIR)/gld-web/gld-web $(BIN)/
