.PHONY: build

go_files = $(wildcard hubtotea/*.go)
build: $(go_files)
	@mkdir -p build
	cd hubtotea && go build -o ../build/hubtotea