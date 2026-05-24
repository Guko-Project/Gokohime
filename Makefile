# Makefile
.PHONY: init
init:
	@echo "Installing Lefthook..."
	go install github.com/evilmartians/lefthook@latest
	@echo "Activating Git Hooks..."
	lefthook install