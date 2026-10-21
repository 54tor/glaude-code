# make css     : régénère gld-web/style.css (Tailwind 3.4.17 épinglé, embarqué dans le binaire)
# make install : compile gld-web et relie tout dans ~/.local/bin (remplace les copies existantes)
BIN ?= $(HOME)/.local/bin

css:
	cd gld-web && npx --yes tailwindcss@3.4.17 -c tailwind.config.js -i input.css -o style.css --minify

install:
	cd gld-web && go build -ldflags '-s -w' -o gld-web .
	ln -sf $(CURDIR)/bin/* $(CURDIR)/gld-web/gld-web $(BIN)/
