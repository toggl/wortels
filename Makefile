default: compile

compile:
	@go build

install: compile
	if [ ! -d "~/.wortels" ]; then mkdir -p ~/.wortels; fi
	cp -r third_party/compiler-latest ~/.wortels/
	sudo cp wortels /usr/bin

uninstall:
	rm -rf ~/.wortels
	if [ -e "/usr/bin/wortels" ]; then sudo rm /usr/bin/wortels; fi

clean:
	rm -f wortels
